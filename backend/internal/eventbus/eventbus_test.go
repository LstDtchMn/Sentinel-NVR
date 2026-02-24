package eventbus

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNew(t *testing.T) {
	bus := New(16, testLogger())
	if bus == nil {
		t.Fatal("New returned nil")
	}
	if bus.bufferSize != 16 {
		t.Errorf("bufferSize = %d, want 16", bus.bufferSize)
	}
}

func TestPublishSubscribe(t *testing.T) {
	bus := New(16, testLogger())
	ch := bus.Subscribe("test.event")

	bus.Publish(Event{Type: "test.event", Label: "hello"})

	select {
	case e := <-ch:
		if e.Label != "hello" {
			t.Errorf("Label = %q, want %q", e.Label, "hello")
		}
		if e.Timestamp.IsZero() {
			t.Error("Timestamp should be auto-filled")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestWildcardSubscriber(t *testing.T) {
	bus := New(16, testLogger())
	ch := bus.Subscribe("*")

	bus.Publish(Event{Type: "any.event", Label: "test"})

	select {
	case e := <-ch:
		if e.Type != "any.event" {
			t.Errorf("Type = %q, want %q", e.Type, "any.event")
		}
	case <-time.After(time.Second):
		t.Fatal("wildcard subscriber did not receive event")
	}
}

func TestTopicIsolation(t *testing.T) {
	bus := New(16, testLogger())
	ch1 := bus.Subscribe("topic.a")
	ch2 := bus.Subscribe("topic.b")

	bus.Publish(Event{Type: "topic.a", Label: "a"})

	select {
	case e := <-ch1:
		if e.Label != "a" {
			t.Errorf("ch1: Label = %q, want %q", e.Label, "a")
		}
	case <-time.After(time.Second):
		t.Fatal("ch1 did not receive event")
	}

	// ch2 should NOT receive the event
	select {
	case e := <-ch2:
		t.Fatalf("ch2 unexpectedly received event: %+v", e)
	case <-time.After(50 * time.Millisecond):
		// Expected: no event for ch2
	}
}

func TestPublishAfterClose(t *testing.T) {
	bus := New(16, testLogger())
	ch := bus.Subscribe("test")
	bus.Close()

	// Publish after close should be a no-op (no panic)
	bus.Publish(Event{Type: "test", Label: "should-not-arrive"})

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after bus.Close()")
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := New(16, testLogger())
	ch := bus.Subscribe("test")

	bus.Unsubscribe(ch)

	// Channel should be closed after unsubscribe
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after Unsubscribe()")
	}

	// Publishing should not panic
	bus.Publish(Event{Type: "test", Label: "after-unsub"})
}

func TestUnsubscribeAfterClose(t *testing.T) {
	bus := New(16, testLogger())
	ch := bus.Subscribe("test")
	bus.Close()

	// Unsubscribe after close should be a no-op (no panic)
	bus.Unsubscribe(ch)
}

func TestNonBlockingPublish(t *testing.T) {
	bus := New(1, testLogger()) // buffer of 1
	_ = bus.Subscribe("test")

	// Fill the buffer
	bus.Publish(Event{Type: "test", Label: "first"})

	// This should not block — event is dropped
	bus.Publish(Event{Type: "test", Label: "second"})

	if bus.dropped.Load() == 0 {
		t.Error("expected at least one dropped event")
	}
}

func TestTimestampAutoFill(t *testing.T) {
	bus := New(16, testLogger())
	ch := bus.Subscribe("test")

	before := time.Now()
	bus.Publish(Event{Type: "test"})
	after := time.Now()

	e := <-ch
	if e.Timestamp.Before(before) || e.Timestamp.After(after) {
		t.Errorf("auto-filled timestamp %v not between %v and %v", e.Timestamp, before, after)
	}
}

func TestTimestampPreserved(t *testing.T) {
	bus := New(16, testLogger())
	ch := bus.Subscribe("test")

	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	bus.Publish(Event{Type: "test", Timestamp: ts})

	e := <-ch
	if !e.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", e.Timestamp, ts)
	}
}

func TestConcurrentPublish(t *testing.T) {
	bus := New(1000, testLogger())
	ch := bus.Subscribe("*")

	const goroutines = 10
	const eventsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				bus.Publish(Event{Type: "concurrent", CameraID: id})
			}
		}(i)
	}

	// Drain events
	received := 0
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	timeout := time.After(5 * time.Second)
	for {
		select {
		case <-ch:
			received++
			if received >= goroutines*eventsPerGoroutine {
				return
			}
		case <-done:
			// All goroutines done, drain remaining
			for {
				select {
				case <-ch:
					received++
				default:
					if received < goroutines*eventsPerGoroutine {
						t.Logf("received %d events (some may have been dropped with buffer of 1000)", received)
					}
					return
				}
			}
		case <-timeout:
			t.Fatalf("timed out after receiving %d events", received)
		}
	}
}

func TestMultipleSubscribers(t *testing.T) {
	bus := New(16, testLogger())
	ch1 := bus.Subscribe("test")
	ch2 := bus.Subscribe("test")

	bus.Publish(Event{Type: "test", Label: "multi"})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case e := <-ch:
			if e.Label != "multi" {
				t.Errorf("ch%d: Label = %q, want %q", i+1, e.Label, "multi")
			}
		case <-time.After(time.Second):
			t.Fatalf("ch%d did not receive event", i+1)
		}
	}
}
