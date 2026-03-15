# Sentinel NVR v0.3 Plan — UI Integration + Detection Filtering

**Context:** Reviewer moved from 5/10 (v0.1) to 7/10 (v0.2). Primary feedback: backend is ahead of frontend. Ship the buttons with the endpoints.

**Goal:** Close the frontend/backend gap. Implement the reviewer's top 3 UI priorities and add the first detection-layer false positive filter.

---

## Implemented in v0.3

### 1. Export Clip UI on Playback Page

**File:** `frontend/src/pages/Playback.tsx`

- Added "Export Clip" button (scissors icon) in the Playback header toolbar
- When clicked, enters export mode showing:
  - Two range sliders (Start / End) spanning 0-24h with HH:MM:SS display
  - Selected duration indicator with color-coded 5-minute max warning
  - "Download" button that POSTs to `POST /api/v1/recordings/export` with RFC3339 timestamps
  - Triggers browser file download via the returned `download_url`
- Handles loading state (spinner), error display, and the 5-minute max gracefully
- "Cancel Export" button exits export mode and clears state
- Default range: 30-second window centered on current playhead position

**File:** `frontend/src/api/client.ts`

- Added `ExportResult` type (`export_id`, `download_url`, `duration_s`, `size_bytes`)
- Added `exportClip()` method (POST /recordings/export)
- Added `exportDownloadURL()` helper

### 2. Per-Camera Cooldown + Detection Interval in Camera Edit UI

**File:** `frontend/src/components/cameras/CameraForm.tsx`

- Added `LabeledSlider` component for numeric settings with value display
- "Notification Cooldown" slider: 30-300 seconds, step 10, default 60s
  - Controls minimum time between notifications for the same label on this camera
- "Detection Interval" slider: 0-10 seconds, step 0.5, 0 = use global default
  - Controls how often frames are grabbed for detection on this camera
- Both fields save via the existing `PUT /cameras/:name` API which already accepts `notification_cooldown_seconds` and `detection_interval`

**File:** `frontend/src/api/client.ts`

- Added `notification_cooldown_seconds` and `detection_interval` to `CameraDetail` interface
- Added same fields (optional) to `CameraInput` interface

### 3. MQTT Configuration in Settings UI

**File:** `frontend/src/pages/Settings.tsx`

- Added new "MQTT" section with:
  - Enabled toggle (enables/disables all other fields)
  - Broker URL text input (e.g., `tcp://localhost:1883`)
  - Topic Prefix input with live preview of topic structure
  - Username and Password fields (password uses type=password)
  - Home Assistant Auto-Discovery toggle
- All fields disable when MQTT is toggled off
- Saves via `PUT /api/v1/config` alongside existing server/storage settings
- Dirty detection includes all MQTT fields

**File:** `frontend/src/api/client.ts`

- Added `MQTTConfig` interface
- Added `mqtt` field to `SystemConfig`
- Updated `updateConfig()` input type to accept `mqtt` partial

**File:** `backend/internal/server/routes_config.go`

- `handleGetConfig` now returns MQTT config (password stripped for non-admin)
- `handleUpdateConfig` now accepts and applies MQTT fields

### 4. Minimum Bounding Box Area Filter (Backend)

**File:** `backend/internal/config/config.go`

- Added `MinBBoxArea *float64` field to `DetectionConfig` (yaml: `min_bbox_area`)
- Added `MinBBoxAreaValue()` method returning effective value (default 0.03 = 3%)
- Added validation: must be in [0.0, 1.0]

**File:** `backend/internal/detection/pipeline.go`

- Added `minBBoxArea float64` field to `DetectionPipeline` struct
- Updated `NewDetectionPipeline()` to accept `minBBoxArea` parameter
- Added bbox area filter in `processFrame()` after confidence threshold filtering and before zone filtering
- Filter computes `(xmax - xmin) * (ymax - ymin)` from normalized [0,1] bbox coordinates
- Detections below the threshold are silently dropped
- This kills most rain/shadow false positives at the detection layer (reviewer's rain test produced tiny scattered bboxes)

**File:** `backend/internal/camera/pipeline.go`

- Updated both `NewDetectionPipeline` call sites to pass `detCfg.MinBBoxAreaValue()`

**Configuration:** Add to `sentinel.yml`:
```yaml
detection:
  min_bbox_area: 0.03  # reject detections smaller than 3% of frame area
```

---

## Reviewer Response

The v0.2 re-evaluation identified UI integration as the new #1 problem. We addressed this directly:

1. **Clip export** now has a full UI — no more API-only access. The scissors-to-download flow is two clicks.
2. **Per-camera cooldown and detection interval** are now editable in the camera settings form with intuitive sliders instead of requiring API calls.
3. **MQTT configuration** is now manageable from the web UI instead of YAML-only editing.
4. **Detection-layer filtering** via minimum bbox area addresses the rain test false positives at the source, not just via notification throttling.

### What's next for v0.4

From the reviewer's "what would make this a 9" list, remaining items:
- **Consecutive frame confirmation** (require same label in 2+ consecutive frames before publishing)
- **HA MQTT auto-discovery** (publish `homeassistant/binary_sensor/...` config topics)
- **Object tracking** (deduplicate same-person-in-N-frames into single event with entry/exit)
- **Mobile app** (Flutter app exists; needs App Store/Play Store publishing)
