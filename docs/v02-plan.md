# Sentinel NVR v0.2 Plan — Review Response and Implementation

## Review Summary

**Score:** 5/10 (architecture 9, implementation 8, features 4, daily-use 3)
**Verdict:** Would not switch from Blue Iris today.
**#1 priority:** Notification cooldown
**Switch conditions:** Cooldown, clip export, detection latency, MQTT, published app, 3 months stability

---

## Triage: Every Finding

### Unacceptable / Dealbreakers → Fix in v0.2

| # | Finding | Reviewer's words | Action |
|---|---------|-----------------|--------|
| **U1** | No notification cooldown | "12 notifications in 3 minutes... 47 false positives in 2 hours... dealbreaker for daily use" | **FIX NOW** — per-camera, per-label cooldown with configurable window |
| **U2** | No clip export | "non-starter for real-world use... spouse asks for a clip, answer is no" | **FIX NOW** — server-side ffmpeg sub-clip extraction, download endpoint |
| **U3** | Detection latency 3-4s | "by the time the notification arrives, the person may have already left the porch" | **PARTIAL FIX** — reduce default frame interval to 1s, add configurable interval in UI. Continuous sub-stream is v0.3. |
| **U4** | No Coral TPU support | "Coral TPU on my desk is completely unused" | **DEFER to v0.3** — requires TFLite runtime integration, significant effort |
| **U5** | No MQTT | "cuts off the entire home automation audience" | **FIX NOW** — bridge event bus to MQTT topics. Minimal: publish detection events. |

### Must-Have for Launch → Fix in v0.2 or v0.3

| # | Finding | v0.2? | Notes |
|---|---------|-------|-------|
| 1 | Notification cooldown | **v0.2** | #1 priority |
| 2 | Clip export | **v0.2** | #2 priority |
| 3 | Coral TPU / hardware accel | v0.3 | Significant effort, needs TFLite |
| 4 | MQTT event publishing | **v0.2** | Event bus bridge, moderate effort |
| 5 | Per-zone cooldown | **v0.2** | Part of cooldown implementation |
| 6 | Object tracking (basic dedup) | v0.3 | Requires detection pipeline rework |

### Must-Have for Year One → v0.3+

| # | Finding | Target |
|---|---------|--------|
| 7 | Two-way audio | v0.3 |
| 8 | PTZ controls | v0.3 |
| 9 | Continuous sub-stream detection | v0.3 |
| 10 | Multi-camera sync playback | v0.4 |
| 11 | Home Assistant add-on | v0.3 |
| 12 | Native binary distribution | v0.4 |
| 13 | Published mobile apps | v0.3 (App Store/Play Store submission) |

### Nice to Have → Backlog

| # | Finding | Notes |
|---|---------|-------|
| 14 | License plate recognition | Model + pipeline |
| 15 | Package detection model | Fine-tuned YOLO |
| 16 | Multi-NVR federation | Future architecture |
| 17 | HomeKit Secure Video | Via Scrypted bridge |
| 18 | Custom notification sounds | Low priority |
| 19 | Dark/light theme toggle | Already dark-first |
| 20 | Audit log | v0.4 |

### Won't Fix

| Finding | Why |
|---------|-----|
| Audio detection beyond glass break | Reviewer: "nobody is switching NVRs because of baby cry detection" |
| OIDC/OAuth2 beyond what exists | Already implemented, not a launch concern |

---

## v0.2 Implementation Plan

### Priority 1: Notification Cooldown (3-4 days)

The reviewer's #1 priority. Currently `notification/service.go` fires on every
detection event with zero suppression.

**Implementation:**

1. **Schema:** Add `notification_cooldowns` table or cooldown fields to notification
   preferences:
   ```sql
   ALTER TABLE notification_preferences ADD COLUMN cooldown_seconds INTEGER DEFAULT 60;
   ```

2. **In-memory cooldown tracker** in NotificationService:
   ```go
   type cooldownKey struct {
       CameraID string
       Label    string
       ZoneID   string  // optional, for per-zone cooldown
   }
   type cooldownTracker struct {
       mu       sync.RWMutex
       lastFired map[cooldownKey]time.Time
   }

   func (t *cooldownTracker) ShouldFire(key cooldownKey, cooldown time.Duration) bool {
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

3. **Integrate** into the notification service's event handler: before sending any
   notification, check `ShouldFire(camera+label+zone, cooldown)`.

4. **UI:** Add cooldown slider (30s-300s) to the camera edit form and notification
   settings page. Default: 60 seconds.

5. **API:** `PUT /api/v1/cameras/:id` accepts `notification_cooldown` field.

**Files to modify:**
- `internal/notification/service.go` — add cooldown check before send
- `internal/notification/repository.go` — add cooldown field to preferences
- `internal/db/migrate.go` — add migration for cooldown column
- `frontend/src/pages/NotificationSettings.tsx` — add cooldown UI
- `frontend/src/pages/Cameras.tsx` — add per-camera cooldown

### Priority 2: Clip Export (2-3 days)

Users need to extract and share specific moments from recordings.

**Implementation:**

1. **API endpoint:** `POST /api/v1/recordings/export`
   ```json
   {
     "camera_id": "front_door",
     "start_time": "2026-03-15T02:47:00Z",
     "end_time": "2026-03-15T02:47:30Z"
   }
   ```
   Returns a download URL for the extracted MP4.

2. **Server-side ffmpeg extraction:**
   ```go
   // Find the segment(s) that span the requested time range
   // Use ffmpeg to extract the sub-clip:
   // ffmpeg -ss <offset> -i segment.mp4 -t <duration> -c copy output.mp4
   ```
   Key: use `-c copy` (no re-encoding) for speed. Only re-encode if the start/end
   points need keyframe-accurate cutting.

3. **UI:** Add a "Export Clip" button to the Playback page timeline. User drags to
   select a time range, clicks Export, gets a download link.

4. **Cleanup:** Exported clips are temporary files, purged after 1 hour.

**Files to modify:**
- `internal/recording/` — new `export.go` with ffmpeg sub-clip extraction
- `internal/server/` — new export route handler
- `frontend/src/pages/Playback.tsx` — add export button + time range selector

### Priority 3: MQTT Event Bridge (1-2 days)

Bridge the internal event bus to MQTT topics for Home Assistant integration.

**Implementation:**

1. **Config:** Add MQTT broker configuration to `sentinel.yml`:
   ```yaml
   mqtt:
     enabled: false
     broker: "tcp://localhost:1883"
     topic_prefix: "sentinel"
     username: ""
     password: ""
   ```

2. **MQTT publisher** subscribes to the event bus and publishes to MQTT:
   ```
   sentinel/events/detection    → {camera, label, confidence, zone, snapshot_url}
   sentinel/events/camera       → {camera, state} (online/offline/restarted)
   sentinel/availability         → "online" (LWT: "offline")
   ```

3. **Home Assistant auto-discovery** (optional but high value):
   Publish HA MQTT discovery messages so cameras appear automatically
   in Home Assistant as `camera` and `binary_sensor` entities.

**Dependencies:** `github.com/eclipse/paho.mqtt.golang`

**Files to create:**
- `internal/mqtt/publisher.go` — event bus subscriber + MQTT publisher
- `internal/mqtt/ha_discovery.go` — Home Assistant auto-discovery messages

**Files to modify:**
- `internal/config/config.go` — add MQTT config struct
- `cmd/sentinel/main.go` — initialize MQTT publisher if enabled

### Priority 4: Reduce Detection Latency (1 day)

Quick win: reduce default frame interval from 5s to 1s and make it configurable
per-camera in the UI.

**Implementation:**

1. Change default `frame_interval` from 5 to 1 in config
2. Add `detection_interval` field to camera config (per-camera override)
3. Add interval slider to camera edit form (0.5s - 10s)
4. Document the CPU/GPU tradeoff in the UI tooltip

This gets average detection latency from 3-4s down to ~1s without any
architectural change. Continuous sub-stream processing (Frigate's approach)
is a v0.3 project.

**Files to modify:**
- `internal/config/config.go` — change default, add per-camera field
- `internal/detection/pipeline.go` — read per-camera interval
- `frontend/src/pages/Cameras.tsx` — add detection interval slider

---

## v0.2 Timeline

| Week | Deliverable |
|------|-------------|
| **Week 1** | Notification cooldown (Priority 1) + detection interval (Priority 4) |
| **Week 2** | Clip export (Priority 2) + MQTT bridge (Priority 3) |
| **Week 3** | Testing, bug fixes, documentation |

---

## Reviewer Response

```
Thank you for the month-long evaluation. The specificity — timing detection latency,
counting false positives in the rain, measuring CPU over 30 days — is exactly what
we needed. A few responses:

Shipping in v0.2 (next 2-3 weeks):

1. Notification cooldown — per-camera, per-label, configurable window (30-300s).
   Default: 60 seconds. This is our #1 priority. You're right that without it,
   notifications are either useless or overwhelming.

2. Clip export — select a time range on the timeline, download an MP4. Server-side
   ffmpeg extraction with -c copy (no re-encoding, fast). Your spouse will get
   that package delivery clip.

3. MQTT event bridge — detection events published to MQTT topics with Home Assistant
   auto-discovery. The Frigate-to-Sentinel migration path depends on this.

4. Detection interval reduced to 1 second (from 5). Average latency drops from
   3-4s to ~1s. Not as fast as Frigate with Coral (500ms), but a 3x improvement
   with zero architectural change.

Planned for v0.3:

- Coral TPU support (TFLite runtime integration)
- Continuous sub-stream detection (Frigate's approach)
- Published mobile apps (App Store + Play Store)
- Two-way audio
- PTZ controls
- Home Assistant add-on packaging

What we're NOT doing (and why):

- Object tracking across frames — this requires a fundamental change to the
  detection pipeline (stateful tracking vs. stateless frame classification).
  The cooldown system addresses the symptom (duplicate notifications) without
  the complexity. We'll revisit for v0.4.

- Native binary distribution — Docker is the right default for now. Native
  binaries are a v0.4 deliverable after the core features stabilize.

One question back to you:

You mentioned the heatmap timeline is "Sentinel's killer feature." We agree.
Would you find value in a heatmap that shows TYPES of detections (person=red,
vehicle=blue, animal=green) instead of just density? Or is pure density
sufficient for your workflow?

Your score of 5/10 with "architecture is a 9" tells us we built the right
foundation but shipped too early on the feature side. v0.2 targets the three
things you need to consider switching: cooldown, clip export, and faster
detection. We'll check back in after v0.2 ships.
```
