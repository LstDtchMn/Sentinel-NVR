package notification

import "testing"

func TestBuildFCMData_WithEventIDIncludesThumbnailURL(t *testing.T) {
	notif := Notification{
		EventID:    123,
		EventType:  "detection",
		CameraName: "Front Door",
		DeepLink:   "/events/123",
	}

	got := buildFCMData(notif)

	if got["event_id"] != "123" {
		t.Fatalf("event_id = %q, want 123", got["event_id"])
	}
	if got["thumbnail_url"] != "/api/v1/events/123/thumbnail" {
		t.Fatalf("thumbnail_url = %q, want /api/v1/events/123/thumbnail", got["thumbnail_url"])
	}
	if got["event_type"] != "detection" {
		t.Fatalf("event_type = %q, want detection", got["event_type"])
	}
	if got["camera_name"] != "Front Door" {
		t.Fatalf("camera_name = %q, want Front Door", got["camera_name"])
	}
	if got["deep_link"] != "/events/123" {
		t.Fatalf("deep_link = %q, want /events/123", got["deep_link"])
	}
}

func TestBuildFCMData_WithoutEventIDNoThumbnailURL(t *testing.T) {
	notif := Notification{
		EventID:    0,
		EventType:  "camera.offline",
		CameraName: "Garage",
		DeepLink:   "",
	}

	got := buildFCMData(notif)

	if got["event_id"] != "0" {
		t.Fatalf("event_id = %q, want 0", got["event_id"])
	}
	if _, ok := got["thumbnail_url"]; ok {
		t.Fatalf("thumbnail_url should be absent when EventID is 0; got %q", got["thumbnail_url"])
	}
}

func TestBuildAPSPayload_CriticalAndNonCritical(t *testing.T) {
	critical := buildAPSPayload("T", "B", true)
	sound, ok := critical["sound"].(map[string]any)
	if !ok {
		t.Fatalf("critical sound should be object, got %T", critical["sound"])
	}
	if sound["critical"] != 1 {
		t.Fatalf("critical sound flag = %v, want 1", sound["critical"])
	}

	normal := buildAPSPayload("T", "B", false)
	if normal["sound"] != "default" {
		t.Fatalf("non-critical sound = %v, want default", normal["sound"])
	}
}

