# Sentinel NVR — Deployment Guide

## Architecture

```
                     ┌─────────────────┐
                     │   Reverse Proxy  │  (nginx/Caddy — TLS termination)
                     │   :443 → :8099  │
                     └────────┬────────┘
                              │
              ┌───────────────┼───────────────┐
              │               │               │
     ┌────────▼──────┐ ┌─────▼──────┐ ┌──────▼──────┐
     │   Sentinel    │ │   go2rtc   │ │   coturn    │
     │   :8099       │ │   :1984    │ │   :3478     │
     │   (Go/Gin)    │ │   (RTSP/   │ │   (TURN     │
     │               │ │    MSE/    │ │    relay)   │
     │   SQLite WAL  │ │    WebRTC) │ │   optional  │
     └───────────────┘ └────────────┘ └─────────────┘
              │               │
     ┌────────▼──────┐ ┌─────▼──────┐
     │   /media/hot  │ │  IP Cameras │
     │   /media/cold │ │  RTSP feeds │
     │   /data/      │ └────────────┘
     └───────────────┘
```

**Services:**
- **Sentinel** — Core NVR: API server, recording, detection, notifications
- **go2rtc** — Stream broker: RTSP re-stream, MSE/WebRTC live viewing
- **coturn** — TURN relay for remote mobile access (optional, `--profile relay`)

---

## Quick Start

### Production

```bash
# 1. Create media directories
mkdir -p media/hot media/cold configs

# 2. Copy sample config
cp configs/sentinel.yml.example configs/sentinel.yml

# 3. Set admin password
export SENTINEL_ADMIN_PASSWORD="your-secure-password"

# 4. Start
docker compose up -d

# 5. Access
#    http://localhost:8099       Sentinel API + Web UI
#    http://127.0.0.1:1984      go2rtc WebUI (localhost only)
```

### Development

```bash
docker compose --profile dev up --build -d

# Includes:
#   - rtsp-test (synthetic RTSP stream at rtsp://localhost:8554)
#   - Frontend dev server at http://localhost:5173
```

### With TURN Relay (Remote Mobile Access)

```bash
docker compose --profile relay up -d

# Then enable in configs/sentinel.yml:
#   relay:
#     enabled: true
#     turn_pass: "your-turn-password"  # MUST match coturn --user flag
```

---

## Configuration

### sentinel.yml

```yaml
server:
  host: "0.0.0.0"
  port: 8099
  log_level: "info"                  # debug | info | warn | error

auth:
  enabled: true                      # false = no authentication (LAN only)
  access_token_ttl: 900              # 15 minutes
  refresh_token_ttl: 604800          # 7 days
  secure_cookie: false               # true when behind HTTPS reverse proxy
  allowed_origins:
    - "https://nvr.example.com"      # CORS origin for cookie auth

database:
  path: "/data/sentinel.db"
  wal_mode: true

storage:
  hot_path: "/media/hot"             # SSD — recent recordings
  cold_path: "/media/cold"           # HDD/NAS — archival (optional)
  hot_retention_days: 3
  cold_retention_days: 30
  segment_duration: 10               # minutes per MP4 segment
  migration_interval_hours: 1        # hot → cold migration cadence
  cleanup_interval_hours: 6          # expired recording cleanup cadence

detection:
  enabled: false
  backend: "remote"                  # remote | onnx
  remote_url: "http://codeproject-ai:32168"
  frame_interval: 5                  # seconds between frame grabs
  confidence_threshold: 0.6
  snapshot_path: "/data/snapshots"
  face_recognition:
    enabled: false
    match_threshold: 0.6
  audio_classification:
    enabled: false
    confidence_threshold: 0.7

notifications:
  enabled: false
  fcm:
    service_account_json: ""         # path to Google service account JSON
  apns:
    key_path: ""                     # path to .p8 Apple auth key
    key_id: ""
    team_id: ""
    bundle_id: ""
    sandbox: false

go2rtc:
  api_url: "http://go2rtc:1984"     # overridable with GO2RTC_API env var
  rtsp_url: "rtsp://go2rtc:8554"    # overridable with GO2RTC_RTSP env var

relay:
  enabled: false
  stun_server: "stun:stun.l.google.com:19302"
  turn_server: "turn:coturn:3478"
  turn_user: "sentinel"
  turn_pass: "changeme"              # MUST match coturn credentials

watchdog:
  enabled: true
  health_interval: 30
  restart_delay: 5

cameras: []                          # Seed cameras on first run only
  # - name: "Front Door"
  #   enabled: true
  #   main_stream: "rtsp://user:pass@192.168.1.100:554/stream1"
  #   sub_stream: "rtsp://user:pass@192.168.1.100:554/stream2"
  #   record: true
  #   detect: false
```

### go2rtc.yaml

```yaml
api:
  listen: ":1984"

rtsp:
  listen: ":8554"

webrtc:
  listen: ":8555"
  candidates:
    - "stun:8555"
  ice_servers:
    - urls: ["stun:stun.l.google.com:19302"]

log:
  level: info
```

> Do NOT add camera streams to go2rtc.yaml. Sentinel manages streams dynamically via the go2rtc REST API.

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SENTINEL_ADMIN_PASSWORD` | (auto-generated) | Initial admin password. If unset, a random 16-char password is logged to stderr on first run |
| `GO2RTC_API` | `http://go2rtc:1984` | Override go2rtc API URL |
| `GO2RTC_RTSP` | `rtsp://go2rtc:8554` | Override go2rtc RTSP URL |
| `TZ` | `America/New_York` | Container timezone |
| `NVIDIA_VISIBLE_DEVICES` | — | Set to `all` for NVIDIA GPU passthrough |
| `NVIDIA_DRIVER_CAPABILITIES` | — | Set to `compute,video,utility` for GPU |

---

## Volumes & Storage

| Mount | Purpose | Notes |
|-------|---------|-------|
| `sentinel-data:/data` | SQLite DB, snapshots, AI models | Named volume — survives `docker compose down` |
| `./media/hot:/media/hot` | Hot storage (recent recordings) | SSD recommended |
| `./media/cold:/media/cold` | Cold storage (archival) | HDD/NAS, optional |
| `./configs/sentinel.yml:/etc/sentinel/sentinel.yml:ro` | Configuration | Read-only |
| `./configs/go2rtc.yaml:/config/go2rtc.yaml:ro` | go2rtc config | Read-only |

### Linux Permissions

Sentinel runs as UID/GID 10001 inside the container:

```bash
sudo mkdir -p media/hot media/cold
sudo chown -R 10001:10001 media/
```

### Database Backups

SQLite WAL mode creates three files — back up all of them:
```
/data/sentinel.db
/data/sentinel.db-wal
/data/sentinel.db-shm
```

Best practice: stop the container, copy all three files, restart.
For hot backups, use SQLite's `.backup` command or `VACUUM INTO`.

---

## Port Reference

| Port | Service | Protocol | Exposure |
|------|---------|----------|----------|
| 8099 | Sentinel API | TCP | Public (behind reverse proxy) |
| 1984 | go2rtc API/WebUI | TCP | localhost only (`127.0.0.1`) |
| 8555 | go2rtc RTSP re-stream | TCP | LAN (camera re-stream) |
| 8556 | go2rtc WebRTC | UDP | Public (NAT traversal) |
| 3478 | coturn STUN/TURN | TCP+UDP | Public (relay, optional) |
| 49152–49200 | coturn relay range | UDP | Public (relay, optional) |

---

## HTTPS / Reverse Proxy

Sentinel does not handle TLS directly. Use a reverse proxy for production.

### nginx Example

```nginx
server {
    listen 443 ssl http2;
    server_name nvr.example.com;

    ssl_certificate     /etc/letsencrypt/live/nvr.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/nvr.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8099;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support (live streaming + SSE)
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400s;

        # Large file streaming (recordings)
        proxy_buffering off;
        proxy_request_buffering off;
        client_max_body_size 10m;
    }
}
```

### Caddy Example

```
nvr.example.com {
    reverse_proxy localhost:8099 {
        flush_interval -1
    }
}
```

After enabling HTTPS, set in `sentinel.yml`:
```yaml
auth:
  secure_cookie: true
  allowed_origins:
    - "https://nvr.example.com"
```

---

## GPU Acceleration

### Intel VAAPI/QSV

```yaml
# docker-compose.yml — sentinel service
devices:
  - /dev/dri:/dev/dri

# sentinel.yml
detection:
  gpu_device: "auto"
```

### NVIDIA CUDA

```yaml
# docker-compose.yml — sentinel service
deploy:
  resources:
    reservations:
      devices:
        - driver: nvidia
          count: 1
          capabilities: [gpu]

environment:
  - NVIDIA_VISIBLE_DEVICES=all
  - NVIDIA_DRIVER_CAPABILITIES=compute,video,utility
```

Requires `nvidia-container-toolkit` on the host.

### Coral USB TPU

```yaml
# docker-compose.yml — sentinel service
devices:
  - /dev/bus/usb:/dev/bus/usb
```

---

## Resource Limits

Default limits in docker-compose.yml:

| Service | Memory | Log rotation |
|---------|--------|-------------|
| Sentinel | 2 GB | 50 MB x 5 files |
| go2rtc | 1 GB | 50 MB x 5 files |
| coturn | — | 10 MB x 3 files |

Adjust based on camera count:
- **1–4 cameras**: 2 GB sentinel, 1 GB go2rtc (defaults)
- **5–16 cameras**: 4 GB sentinel, 2 GB go2rtc
- **16+ cameras**: 8 GB sentinel, 4 GB go2rtc

---

## Security Checklist

- [ ] Set `auth.enabled: true`
- [ ] Set `auth.secure_cookie: true` (when behind HTTPS)
- [ ] Set `auth.allowed_origins` to your domain(s)
- [ ] Change `SENTINEL_ADMIN_PASSWORD` from default
- [ ] Change `relay.turn_pass` from `"changeme"` (if using TURN)
- [ ] Put Sentinel behind a reverse proxy with TLS
- [ ] Ensure go2rtc API (port 1984) is not exposed to the internet
- [ ] Set firewall rules: only expose 443 (HTTPS), 3478 (TURN), 8556 (WebRTC)
- [ ] Use separate VLANs for cameras vs. user traffic if possible
- [ ] Back up `/data/sentinel.db` regularly

### Camera Credential Security

- Camera passwords are encrypted with AES-256-GCM in the database
- The encryption key is stored in the `system_settings` table (auto-generated on first run)
- ONVIF passwords are never included in API responses (`json:"-"`)
- RTSP URLs with credentials are redacted in logs

---

## Troubleshooting

### Container won't start

```bash
# Check logs
docker compose logs sentinel
docker compose logs go2rtc

# Verify go2rtc is healthy (sentinel depends on it)
docker compose ps
```

### Cameras not connecting

```bash
# Check go2rtc stream status
curl http://127.0.0.1:1984/api/streams

# Test RTSP directly
ffprobe rtsp://user:pass@camera-ip:554/stream
```

### Permission denied on media directories

```bash
# Fix ownership (Linux)
sudo chown -R 10001:10001 media/

# Verify
ls -la media/
```

### Database locked errors

- Ensure only one Sentinel instance accesses the DB
- WAL mode allows concurrent reads but only one writer
- If the container was killed, `.db-wal` and `.db-shm` files may need recovery — restart the container and SQLite auto-recovers

### Admin password lost

```bash
# Reset via CLI
docker compose exec sentinel sentinel -reset-password admin

# Or set via environment variable
export SENTINEL_ADMIN_PASSWORD="newpassword"
docker compose restart sentinel
```

### WebRTC not working (remote access)

1. Verify coturn is running: `docker compose --profile relay ps`
2. Check TURN credentials match between `sentinel.yml` and coturn command
3. Verify firewall allows UDP 3478 and 49152–49200
4. For Docker, ensure `--external-ip` is set to your public IP

---

## Clean Rebuild

```bash
# WARNING: This deletes all data (recordings, DB, models)
docker compose --profile dev down -v
docker compose --profile dev build --no-cache
docker compose --profile dev up -d
```

To rebuild without data loss:
```bash
docker compose down
docker compose build --no-cache
docker compose up -d
```

---

## Native Binary (No Docker)

```bash
# Build
cd backend && go build -o sentinel ./cmd/sentinel

# Run (requires go2rtc running separately)
./sentinel -config /path/to/sentinel.yml
```

Ensure go2rtc is running and accessible at the configured `go2rtc.api_url`.

---

## Startup & Shutdown Order

**Startup:**
1. Config load + validation
2. SQLite DB open (auto-runs migrations)
3. Auth service (generates JWT secret + credential key on first run)
4. Camera repository (seeds from `cameras:` config on first run only)
5. go2rtc health check (30s timeout)
6. Detection + notification services (if enabled)
7. Camera pipelines start (connects to go2rtc, starts recording/detection)
8. HTTP server starts

**Graceful Shutdown** (30s timeout):
1. Stop accepting new HTTP requests
2. Close event bus (unblocks SSE handlers)
3. Drain event persister (flush to DB)
4. Stop notification service
5. Graceful HTTP shutdown (drain in-flight requests)
6. Stop camera pipelines
7. Stop storage manager
8. Close database
