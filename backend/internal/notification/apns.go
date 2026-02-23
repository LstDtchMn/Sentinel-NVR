package notification

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// APNsSender delivers push notifications via Apple Push Notification service (APNs)
// HTTP/2 API using .p8 auth key (JWT-based authentication).
//
// APNs requires HTTP/2, which Go's net/http negotiates automatically via ALPN
// when connecting to an HTTPS endpoint. The auth JWT is valid for up to 1 hour
// per Apple's requirements and is cached + refreshed automatically.
//
// Reference: https://developer.apple.com/documentation/usernotifications/sending-notification-requests-to-apns
type APNsSender struct {
	keyID    string
	teamID   string
	bundleID string
	sandbox  bool
	key      *ecdsa.PrivateKey

	mu      sync.Mutex
	jwtStr  string
	jwtExp  time.Time

	client *http.Client
	logger *slog.Logger
}

// NewAPNsSender loads the .p8 private key and creates an APNsSender.
//
//   - keyPath:  path to the .p8 auth key file from Apple Developer portal
//   - keyID:    10-char key identifier shown on the Apple Developer portal
//   - teamID:   10-char team identifier from Apple Developer portal
//   - bundleID: app bundle identifier (e.g. "com.example.SentinelNVR")
//   - sandbox:  true = development APNs endpoint; false = production
func NewAPNsSender(keyPath, keyID, teamID, bundleID string, sandbox bool, logger *slog.Logger) (*APNsSender, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("apns: reading key file %q: %w", keyPath, err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("apns: failed to decode PEM block from %q", keyPath)
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: parsing private key: %w", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("apns: key is not ECDSA (got %T)", key)
	}

	// Go's net/http automatically negotiates HTTP/2 for TLS connections.
	return &APNsSender{
		keyID:    keyID,
		teamID:   teamID,
		bundleID: bundleID,
		sandbox:  sandbox,
		key:      ecKey,
		client:   &http.Client{Timeout: 30 * time.Second},
		logger:   logger.With("component", "apns"),
	}, nil
}

// Send delivers a push notification to an APNs device token.
func (a *APNsSender) Send(ctx context.Context, token string, notif Notification) error {
	authJWT, err := a.getAuthJWT()
	if err != nil {
		return fmt.Errorf("apns: get auth JWT: %w", err)
	}

	payload := map[string]any{
		"aps": buildAPSPayload(notif.Title, notif.Body, notif.Critical),
	}
	if notif.DeepLink != "" {
		payload["deep_link"] = notif.DeepLink
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("apns: marshal payload: %w", err)
	}

	host := "https://api.push.apple.com"
	if a.sandbox {
		host = "https://api.sandbox.push.apple.com"
	}
	u := fmt.Sprintf("%s/3/device/%s", host, token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("apns: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "bearer "+authJWT)
	req.Header.Set("apns-topic", a.bundleID)
	// apns-push-type must be "critical-alert" (not "alert") to bypass iOS Do Not
	// Disturb (R9). Critical alerts also require apns-priority 10.
	if notif.Critical {
		req.Header.Set("apns-push-type", "critical-alert")
		req.Header.Set("apns-priority", "10")
	} else {
		req.Header.Set("apns-push-type", "alert")
		req.Header.Set("apns-priority", "5")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("apns: HTTP/2 request: %w", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body) // always drain to allow HTTP/2 stream reuse
	if resp.StatusCode != http.StatusOK {
		var apnsErr struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(b, &apnsErr)
		reason := apnsErr.Reason
		if reason == "" {
			reason = string(b)
		}
		return fmt.Errorf("apns: HTTP %d: %s", resp.StatusCode, reason)
	}
	return nil
}

// getAuthJWT returns a cached or freshly-signed APNs provider JWT.
// APNs JWTs are valid for up to 1 hour; we refresh 5 minutes before expiry.
func (a *APNsSender) getAuthJWT() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.jwtStr != "" && time.Now().Before(a.jwtExp.Add(-5*time.Minute)) {
		return a.jwtStr, nil
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": a.teamID,
		"iat": now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = a.keyID

	signed, err := token.SignedString(a.key)
	if err != nil {
		return "", fmt.Errorf("signing APNs JWT: %w", err)
	}

	a.jwtStr = signed
	a.jwtExp = now.Add(time.Hour) // APNs JWTs are valid for up to 1 hour
	return a.jwtStr, nil
}
