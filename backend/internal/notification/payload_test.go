package notification

import (
	"testing"
)

// ─── buildAPSPayload ─────────────────────────────────────────────────────────

func TestBuildAPSPayload_Normal(t *testing.T) {
	aps := buildAPSPayload("Test Title", "Test Body", false)

	alert, ok := aps["alert"].(map[string]string)
	if !ok {
		t.Fatal("aps[alert] is not map[string]string")
	}
	if alert["title"] != "Test Title" {
		t.Errorf("title = %q, want %q", alert["title"], "Test Title")
	}
	if alert["body"] != "Test Body" {
		t.Errorf("body = %q, want %q", alert["body"], "Test Body")
	}

	// Non-critical: sound should be a simple string
	sound, ok := aps["sound"].(string)
	if !ok {
		t.Fatal("aps[sound] should be a string for non-critical alerts")
	}
	if sound != "default" {
		t.Errorf("sound = %q, want %q", sound, "default")
	}
}

func TestBuildAPSPayload_Critical(t *testing.T) {
	aps := buildAPSPayload("Alert", "Body", true)

	// Critical alerts require sound object with critical=1 for DND bypass (R9)
	soundObj, ok := aps["sound"].(map[string]any)
	if !ok {
		t.Fatal("aps[sound] should be a map for critical alerts")
	}
	if soundObj["critical"] != 1 {
		t.Errorf("critical = %v, want 1", soundObj["critical"])
	}
	if soundObj["name"] != "default" {
		t.Errorf("name = %v, want %q", soundObj["name"], "default")
	}
	if soundObj["volume"] != 1.0 {
		t.Errorf("volume = %v, want 1.0", soundObj["volume"])
	}
}

// ─── buildFCMData ────────────────────────────────────────────────────────────

func TestBuildFCMData_WithEventID(t *testing.T) {
	notif := Notification{
		EventID:    42,
		EventType:  "detection",
		CameraName: "Front Door",
		DeepLink:   "/events/42",
	}
	data := buildFCMData(notif)

	if data["event_type"] != "detection" {
		t.Errorf("event_type = %q, want %q", data["event_type"], "detection")
	}
	if data["camera_name"] != "Front Door" {
		t.Errorf("camera_name = %q, want %q", data["camera_name"], "Front Door")
	}
	if data["deep_link"] != "/events/42" {
		t.Errorf("deep_link = %q, want %q", data["deep_link"], "/events/42")
	}
	if data["event_id"] != "42" {
		t.Errorf("event_id = %q, want %q", data["event_id"], "42")
	}

	// thumbnail_url present when EventID is non-zero
	want := "/api/v1/events/42/thumbnail"
	if data["thumbnail_url"] != want {
		t.Errorf("thumbnail_url = %q, want %q", data["thumbnail_url"], want)
	}
}

func TestBuildFCMData_WithoutEventID(t *testing.T) {
	notif := Notification{
		EventID:    0,
		EventType:  "camera.offline",
		CameraName: "Back Yard",
	}
	data := buildFCMData(notif)

	if _, ok := data["thumbnail_url"]; ok {
		t.Errorf("thumbnail_url should not be present when EventID is 0, got %q", data["thumbnail_url"])
	}
	if data["event_type"] != "camera.offline" {
		t.Errorf("event_type = %q, want %q", data["event_type"], "camera.offline")
	}
}

func TestBuildFCMData_AllFieldsPopulated(t *testing.T) {
	notif := Notification{
		EventID:    99,
		EventType:  "face_match",
		CameraName: "Entrance",
		DeepLink:   "/events/99",
	}
	data := buildFCMData(notif)

	requiredKeys := []string{"event_type", "camera_name", "deep_link", "event_id", "thumbnail_url"}
	for _, key := range requiredKeys {
		if data[key] == "" {
			t.Errorf("missing or empty key %q in FCM data", key)
		}
	}
}
