// Package notification delivers push and webhook alerts to registered devices
// when detection and camera events fire on the event bus (R9).
package notification

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a DB record doesn't exist.
var ErrNotFound = errors.New("notification record not found")

// Notification is the content payload sent to a device.
// Senders adapt this into their provider-specific wire format.
type Notification struct {
	EventID    int64     // DB events.id (0 when not from events table)
	EventType  string    // original event type, e.g. "detection"
	Title      string
	Body       string
	CameraName string
	Thumbnail  string // absolute path to JPEG snapshot; FCM/APNs may attach inline
	DeepLink   string // e.g. "/events/123" — deep link for mobile app (Phase 11)
	// Critical enables iOS Critical Alerts, bypassing Do Not Disturb (R9).
	// Only set when the matching notification preference has critical=true.
	Critical  bool
	Timestamp time.Time
}

// Sender delivers a Notification to a single registered endpoint.
// token is provider-specific: FCM registration token, APNs device token, or webhook URL.
type Sender interface {
	Send(ctx context.Context, token string, notif Notification) error
}

// TokenRecord is a row from notification_tokens.
type TokenRecord struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Token     string    `json:"token"`
	Provider  string    `json:"provider"` // "fcm" | "apns" | "webhook"
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PrefRecord is a row from notification_prefs.
type PrefRecord struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	EventType string `json:"event_type"`
	CameraID  *int   `json:"camera_id"` // nil = all cameras
	Enabled   bool   `json:"enabled"`
	Critical  bool   `json:"critical"`
}

// LogRecord is a row from notification_log.
type LogRecord struct {
	ID          int        `json:"id"`
	EventID     *int       `json:"event_id,omitempty"`
	TokenID     int        `json:"token_id"`
	Provider    string     `json:"provider"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	DeepLink    string     `json:"deep_link,omitempty"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"last_error,omitempty"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
}
