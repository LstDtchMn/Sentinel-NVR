# Webhook Notification Payload

Sentinel NVR delivers webhook notifications as HTTP POST requests with a JSON body.

## Delivery Details

| Property | Value |
|----------|-------|
| Method | `POST` |
| Content-Type | `application/json` |
| User-Agent | `SentinelNVR/1.0` |
| Timeout | 10 seconds |
| Redirects | Blocked (SSRF prevention) |
| Expected response | Any 2xx status code |
| Retry policy | Failed deliveries are retried at the configured `retry_interval` (default: 60s) |

## Payload Schema

```json
{
  "event_id": 12345,
  "event_type": "detection",
  "title": "Person detected",
  "body": "Person detected on Front Door (87% confidence)",
  "camera_name": "Front Door",
  "deep_link": "/events/12345",
  "critical": false,
  "timestamp": "2026-03-15T14:30:00-04:00"
}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `event_id` | integer | No | Database ID of the event. Omitted for system events without a persisted record. |
| `event_type` | string | Yes | One of: `detection`, `face_match`, `audio_detection`, `camera.connected`, `camera.disconnected`, `camera.offline`, `camera.error`, `recording.gap_detected` |
| `title` | string | Yes | Human-readable notification title (e.g., "Person detected") |
| `body` | string | Yes | Detailed description including camera name, label, and confidence percentage |
| `camera_name` | string | No | Name of the camera that generated the event. Omitted for system-wide events. |
| `deep_link` | string | No | Relative URL path to the event detail page (e.g., `/events/12345`) |
| `critical` | boolean | No | `true` if the notification rule has critical alert enabled (iOS Do Not Disturb bypass). Defaults to `false`. |
| `timestamp` | string (RFC 3339) | Yes | Time the event occurred, in the server's local timezone |

## Event Types

### `detection`
Triggered when the AI detection backend identifies an object (person, vehicle, animal, etc.).
```json
{
  "event_type": "detection",
  "title": "Person detected",
  "body": "Person detected on Front Door (87% confidence)",
  "camera_name": "Front Door",
  "deep_link": "/events/12345"
}
```

### `face_match`
Triggered when a detected person matches an enrolled face.
```json
{
  "event_type": "face_match",
  "title": "Face recognized: Alice",
  "body": "Alice recognized on Front Door (92% similarity)",
  "camera_name": "Front Door",
  "deep_link": "/events/12346"
}
```

### `audio_detection`
Triggered when the audio classification pipeline detects a sound event.
```json
{
  "event_type": "audio_detection",
  "title": "Glass break detected",
  "body": "Glass break detected on Warehouse Interior (78% confidence)",
  "camera_name": "Warehouse Interior",
  "deep_link": "/events/12347"
}
```

### `camera.disconnected`
Triggered when a camera stream loses all producers (RTSP source drops).
```json
{
  "event_type": "camera.disconnected",
  "title": "Camera offline",
  "body": "Front Door is offline",
  "camera_name": "Front Door"
}
```

### `camera.connected`
Triggered when a camera stream regains producers after being disconnected.
```json
{
  "event_type": "camera.connected",
  "title": "Camera online",
  "body": "Front Door is back online",
  "camera_name": "Front Door"
}
```

## Testing

Use the test endpoint to send a sample notification to a registered webhook:

```bash
curl -X POST http://localhost:8099/api/v1/notifications/test \
  -H "Content-Type: application/json" \
  -H "Cookie: sentinel_access=<token>" \
  -d '{"token_id": 1}'
```

## Registration

Register a webhook endpoint:

```bash
curl -X POST http://localhost:8099/api/v1/notifications/tokens \
  -H "Content-Type: application/json" \
  -H "Cookie: sentinel_access=<token>" \
  -d '{
    "provider": "WEBHOOK",
    "token": "https://your-server.com/sentinel-webhook",
    "label": "My Webhook"
  }'
```

**URL requirements:**
- Must use `http://` or `https://` scheme
- Must have a valid hostname (no bare IPs without scheme)
- Redirects are blocked for SSRF prevention
