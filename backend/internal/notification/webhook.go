package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebhookSender delivers notifications via an HTTP POST to a user-supplied URL.
// The token field is treated as the destination URL.
// Payload is a JSON object with the notification fields.
type WebhookSender struct {
	client *http.Client
}

// NewWebhookSender creates a WebhookSender with a 15-second HTTP timeout.
// Redirects are never followed: an attacker-controlled endpoint could redirect
// to an internal/private target (SSRF). Any non-2xx response is treated as an error.
func NewWebhookSender() *WebhookSender {
	return &WebhookSender{
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// webhookPayload is the JSON body sent to webhook receivers.
type webhookPayload struct {
	EventID    int64     `json:"event_id,omitempty"`
	EventType  string    `json:"event_type"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	CameraName string    `json:"camera_name,omitempty"`
	DeepLink   string    `json:"deep_link,omitempty"`
	Critical   bool      `json:"critical,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// Send POSTs a JSON payload to the webhook URL (token).
// The payload includes all notification fields (event_id, event_type, title, body,
// camera_name, deep_link, critical, timestamp). Returns an error for any non-2xx
// response — 3xx redirects are blocked by CheckRedirect (SSRF prevention).
func (w *WebhookSender) Send(ctx context.Context, token string, notif Notification) error {
	payload := webhookPayload{
		EventID:    notif.EventID,
		EventType:  notif.EventType,
		Title:      notif.Title,
		Body:       notif.Body,
		CameraName: notif.CameraName,
		DeepLink:   notif.DeepLink,
		Critical:   notif.Critical,
		Timestamp:  notif.Timestamp,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, token, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: create request to %q: %w", token, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "SentinelNVR/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: POST to %q: %w", token, err)
	}
	// Always drain and close the body to allow HTTP keep-alive connection reuse.
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: %q returned HTTP %d: %s", token, resp.StatusCode, b)
	}
	return nil
}
