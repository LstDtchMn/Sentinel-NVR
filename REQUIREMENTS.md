# Sentinel NVR — Requirements Constitution

> This document is the authoritative source of truth for what Sentinel NVR must be.
> Every feature, pull request, and architectural decision should be traceable back to
> a requirement listed here. If it conflicts with this document, the code is wrong.

---

## Mission Statement

Sentinel NVR fills the gap between **Frigate NVR** and **Blue Iris** — combining
Frigate's open-source AI-first approach with Blue Iris's broad compatibility, while
fixing the critical shortcomings of both.

---

## Core Goals & Technical Decisions

These are the foundational decisions that shape every line of code in the project.

### CG1. Performance

Fix the high CPU usage common in NVRs by utilizing Direct-to-Disk recording and
sub-stream detection. Zero transcoding by default.

### CG2. Modern Stack

| Layer          | Technology                          | Rationale                                    |
|----------------|-------------------------------------|----------------------------------------------|
| **Backend**    | Go (Golang)                         | Goroutines for per-camera pipelines, easy cross-compile (R1) |
| **Frontend**   | React (TypeScript) + Vite + Tailwind | Modern, fast dev cycle                       |
| **Database**   | SQLite (WAL mode) via `modernc.org/sqlite` | Zero deps, crash-resistant (R4), pure Go (no cgo) |
| **HTTP**       | Gin (`github.com/gin-gonic/gin`)    | Largest ecosystem, built-in JWT/CORS/rate-limit middleware |
| **API Style**  | REST (JSON) + WebSocket for events  | Universal, any client can consume it         |

### CG3. Video Pipeline Architecture

**Hybrid approach: go2rtc + ffmpeg**

```
Go Engine
  ├── go2rtc (library/sidecar)
  │     ├── RTSP intake for all cameras
  │     ├── Live view → WebRTC/MSE to browser
  │     └── Sub-stream → AI detection pipeline
  │
  └── ffmpeg processes (per camera)
        └── Main stream → direct-to-disk recording
```

- **go2rtc** handles live streaming (WebRTC, MSE, HLS) and RTSP management
- **ffmpeg** processes (one per camera) handle recording — crash-isolated via managed goroutines
- **Live streaming to browser**: MSE (fragmented MP4) by default (~1s latency), WebRTC opt-in for low-latency/P2P

### CG4. Recording Format

**Fixed-duration MP4 segments (10 minutes)**

```
/recordings/
  /front_door/
    /2025-01-15/
      /08/
        00.00.mp4   (minutes 0–9)
        10.00.mp4   (minutes 10–19)
        20.00.mp4   (minutes 20–29)
```

- Each segment is independently playable (plays in VLC, browser)
- Corruption only loses 10 minutes max
- Tiered storage (R13) = just `mv` files from hot → cold
- Direct copy from camera (H.264/H.265, no re-encoding)

### CG5. Deployment

**Docker-primary, native binary available**

| Path            | Target Audience | Hardware Access |
|-----------------|-----------------|-----------------|
| Docker Compose  | 80% of users    | GPU via `--device`/`--runtime`, TPU via `--device` |
| Native binary   | Power users     | Full native access (GPU, TPU, disk I/O) |

Build matrix: `linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/arm64`

### CG6. Secure by Default

- **Local auth** with bcrypt-hashed passwords + JWT sessions, enabled on first run
- **Optional OIDC/OAuth2** for users with existing SSO (Google, Authelia, Keycloak)
- Camera credentials **encrypted at rest** (AES-256, key derived from admin password)
- API endpoints require auth — no anonymous access by default

### CG7. API-First

Every feature is an API endpoint first, UI second. The Web UI and mobile app (R8) are
equal consumers of the same REST API. If the API can't do it, the UI can't do it.

### CG8. Internal Event Architecture

**Go channels + SQLite write-ahead for durability**

```
Detection event fires
  │
  ├→ 1. Write to SQLite (durable record)
  │
  └→ 2. Broadcast via Go channel
        ├→ Notification sender → FCM/APNs (mobile push)
        ├→ WebSocket → live UI update
        └→ Timeline indexer
```

On crash recovery: check SQLite for unsent notifications, retry any that failed.

### CG9. Crash Resilience

- Per-camera process isolation (ffmpeg processes managed by goroutines)
- WAL-mode SQLite — no database corruption
- Watchdog supervisor restarts crashed services (R4)
- Event write-ahead log ensures no silent failures

### CG10. AI Detection Pipeline

**Dedicated detector processes + external API support**

```
go2rtc (sub-stream, e.g. 640x480)
  │
  ▼
Frame Buffer (shared memory ring)
  │
  ▼
Detector Interface
  ├─ LocalDetector (managed processes, crash-isolated)
  │    ├─ OpenVINO   (Intel iGPU)
  │    ├─ TensorRT   (NVIDIA GPU)
  │    ├─ CoreML     (Apple Silicon)
  │    └─ Coral      (USB TPU)
  │
  └─ RemoteDetector (HTTP API adapter)
       ├─ Blue Onyx       (HTTP API)
       ├─ CodeProject.AI  (HTTP API)
       ├─ Ollama          (HTTP API, for LLM-based)
       └─ Custom URL      (any compatible endpoint)
  │
  ▼
Detection results → Go Event Bus (CG8)
```

- **Model format**: ONNX (`.onnx`) as the universal format — one model file runs on any backend
  via ONNX Runtime execution providers (OpenVINO EP, TensorRT EP, CoreML EP, CPU EP)
- **Coral TPU**: auto-convert ONNX → TFLite at first startup
- **Shipped models**:
  - `general_security.onnx` — YOLOv8n (Person, Vehicle, Animal)
  - `package_delivery.onnx` — fine-tuned for packages at doorsteps
  - `face_recognition.onnx` — ArcFace or similar (R11, opt-in)
  - `audio_classify.onnx` — YAMNet (Glass Break, Dog Bark, Baby Cry) (R12)
- **External detectors**: any service with a compatible HTTP API (send frame → get bounding boxes)
  is selectable in the UI alongside built-in backends — no YAML
- **Crash isolation**: a detector crash never kills recording — frames queue and retry on restart

### CG11. Mobile Companion App

**Flutter (Dart) → iOS + Android from a single codebase**

```
mobile/
  lib/
    screens/
      live_view.dart         (camera grid, focus mode)
      timeline.dart          (24/7 scrubber, heatmap overlay)
      events.dart            (AI detections, snapshots)
      settings.dart          (camera config, zone editor)
    services/
      api_client.dart        (REST API consumer)
      webrtc_service.dart    (live streaming via go2rtc)
      push_service.dart      (FCM / APNs)
      auth_service.dart      (JWT login, biometric unlock)
    widgets/
      video_player.dart      (MSE / WebRTC adaptive player)
      timeline_bar.dart      (heatmap scrubber)
      zone_editor.dart       (draw exclusion zones on feed)
```

**Remote access: Cloud relay + direct P2P fallback**

```
1. App opens → authenticates with cloud relay
2. Relay brokers WebRTC connection to home NVR
3. If P2P possible → direct connection (lowest latency)
   If not → traffic flows through TURN relay (always works)
```

- **Pairing**: scan QR code from web UI — zero config, no port forwarding
- **Relay options**: hosted relay (free tier + paid) or self-hostable TURN server for power users

**Design language: Dark-first, minimal, camera-focused**

- Dark background (`#0D1117`) by default — OLED-friendly, saves battery
- Camera feeds are hero content (80%+ of screen real estate)
- Thin header, minimal navigation chrome
- Tap camera → fullscreen Focus Mode (R7)
- Accent: blue (`#58A6FF`) for detections, red (`#F85149`) for critical alerts only
- Bottom nav: Live | Events | Settings (3 tabs max)

---

## 1. Core Architecture & Performance

**Goal:** Fix Blue Iris's resource bloat and Frigate's hardware dependency.

### R1. Cross-Platform Native

- The core engine must be compiled for **Linux (Docker), Windows, and macOS**.
- Addresses: Blue Iris (Windows only) and Frigate (Linux/Docker only).

### R2. Hybrid Video Pipeline

- Default behavior is **Direct-to-Disk recording** (0% transcoding) for high-res streams.
- Detection logic runs on **low-res sub-streams**.
- **"Messy Stream Handling"**: If a camera has no sub-stream or a corrupted one, the
  software must transparently decode the main stream (via hardware acceleration) without
  crashing the pipeline.
- Addresses: Blue Iris (CPU hog) and Frigate (fails if sub-stream is bad).

### R3. Agnostic AI Acceleration

- AI inference must support **OpenVINO** (Intel iGPU), **TensorRT** (NVIDIA),
  **CoreML** (Apple Silicon), and **Coral TPU** indiscriminately.
- Addresses: Frigate (heavily biased toward Coral TPU) and Blue Iris (requires complex
  3rd-party install).

### R4. Watchdog & Auto-Repair

- A supervisor process must monitor the engine. If a crash occurs, it restarts the
  service and marks the timeline with a **"System Restart"** event.
- Database must be crash-resistant (**WAL mode**).
- Addresses: Blue Iris (database corruption) and Frigate (silent failures).

---

## 2. User Experience (UI/UX)

**Goal:** Fix Blue Iris's "Windows 95" look and Frigate's "YAML Hell."

### R5. Visual Configuration (No YAML)

- Every setting (resolution, masks, zones, triggers) must be adjustable via a
  **visual Web UI**.
- **"Visual Masking"**: Users draw exclusion zones directly on the live feed with a mouse.
- Addresses: Frigate (YAML config nightmares).

### R6. The "Fluid" Timeline

- A unified **24/7 scrubbable timeline**.
- **"Heatmap Overlay"**: The timeline bar shows density of motion (blue) or AI
  detections (red) so users can instantly see when things happened without clicking
  individual clips.
- Addresses: Frigate (clip-centric, hard to scrub 24/7) and Blue Iris (clunky list view).

### R7. Adaptive Dashboard

- The live view grid must support **"Focus Mode."** Clicking a camera expands it,
  while others pause or drop to 1 fps to save client bandwidth.
- Addresses: Blue Iris (resource-heavy client) and Frigate (static layouts).

---

## 3. Mobile & Remote Access

**Goal:** Fix Blue Iris's reliable access issues and Frigate's lack of a native app.

### R8. Native Mobile App (iOS/Android)

- A dedicated **Flutter (Dart)** app that mirrors the Web UI capabilities on both platforms.
- **Zero-Config P2P**: Cloud relay brokers the initial WebRTC connection; upgrades to direct
  P2P when possible. QR code pairing — no port forwarding, no VPN, no DDNS.
- **Self-hostable**: Power users can run their own TURN relay server.
- Addresses: Blue Iris (requires complex VPN/port forwarding) and Frigate (no official app).

### R9. Rich Notifications

- Notifications must include a **snapshot** and a **direct link** to the event clip.
- **"Critical Alerts"** support (bypass Do Not Disturb on iOS) for specific triggers
  (e.g., "Person" inside "House").
- Addresses: Blue Iris (plain text alerts often fail).

---

## 4. AI & Intelligence

**Goal:** Make it smarter than Blue Iris but easier than Frigate.

### R10. "One-Click" AI Models

- Pre-packaged models for **"General Security"** (Person/Vehicle), **"Package Delivery,"**
  and **"Animals."**
- No external Docker containers to manage (unlike CodeProject.AI).
- Addresses: Blue Iris (integration complexity).

### R11. Face Recognition (Opt-In)

- Native ability to label faces ("John," "Mom," "Unknown") and alert based on who is seen.
- Addresses: Frigate (requires heavy customization for facial rec).

### R12. Audio Intelligence

- Native classification for **Glass Break**, **Dog Bark**, and **Baby Cry**.
- Addresses: Frigate (weak audio support) and Blue Iris (basic decibel triggering).

---

## 5. Storage & Retention

**Goal:** Fix the "Disk Full" panic.

### R13. Tiered Storage Management

- Support for **"Hot" storage** (SSD for recent 24h) and **"Cold" storage** (HDD/NAS
  for archival). The system automatically moves clips.
- Addresses: Blue Iris (complex storage configuration).

### R14. Smart Retention Policies

- Example policy: *"Keep 24/7 for 3 days, but keep 'Person' events for 30 days."*
- Addresses: Frigate (confusing retention settings in YAML).

---

## 6. Migration Path (The "Trojan Horse")

**Goal:** Make switching painless.

### R15. Importer Tool

- A script that parses **Blue 
Iris `.reg` files** or **Frigate `config.yml` files** and
  auto-populates cameras, IPs, and passwords into Sentinel.
- Addresses: The pain of re-entering 10+ cameras manually.

---

## Build Roadmap

Each phase produces a testable milestone. Build in order — each phase depends on the last.

| Phase | Name               | Delivers                                       | Key CG/R    |
|-------|--------------------|-------------------------------------------------|-------------|
| 0     | Foundation         | Project structure, SQLite, config, Gin boots    | CG2, CG9    |
| 1     | Camera Pipeline    | go2rtc RTSP intake, camera management API       | CG3, R2     |
| 2     | Recording          | ffmpeg direct-to-disk, 10-min MP4 segments      | CG4, R2     |
| 3     | Live View          | MSE streaming to React UI, camera grid          | CG3, R7     |
| 4     | Playback           | Timeline API, video scrubbing, segment stitching | R6          |
| 5     | AI Detection       | Detector interface, ONNX inference, person/vehicle | CG10, R3, R10 |
| 6     | Events & Timeline  | Event bus, heatmap timeline, snapshot capture    | CG8, R6     |
| 7     | Auth & Security    | Login, JWT, OIDC, encrypted camera creds        | CG6         |
| 8     | Notifications      | FCM/APNs push, rich snapshots, critical alerts  | R9          |
| 9     | Visual Config      | Zone editor, mask drawing, settings UI          | R5          |
| 10    | Storage Management | Hot/cold tiers, retention policies, auto-cleanup | R13, R14    |
| 11    | Mobile App         | Flutter app, live view, events, push            | CG11, R8    |
| 12    | Remote Access      | Cloud relay, WebRTC P2P, QR pairing             | CG11, R8    |
| 13    | Advanced AI        | Face recognition, audio intelligence            | R11, R12    |
| 14    | Migration Tool     | Blue Iris / Frigate config importers            | R15         |

---

## Requirement Traceability

When implementing or reviewing code, reference requirements by their ID:

### Core Goals (CG)

| ID  | Short Name               | Summary                                      |
|-----|--------------------------|----------------------------------------------|
| CG1 | Performance              | Direct-to-disk, sub-stream detection, zero transcoding |
| CG2 | Modern Stack             | Go + React/TS + SQLite (modernc) + Gin + REST |
| CG3 | Video Pipeline           | go2rtc (live) + ffmpeg (recording), MSE default |
| CG4 | Recording Format         | 10-minute MP4 segments, directory-per-day     |
| CG5 | Deployment               | Docker-primary, native binary available        |
| CG6 | Secure by Default        | Local auth + JWT, optional OIDC, encrypted creds |
| CG7 | API-First                | REST API first, UI is a consumer               |
| CG8 | Event Architecture       | Go channels + SQLite write-ahead               |
| CG9 | Crash Resilience         | Per-camera isolation, WAL DB, watchdog         |
| CG10 | AI Detection Pipeline   | Dedicated processes + external API (Blue Onyx, etc.), ONNX models |
| CG11 | Mobile Companion App    | Flutter (Dart), cloud relay + P2P, dark-first design |

### Feature Requirements (R)

| ID  | Short Name               | Section                      |
|-----|--------------------------|------------------------------|
| R1  | Cross-Platform Native    | Core Architecture            |
| R2  | Hybrid Video Pipeline    | Core Architecture            |
| R3  | Agnostic AI Acceleration | Core Architecture            |
| R4  | Watchdog & Auto-Repair   | Core Architecture            |
| R5  | Visual Config (No YAML)  | UI/UX                        |
| R6  | Fluid Timeline           | UI/UX                        |
| R7  | Adaptive Dashboard       | UI/UX                        |
| R8  | Native Mobile App        | Mobile & Remote Access       |
| R9  | Rich Notifications       | Mobile & Remote Access       |
| R10 | One-Click AI Models      | AI & Intelligence            |
| R11 | Face Recognition         | AI & Intelligence            |
| R12 | Audio Intelligence       | AI & Intelligence            |
| R13 | Tiered Storage           | Storage & Retention          |
| R14 | Smart Retention Policies | Storage & Retention          |
| R15 | Importer Tool            | Migration Path               |
