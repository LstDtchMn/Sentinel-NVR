# Sentinel NVR -- API Reference

Base URL: `http://localhost:8099/api/v1`

All endpoints return JSON unless otherwise noted. Errors use the format `{"error": "message"}`.

Authentication is cookie-based. Protected endpoints require a valid `sentinel_access` JWT cookie (set by `/auth/login`). Admin-only endpoints additionally require the user to have the `admin` role. When `auth.enabled: false` in config, all endpoints are accessible without authentication.

---

## Health

### GET /health

Public health check. Returns 200 when the server is running.

| Field     | Type   | Description          |
|-----------|--------|----------------------|
| `status`  | string | Always `"ok"`        |
| `version` | string | Server version       |

```json
{"status": "ok", "version": "0.1.0"}
```

**Status codes**: 200

---

### GET /admin/health

Detailed system health with subsystem status. Admin only.

| Field                  | Type   | Description                       |
|------------------------|--------|-----------------------------------|
| `status`               | string | `"ok"` or `"degraded"`           |
| `version`              | string | Server version                    |
| `uptime`               | string | e.g. `"2h15m30s"`                |
| `go_version`           | string | Go runtime version                |
| `os`                   | string | Operating system                  |
| `arch`                 | string | CPU architecture                  |
| `cameras_configured`   | int    | Total cameras in DB               |
| `recordings_count`     | int    | Total recording segments          |
| `database`             | string | `"connected"` or `"error"`       |
| `go2rtc`               | string | `"connected"` or `"disconnected"`|

**Status codes**: 200 (healthy), 403 (not admin), 503 (degraded)

---

## Auth

### POST /auth/login

Authenticate with username and password. Sets `sentinel_access` and `sentinel_refresh` httpOnly cookies.

**Request body:**

```json
{"username": "admin", "password": "mypassword"}
```

**Response (200):**

```json
{"message": "logged in"}
```

**Status codes**: 200, 400 (missing fields), 401 (invalid credentials), 429 (rate limited)

Rate limit: 5 attempts per IP per 5-minute window. Resets on successful login.

---

### POST /auth/refresh

Rotate the refresh token and issue a new access token. Reads the `sentinel_refresh` cookie automatically.

**Response (200):**

```json
{"message": "refreshed"}
```

**Status codes**: 200, 401 (expired/missing token)

---

### POST /auth/logout

Revoke the refresh token and clear auth cookies.

**Response (200):**

```json
{"message": "logged out"}
```

**Status codes**: 200

---

### GET /auth/me

Returns the authenticated user's profile. Requires valid JWT.

**Response (200):**

```json
{"id": 1, "username": "admin", "role": "admin"}
```

**Status codes**: 200, 401

---

### GET /setup

Check if first-run setup is needed. Public.

**Response (200):**

```json
{"needs_setup": true, "oidc_enabled": false}
```

---

### POST /setup

Create the first admin account during initial setup. Public, but returns 409 if setup is already complete.

**Request body:**

```json
{"username": "admin", "password": "securepass123"}
```

**Response (201):**

```json
{
  "user": {"id": 1, "username": "admin", "role": "admin"}
}
```

Sets auth cookies on success (auto-login).

**Status codes**: 201, 400 (validation), 409 (already set up)

---

### GET /auth/oidc/login

Redirects the browser to the configured OIDC identity provider. Only registered when OIDC is enabled.

**Status codes**: 302 (redirect), 500

---

### GET /auth/oidc/callback

Handles the OIDC authorization code redirect. Sets auth cookies and redirects to `/live` on success.

**Query params**: `code`, `state`

**Status codes**: 302

---

## Cameras

### GET /cameras

List all cameras with pipeline status.

**Response (200):**

```json
[
  {
    "id": 1,
    "name": "Front Door",
    "enabled": true,
    "main_stream": "rtsp://...",
    "sub_stream": "",
    "record": true,
    "detect": true,
    "onvif_host": "192.168.1.100",
    "onvif_port": 80,
    "onvif_user": "admin",
    "zones": [{"id": "z1", "name": "Driveway", "type": "include", "points": [{"x": 0.1, "y": 0.2}]}],
    "pipeline_status": {"state": "streaming", "connected_at": "2025-01-15T10:30:00Z"},
    "created_at": "2025-01-01T00:00:00Z",
    "updated_at": "2025-01-15T00:00:00Z"
  }
]
```

---

### GET /cameras/:name

Get a single camera by name.

**Status codes**: 200, 404

---

### GET /cameras/:name/status

Get pipeline status for a camera. Returns idle status for disabled cameras.

**Response (200):**

```json
{"state": "streaming", "connected_at": "2025-01-15T10:30:00Z"}
```

**Status codes**: 200, 404

---

### POST /cameras

Create a new camera. Admin only.

**Request body:**

```json
{
  "name": "Front Door",
  "enabled": true,
  "main_stream": "rtsp://user:pass@192.168.1.100:554/stream1",
  "sub_stream": "rtsp://user:pass@192.168.1.100:554/stream2",
  "record": true,
  "detect": false,
  "onvif_host": "192.168.1.100",
  "onvif_port": 80,
  "onvif_user": "admin",
  "onvif_pass": "password",
  "zones": []
}
```

Required fields: `name`, `main_stream`. All others have defaults.

**Status codes**: 201, 400 (validation), 403, 409 (duplicate name)

---

### PUT /cameras/:name

Update an existing camera. Admin only. The name in the URL is canonical (body name is ignored).

Omitting `zones` (or sending `null`) preserves existing zones. Sending `[]` clears all zones.

**Request body**: Same schema as POST.

**Status codes**: 200, 400, 403, 404

---

### DELETE /cameras/:name

Delete a camera, its pipeline, and go2rtc stream. Admin only.

**Status codes**: 204 (no body), 403, 404

---

### POST /cameras/:name/restart

Restart a camera's pipeline without modifying the DB. Admin only.

**Response (200):**

```json
{"status": "restarted"}
```

**Status codes**: 200, 403, 404

---

### GET /cameras/:name/snapshot

Get a JPEG snapshot from the camera's live stream. Prefers sub-stream when configured.

**Response**: `image/jpeg` binary data with `Cache-Control: no-cache`.

**Status codes**: 200, 404, 503 (stream unavailable or camera disabled)

---

### POST /cameras/test-stream

Test whether a stream URL is reachable via go2rtc. Rate-limited to one call per 5 seconds.

**Request body:**

```json
{"url": "rtsp://user:pass@192.168.1.100:554/stream1"}
```

**Response (200):**

```json
{"status": "ok", "message": "Stream is reachable"}
```

**Status codes**: 200, 400, 422 (unreachable), 429 (rate limited)

---

### POST /cameras/discover

Run ONVIF WS-Discovery multicast probe. Admin only. No request body.

**Response (200):**

```json
{
  "cameras": [
    {"xaddr": "http://192.168.1.100:80/onvif/device_service", "name": "IPC", "manufacturer": "Hikvision"}
  ],
  "warning": "..."
}
```

The `warning` field is set when multicast fails (e.g., Docker bridge networking).

**Status codes**: 200, 403

---

### POST /cameras/discover/probe

Query a specific ONVIF camera by IP. Admin only. Returns device info; with credentials, also returns stream profiles with RTSP URIs.

**Request body:**

```json
{"host": "192.168.1.100", "port": 80, "username": "admin", "password": "pass"}
```

`username` and `password` are optional -- without them, only device info is returned.

**Response (200):**

```json
{
  "device": {"manufacturer": "Hikvision", "model": "DS-2CD2143G2-I", "firmware": "V5.7.1"},
  "streams": [
    {"name": "mainStream", "uri": "rtsp://192.168.1.100:554/Streaming/Channels/101", "resolution": "2688x1520"}
  ]
}
```

**Status codes**: 200, 400, 403, 502 (device unreachable)

---

## Events

### GET /events

List detection events with filtering and pagination.

**Query params:**

| Param            | Type   | Default | Description                          |
|------------------|--------|---------|--------------------------------------|
| `camera_id`      | int    |         | Filter by camera ID                  |
| `type`           | string |         | Filter by event type                 |
| `date`           | string |         | Filter by date (YYYY-MM-DD)          |
| `min_confidence` | float  |         | Minimum confidence (0.0-1.0)         |
| `limit`          | int    | 50      | Results per page (1-500)             |
| `offset`         | int    | 0       | Pagination offset                    |

**Response (200):**

```json
{
  "events": [
    {
      "id": 42,
      "camera_id": 1,
      "camera_name": "Front Door",
      "type": "detection",
      "label": "person",
      "confidence": 0.87,
      "start_time": "2025-01-15T14:30:00Z",
      "thumbnail": "/api/v1/events/42/thumbnail",
      "has_clip": true,
      "zones": ["Driveway"]
    }
  ],
  "total": 156
}
```

---

### GET /events/:id

Get a single event by ID.

**Status codes**: 200, 400, 404

---

### GET /events/:id/thumbnail

Serve the JPEG snapshot for an event.

**Response**: `image/jpeg` binary data.

**Status codes**: 200, 400, 403 (path traversal), 404

---

### DELETE /events/:id

Delete an event and its thumbnail file. Admin only.

**Status codes**: 204, 400, 403, 404

---

### GET /events/heatmap

Get detection event density in 5-minute buckets for a camera on a given date. Used for timeline heatmap overlay.

**Query params (required):**

| Param       | Type   | Description            |
|-------------|--------|------------------------|
| `camera_id` | int    | Camera ID              |
| `date`      | string | Date (YYYY-MM-DD)      |

**Response (200):**

```json
[0, 0, 3, 7, 2, 0, 0, 1, ...]
```

Array of 288 integers (one per 5-minute bucket across 24 hours).

**Status codes**: 200, 400, 404 (camera not found)

---

### GET /events/stream

Open a Server-Sent Events (SSE) connection for real-time event updates. The connection stays open indefinitely with a 30-second heartbeat.

**Response**: `text/event-stream`

```
: connected

data: {"id":43,"camera_id":1,"type":"detection","label":"person","confidence":0.92,...}

: heartbeat
```

**Status codes**: 200

---

## Recordings

### GET /recordings

List recording segments with filtering.

**Query params:**

| Param    | Type   | Default | Description                      |
|----------|--------|---------|----------------------------------|
| `camera` | string |         | Filter by camera name            |
| `start`  | string |         | Start time (RFC3339)             |
| `end`    | string |         | End time (RFC3339)               |
| `limit`  | int    | 50      | Results per page (1-1000)        |
| `offset` | int    | 0       | Pagination offset                |

**Response (200):**

```json
{
  "recordings": [
    {
      "id": 100,
      "camera_id": 1,
      "camera_name": "Front Door",
      "path": "/media/hot/Front_Door/2025-01-15/seg_143000.mp4",
      "start_time": "2025-01-15T14:30:00Z",
      "end_time": "2025-01-15T14:40:00Z",
      "duration_s": 600.0,
      "size_bytes": 52428800
    }
  ],
  "total": 432
}
```

---

### GET /recordings/:id

Get a single recording segment's metadata.

**Status codes**: 200, 400, 404

---

### GET /recordings/:id/play

Serve the MP4 file for a recording segment. Supports HTTP Range headers for seeking.

**Response**: `video/mp4` binary data.

**Status codes**: 200 (or 206 partial), 400, 403 (path traversal), 404

---

### DELETE /recordings/:id

Delete a recording segment from DB and disk. Admin only.

**Status codes**: 204, 400, 403, 404

---

### GET /recordings/timeline

Get all completed segments for a camera on a given day, optimized for timeline rendering. Returns segments without file paths, sorted chronologically.

**Query params (required):**

| Param    | Type   | Description            |
|----------|--------|------------------------|
| `camera` | string | Camera name            |
| `date`   | string | Date (YYYY-MM-DD)      |

**Response (200):**

```json
[
  {"id": 100, "start_time": "2025-01-15T00:00:00Z", "end_time": "2025-01-15T00:10:00Z", "duration_s": 600},
  {"id": 101, "start_time": "2025-01-15T00:10:00Z", "end_time": "2025-01-15T00:20:00Z", "duration_s": 600}
]
```

**Status codes**: 200, 400, 404 (camera not found)

---

### GET /recordings/days

Get dates with recordings for a camera in a given month. Used by the date picker.

**Query params (required):**

| Param   | Type   | Description             |
|---------|--------|-------------------------|
| `camera`| string | Camera name             |
| `month` | string | Month (YYYY-MM)         |

**Response (200):**

```json
["2025-01-01", "2025-01-02", "2025-01-05"]
```

**Status codes**: 200, 400, 404 (camera not found)

---

## Storage

### GET /storage/stats

Aggregate storage usage per tier.

**Response (200):**

```json
{
  "hot": {
    "path": "/media/hot",
    "used_bytes": 5368709120,
    "segment_count": 432
  },
  "cold": {
    "path": "/media/cold",
    "used_bytes": 21474836480,
    "segment_count": 1728
  }
}
```

`cold` is `null` when cold storage is not configured.

**Status codes**: 200

---

## Config

### GET /config

Get current system configuration. Sensitive fields (passwords, keys, storage paths for non-admins) are stripped.

**Response (200):**

```json
{
  "server": {"host": "0.0.0.0", "port": 8099, "log_level": "info"},
  "storage": {
    "hot_path": "/media/hot",
    "cold_path": "/media/cold",
    "hot_retention_days": 3,
    "cold_retention_days": 30,
    "segment_duration": 10,
    "segment_format": "mp4"
  },
  "detection": {"enabled": true, "backend": "remote"},
  "cameras": [
    {"name": "Front Door", "enabled": true, "record": true, "detect": true}
  ]
}
```

`hot_path` and `cold_path` are only included for admin users.

---

### PUT /config

Update runtime configuration. Admin only. Only non-sensitive, runtime-safe fields are updatable.

**Request body (all fields optional):**

```json
{
  "server": {"log_level": "debug"},
  "storage": {
    "hot_retention_days": 7,
    "cold_retention_days": 60,
    "segment_duration": 15
  }
}
```

Returns the full sanitized config on success (same format as GET /config).

**Status codes**: 200, 400 (validation), 403, 500 (save failed)

---

## Notifications

All notification endpoints return 503 if `notifications.enabled: false`.

### POST /notifications/tokens

Register a device token for push delivery.

**Request body:**

```json
{"provider": "webhook", "token": "https://hooks.example.com/sentinel", "label": "My Webhook"}
```

`provider` must be `fcm`, `apns`, or `webhook`. Webhook URLs are validated for SSRF (no private/loopback IPs).

**Status codes**: 201, 400, 503

---

### GET /notifications/tokens

List registered device tokens for the current user.

**Response (200):**

```json
[
  {"id": 1, "provider": "webhook", "token": "https://...", "label": "My Webhook", "created_at": "..."}
]
```

---

### DELETE /notifications/tokens/:id

Remove a registered device token.

**Status codes**: 204, 400, 404, 503

---

### GET /notifications/prefs

List notification preferences for the current user.

**Response (200):**

```json
[
  {"id": 1, "event_type": "detection", "camera_id": null, "enabled": true, "critical": false}
]
```

---

### PUT /notifications/prefs

Create or update a notification preference.

**Request body:**

```json
{"event_type": "detection", "camera_id": 1, "enabled": true, "critical": false}
```

Valid event types: `*`, `detection`, `face_match`, `audio_detection`, `camera.offline`, `camera.online`, `camera.connected`, `camera.disconnected`, `camera.error`.

`camera_id: null` applies to all cameras.

**Status codes**: 200, 400, 503

---

### DELETE /notifications/prefs/:id

Remove a notification preference.

**Status codes**: 204, 400, 404, 503

---

### GET /notifications/log

Recent notification delivery log for the current user.

**Query params:**

| Param   | Type | Default | Description      |
|---------|------|---------|------------------|
| `limit` | int  | 50      | Max entries (1-500) |

**Status codes**: 200, 400, 503

---

### POST /notifications/test

Send a test notification to a registered device token.

**Request body:**

```json
{"token_id": 1}
```

**Response (200):**

```json
{"status": "sent"}
```

**Status codes**: 200, 400, 404 (token not found), 422 (delivery failed / provider not configured), 503

---

## Faces

All face endpoints return 503 if face recognition is not configured. Write operations (POST, PUT, DELETE) are admin only.

### GET /faces

List all enrolled faces (without embedding vectors).

**Response (200):**

```json
{
  "faces": [
    {"id": 1, "name": "John Doe", "photo_path": "", "created_at": "2025-01-15T00:00:00Z"}
  ]
}
```

---

### GET /faces/:id

Get a single enrolled face by ID.

**Status codes**: 200, 400, 404, 503

---

### POST /faces

Enroll a new face with a pre-computed embedding vector. Admin only.

**Request body:**

```json
{"name": "John Doe", "embedding": [0.123, -0.456, ...]}
```

The embedding must have exactly 512 dimensions (ArcFace format). Name must be 1-128 characters.

**Status codes**: 201, 400, 403, 503

---

### POST /faces/enroll

Enroll a new face from a JPEG photo. Admin only. Requires `multipart/form-data`.

**Form fields:**

| Field   | Type | Description                    |
|---------|------|--------------------------------|
| `name`  | text | Person name (1-128 chars)      |
| `image` | file | JPEG photo (max 16 MB)         |

The image is sent to the sentinel-infer backend for face embedding extraction. Returns 422 if no face is detected.

**Status codes**: 201, 400, 403, 413 (too large), 422 (no face), 502 (inference failed), 503

---

### PUT /faces/:id

Rename an enrolled face. Admin only.

**Request body:**

```json
{"name": "Jane Doe"}
```

**Status codes**: 200, 400, 403, 404, 503

---

### DELETE /faces/:id

Delete an enrolled face. Admin only.

**Status codes**: 204, 400, 403, 404, 503

---

## Models

All model endpoints are admin only.

### GET /models

List curated manifest models merged with locally installed models.

**Response (200):**

```json
[
  {
    "filename": "yolov8n.onnx",
    "name": "YOLOv8 Nano",
    "description": "Fast general object detection",
    "size_bytes": 12345678,
    "installed": true,
    "curated": true
  },
  {
    "filename": "custom_model.onnx",
    "name": "custom_model.onnx",
    "size_bytes": 98765432,
    "installed": true,
    "curated": false
  }
]
```

---

### POST /models/:filename/download

Download a curated model from the manifest.

**Response (200):**

```json
{"filename": "yolov8n.onnx", "path": "/data/models/yolov8n.onnx", "status": "installed"}
```

**Status codes**: 200, 403, 404 (not in manifest), 502 (download failed)

---

### POST /models/upload

Upload a custom ONNX model file. `multipart/form-data` with a `file` field.

Max file size: 2 GiB. Only `.onnx` files are accepted.

**Response (201):**

```json
{"filename": "custom_model.onnx", "size_bytes": 98765432, "status": "installed"}
```

**Status codes**: 201, 400, 403, 413 (too large)

---

### DELETE /models/:filename

Delete a locally installed model file.

**Status codes**: 204, 400, 403, 404

---

## Import

Import cameras from Blue Iris or Frigate configuration files. Both endpoints accept `multipart/form-data` with `format` (text) and `file` (upload). Admin only. Max file size: 5 MB.

### POST /import/preview

Dry-run parse of an uploaded config file. Returns what would be imported without modifying the database.

**Form fields:**

| Field    | Type | Description                         |
|----------|------|-------------------------------------|
| `format` | text | `blue_iris` or `frigate`            |
| `file`   | file | `.reg` file (Blue Iris) or `.yml` (Frigate) |

**Response (200):**

```json
{
  "format": "blue_iris",
  "cameras": [
    {"name": "cam_FrontDoor", "main_stream": "rtsp://...", "enabled": true, "record": true}
  ],
  "warnings": ["Camera 'Backyard' has no RTSP URL configured"],
  "errors": []
}
```

---

### POST /import

Execute the import: parse the file and create cameras in the database. Existing cameras (by name) are skipped.

**Response (200):**

```json
{
  "imported": 3,
  "skipped": 1,
  "errors": [],
  "warnings": ["camera \"Backyard\": already exists, skipped"]
}
```

**Status codes**: 200, 400, 403

---

## Retention Rules

Per-camera, per-event-type retention policy matrix. All endpoints are admin only.

### GET /retention/rules

List all configured retention rules.

**Response (200):**

```json
[
  {"id": 1, "camera_id": 1, "event_type": "detection", "events_days": 30, "created_at": "..."},
  {"id": 2, "camera_id": null, "event_type": null, "events_days": 14, "created_at": "..."}
]
```

`camera_id: null` = all cameras. `event_type: null` = all event types.

---

### POST /retention/rules

Create a new retention rule.

**Request body:**

```json
{"camera_id": 1, "event_type": "detection", "events_days": 30}
```

`camera_id` and `event_type` are optional (omit for wildcard rules). Valid event types: `detection`, `face_match`, `audio_detection`, `camera.online`, `camera.offline`, `camera.connected`, `camera.disconnected`, `camera.error`.

**Status codes**: 201, 400, 403, 409 (duplicate camera/event-type combination)

---

### PUT /retention/rules/:id

Update the retention period for an existing rule.

**Request body:**

```json
{"events_days": 60}
```

**Status codes**: 200, 400, 403, 404

---

### DELETE /retention/rules/:id

Delete a retention rule.

**Status codes**: 204, 400, 403, 404

---

## Pairing and Remote Access

### POST /pairing/qr

Generate a short-lived pairing code for QR-based mobile pairing. Admin only. Requires auth to be enabled.

**Response (201):**

```json
{
  "code": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
  "expires_at": "2025-01-15T15:15:00Z"
}
```

The code expires after 15 minutes. The web UI encodes this into a QR image for the mobile app to scan.

**Status codes**: 201, 403

---

### POST /pairing/redeem

Exchange a valid pairing code for a session. Public endpoint (no auth required). Sets auth cookies on success.

**Request body:**

```json
{"code": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"}
```

**Response (200):**

```json
{"message": "paired"}
```

Rate-limited separately from `/auth/login` (5 attempts per IP per 5-minute window).

**Status codes**: 200, 400, 401 (invalid/expired/used code), 429

---

### GET /relay/ice-servers

Get ICE server configuration for WebRTC peer connections. Returns STUN server always; includes TURN credentials when `relay.enabled: true`.

**Response (200):**

```json
{
  "ice_servers": [
    {"urls": ["stun:stun.l.google.com:19302"]},
    {"urls": ["turn:coturn:3478"], "username": "sentinel", "credential": "password"}
  ]
}
```

The TURN entry is only included when relay is enabled in config.

**Status codes**: 200

---

## Live Streaming

### GET /streams/:name/ws

WebSocket proxy to go2rtc for MSE live streaming. Upgrades to WebSocket protocol.

The client sends codec negotiation after connection, then receives binary MSE media segments. Connection stays open until the client disconnects or the camera stream stops.

This is not a REST endpoint -- it requires a WebSocket client.

**Status codes**: 101 (upgrade), 404

---

## Error Format

All error responses follow this format:

```json
{"error": "descriptive error message"}
```

Common status codes across all endpoints:

| Code | Meaning                                      |
|------|----------------------------------------------|
| 400  | Bad request (validation error, missing field) |
| 401  | Unauthorized (missing or expired JWT)         |
| 403  | Forbidden (admin role required)               |
| 404  | Resource not found                            |
| 409  | Conflict (duplicate resource)                 |
| 413  | Request entity too large                      |
| 422  | Unprocessable entity (valid request but cannot fulfill) |
| 429  | Too many requests (rate limited)              |
| 500  | Internal server error                         |
| 503  | Service unavailable (feature not configured)  |
