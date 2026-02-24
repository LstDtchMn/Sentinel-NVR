package notification

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ─── buildNotification ───────────────────────────────────────────────────────

func TestBuildNotification_Detection(t *testing.T) {
	event := eventbus.Event{
		Type:       "detection",
		Label:      "person",
		Confidence: 0.95,
		CameraName: "Front Door",
		EventID:    42,
		Timestamp:  time.Now(),
	}
	n := buildNotification(event)

	if n.Title != "Detection: person" {
		t.Errorf("Title = %q, want %q", n.Title, "Detection: person")
	}
	if !strings.Contains(n.Body, "95%") {
		t.Errorf("Body %q should contain confidence percentage 95%%", n.Body)
	}
	if n.CameraName != "Front Door" {
		t.Errorf("CameraName = %q, want %q", n.CameraName, "Front Door")
	}
	if n.EventType != "detection" {
		t.Errorf("EventType = %q, want %q", n.EventType, "detection")
	}
	if n.EventID != 42 {
		t.Errorf("EventID = %d, want 42", n.EventID)
	}
}

func TestBuildNotification_DetectionEmptyLabel_FallsBackToObject(t *testing.T) {
	event := eventbus.Event{
		Type:       "detection",
		Label:      "", // empty — should fall back to "object"
		Confidence: 0.80,
	}
	n := buildNotification(event)
	if !strings.Contains(n.Body, "object") {
		t.Errorf("Body %q should use 'object' as fallback when label is empty", n.Body)
	}
}

func TestBuildNotification_DetectionConfidencePct(t *testing.T) {
	tests := []struct {
		confidence float64
		wantPct    string
	}{
		{0.99, "99%"},
		{0.50, "50%"},
		{0.01, "1%"},
	}
	for _, tt := range tests {
		event := eventbus.Event{Type: "detection", Label: "car", Confidence: tt.confidence}
		n := buildNotification(event)
		if !strings.Contains(n.Body, tt.wantPct) {
			t.Errorf("confidence %.2f: body %q does not contain %s", tt.confidence, n.Body, tt.wantPct)
		}
	}
}

func TestBuildNotification_CameraOffline(t *testing.T) {
	event := eventbus.Event{
		Type:       "camera.offline",
		CameraName: "Back Yard",
	}
	n := buildNotification(event)

	if n.Title != "Camera Offline" {
		t.Errorf("Title = %q, want %q", n.Title, "Camera Offline")
	}
	if !strings.Contains(n.Body, "Back Yard") {
		t.Errorf("Body %q should contain camera name", n.Body)
	}
}

func TestBuildNotification_CameraDisconnected(t *testing.T) {
	event := eventbus.Event{
		Type:       "camera.disconnected",
		CameraName: "Pool",
	}
	n := buildNotification(event)
	if n.Title != "Camera Offline" {
		t.Errorf("Title = %q, want %q", n.Title, "Camera Offline")
	}
}

func TestBuildNotification_CameraOnline(t *testing.T) {
	event := eventbus.Event{
		Type:       "camera.online",
		CameraName: "Garage",
	}
	n := buildNotification(event)
	if n.Title != "Camera Online" {
		t.Errorf("Title = %q, want %q", n.Title, "Camera Online")
	}
}

func TestBuildNotification_CameraConnected(t *testing.T) {
	event := eventbus.Event{
		Type:       "camera.connected",
		CameraName: "Driveway",
	}
	n := buildNotification(event)
	if n.Title != "Camera Online" {
		t.Errorf("Title = %q, want %q", n.Title, "Camera Online")
	}
}

func TestBuildNotification_CameraNameFallsBackToLabel(t *testing.T) {
	// When CameraName is empty, Label is used as the camera name.
	event := eventbus.Event{
		Type:       "camera.offline",
		CameraName: "",
		Label:      "Side Gate",
	}
	n := buildNotification(event)
	if n.CameraName != "Side Gate" {
		t.Errorf("CameraName should fall back to Label, got %q", n.CameraName)
	}
	if !strings.Contains(n.Body, "Side Gate") {
		t.Errorf("Body %q should contain fallback camera name", n.Body)
	}
}

func TestBuildNotification_ThumbnailPassedThrough(t *testing.T) {
	thumbPath := "/data/snapshots/detection_123.jpg"
	event := eventbus.Event{
		Type:      "detection",
		Thumbnail: thumbPath,
	}
	n := buildNotification(event)
	if n.Thumbnail != thumbPath {
		t.Errorf("Thumbnail = %q, want %q", n.Thumbnail, thumbPath)
	}
}

func TestBuildNotification_TimestampPassedThrough(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	event := eventbus.Event{
		Type:      "detection",
		Timestamp: ts,
	}
	n := buildNotification(event)
	if !n.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", n.Timestamp, ts)
	}
}

func TestBuildNotification_UnknownEventType_UsesTypeAsTitle(t *testing.T) {
	event := eventbus.Event{Type: "system.restart"}
	n := buildNotification(event)
	if n.EventType != "system.restart" {
		t.Errorf("EventType = %q, want %q", n.EventType, "system.restart")
	}
	if n.Title != "system.restart" {
		t.Errorf("Title = %q, want event type as title for unknown events", n.Title)
	}
}

func TestBuildNotification_UnknownTypeWithCameraName_IncludesNameInBody(t *testing.T) {
	event := eventbus.Event{
		Type:       "custom.alert",
		CameraName: "Pool Camera",
	}
	n := buildNotification(event)
	if !strings.Contains(n.Body, "Pool Camera") {
		t.Errorf("Body %q should include camera name for unknown event type", n.Body)
	}
}

// ─── Service lifecycle ───────────────────────────────────────────────────────

func TestService_StopBeforeStart_IsNoop(t *testing.T) {
	// Stop() called before Start() must not block or panic.
	// startOnce is zero value (valid), started is false, so Stop() won't wait on s.done.
	svc := &Service{
		senders: map[string]Sender{},
		bus:     eventbus.New(64, discardLogger()),
		logger:  discardLogger(),
		done:    make(chan struct{}),
		stopCh:  make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.Stop()
	}()

	select {
	case <-done:
		// OK
	case <-time.After(time.Second):
		t.Error("Stop() before Start() blocked for >1s")
	}
}

func TestService_DoubleStop_IsNoop(t *testing.T) {
	// Two Stop() calls on a not-started service must not panic.
	svc := &Service{
		senders: map[string]Sender{},
		bus:     eventbus.New(64, discardLogger()),
		logger:  discardLogger(),
		done:    make(chan struct{}),
		stopCh:  make(chan struct{}),
	}
	svc.Stop()
	svc.Stop() // must not panic (stopCh already closed)
}
