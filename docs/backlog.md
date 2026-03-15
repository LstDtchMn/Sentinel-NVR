# Sentinel NVR — Feature Backlog

Feature gaps identified from customer evaluations, team reviews, and QA testing.
Items are grouped by category and roughly prioritized within each group.

## Camera Management
- **Camera rename** — allow renaming without delete+recreate (requires recording dir rename + go2rtc re-registration)
- **Camera grid reorder** — drag-and-drop sort order (needs `sort_order` DB column)
- ~~**ONVIF auto-discovery**~~ — DONE (pkg/onvif, Scan Network UI on Cameras page)
- **Smart ONVIF discovery** — detect Docker bridge mode, skip multicast, go straight to manual probe
- **PTZ controls** — pan/tilt/zoom for PTZ cameras (ONVIF PTZ profile)
- **PTZ presets** — save/recall PTZ positions, switch on schedule or via UI
- ~~**Camera snapshot on cards**~~ — DONE (snapshot thumbnails on camera cards)
- **Camera grid size selector** — user-controlled 2x2/3x3/4x4 grid, or layout modes (grid/strip)
- **Camera stream status in edit form** — show resolution, fps, codec, connection state in edit dialog
- **Stream URL on camera cards** — show truncated main stream URL with copy button
- **Bulk camera operations** — select all + enable/disable recording/detection in bulk
- **Delete confirm shows recording count** — "Delete 'X'? 47 segments (2.3 GB) will be removed"
- **Two-way audio** — browser/mobile mic → camera intercom via WebRTC audio channel
- **Resolution badge on live tiles** — small "1080p" / "4K" badge on each camera tile

## Live View
- **Focus Mode camera cycling** — left/right arrow keys to move between cameras in Focus Mode
- **Theater mode** — fullscreen Live View hiding sidebar + header, just camera grid
- ~~**WebSocket reconnect backoff**~~ — DONE (30s timeout → "Stream Unavailable" state)

## Playback & Timeline
- **Download trimmed clip** — set start/end timestamps, download just that portion (not whole segment)
- **Brighter heatmap colors** — more vivid blues/reds for detection density overlay
- **Split-view playback** — compare two cameras side-by-side at same timestamp
- **8x/16x playback speed** — fast-forward for scanning hours of footage
- **Available dates more visible** — bold or dot indicator on calendar dates with recordings
- **Timeline event markers** — clickable detection markers at event timestamps on timeline
- **Multi-day playback** — prev/next day buttons or multi-day timeline view

## Events & Detection
- **Event text search** — search by label within detection events (LIKE query on label field)
- **Bulk event management** — "delete all matching filter", "delete older than X days", CSV export
- **Event rate display** — show "12 events/hour" next to total count
- **Time range filter** — filter events within a day by time range (e.g., 2pm-4pm)
- **Per-camera per-label alert rules** — "Person on Front Porch" not just "any detection"
- **Zone editor color distinction** — include zones blue, exclude zones red, with labels on canvas
- **Zone editor vertex editing** — edit/undo vertices after polygon close, snap to edges, show coordinates
- **Event severity / reviewed status** — mark events as reviewed/important, suppress repeat detections

## Dashboard
- **Per-camera storage usage** — show storage consumed per camera in the Camera Status table
- **System resource monitoring** — CPU, memory, disk I/O, goroutine count
- **Dashboard warning banners** — persistent alerts for storage full, cameras disconnected, subsystem errors
- **Dashboard disk capacity** — show % full, projected "days until full"

## Notifications
- **Email notifications** — SMTP configuration + email delivery (every competitor supports this)
- **Notification sound customization** — per-event-type sounds (Android channels, iOS sound files)
- **Notification pipeline debugging** — investigate why webhook/push may not trigger for detection events
- **Recording event debounce** — coalesce rapid recording start/stop events when stream is flaky

## Settings & Configuration
- **Change password from UI** — add password change form to Settings or user menu
- **Getting started banner** — first-run welcome with "Add Camera" CTA for new users
- **Setup wizard collects NVR URL** — ask for LAN IP/hostname during initial setup
- **Detection backend editable** — make detection URL + enabled/disabled configurable from Settings UI
- **Retention rule preview** — show computed retention per camera/type with rule priority
- **Larger QR code + print** — 200x200px QR with print-friendly page and instructions
- **Storage migration visibility** — show last migration timestamp + segment counts
- **Settings export/import** — backup camera configs + settings as JSON/YAML
- **Scheduled recording** — per-camera day-of-week/time-of-day recording schedule (saves storage)
- **Model download progress** — progress bar for AI model downloads

## Mobile App
- **Playback as top-level tab** — move Timeline from Settings sub-route to 4th tab
- **Mobile event type badges** — colored type badges matching web UI
- **WebRTC error messaging** — clear error when NVR reachable but streaming fails (subnet/NAT issue)
- **Mobile notification test** — test notification button in mobile Settings
- **Landscape mode in Focus Mode** — allow rotation when viewing single camera
- **Timeline pinch-to-zoom** — pinch gesture on mobile timeline scrubber

## Responsive Web
- **Tablet icon-only sidebar** — collapse to icons at 768px, expand on hover
- **Mobile settings input sizing** — wider number inputs for tap accuracy at 360px

## Faces & AI
- **Face test recognition** — upload photo + see which enrolled face matches with confidence
- **Import ONVIF credential pass-through** — imported cameras inherit ONVIF credentials

## Performance & Reliability
- **Lazy-load event thumbnails** — IntersectionObserver, only load when card scrolls into view
- **SSE reconnect gap recovery** — re-fetch missed events on SSE reconnection
- **MSE buffer pruning** — investigate MediaSource buffer growth on long-running Live View sessions

## Security & Infrastructure
- **SSE session expiry** — validate JWT periodically during long-lived SSE connections
- ~~**Production frontend Dockerfile**~~ — DONE (Dockerfile.prod + nginx.conf)
- **Structured JSON logging** — configurable log format for production log aggregation
- **Backup/restore procedure** — documented SQLite .backup + recording archival workflow
- **MQTT integration** — publish camera events to MQTT broker for Home Assistant / automation
- **User management page** — list, add, edit roles, delete users from web UI

## General
- **Keyboard shortcuts for playback** — Space=pause, arrows=seek, +/-=speed
- **Changelog / what's new** — show release notes on version update
- **Loading skeletons** — replace "Loading..." text with shimmer placeholder boxes
- **Breadcrumbs on sub-pages** — navigation trail on Event Detail, Zone Editor
