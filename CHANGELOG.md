# Changelog

## v0.2.1 — Beta Feedback Fixes + Sprint 1

### Added
- **User management page** — full CRUD: create users, change roles (admin/viewer), change passwords, delete (admin-only, self-deletion blocked)
- **Camera rename** — PATCH /cameras/:name/rename API + editable name field in edit form with rename hint
- **Video export** — GET /recordings/:id/download serves MP4 with Content-Disposition attachment header
- **SQLite scheduled backups** — VACUUM INTO every 6h, 5-backup rotation, manual trigger via POST /admin/backup
- **Collapsible sidebar** — toggle between 64px icon-only and 256px full mode, localStorage persistence, smooth transition
- **Storage capacity display** — cross-platform disk usage (Statfs/GetDiskFreeSpaceEx), percentage bars with color coding
- **Edit Zones button** — link to zone editor in camera edit form (previously hidden)
- **Login forgot-password hint** — CLI reset command shown below Sign in button
- **Notification channel labels** — "Channel type" with dynamic placeholders per provider
- **Models installed badge** — green checkmark for downloaded models
- **Playback camera memory** — last selected camera persisted in localStorage

### Fixed
- **Recording segment path reconstruction** — ffmpeg segment_list outputs relative filenames with strftime patterns; now reconstructs absolute paths using hotPath/camera/date/hour structure (fixes zero recordings in DB)
- **WebSocket live view** — unwrap Gin ResponseWriter for nhooyr.io/websocket hijack; skip CORS headers for WS upgrade requests (fixes "Invalid frame header" / "response already written")
- **Recording crash loop** — exponential backoff (10s→5min cap) prevents event flooding; pipeline shows StateError after 3+ failures instead of false "recording" status
- **Recording event debounce** — suppress recording.started if last exit <5s ago; suppress recording.stopped if run <2s
- **ffmpeg stderr logging** — log ALL output at Debug level (previously silently discarded non-error lines, hiding crash causes)
- **Settings Save button** — always visible (disabled when form is clean), previously hidden until first change
- **Camera snapshot refresh** — 30s→5s interval on camera cards
- **Delete camera confirmation** — shows recording and event counts before deletion
- **Sidebar version string** — smaller text with truncate + tooltip to prevent overflow

### Known Issues
- Playback localStorage pre-fill doesn't auto-trigger timeline data fetch — user must re-select camera or change date on first visit after rebuild
- Historical recording events from pre-debounce crash loops remain in event log (not retroactively cleaned)

---

## v0.1.0 — Initial Release

### Core Architecture
- Go backend with Gin framework, SQLite/WAL mode (pure Go, no cgo)
- Event bus (Go channels + SQLite write-ahead for durability)
- Watchdog supervisor with auto-restart and system restart events
- Graceful shutdown: HTTP → cameras → watchdog → bus → DB

### Video Pipeline
- go2rtc sidecar for RTSP/WebRTC/MSE live streaming
- ffmpeg direct-to-disk recording (zero transcoding, 10-min MP4 segments)
- Camera CRUD API with DB-backed management (YAML seeds on first run only)
- Pipeline auto-recovery from go2rtc restart

### Web UI (React 19 + Vite + Tailwind)
- Dark-first responsive design with mobile hamburger menu
- Live View with camera grid + Focus Mode (click to expand, Escape to exit)
- 24/7 scrubbable timeline with heatmap overlay and 4 zoom levels
- Event feed with SSE live updates, filters, and event detail page
- System dashboard with health monitoring ("Streaming Engine" status)
- Visual zone editor (canvas polygon drawing with include/exclude zones)
- Camera management with add/edit/delete forms
- Settings page with sticky save button
- Toast notification system for user feedback
- PWA manifest for "Add to Home Screen"

### AI & Detection
- Remote HTTP detection backend support (Blue Onyx, CodeProject.AI, Ollama)
- Face recognition (ArcFace embeddings + cosine similarity, CRUD API)
- Audio intelligence (YAMNet classification stub)
- One-click AI model management (ONNX download/upload)

### Storage & Retention
- Hot/cold tiered storage with automatic migration
- Smart retention policies (per-camera x per-event-type matrix)
- Storage usage stats with hot/cold segment counts

### Security
- JWT auth with httpOnly cookies (15-min access + 7-day refresh rotation)
- AES-256-GCM camera credential encryption at rest
- CORS origin reflection + SSRF webhook validation + path traversal protection
- Login rate limiting (5 attempts / 5 min per IP)
- Admin-only routes for destructive operations

### Notifications
- Push notifications via FCM, APNs, and webhook
- Critical alerts (bypass iOS Do Not Disturb)
- Crash recovery for pending notifications
- Test notification endpoint
- Delivery log with status tracking

### Mobile Companion (Flutter)
- iOS + Android app from single codebase
- WebRTC live streaming with TURN relay support
- QR code pairing (zero-config remote access)
- Push notification deep links to events
- Biometric unlock (Face ID / fingerprint)
- Splash screen with auth state restoration

### Migration
- Blue Iris .reg file importer
- Frigate config.yml importer
- Two-step preview + import workflow

### Testing
- ~280 Go backend tests (unit + integration)
- 32 HTTP API integration tests (auth, cameras, events, config, notifications, authorization)
- 117 Playwright E2E browser tests
- Security audit: CORS, SSRF, path traversal, XSS, credential isolation verified

### Deployment
- Docker Compose primary (dev + production configs)
- Production frontend: nginx multi-stage build with gzip, SPA routing, API proxy
- Health checks on all containers
- Resource limits and log rotation
