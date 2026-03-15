package notification

import (
	"fmt"
	"sync"
	"time"
)

// CooldownTracker suppresses duplicate notifications within a configurable window.
// Thread-safe. Keyed by camera_id:label combination.
type CooldownTracker struct {
	mu        sync.RWMutex
	lastFired map[string]time.Time
}

func NewCooldownTracker() *CooldownTracker {
	return &CooldownTracker{
		lastFired: make(map[string]time.Time),
	}
}

// ShouldFire returns true if enough time has elapsed since the last notification
// for this camera+label combination. If true, records the current time.
func (t *CooldownTracker) ShouldFire(cameraID int, label string, cooldown time.Duration) bool {
	if cooldown <= 0 {
		return true // cooldown disabled
	}
	key := fmt.Sprintf("%d:%s", cameraID, label)
	t.mu.RLock()
	last, exists := t.lastFired[key]
	t.mu.RUnlock()

	if !exists || time.Since(last) >= cooldown {
		t.mu.Lock()
		t.lastFired[key] = time.Now()
		t.mu.Unlock()
		return true
	}
	return false
}
