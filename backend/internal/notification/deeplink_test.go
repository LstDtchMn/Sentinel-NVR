package notification

import (
	"fmt"
	"testing"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
)

// ─── Deep link construction ──────────────────────────────────────────────────

func TestBuildNotification_DeepLink_WithEventID(t *testing.T) {
	event := eventbus.Event{
		Type:       "detection",
		Label:      "person",
		Confidence: 0.9,
		EventID:    123,
		Timestamp:  time.Now(),
	}
	n := buildNotification(event)

	want := "/events/123"
	if n.DeepLink != want {
		t.Errorf("DeepLink = %q, want %q", n.DeepLink, want)
	}
}

func TestBuildNotification_DeepLink_WithoutEventID(t *testing.T) {
	event := eventbus.Event{
		Type:       "camera.offline",
		CameraName: "Back Yard",
		EventID:    0,
		Timestamp:  time.Now(),
	}
	n := buildNotification(event)

	if n.DeepLink != "" {
		t.Errorf("DeepLink = %q, want empty for zero EventID", n.DeepLink)
	}
}

func TestBuildNotification_DeepLink_FormatConsistency(t *testing.T) {
	ids := []int64{1, 42, 99999}
	for _, id := range ids {
		event := eventbus.Event{
			Type:    "detection",
			EventID: id,
		}
		n := buildNotification(event)
		want := fmt.Sprintf("/events/%d", id)
		if n.DeepLink != want {
			t.Errorf("EventID %d: DeepLink = %q, want %q", id, n.DeepLink, want)
		}
	}
}

func TestBuildNotification_EventID_PassedThrough(t *testing.T) {
	event := eventbus.Event{
		Type:    "detection",
		EventID: 456,
	}
	n := buildNotification(event)
	if n.EventID != 456 {
		t.Errorf("EventID = %d, want 456", n.EventID)
	}
}
