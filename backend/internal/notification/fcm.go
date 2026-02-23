package notification

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// FCMSender delivers push notifications via the Firebase Cloud Messaging HTTP v1 API.
//
// Authentication uses a service account JSON file:
//  1. Load private key + client_email from the JSON file.
//  2. Sign a short-lived JWT (RS256) asserting the service account identity.
//  3. POST the JWT to Google's token endpoint to exchange it for an OAuth2 access token.
//  4. Include the access token as `Authorization: Bearer …` on each FCM request.
//
// The access token is cached and refreshed automatically before expiry.
type FCMSender struct {
	projectID   string
	clientEmail string
	privateKey  *rsa.PrivateKey
	tokenURI    string

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time

	client *http.Client
	logger *slog.Logger
}

// serviceAccountJSON is the subset of fields we need from the service account file.
type serviceAccountJSON struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// NewFCMSender loads the service account JSON file and prepares an FCMSender.
// serviceAccountPath is the path to the Google service account .json file.
func NewFCMSender(serviceAccountPath string, logger *slog.Logger) (*FCMSender, error) {
	data, err := os.ReadFile(serviceAccountPath)
	if err != nil {
		return nil, fmt.Errorf("fcm: reading service account file: %w", err)
	}

	var sa serviceAccountJSON
	if err := json.Unmarshal(data, &sa); err != nil {
		return nil, fmt.Errorf("fcm: parsing service account JSON: %w", err)
	}
	if sa.ProjectID == "" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, fmt.Errorf("fcm: service account JSON missing required fields (project_id, client_email, private_key)")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}

	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("fcm: failed to decode private_key PEM block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("fcm: parsing private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("fcm: private_key is not RSA (got %T)", key)
	}

	return &FCMSender{
		projectID:   sa.ProjectID,
		clientEmail: sa.ClientEmail,
		privateKey:  rsaKey,
		tokenURI:    sa.TokenURI,
		client:      &http.Client{Timeout: 30 * time.Second},
		logger:      logger.With("component", "fcm"),
	}, nil
}

// Send delivers a push notification to an FCM registration token.
func (f *FCMSender) Send(ctx context.Context, token string, notif Notification) error {
	accessToken, err := f.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("fcm: get access token: %w", err)
	}

	// Build FCM v1 message payload.
	msg := map[string]any{
		"message": map[string]any{
			"token": token,
			"notification": map[string]any{
				"title": notif.Title,
				"body":  notif.Body,
			},
			"data": map[string]string{
				"event_type":  notif.EventType,
				"camera_name": notif.CameraName,
				"deep_link":   notif.DeepLink,
				"event_id":    fmt.Sprintf("%d", notif.EventID),
			},
			// Android high-priority channel for near-instant delivery.
			"android": map[string]any{
				"priority": "HIGH",
				"notification": map[string]any{
					"channel_id": "sentinel_alerts",
				},
			},
			// iOS: critical flag maps to sound/critical level (R9).
			"apns": map[string]any{
				"payload": map[string]any{
					"aps": buildAPSPayload(notif.Title, notif.Body, notif.Critical),
				},
			},
		},
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("fcm: marshal message: %w", err)
	}

	fcmURL := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", f.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fcmURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("fcm: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("fcm: HTTP request: %w", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body) // always drain to allow connection reuse
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fcm: HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

// getAccessToken returns a cached or freshly-fetched OAuth2 access token.
// The token is refreshed 60 seconds before expiry.
func (f *FCMSender) getAccessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.accessToken != "" && time.Now().Before(f.tokenExpiry.Add(-60*time.Second)) {
		return f.accessToken, nil
	}

	// Sign a JWT for the Google OAuth2 token exchange.
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   f.clientEmail,
		"scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud":   f.tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(f.privateKey)
	if err != nil {
		return "", fmt.Errorf("signing service-account JWT: %w", err)
	}

	// Exchange for an access token.
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {signed},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.tokenURI,
		bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token exchange HTTP %d: %s", resp.StatusCode, b)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access_token in response")
	}

	f.accessToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		f.tokenExpiry = now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	} else {
		f.tokenExpiry = now.Add(time.Hour)
	}
	return f.accessToken, nil
}

// buildAPSPayload builds the Apple Push Notification Service (APS) payload object.
// When critical=true, sets sound.critical=1 to bypass Do Not Disturb (R9).
func buildAPSPayload(title, body string, critical bool) map[string]any {
	aps := map[string]any{
		"alert": map[string]string{
			"title": title,
			"body":  body,
		},
	}
	if critical {
		// critical=1 + sound name required; volume defaults to 1.0
		aps["sound"] = map[string]any{
			"critical": 1,
			"name":     "default",
			"volume":   1.0,
		}
	} else {
		aps["sound"] = "default"
	}
	return aps
}
