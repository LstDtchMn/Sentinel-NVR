# Sentinel NVR — Feature Backlog

Feature gaps identified from customer beta testing. Items are grouped by category
and roughly prioritized within each group.

## Camera Management
- **Camera rename** — allow renaming without delete+recreate (requires recording dir rename + go2rtc re-registration)
- **Camera grid reorder** — drag-and-drop sort order (needs `sort_order` DB column)
- **ONVIF auto-discovery** — scan network for ONVIF cameras (WS-Discovery multicast probe)
- **PTZ controls** — pan/tilt/zoom for PTZ cameras (ONVIF PTZ profile)

## Events & Detection
- **Event text search** — search by label within detection events (LIKE query on label field)
- **Bulk delete events** — select multiple + batch delete, or "delete all filtered"

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
