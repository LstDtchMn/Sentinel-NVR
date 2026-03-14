# Sentinel NVR — Feature Backlog

Feature gaps identified from customer beta testing and cross-functional team review.
Items are grouped by category and roughly prioritized within each group.

## Camera Management
- **Camera rename** — allow renaming without delete+recreate (requires recording dir rename + go2rtc re-registration)
- **Camera grid reorder** — drag-and-drop sort order (needs `sort_order` DB column)
- ~~**ONVIF auto-discovery**~~ — DONE (pkg/onvif, Scan Network UI on Cameras page)
- **PTZ controls** — pan/tilt/zoom for PTZ cameras (ONVIF PTZ profile)
- **PTZ presets** — save/recall PTZ positions, switch on schedule or via UI
- ~~**Camera snapshot on cards**~~ — DONE (snapshot thumbnails on camera cards)
- **Camera grid size selector** — user-controlled 2x2/3x3/4x4 grid, or camera groups/folders
- **Camera stream status in edit form** — show resolution, fps, codec, connection state in edit dialog
- **Two-way audio** — browser/mobile mic → camera intercom via WebRTC audio channel
- **Stream URL show/hide toggle** — mask camera RTSP URLs by default with eye toggle (like password fields)

## Events & Detection
- **Event text search** — search by label within detection events (LIKE query on label field)
- **Bulk delete events** — select multiple + batch delete, or "delete all filtered"
- **Time range filter** — filter events within a day by time range (e.g., 2pm-4pm)
- **Zone editor color distinction** — include zones blue, exclude zones red, with labels on canvas
- **Zone editor vertex editing** — edit/undo vertices after polygon close, snap to edges, show coordinates
- **Timeline event markers** — clickable detection markers on timeline at event timestamps
- **Event severity / reviewed status** — mark events as reviewed/important, suppress repeat detections
- **Multi-day playback** — view multiple days or prev/next day navigation on timeline

## Notifications
- **Email notifications** — SMTP configuration + email delivery (every competitor supports this)
- **Notification sound customization** — per-event-type sounds (Android channels, iOS sound files)

## Mobile App
- **Playback as top-level tab** — move Timeline from Settings sub-route to 4th tab or replace Settings tab
- **Landscape mode in Focus Mode** — allow rotation when viewing single camera
- **Notification sound customization** — platform-specific notification channels
- **Timeline pinch-to-zoom** — pinch gesture on mobile timeline scrubber

## Settings & Configuration
- **Storage migration visibility** — show last migration timestamp + segment counts
- **Settings export/import** — backup camera configs + settings as JSON/YAML
- **Scheduled recording** — per-camera day-of-week/time-of-day recording schedule (saves storage)

## General
- **User management page** — list, add, edit roles, delete users from web UI
- **Keyboard shortcuts for playback** — Space=pause, arrows=seek, +/-=speed
- **Changelog / what's new** — show release notes on version update
- **Loading skeletons** — replace "Loading..." text with shimmer placeholder boxes
- **Breadcrumbs on sub-pages** — navigation trail on Event Detail, Zone Editor
- **Proactive form validation** — green checkmark on valid stream URL format before submit
- **Sidebar camera indicators** — recording status dots next to camera names in sidebar
- **Dashboard disk capacity** — show % full, projected "days until full", disk warnings

## Video Pipeline
- **WebSocket reconnect backoff** — show "Stream unavailable" after N failed retries instead of infinite spinner
- **Recording event debounce** — coalesce rapid recording start/stop events when stream is flaky

## Security & Infrastructure
- **SSE session expiry** — validate JWT periodically during long-lived SSE connections
- ~~**Production frontend Dockerfile**~~ — DONE (Dockerfile.prod + nginx.conf)
- **Structured JSON logging** — configurable log format for production log aggregation
- **Backup/restore procedure** — documented SQLite .backup + recording archival workflow
- **MQTT integration** — publish camera events to MQTT broker for Home Assistant / automation
