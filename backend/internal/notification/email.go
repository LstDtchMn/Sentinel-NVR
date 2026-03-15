package notification

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

// EmailSender delivers notifications via SMTP email. The token field is treated
// as the recipient email address. When a Notification has a non-empty Thumbnail
// path, the JPEG snapshot is attached as an inline image in the email body.
type EmailSender struct {
	cfg config.SMTPConfig
}

// NewEmailSender creates an EmailSender with the given SMTP configuration.
func NewEmailSender(cfg config.SMTPConfig) *EmailSender {
	return &EmailSender{cfg: cfg}
}

// Send composes and sends an email notification to the given recipient address.
// The email includes the notification title as the subject and the body as text,
// with an optional inline JPEG snapshot attachment.
func (e *EmailSender) Send(ctx context.Context, token string, notif Notification) error {
	to := token
	if to == "" {
		return fmt.Errorf("email: recipient address is empty")
	}

	// Read snapshot if available.
	var snapshot []byte
	if notif.Thumbnail != "" {
		data, err := os.ReadFile(notif.Thumbnail)
		if err == nil {
			snapshot = data
		}
		// Non-fatal: send email without attachment if snapshot read fails.
	}

	msg, err := buildEmailMessage(e.cfg.From, to, notif, snapshot)
	if err != nil {
		return fmt.Errorf("email: build message: %w", err)
	}

	// Use a channel to run the SMTP send with context cancellation.
	// net/smtp does not natively support context, so we wrap it.
	errCh := make(chan error, 1)
	go func() {
		errCh <- e.sendSMTP(to, msg)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return fmt.Errorf("email: %w", ctx.Err())
	}
}

// sendSMTP handles the SMTP connection, optional STARTTLS, authentication, and
// message transmission.
func (e *EmailSender) sendSMTP(to string, msg []byte) error {
	addr := net.JoinHostPort(e.cfg.Host, fmt.Sprintf("%d", e.cfg.Port))

	var conn net.Conn
	var err error

	// For port 465 (implicit TLS / SMTPS), establish a TLS connection directly.
	// For other ports with TLS enabled, use STARTTLS after plain connection.
	if e.cfg.Port == 465 {
		tlsCfg := &tls.Config{ServerName: e.cfg.Host}
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("email: TLS dial to %s: %w", addr, err)
		}
	} else {
		conn, err = net.DialTimeout("tcp", addr, 15*time.Second)
		if err != nil {
			return fmt.Errorf("email: dial to %s: %w", addr, err)
		}
	}

	client, err := smtp.NewClient(conn, e.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("email: SMTP client: %w", err)
	}
	defer client.Close()

	// STARTTLS for non-465 ports when TLS is enabled.
	if e.cfg.TLS && e.cfg.Port != 465 {
		tlsCfg := &tls.Config{ServerName: e.cfg.Host}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("email: STARTTLS: %w", err)
		}
	}

	// Authenticate if credentials are provided.
	if e.cfg.Username != "" || e.cfg.Password != "" {
		auth := smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}

	if err := client.Mail(e.cfg.From); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("email: RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("email: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close body: %w", err)
	}

	return client.Quit()
}

// buildEmailMessage constructs a MIME email with optional inline JPEG attachment.
func buildEmailMessage(from, to string, notif Notification, snapshot []byte) ([]byte, error) {
	var buf strings.Builder

	// Build the text body with event details.
	textBody := buildEmailTextBody(notif)

	if len(snapshot) == 0 {
		// Simple plain-text email — no attachment.
		buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
		buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
		buf.WriteString(fmt.Sprintf("Subject: %s\r\n", notif.Title))
		buf.WriteString(fmt.Sprintf("Date: %s\r\n", notif.Timestamp.Format(time.RFC1123Z)))
		buf.WriteString("MIME-Version: 1.0\r\n")
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(textBody)
		return []byte(buf.String()), nil
	}

	// Multipart MIME email with inline JPEG snapshot.
	boundary := fmt.Sprintf("sentinel-nvr-%d", time.Now().UnixNano())

	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", notif.Title))
	buf.WriteString(fmt.Sprintf("Date: %s\r\n", notif.Timestamp.Format(time.RFC1123Z)))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n", boundary))
	buf.WriteString("\r\n")

	// Use multipart writer for correct boundary formatting.
	mpBuf := &strings.Builder{}
	mp := multipart.NewWriter(mpBuf)
	mp.SetBoundary(boundary)

	// Text part.
	textHeader := make(textproto.MIMEHeader)
	textHeader.Set("Content-Type", "text/plain; charset=utf-8")
	textPart, err := mp.CreatePart(textHeader)
	if err != nil {
		return nil, err
	}
	textPart.Write([]byte(textBody))

	// JPEG attachment part.
	imgHeader := make(textproto.MIMEHeader)
	imgHeader.Set("Content-Type", "image/jpeg")
	imgHeader.Set("Content-Disposition", "inline; filename=\"snapshot.jpg\"")
	imgHeader.Set("Content-Transfer-Encoding", "base64")
	imgPart, err := mp.CreatePart(imgHeader)
	if err != nil {
		return nil, err
	}

	// Base64-encode the snapshot in 76-char lines per RFC 2045.
	encoded := base64.StdEncoding.EncodeToString(snapshot)
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		imgPart.Write([]byte(encoded[i:end] + "\r\n"))
	}

	mp.Close()
	buf.WriteString(mpBuf.String())

	return []byte(buf.String()), nil
}

// buildEmailTextBody creates a human-readable plain-text email body from notification fields.
func buildEmailTextBody(notif Notification) string {
	var sb strings.Builder
	sb.WriteString(notif.Body)
	sb.WriteString("\r\n\r\n")

	if notif.EventType != "" {
		sb.WriteString(fmt.Sprintf("Event Type: %s\r\n", notif.EventType))
	}
	if notif.CameraName != "" {
		sb.WriteString(fmt.Sprintf("Camera: %s\r\n", notif.CameraName))
	}
	sb.WriteString(fmt.Sprintf("Time: %s\r\n", notif.Timestamp.Format(time.RFC1123)))

	if notif.DeepLink != "" {
		sb.WriteString(fmt.Sprintf("Details: %s\r\n", notif.DeepLink))
	}

	sb.WriteString("\r\n-- \r\nSentinel NVR\r\n")
	return sb.String()
}
