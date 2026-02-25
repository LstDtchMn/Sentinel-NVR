# Sentinel NVR — REST API Reference

Base URL: `http://<host>:8099/api/v1`

Authentication: JWT access token in `sentinel_access` httpOnly cookie.
Tokens issued via `/auth/login` or `/auth/oidc/callback`.

---

## Authentication

### Check Setup Status

```
GET /setup
```

Returns whether first-run setup is needed.

**Response** `200`
```json
{ "needs_setup": true, "oidc_enabled": false }
```

### Create Admin (First Run)

```
POST /setup
```

Creates the initial admin account. Returns `409` if users already exist.

**Body**
```json
{ "username": "admin", "password": "s3cureP@ss" }
```

**Response** `201` — Sets auth cookies automatically.

### Login

```
POST /auth/login
```

Rate-limited: 5 attempts per IP per 5 minutes.

**Body**
```json
{ "username": "admin", "password": "s3cureP@ss" }
```

**Response** `200` — Sets `sentinel_access` (15 min) and `sentinel_refresh` (7 day) httpOnly cookies.
```json
{ "id": 1, "username": "admin", "role": "admin" }
```

### Refresh Token

```
POST /auth/refresh
```

Rotates the refresh token (atomic DELETE...RETURNING) and issues a new access token.
Reads `sentinel_refresh` cookie.

**Response** `200` — New cookies set.

### Logout

```
POST /auth/logout
```

Revokes the refresh token and clears auth cookies. Idempotent.

### Current User

```
GET /auth/me
```

**Auth required.**

**Response** `200`
```json
{ "id": 1, "username": "admin", "role": "admin" }
```

### OIDC Login (Optional)

```
GET /auth/oidc/login      → Redirects to identity provider
GET /auth/oidc/callback    → Handles provider callback, sets session cookies
```

Only available when OIDC is configured in `sentinel.yml`.

---

## Health

```
GET /health
```

Public. Returns `200` when healthy, `503` when degraded.

**Response**
```json
{
  "status": "ok",
  "version": "1.0.0",
  "uptime": "2h15m",
  "cameras_configured": 4,
  "recordings_count": 1247,
  "database": "connected",
  "go2rtc": "connected"
}
```

---

## Cameras

### List Cameras

```
GET /cameras
```

**Response** `200`
```json
[
  {
    "id": 1,
    "name": "Front Door",
    "enabled": true,
    "main_stream": "rtsp://user:***@192.168.1.100:554/stream1",
    "sub_stream": "rtsp://user:***@192.168.1.100:554/stream2",
    "record": true,
    "detect": true,
    "zones": [{"id":"z1","name":"Driveway","type":"include","points":[[0.1,0.2],[0.9,0.2],[0.9,0.8]]}],
    "onvif_host": "192.168.1.100",
    "onvif_port": 80,
    "onvif_user": "admin",
    "pipeline_status": "running",
    "created_at": "2026-01-15T10:00:00Z",
    "updated_at": "2026-01-15T10:00:00Z"
  }
]
```

> `onvif_pass` is never included in API responses (`json:"-"`).

### Get Camera

```
GET /cameras/:name
```

### Get Camera Status

```
GET /cameras/:name/status
```

**Response** `200`
```json
{ "status": "running" }
```

Returns `"idle"` for valid-but-disabled cameras (not 404).

### Get Camera Snapshot

```
GET /cameras/:name/snapshot
```

Returns a JPEG image proxied from go2rtc. Prefers sub-stream when configured.
Returns `503` when stream is unavailable.

**Response** `200` — `Content-Type: image/jpeg`

### Create Camera

```
POST /cameras
```

**Admin only.**

**Body**
```json
{
  "name": "Back Yard",
  "enabled": true,
  "main_stream": "rtsp://user:pass@192.168.1.101:554/stream1",
  "sub_stream": "",
  "record": true,
  "detect": false,
  "onvif_host": "",
  "onvif_port": 0,
  "onvif_user": "",
  "onvif_pass": "",
  "zones": null
}
```

**Response** `201`

### Update Camera

```
PUT /cameras/:name
```

**Admin only.** Camera name comes from the URL (body `name` is ignored).
Omitting `zones` preserves existing zones; sending `[]` clears them.

### Delete Camera

```
DELETE /cameras/:name
```

**Admin only.** Stops pipeline, removes from go2rtc, deletes from DB.

**Response** `204`

---

## Live Streaming

### WebSocket MSE Stream

```
GET /streams/:name/ws
```

Upgrades to WebSocket. Proxies go2rtc MSE (Media Source Extensions) for low-latency live viewing.
Long-lived connection — write timeout cleared per-connection.

---

## Recordings

### List Recordings

```
GET /recordings?camera=Front+Door&start=2026-01-15T00:00:00Z&end=2026-01-16T00:00:00Z&limit=50&offset=0
```

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `camera` | string | — | Filter by camera name |
| `start` | RFC3339 | — | Start of date range |
| `end` | RFC3339 | — | End of date range |
| `limit` | int | 50 | Max results (1–1000) |
| `offset` | int | 0 | Pagination offset |

**Response** `200`
```json
{
  "recordings": [
    {
      "id": 1,
      "camera_id": 1,
      "camera_name": "Front Door",
      "path": "/media/hot/Front_Door/2026-01-15/2026-01-15_10-00-00.mp4",
      "start_time": "2026-01-15T10:00:00Z",
      "end_time": "2026-01-15T10:10:00Z",
      "duration_s": 600.0,
      "size_bytes": 52428800,
      "created_at": "2026-01-15T10:00:00Z"
    }
  ],
  "total": 1
}
```

### Get Recording

```
GET /recordings/:id
```

### Play Recording

```
GET /recordings/:id/play
```

Streams the MP4 segment. Path-containment check prevents directory traversal.

**Response** `200` — `Content-Type: video/mp4`

### Delete Recording

```
DELETE /recordings/:id
```

Deletes from DB first (leaked file is recoverable; dangling DB row is not), then removes file.

**Response** `204`

### Timeline Segments

```
GET /recordings/timeline?camera=Front+Door&date=2026-01-15
```

Optimized for timeline rendering. Returns segments without file paths, sorted chronologically.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `camera` | string | Yes | Camera name |
| `date` | YYYY-MM-DD | Yes | Day to query |

### Days With Recordings

```
GET /recordings/days?camera=Front+Door&month=2026-01
```

Returns dates with recordings for date-picker highlighting.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `camera` | string | Yes | Camera name |
| `month` | YYYY-MM | Yes | Month to query |

**Response** `200`
```json
{ "days": ["2026-01-12", "2026-01-13", "2026-01-15"] }
```

---

## Events

### List Events

```
GET /events?camera_id=1&type=detection&date=2026-01-15&limit=50&offset=0
```

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `camera_id` | int | — | Filter by camera ID |
| `type` | string | — | Filter by event type |
| `date` | YYYY-MM-DD | — | Filter by date (server local time) |
| `limit` | int | 50 | Max results (1–500) |
| `offset` | int | 0 | Pagination offset |

**Response** `200`
```json
{
  "events": [
    {
      "id": 42,
      "camera_id": 1,
      "type": "detection",
      "label": "person",
      "confidence": 0.95,
      "data": "{\"objects\":[]}",
      "thumbnail": "/api/v1/events/42/thumbnail",
      "has_clip": true,
      "start_time": "2026-01-15T10:30:00Z",
      "end_time": "2026-01-15T10:31:00Z",
      "created_at": "2026-01-15T10:30:00Z"
    }
  ],
  "total": 1
}
```

> Thumbnails are returned as API URLs (not filesystem paths).

### Get Event

```
GET /events/:id
```

### Event Thumbnail

```
GET /events/:id/thumbnail
```

**Response** `200` — `Content-Type: image/jpeg`

### Delete Event

```
DELETE /events/:id
```

**Response** `204`

### Detection Heatmap

```
GET /events/heatmap?camera_id=1&date=2026-01-15
```

Returns detection density in 5-minute buckets for timeline overlay.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `camera_id` | int | Yes | Camera ID |
| `date` | YYYY-MM-DD | Yes | Day to query |

**Response** `200`
```json
[
  { "bucket_start": "2026-01-15T10:00:00Z", "detection_count": 5 },
  { "bucket_start": "2026-01-15T10:05:00Z", "detection_count": 12 }
]
```

### Event Stream (SSE)

```
GET /events/stream
```

Server-Sent Events for real-time event notifications. Long-lived connection.

**Event format:**
```
data: {"id":42,"camera_id":1,"type":"detection","label":"person","confidence":0.95,"thumbnail":"/api/v1/events/42/thumbnail","has_clip":false,"start_time":"2026-01-15T10:30:00Z"}
```

---

## Storage

### Storage Statistics

```
GET /storage/stats
```

**Response** `200`
```json
{
  "hot": { "path": "/media/hot", "used_bytes": 5368709120, "segment_count": 512 },
  "cold": { "path": "/media/cold", "used_bytes": 21474836480, "segment_count": 2048 }
}
```

---

## Notifications

### Register Device Token

```
POST /notifications/tokens
```

**Body**
```json
{ "provider": "fcm", "token": "dGVzdC10b2tlbg==", "label": "Pixel 9" }
```

Provider must be `fcm`, `apns`, or `webhook`. For webhooks, `token` is the destination URL.

**Response** `201`

### List Device Tokens

```
GET /notifications/tokens
```

Returns tokens for the current user.

### Delete Device Token

```
DELETE /notifications/tokens/:id
```

**Response** `204`

### List Notification Preferences

```
GET /notifications/prefs
```

**Response** `200`
```json
[
  {
    "id": 1,
    "user_id": 1,
    "event_type": "detection",
    "camera_id": null,
    "enabled": true,
    "critical": true
  }
]
```

### Upsert Notification Preference

```
PUT /notifications/prefs
```

**Body**
```json
{
  "event_type": "detection",
  "camera_id": null,
  "enabled": true,
  "critical": true
}
```

`event_type` must be a known type: `detection`, `face_match`, `audio_detection`, `camera.online`, `camera.offline`, `camera.connected`.
`camera_id: null` means all cameras. `critical: true` enables DND bypass on iOS.

### Delete Notification Preference

```
DELETE /notifications/prefs/:id
```

**Response** `204`

### Notification Log

```
GET /notifications/log?limit=50
```

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 50 | Max entries (1–500) |

**Response** `200`
```json
[
  {
    "id": 1,
    "event_id": 42,
    "token_id": 1,
    "provider": "fcm",
    "title": "Person detected",
    "body": "Front Door: person (95%)",
    "deep_link": "/events/42",
    "status": "sent",
    "attempts": 1,
    "scheduled_at": "2026-01-15T10:30:00Z",
    "sent_at": "2026-01-15T10:30:01Z"
  }
]
```

---

## Retention Rules

### List Rules

```
GET /retention/rules
```

**Response** `200`
```json
[
  {
    "id": 1,
    "camera_id": 1,
    "event_type": "detection",
    "events_days": 30,
    "created_at": "2026-01-15T10:00:00Z",
    "updated_at": "2026-01-15T10:00:00Z"
  }
]
```

`camera_id: null` = all cameras. `event_type: null` = all event types.

### Create Rule

```
POST /retention/rules
```

**Admin only.**

**Body**
```json
{ "camera_id": 1, "event_type": "detection", "events_days": 30 }
```

### Update Rule

```
PUT /retention/rules/:id
```

**Admin only.**

**Body**
```json
{ "events_days": 60 }
```

### Delete Rule

```
DELETE /retention/rules/:id
```

**Admin only. Response** `204`

---

## Face Recognition

All face endpoints return `503` if face recognition is not configured.

### List Faces

```
GET /faces
```

### Get Face

```
GET /faces/:id
```

### Create Face (Raw Embedding)

```
POST /faces
```

**Admin only.**

**Body**
```json
{ "name": "John Doe", "embedding": [0.123, -0.456, ...] }
```

Embedding must be exactly 512 dimensions (ArcFace).

### Enroll Face (JPEG Upload)

```
POST /faces/enroll
```

**Admin only.** `multipart/form-data`

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Person name (1–128 chars) |
| `image` | file | JPEG photo |

Returns `422` if no face detected in the image.

### Update Face

```
PUT /faces/:id
```

**Admin only.**

**Body**
```json
{ "name": "Jane Doe" }
```

### Delete Face

```
DELETE /faces/:id
```

**Admin only. Response** `204`

---

## AI Models

### List Models

```
GET /models
```

Returns curated manifest merged with locally installed models.

**Response** `200`
```json
[
  { "filename": "yolov8n.onnx", "name": "YOLOv8 Nano", "description": "General object detection", "size_bytes": 12582912, "installed": true, "curated": true }
]
```

### Download Curated Model

```
POST /models/:filename/download
```

**Admin only.** Triggers download from curated manifest.

### Upload Model

```
POST /models/upload
```

**Admin only.** `multipart/form-data`, field: `file` (ONNX).

### Delete Model

```
DELETE /models/:filename
```

**Admin only. Response** `204`

---

## Import / Migration

### Preview Import

```
POST /import/preview
```

**Admin only.** `multipart/form-data`

| Field | Type | Description |
|-------|------|-------------|
| `format` | string | `blue_iris` or `frigate` |
| `file` | file | .reg or config.yml (max 5 MB) |

Dry-run: parses and validates without modifying DB.

**Response** `200`
```json
{
  "format": "blue_iris",
  "cameras": [{ "name": "Front Door", "enabled": true, "main_stream": "rtsp://..." }],
  "warnings": ["Camera 'Garage' has no RTSP URL"],
  "errors": []
}
```

### Execute Import

```
POST /import
```

**Admin only.** Same form fields as preview. Creates cameras, skipping duplicates.

**Response** `200`
```json
{ "imported": 3, "skipped": 1, "errors": [], "warnings": [] }
```

---

## Remote Access

### ICE Servers

```
GET /relay/ice-servers
```

Returns STUN/TURN servers for WebRTC NAT traversal.

**Response** `200`
```json
[
  { "urls": ["stun:stun.l.google.com:19302"] },
  { "urls": ["turn:coturn:3478"], "username": "sentinel", "credential": "..." }
]
```

### Generate Pairing Code

```
POST /pairing/qr
```

**Admin only.** Generates a one-time pairing code for mobile app remote access.

**Response** `200`
```json
{ "code": "a1b2c3d4-e5f6-7890-abcd-ef1234567890", "expires_at": "2026-01-15T10:15:00Z" }
```

### Redeem Pairing Code

```
POST /pairing/redeem
```

**Public** (rate-limited). Mobile app exchanges pairing code for session tokens.

**Body**
```json
{ "code": "a1b2c3d4-e5f6-7890-abcd-ef1234567890" }
```

---

## Configuration

### Get Config

```
GET /config
```

Returns current configuration with sensitive fields stripped.

### Update Config

```
PUT /config
```

**Admin only.** Updates runtime-safe fields. Storage paths require a restart.

**Body**
```json
{
  "server": { "log_level": "debug" },
  "storage": { "hot_retention_days": 5, "cold_retention_days": 60, "segment_duration": 15 }
}
```

---

## Error Responses

All errors follow the format:
```json
{ "error": "description of what went wrong" }
```

| Code | Meaning |
|------|---------|
| 400 | Validation failure (bad input) |
| 401 | Not authenticated |
| 403 | Not authorized (admin required) |
| 404 | Resource not found |
| 409 | Conflict (duplicate resource) |
| 422 | Semantic error (e.g., no face detected in image) |
| 500 | Internal server error |
| 503 | Service unavailable (stream offline, feature disabled) |

## Rate Limits

| Endpoint | Limit |
|----------|-------|
| `POST /auth/login` | 5 attempts per IP per 5 minutes |
| `POST /pairing/redeem` | 5 attempts per IP per 5 minutes |
