package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhookSender_SendPostsPayload(t *testing.T) {
	var got webhookPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
		if ua := r.Header.Get("User-Agent"); ua != "SentinelNVR/1.0" {
			t.Fatalf("User-Agent = %q, want SentinelNVR/1.0", ua)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sender := NewWebhookSender()
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	notif := Notification{
		EventID:    42,
		EventType:  "detection",
		Title:      "Detection: person",
		Body:       "Detected person (97% confidence)",
		CameraName: "Front Door",
		DeepLink:   "/events/42",
		Critical:   true,
		Timestamp:  ts,
	}

	if err := sender.Send(context.Background(), srv.URL, notif); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.EventID != notif.EventID {
		t.Fatalf("event_id = %d, want %d", got.EventID, notif.EventID)
	}
	if got.EventType != notif.EventType {
		t.Fatalf("event_type = %q, want %q", got.EventType, notif.EventType)
	}
	if got.Title != notif.Title || got.Body != notif.Body {
		t.Fatalf("title/body mismatch: got %q/%q", got.Title, got.Body)
	}
	if got.CameraName != notif.CameraName {
		t.Fatalf("camera_name = %q, want %q", got.CameraName, notif.CameraName)
	}
	if got.DeepLink != notif.DeepLink {
		t.Fatalf("deep_link = %q, want %q", got.DeepLink, notif.DeepLink)
	}
	if !got.Critical {
		t.Fatal("critical = false, want true")
	}
	if !got.Timestamp.Equal(ts) {
		t.Fatalf("timestamp = %v, want %v", got.Timestamp, ts)
	}
}

func TestWebhookSender_DoesNotFollowRedirect(t *testing.T) {
	var targetHits atomic.Int32

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()

	sender := NewWebhookSender()
	err := sender.Send(context.Background(), redirect.URL, Notification{
		EventType: "detection",
		Title:     "Detection",
		Body:      "Body",
		Timestamp: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected non-2xx redirect response to fail")
	}
	if !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("error %q does not mention HTTP 302", err.Error())
	}
	if targetHits.Load() != 0 {
		t.Fatalf("redirect target was called %d times, expected 0", targetHits.Load())
	}
}
