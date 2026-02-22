// Package eventbus provides a Go channel-based pub/sub event bus (CG8).
package eventbus

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Event represents something that happened in the system.
type Event struct {
	Type       string    `json:"type"`
	CameraID   int       `json:"camera_id,omitempty"`
	Label      string    `json:"label,omitempty"`
	Confidence float64   `json:"confidence,omitempty"` // 0.0–1.0 for detection events
	Data       any       `json:"data,omitempty"`       // Arbitrary payload (bounding boxes, metadata, etc.)
	Timestamp  time.Time `json:"timestamp"`
}

// Bus is a process-local event bus using Go channels.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
	bufferSize  int
	closed      atomic.Bool
	logger      *slog.Logger
	dropped     atomic.Int64 // cumulative count of dropped events
}

// New creates an event bus with the given per-subscriber buffer size.
func New(bufferSize int, logger *slog.Logger) *Bus {
	return &Bus{
		subscribers: make(map[string][]chan Event),
		bufferSize:  bufferSize,
		logger:      logger.With("component", "event_bus"),
	}
}

// Subscribe returns a channel that receives events for the given topic.
// Use "*" to subscribe to all events.
func (b *Bus) Subscribe(topic string) <-chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, b.bufferSize)
	b.subscribers[topic] = append(b.subscribers[topic], ch)
	return ch
}

// Publish sends an event to all subscribers of its type and wildcard subscribers.
// Non-blocking: drops events if a subscriber's buffer is full.
// Auto-fills Timestamp if not set. Safe to call after Close() (no-op).
func (b *Bus) Publish(event Event) {
	if b.closed.Load() {
		return
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	// Send to topic-specific subscribers
	for _, ch := range b.subscribers[event.Type] {
		select {
		case ch <- event:
		default:
			n := b.dropped.Add(1)
			if n == 1 || n%100 == 0 {
				b.logger.Warn("event dropped (subscriber buffer full)", "type", event.Type, "total_dropped", n)
			}
		}
	}
	// Send to wildcard subscribers
	for _, ch := range b.subscribers["*"] {
		select {
		case ch <- event:
		default:
			n := b.dropped.Add(1)
			if n == 1 || n%100 == 0 {
				b.logger.Warn("event dropped (subscriber buffer full)", "type", event.Type, "total_dropped", n)
			}
		}
	}
}

// TODO: Phase 2 — Add Unsubscribe(ch) method to remove a single subscriber
// without closing the entire bus, needed for WebSocket client disconnect cleanup.

// Close shuts down the bus and closes all subscriber channels.
// After Close(), Publish() is a safe no-op — no panic on send-to-closed-channel.
func (b *Bus) Close() {
	b.closed.Store(true)

	b.mu.Lock()
	defer b.mu.Unlock()

	for topic, subs := range b.subscribers {
		for _, ch := range subs {
			close(ch)
		}
		delete(b.subscribers, topic)
	}
}
