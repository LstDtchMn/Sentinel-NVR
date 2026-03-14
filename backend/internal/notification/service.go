// Package notification delivers push and webhook alerts to registered devices
// when detection and camera events fire on the event bus (R9, Phase 8).
package notification

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
)

// Service subscribes to the event bus, looks up matching notification preferences,
// and dispatches Notification payloads via the registered Sender implementations.
// Crash recovery: on startup, pending log rows older than 5 minutes are retried.
type Service struct {
	repo      *Repository
	senders   map[string]Sender // provider → Sender
	bus       *eventbus.Bus
	logger    *slog.Logger
	done      chan struct{}
	stopCh    chan struct{} // closed by Stop() to force run() exit independently of bus.Close()
	wg        sync.WaitGroup // tracks all goroutines (run + recoverPending)
	startOnce sync.Once      // prevents double-Start
	started   atomic.Bool    // true after Start() has launched goroutines
}

// NewService creates a notification service.
// senders maps provider names ("fcm", "apns", "webhook") to their Sender implementations.
// Any provider with no Sender entry is silently skipped at dispatch time.
func NewService(repo *Repository, senders map[string]Sender, bus *eventbus.Bus, logger *slog.Logger) *Service {
	return &Service{
		repo:    repo,
		senders: senders,
		bus:     bus,
		logger:  logger.With("component", "notification"),
		done:    make(chan struct{}),
		stopCh:  make(chan struct{}),
	}
}

// Start begins the event-bus subscriber goroutine and performs crash recovery.
// It is idempotent — safe to call multiple times; subsequent calls are no-ops.
// It returns immediately; goroutines run until Stop() is called.
//
// Crash recovery uses context.Background() deliberately: the caller's context
// (typically an init timeout) is cancelled immediately after Start() returns,
// so we must not inherit it for the recovery scan.
func (s *Service) Start() {
	s.startOnce.Do(func() {
		// Set started BEFORE goroutine launch so Stop() can reliably
		// detect that Start() has been called and wait on <-s.done.
		s.started.Store(true)

		// Track both goroutines so Stop() blocks until all work finishes —
		// prevents use-after-close on the DB if recoverPending() is still
		// sending when the database closes during shutdown.
		s.wg.Add(2)
		go func() {
			defer s.wg.Done()
			s.recoverPending()
		}()
		go func() {
			defer s.wg.Done()
			s.run()
		}()
	})
}

// Stop signals run() to exit (via stopCh) and waits for all goroutines to finish.
// Safe to call whether or not the event bus has been closed first.
func (s *Service) Stop() {
	select {
	case <-s.stopCh:
		// already stopped
	default:
		close(s.stopCh)
	}
	if s.started.Load() {
		<-s.done // wait for run() to acknowledge stopCh
	}
	s.wg.Wait()
}

// run subscribes to all bus events and dispatches notifications for each.
// Exits when either the bus channel closes (normal shutdown) or stopCh is closed
// (Stop() called before bus.Close() — prevents deadlock).
func (s *Service) run() {
	defer close(s.done)

	ch := s.bus.Subscribe("*")
	for {
		select {
		case <-s.stopCh:
			s.bus.Unsubscribe(ch)
			return
		case event, ok := <-ch:
			if !ok {
				return // bus closed
			}
			// Route events: detection is handled via events.persisted to obtain
			// the DB-assigned EventID for deep links; other user-facing events are
			// handled directly from the raw event.
			switch event.Type {
			case "recording.segment_complete":
				continue // not user-facing
			case "detection":
				continue // handled below via events.persisted with DB EventID
			case "events.persisted":
				// Unwrap to the original event type so MatchingPrefs uses the
				// correct type (e.g. "detection"), and carry the EventID for the
				// notification log deep link. Only dispatch for detection events —
				// other types are already handled from their raw event above.
				if data, ok := event.Data.(map[string]any); ok {
					if origType, _ := data["type"].(string); origType == "detection" {
						event.Type = origType
						s.handleEvent(event) // event.EventID is the DB-assigned id
					}
				}
				continue
			}
			s.handleEvent(event)
		}
	}
}

// handleEvent looks up matching prefs and dispatches a notification for each
// matching (user, token) pair. Each delivery is logged to notification_log for
// crash recovery.
func (s *Service) handleEvent(event eventbus.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	prefs, err := s.repo.MatchingPrefs(ctx, event.Type, event.CameraID)
	if err != nil {
		s.logger.Warn("notification: failed to query prefs", "error", err, "event_type", event.Type)
		return
	}
	if len(prefs) == 0 {
		return
	}

	notif := buildNotification(event)

	// Collect all (token, notification) pairs to dispatch, deduplicating by user.
	type dispatchItem struct {
		tok  TokenRecord
		n    Notification
	}
	var items []dispatchItem
	seen := make(map[int]bool)
	for _, pref := range prefs {
		if seen[pref.UserID] {
			continue
		}
		seen[pref.UserID] = true

		tokens, err := s.repo.TokensForUser(ctx, pref.UserID)
		if err != nil {
			s.logger.Warn("notification: failed to get tokens for user",
				"user_id", pref.UserID, "error", err)
			continue
		}
		userNotif := notif
		userNotif.Critical = pref.Critical
		for _, tok := range tokens {
			items = append(items, dispatchItem{tok, userNotif})
		}
	}

	// Fan out delivery concurrently. A bounded WaitGroup prevents a slow webhook
	// (15 s timeout) from blocking all other notifications and the event bus.
	var wg sync.WaitGroup
	for _, item := range items {
		wg.Add(1)
		go func(tok TokenRecord, n Notification) {
			defer wg.Done()
			s.dispatch(ctx, tok, n)
		}(item.tok, item.n)
	}
	wg.Wait()
}

// dispatch sends a notification to a single token and records the result.
func (s *Service) dispatch(ctx context.Context, tok TokenRecord, notif Notification) {
	logRec := LogRecord{
		TokenID:  tok.ID,
		Provider: tok.Provider,
		Title:    notif.Title,
		Body:     notif.Body,
		DeepLink: notif.DeepLink,
	}
	if notif.EventID != 0 {
		id := int(notif.EventID)
		logRec.EventID = &id
	}

	logID, err := s.repo.CreateLog(ctx, logRec)
	if err != nil {
		s.logger.Warn("notification: failed to create log entry",
			"provider", tok.Provider, "error", err)
		// Continue — attempt delivery even without a log entry.
	}

	sender, ok := s.senders[tok.Provider]
	if !ok {
		s.logger.Warn("notification: no sender configured for provider",
			"provider", tok.Provider, "token_id", tok.ID)
		if logID != 0 {
			_ = s.repo.MarkFailed(ctx, logID, fmt.Sprintf("no sender for provider %q", tok.Provider))
		}
		return
	}

	if err := sender.Send(ctx, tok.Token, notif); err != nil {
		s.logger.Warn("notification: delivery failed",
			"provider", tok.Provider, "token_id", tok.ID, "error", err)
		if logID != 0 {
			_ = s.repo.MarkFailed(ctx, logID, err.Error())
		}
		return
	}

	s.logger.Debug("notification: delivered",
		"provider", tok.Provider, "token_id", tok.ID)
	if logID != 0 {
		_ = s.repo.MarkSent(ctx, logID)
	}
}

// recoverPending re-sends notification_log rows with status='pending' older
// than 5 minutes — these survived a crash without being sent (CG9, R9).
// Uses context.Background() so the caller's (possibly short-lived) init
// context does not cancel the recovery scan before it completes.
func (s *Service) recoverPending() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pending, err := s.repo.PendingLogs(ctx, 5*time.Minute)
	if err != nil {
		s.logger.Warn("notification: crash recovery query failed", "error", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	s.logger.Info("notification: recovering pending deliveries", "count", len(pending))

	for _, pl := range pending {
		sender, ok := s.senders[pl.Provider]
		if !ok {
			_ = s.repo.MarkFailed(ctx, pl.LogID, fmt.Sprintf("no sender for provider %q", pl.Provider))
			continue
		}

		// Reconstruct a minimal Notification from the log row.
		notif := Notification{
			Title:     pl.Title,
			Body:      pl.Body,
			DeepLink:  pl.DeepLink,
			Timestamp: time.Now(),
		}
		if pl.EventID != nil {
			notif.EventID = int64(*pl.EventID)
		}

		if err := sender.Send(ctx, pl.Token, notif); err != nil {
			s.logger.Warn("notification: recovery delivery failed",
				"log_id", pl.LogID, "provider", pl.Provider, "error", err)
			_ = s.repo.MarkFailed(ctx, pl.LogID, err.Error())
			continue
		}

		_ = s.repo.MarkSent(ctx, pl.LogID)
		s.logger.Info("notification: recovered delivery", "log_id", pl.LogID, "provider", pl.Provider)
	}
}

// buildNotification constructs a Notification from a raw eventbus.Event.
// Human-readable title/body are derived from the event type and metadata.
func buildNotification(event eventbus.Event) Notification {
	// CameraName is set by the camera pipeline when publishing events (Phase 8, M-10).
	// Fall back to Label only for legacy/non-camera events that lack CameraName.
	cameraName := event.CameraName
	if cameraName == "" {
		cameraName = event.Label
	}

	var title, body string
	switch event.Type {
	case "detection":
		label := event.Label
		if label == "" {
			label = "object"
		}
		pct := int(event.Confidence * 100)
		title = fmt.Sprintf("Detection: %s", label)
		body = fmt.Sprintf("Detected %s (%d%% confidence)", label, pct)
	case "camera.offline", "camera.disconnected":
		title = "Camera Offline"
		body = fmt.Sprintf("Camera %q has gone offline", cameraName)
	case "camera.online", "camera.connected":
		title = "Camera Online"
		body = fmt.Sprintf("Camera %q connected", cameraName)
	default:
		title = event.Type
		body = event.Type
		if cameraName != "" {
			body = fmt.Sprintf("%s on %s", event.Type, cameraName)
		}
	}

	// Build deep link for mobile app (R9). Events with a persisted ID can be
	// opened directly; others carry no deep link (empty string).
	var deepLink string
	if event.EventID != 0 {
		deepLink = fmt.Sprintf("/events/%d", event.EventID)
	}

	return Notification{
		EventType:  event.Type,
		EventID:    event.EventID, // non-zero for detection events (set via events.persisted)
		Title:      title,
		Body:       body,
		DeepLink:   deepLink,
		Thumbnail:  event.Thumbnail,
		Timestamp:  event.Timestamp,
		CameraName: cameraName,
	}
}
