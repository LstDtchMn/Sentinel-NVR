# Changelog

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
