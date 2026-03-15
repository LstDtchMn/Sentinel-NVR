package notification

import (
	"testing"
	"time"
)

func TestCooldownTracker_SuppressesDuplicates(t *testing.T) {
	tracker := NewCooldownTracker()
	cooldown := 60 * time.Second

	if !tracker.ShouldFire(1, "person", cooldown) {
		t.Fatal("first event should fire")
	}
	if tracker.ShouldFire(1, "person", cooldown) {
		t.Fatal("immediate duplicate should be suppressed")
	}
	if !tracker.ShouldFire(2, "person", cooldown) {
		t.Fatal("different camera should fire")
	}
	if !tracker.ShouldFire(1, "vehicle", cooldown) {
		t.Fatal("different label on same camera should fire")
	}
}

func TestCooldownTracker_ExpiresAfterWindow(t *testing.T) {
	tracker := NewCooldownTracker()
	cooldown := 50 * time.Millisecond

	if !tracker.ShouldFire(1, "person", cooldown) {
		t.Fatal("first event should fire")
	}
	time.Sleep(100 * time.Millisecond)
	if !tracker.ShouldFire(1, "person", cooldown) {
		t.Fatal("should fire again after cooldown expires")
	}
}

func TestCooldownTracker_ZeroCooldownAlwaysFires(t *testing.T) {
	tracker := NewCooldownTracker()
	if !tracker.ShouldFire(1, "person", 0) {
		t.Fatal("zero cooldown should always fire")
	}
	if !tracker.ShouldFire(1, "person", 0) {
		t.Fatal("zero cooldown should always fire (2nd)")
	}
}
