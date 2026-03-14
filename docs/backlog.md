# Sentinel NVR — Feature Backlog

Feature gaps identified from customer beta testing and cross-functional team review.
Items are grouped by category and roughly prioritized within each group.

## Camera Management
- **Camera rename** — allow renaming without delete+recreate (requires recording dir rename + go2rtc re-registration)
- **Camera grid reorder** — drag-and-drop sort order (needs `sort_order` DB column)
- **ONVIF auto-discovery** — scan network for ONVIF cameras (WS-Discovery multicast probe)
- **PTZ controls** — pan/tilt/zoom for PTZ cameras (ONVIF PTZ profile)
- **Camera snapshot on cards** — small thumbnail preview on each camera card (snapshot endpoint exists)
- **Stream URL show/hide toggle** — mask camera RTSP URLs by default with eye toggle (like password fields)

## Events & Detection
- **Event text search** — search by label within detection events (LIKE query on label field)
- **Bulk delete events** — select multiple + batch delete, or "delete all filtered"
- **Time range filter** — filter events within a day by time range (e.g., 2pm-4pm)
- **Zone editor color distinction** — include zones blue, exclude zones red, with labels on canvas

## Notifications
- **Email notifications** — SMTP configuration + email delivery (every competitor supports this)
- **Notification sound customization** — per-event-type sounds (Android channels, iOS sound files)

## Mobile App
- **Playback as top-level tab** — move Timeline from Settings sub-route to 4th tab or replace Settings tab
- **Landscape mode in Focus Mode** — allow rotation when viewing single camera
- **Notification sound customization** — platform-specific notification channels

## Settings & Configuration
- **Storage migration visibility** — show last migration timestamp + segment counts
- **Settings export/import** — backup camera configs + settings as JSON/YAML

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
