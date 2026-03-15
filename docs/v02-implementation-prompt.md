# Dev Prompt: Sentinel NVR v0.2 — Implement the Four Priorities

You are the Sentinel NVR engineering team. A veteran NVR user (3 years Blue Iris,
12 cameras, Coral TPU) gave v0.1 a 5/10 and said they wouldn't switch from Blue Iris
until four things are fixed. The v0.2 plan (`docs/v02-plan.md`) was approved. Now build
it.

The reviewer's exact words on what moves Sentinel from "interesting project" to "daily
driver":

> "Notification cooldown is the thing that, if fixed, moves Sentinel from 'interesting
> project I run alongside my real NVR' to 'this is my daily driver.'"

> "When my spouse asks 'can you send me that clip of the package being delivered,' the
> answer is 'no.' This is a non-starter for real-world use."

> "Average detection latency from a person entering the frame to seeing the event in
> the UI: approximately 3-4 seconds. Frigate detects in under 500ms."

> "No MQTT. No Home Assistant integration. This cuts off the entire home automation
> audience."

---

## The Four Priorities (in order)

### Priority 1: Notification Cooldown (3-4 days)

**The problem:** Every detection event fires a notification. A garbage truck triggers
12 alerts in 3 minutes. Rain triggers 47 false positives in 2 hours. Users either
drown in notifications or disable them entirely.

**The solution:** Per-camera, per-label cooldown with configurable window.

**Implementation:**

#### 1a. Database migration

Add cooldown configuration to the notification system. The simplest approach: add a
`cooldown_seconds` column to whatever table stores notification preferences, or add
it to the camera configuration.

```sql
-- Option A: Per-camera cooldown (simpler)
ALTER TABLE cameras ADD COLUMN notification_cooldown_seconds INTEGER NOT NULL DEFAULT 60;

-- Option B: Per-camera per-label cooldown (more granular, matches reviewer's request)
CREATE TABLE notification_cooldowns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '*',  -- '*' = all labels
    zone_id TEXT NOT NULL DEFAULT '*',  -- '*' = all zones
    cooldown_seconds INTEGER NOT NULL DEFAULT 60,
    UNIQUE(camera_id, label, zone_id)
);
```

Recommendation: Start with Option A (per-camera). Add per-label granularity in v0.3
if users ask for it. The reviewer's #1 complaint was about ALL notifications being
too frequent, not about needing different cooldowns for "person" vs "vehicle."

#### 1b. In-memory cooldown tracker

In the notification service, maintain a map of last-fired times:

```go
// internal/notification/cooldown.go

type CooldownTracker struct {
    mu       sync.RWMutex
    lastFired map[string]time.Time  // key: "cameraID:label"
}

func NewCooldownTracker() *CooldownTracker {
    return &CooldownTracker{
        lastFired: make(map[string]time.Time),
    }
}

func (t *CooldownTracker) ShouldFire(cameraID, label string, cooldown time.Duration) bool {
    key := cameraID + ":" + label
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
```

#### 1c. Integrate into notification service

In `internal/notification/service.go`, before sending any notification:

```go
func (s *Service) handleDetectionEvent(event DetectionEvent) {
    // Look up camera's cooldown setting
    cooldown := s.getCameraCooldown(event.CameraID)

    // Check if we should fire
    if !s.cooldownTracker.ShouldFire(event.CameraID, event.Label, cooldown) {
        s.logger.Debug("notification suppressed by cooldown",
            "camera", event.CameraID,
            "label", event.Label,
            "cooldown", cooldown)
        return
    }

    // ... existing notification sending logic ...
}
```

#### 1d. UI

Add a cooldown slider to the camera edit form:

```
Notification Cooldown: [====60s====] (30s - 300s)
After detecting an event, suppress further notifications for this duration.
```

Also add a global default in the notification settings page.

#### 1e. API

- `PUT /api/v1/cameras/:id` — accept `notification_cooldown_seconds` field
- `GET /api/v1/cameras/:id` — return the field
- `PUT /api/v1/settings/notifications` — accept `default_cooldown_seconds`

#### 1f. Tests

```go
func TestCooldownTracker_SuppressesDuplicates(t *testing.T) {
    tracker := NewCooldownTracker()
    cooldown := 60 * time.Second

    // First event should fire
    assert.True(t, tracker.ShouldFire("cam1", "person", cooldown))
    // Immediate second event should be suppressed
    assert.False(t, tracker.ShouldFire("cam1", "person", cooldown))
    // Different camera should fire
    assert.True(t, tracker.ShouldFire("cam2", "person", cooldown))
    // Different label on same camera should fire
    assert.True(t, tracker.ShouldFire("cam1", "vehicle", cooldown))
}

func TestCooldownTracker_ExpiresAfterWindow(t *testing.T) {
    tracker := NewCooldownTracker()
    cooldown := 100 * time.Millisecond

    assert.True(t, tracker.ShouldFire("cam1", "person", cooldown))
    time.Sleep(150 * time.Millisecond)
    // Should fire again after cooldown expires
    assert.True(t, tracker.ShouldFire("cam1", "person", cooldown))
}
```

---

### Priority 2: Clip Export (2-3 days)

**The problem:** Users can only download full 10-minute segments. No way to extract
and share a 30-second clip.

**Implementation:**

#### 2a. API endpoint

```
POST /api/v1/recordings/export
{
  "camera_id": "front_door",
  "start": "2026-03-15T02:47:00Z",
  "end": "2026-03-15T02:47:30Z"
}

Response:
{
  "export_id": "abc123",
  "status": "processing",
  "download_url": "/api/v1/recordings/export/abc123/download"
}
```

#### 2b. Server-side ffmpeg extraction

```go
// internal/recording/export.go

func (s *Service) ExportClip(cameraID string, start, end time.Time) (string, error) {
    // 1. Find the segment(s) that span the time range
    segments := s.findSegments(cameraID, start, end)
    if len(segments) == 0 {
        return "", ErrNoRecordings
    }

    // 2. Generate output path
    exportID := uuid.New().String()
    outputPath := filepath.Join(s.exportDir, exportID+".mp4")

    // 3. Build ffmpeg command
    // For single segment:
    //   ffmpeg -ss <offset> -i segment.mp4 -t <duration> -c copy -movflags +faststart output.mp4
    // For multi-segment (spanning two 10-min files):
    //   Use concat demuxer or sequential extraction

    if len(segments) == 1 {
        offset := start.Sub(segments[0].StartTime)
        duration := end.Sub(start)
        cmd := exec.CommandContext(ctx, "ffmpeg",
            "-ss", fmt.Sprintf("%.3f", offset.Seconds()),
            "-i", segments[0].FilePath,
            "-t", fmt.Sprintf("%.3f", duration.Seconds()),
            "-c", "copy",
            "-movflags", "+faststart",
            "-y", outputPath,
        )
        if err := cmd.Run(); err != nil {
            return "", fmt.Errorf("ffmpeg export failed: %w", err)
        }
    } else {
        // Multi-segment: create concat list, extract across boundary
        return s.exportMultiSegment(segments, start, end, outputPath)
    }

    // 4. Schedule cleanup (delete export after 1 hour)
    time.AfterFunc(1*time.Hour, func() {
        os.Remove(outputPath)
    })

    return exportID, nil
}
```

#### 2c. UI: Timeline range selector

In `Playback.tsx`, add a "Export Clip" mode:

1. User clicks "Export" button on the timeline toolbar
2. Two draggable handles appear on the timeline (start/end)
3. User positions the handles around the desired time range
4. Preview shows the selected duration (e.g., "32 seconds selected")
5. Click "Download" — POST to the export API, show progress, trigger download

#### 2d. Cleanup

- Exported clips stored in a temp directory
- Background goroutine purges files older than 1 hour
- Maximum concurrent exports: 3 (prevent abuse)
- Maximum clip duration: 5 minutes (prevent full-day exports)

---

### Priority 3: MQTT Event Bridge (1-2 days)

**The problem:** No Home Assistant integration. Frigate's MQTT events are the backbone
of the HA ecosystem. Without MQTT, Sentinel is invisible to the smart home crowd.

**Implementation:**

#### 3a. MQTT config

```yaml
# sentinel.yml
mqtt:
  enabled: false
  broker: "tcp://localhost:1883"
  topic_prefix: "sentinel"
  username: ""
  password: ""
  ha_discovery: true  # publish Home Assistant auto-discovery messages
```

#### 3b. MQTT publisher service

```go
// internal/mqtt/publisher.go

type Publisher struct {
    client      mqtt.Client
    prefix      string
    eventBus    *eventbus.Bus
    haDiscovery bool
}

func (p *Publisher) Start() {
    // Subscribe to event bus
    p.eventBus.Subscribe("detection.*", p.onDetection)
    p.eventBus.Subscribe("camera.*", p.onCameraEvent)

    // Publish availability
    p.publish(p.prefix+"/availability", "online")

    // Publish HA discovery (if enabled)
    if p.haDiscovery {
        p.publishHADiscovery()
    }
}

func (p *Publisher) onDetection(event eventbus.Event) {
    // Publish to: sentinel/events/front_door/person
    topic := fmt.Sprintf("%s/events/%s/%s",
        p.prefix, event.CameraID, event.Label)
    payload := map[string]interface{}{
        "camera":     event.CameraID,
        "label":      event.Label,
        "confidence": event.Confidence,
        "zone":       event.Zone,
        "snapshot":   event.SnapshotURL,
        "timestamp":  event.Timestamp.Unix(),
    }
    p.publishJSON(topic, payload)
}
```

#### 3c. Home Assistant auto-discovery

```go
// Publish discovery messages so cameras appear automatically in HA
func (p *Publisher) publishHADiscovery() {
    cameras := p.getCameras()
    for _, cam := range cameras {
        // Binary sensor for person detection
        topic := fmt.Sprintf("homeassistant/binary_sensor/sentinel_%s_person/config", cam.ID)
        config := map[string]interface{}{
            "name":          cam.Name + " Person",
            "state_topic":   fmt.Sprintf("%s/events/%s/person", p.prefix, cam.ID),
            "device_class":  "occupancy",
            "unique_id":     fmt.Sprintf("sentinel_%s_person", cam.ID),
            "availability_topic": p.prefix + "/availability",
        }
        p.publishJSON(topic, config)
    }
}
```

#### 3d. Dependency

Add `github.com/eclipse/paho.mqtt.golang` to `go.mod`.

---

### Priority 4: Reduce Detection Interval (1 day)

**The problem:** Default 5-second frame interval means 3-4s average detection latency.

**Implementation:**

1. Change default `frame_interval` from 5 to 1 in `internal/config/config.go`
2. Add `detection_interval` field to camera model (per-camera override)
3. In `internal/detection/pipeline.go`, read per-camera interval if set, else use global
4. Add slider to camera edit form: "Detection Interval: 0.5s - 10s" with tooltip
   explaining CPU tradeoff
5. Document: "Lower intervals = faster detection but higher CPU usage. Recommended:
   1s for important cameras, 5s for interior/low-priority cameras."

---

## Execution Order

```
Day 1-2:  Priority 1a-1c (cooldown backend + tracker + integration)
Day 3:    Priority 1d-1f (cooldown UI + API + tests)
Day 4:    Priority 4 (detection interval — quick win, ship early)
Day 5-6:  Priority 2a-2b (clip export backend + ffmpeg)
Day 7:    Priority 2c-2d (clip export UI + cleanup)
Day 8-9:  Priority 3a-3c (MQTT bridge + HA discovery)
Day 10:   Priority 3d (MQTT testing with real HA instance)
Day 11-12: Integration testing, bug fixes, documentation
Day 13:   Tag v0.2.0
```

## Definition of Done

v0.2 is done when:

- [ ] The garbage truck test: 12 detections in 3 minutes produces exactly 1 notification
      (with 60s cooldown)
- [ ] The rain test: 47 false positives in 2 hours produces ≤5 notifications
      (with 300s cooldown on that camera)
- [ ] The clip test: user selects 30 seconds on the timeline, clicks Export, downloads
      a playable MP4 within 10 seconds
- [ ] The MQTT test: detection event appears in Home Assistant within 2 seconds
- [ ] The latency test: person detection fires within 1.5 seconds of entering frame
      (with 1s interval)
- [ ] The reviewer re-tests and moves their score from 5/10 toward 7/10

## What NOT to Do

- Do NOT refactor the detection pipeline for continuous sub-stream processing. That's
  v0.3. The frame-interval reduction (5s → 1s) is the quick win.
- Do NOT add Coral TPU support. That requires TFLite runtime integration and is a
  separate workstream.
- Do NOT build object tracking. The cooldown system addresses the symptom (duplicate
  notifications) without the architectural complexity.
- Do NOT publish the mobile app to app stores yet. The Flutter app needs the cooldown
  and clip export features integrated first.
- Do NOT start on two-way audio or PTZ. Those are v0.3 features.

Focus. Four features. Thirteen days. Ship.
