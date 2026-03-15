# Sentinel NVR -- Getting Started Guide

## 1. Requirements

**Hardware**

- CPU: x86_64 or ARM64, 2+ cores recommended (4+ cores for AI detection)
- RAM: 4 GB minimum, 8 GB recommended
- Storage: SSD for hot storage (recent recordings), HDD/NAS for cold archival
- GPU (optional): Intel iGPU (VAAPI/QSV), NVIDIA (CUDA), or Coral USB TPU for accelerated AI

**Software**

- Docker Engine 24+ and Docker Compose v2
- Git (for cloning the repository)

**Supported Platforms**

- Linux (primary target -- Ubuntu 22.04+, Debian 12+)
- macOS (Apple Silicon and Intel)
- Windows (WSL2 with Docker Desktop)

**Network**

- Cameras must be reachable from the machine running Sentinel NVR
- Port 8099 (backend API) and 5173 (web UI) must be available
- Port 8555 (RTSP re-stream) and 8556/udp (WebRTC) if using remote access

---

## 2. Quick Start (Docker)

### Clone the repository

```bash
git clone https://github.com/LstDtchMn/Sentinel-NVR.git
cd Sentinel-NVR
```

### Prepare configuration

```bash
cp configs/sentinel.yml configs/sentinel.yml.backup  # optional safety copy
```

Edit `configs/sentinel.yml` to set your timezone and storage paths. The defaults work out of the box for Docker.

### Create storage directories

On Linux, create the media directories and set ownership to the sentinel container user (UID 10001):

```bash
sudo mkdir -p media/hot media/cold
sudo chown -R 10001:10001 media/
```

On macOS/Windows (Docker Desktop), the directories are created automatically.

### Start the stack

```bash
docker compose up -d
```

This starts three services:

| Service     | Port  | Description                        |
|-------------|-------|------------------------------------|
| sentinel    | 8099  | Backend API                        |
| go2rtc      | 1984  | Stream broker (localhost only)     |
| frontend    | 5173  | Web UI (Vite dev server)           |

### Verify everything is running

```bash
docker compose ps
```

All three services should show `healthy` status within 30 seconds.

### Open the web UI

Navigate to **http://localhost:5173** in your browser.

### Create your admin account

On first launch, you will be redirected to the **Setup** page. Enter a username and password (minimum 8 characters, maximum 72). This creates the admin account and logs you in automatically.

---

## 3. Adding Cameras

There are two ways to add cameras: ONVIF auto-discovery or manual RTSP entry.

### Option A: ONVIF Discovery

1. Navigate to **Cameras** in the sidebar
2. Click **Discover Cameras**
3. Sentinel sends a multicast probe on your LAN. Discovered cameras appear in a list with their IP, manufacturer, and model
4. Click a discovered camera, enter ONVIF credentials, and click **Probe** to retrieve stream profiles
5. Select a profile and click **Add**

> Note: ONVIF discovery uses multicast, which does not work inside Docker bridge networking. Use the **Probe by IP** option instead -- enter the camera IP directly.

### Option B: Manual RTSP

1. Navigate to **Cameras** > **Add Camera**
2. Fill in:
   - **Name**: A descriptive label (alphanumeric, spaces, dashes, underscores; 1-64 characters)
   - **Main Stream**: The high-resolution RTSP URL
   - **Sub Stream** (optional): A lower-resolution stream for AI detection and snapshots
   - **Record**: Enable 24/7 recording (default: on)
   - **Detect**: Enable AI detection (requires detection backend)
3. Click **Test Stream** to verify connectivity before saving
4. Click **Save**

### Common RTSP URL formats

| Brand     | Main Stream URL                                                     |
|-----------|---------------------------------------------------------------------|
| Hikvision | `rtsp://user:pass@192.168.1.x:554/Streaming/Channels/101`          |
| Reolink   | `rtsp://user:pass@192.168.1.x:554/h264Preview_01_main`             |
| Amcrest   | `rtsp://user:pass@192.168.1.x:554/cam/realmonitor?channel=1&subtype=0` |
| Dahua     | `rtsp://user:pass@192.168.1.x:554/cam/realmonitor?channel=1&subtype=0` |
| Generic   | `rtsp://user:pass@192.168.1.x:554/stream1`                         |

For sub-streams, replace `main` with `sub`, `101` with `102`, or `subtype=0` with `subtype=1` depending on the brand.

You can also use HTTP/HTTPS MJPEG streams:

```
http://user:pass@192.168.1.x/cgi-bin/mjpg/video.cgi
```

---

## 4. Live View

The **Live View** page is the default landing page. It displays all enabled cameras in a responsive grid.

- **Grid layout**: Cameras are arranged automatically based on screen size
- **Focus Mode**: Click any camera tile to expand it full-screen. Press **Escape** or click outside to return to the grid
- **Status indicators**: Each tile shows connection status (green = streaming, red = error, gray = disabled)
- **Streams**: Live video is delivered via MSE (Media Source Extensions) over WebSocket -- low latency, no plugins required

When you focus on one camera, the other camera WebSocket connections are paused to save bandwidth.

---

## 5. Playback

The **Playback** page lets you review recorded footage.

1. **Select a camera** from the dropdown
2. **Pick a date** -- dates with recordings are highlighted in the calendar
3. **Timeline**: A horizontal bar shows recording coverage for the selected day. Blue segments indicate recorded footage; gaps are periods with no recording
4. **Heatmap overlay**: Orange/red bands on the timeline show detection event density
5. **Scrubbing**: Click anywhere on the timeline to jump to that time. Drag to scrub
6. **Zoom**: Use the mouse wheel on the timeline to zoom in/out (4 zoom levels: 24h, 6h, 1h, 15min)
7. **Playback speed**: Adjust speed with the 0.5x / 1x / 2x / 4x / 8x controls
8. **Download**: Click the download button to save the current segment as an MP4 file

Recordings are stored as 10-minute MP4 segments. The player automatically stitches consecutive segments for seamless playback.

---

## 6. Events and Detection

### Enabling detection

1. Set up a detection backend. Sentinel supports remote HTTP backends:
   - [CodeProject.AI](https://www.codeproject.com/ai/docs/) -- recommended for beginners
   - [Blue Onyx](https://github.com/enzo1982/blue-onyx)
   - Any OpenAI-compatible vision API
2. In `sentinel.yml`, enable detection:
   ```yaml
   detection:
     enabled: true
     backend: "remote"
     remote_url: "http://your-detection-server:32168"
     frame_interval: 5      # seconds between frame grabs
     confidence_threshold: 0.6
   ```
3. On each camera, toggle **Detect** to `true`
4. Restart Sentinel: `docker compose restart sentinel`

### Viewing events

- Navigate to **Events** in the sidebar
- Events appear in reverse chronological order with thumbnails
- **Filters**: Filter by camera, event type (detection, face_match, audio_detection), date, and minimum confidence
- **Live updates**: New events appear in real-time via Server-Sent Events (no page refresh needed)
- **Event detail**: Click an event to see the full snapshot, bounding boxes, confidence scores, and associated recording clip

### Event types

| Type              | Description                                      |
|-------------------|--------------------------------------------------|
| detection         | Object detected (person, car, animal, etc.)      |
| face_match        | Known face recognized (requires face enrollment) |
| audio_detection   | Audio event classified (gunshot, glass break)    |
| camera.offline    | Camera stream lost                               |
| camera.online     | Camera stream restored                           |

---

## 7. Notifications

### Enable notifications in config

```yaml
notifications:
  enabled: true
  retry_interval: 60
```

Restart after changing: `docker compose restart sentinel`

### Add a webhook channel

1. Navigate to **Notifications** in the sidebar
2. Click **Add Token**
3. Select provider: **webhook**
4. Enter your webhook URL (must be a public HTTPS endpoint -- private/loopback addresses are blocked for security)
5. Click **Save**

### Configure alert rules (preferences)

1. On the Notifications page, click **Add Rule**
2. Select the event type you want alerts for (e.g., `detection`, `face_match`, or `*` for all)
3. Optionally scope to a specific camera
4. Enable **Critical** to bypass iOS Do Not Disturb (APNs only)

### Test your setup

Click the **Test** button next to a registered token. You should receive a test notification within seconds.

### Mobile push (FCM / APNs)

For push notifications to the mobile app:

- **FCM (Android)**: Set `fcm.service_account_json` to the path of your Firebase service account key file
- **APNs (iOS)**: Set `apns.key_path`, `apns.key_id`, `apns.team_id`, and `apns.bundle_id`

---

## 8. Mobile App

The Sentinel NVR mobile companion app (Flutter) is available for iOS and Android.

### Pairing with QR code

1. In the web UI, navigate to **Settings**
2. Click **Generate Pairing QR Code** (admin only)
3. A QR code appears with a 15-minute expiration
4. Open the mobile app and tap **Scan QR Code**
5. The app scans the code, authenticates, and connects to your NVR

### Mobile features

- **Live View**: WebRTC streaming with STUN/TURN relay for remote access
- **Events**: Browse and filter detection events with thumbnails
- **Timeline**: Scrub through recordings with the same timeline interface
- **Push notifications**: Tap a notification to jump directly to the event
- **Biometric unlock**: Face ID / fingerprint authentication

### Remote access (TURN relay)

If you need to access the NVR from outside your LAN:

1. Start the coturn relay: `docker compose --profile relay up -d`
2. In `sentinel.yml`, enable the relay:
   ```yaml
   relay:
     enabled: true
     stun_server: "stun:stun.l.google.com:19302"
     turn_server: "turn:your-server:3478"
     turn_user: "sentinel"
     turn_pass: "your-secure-password"
   ```
3. Match the credentials in `docker-compose.yml` under the coturn service
4. Restart Sentinel

---

## 9. Storage

Sentinel uses a two-tier storage model.

### Hot storage (SSD)

- Fast storage for recent recordings
- Default path: `/media/hot` (mapped to `./media/hot` on host)
- Default retention: 3 days

### Cold storage (HDD/NAS)

- Archival storage for older recordings
- Default path: `/media/cold` (mapped to `./media/cold` on host)
- Default retention: 30 days
- Optional -- leave `cold_path` empty to disable

### How it works

1. New recordings are written to hot storage
2. After `hot_retention_days`, segments are automatically migrated to cold storage
3. After `cold_retention_days`, segments are permanently deleted
4. The migration runs continuously in the background

### Retention rules

For fine-grained control, create per-camera and per-event-type retention rules:

1. Navigate to **Settings**
2. Under **Retention Rules**, click **Add Rule**
3. Select a camera (or leave blank for all cameras)
4. Select an event type (or leave blank for all types)
5. Set the retention period in days

These rules override the global `hot_retention_days` / `cold_retention_days` settings.

### Storage stats

The **Dashboard** page shows storage usage per tier:
- Used bytes and segment counts for hot and cold storage
- Per-camera breakdown

---

## 10. Zone Editor

Zones define areas within a camera's field of view where detection should (or should not) trigger events.

### Drawing a zone

1. Navigate to **Cameras**, then click **Edit Zones** on a camera
2. The zone editor opens with a live snapshot from the camera as the background
3. Click to place polygon vertices (minimum 3 points)
4. To close the polygon: click near the first vertex (when you have 3+ points) or double-click
5. Name the zone and choose its type:
   - **Include**: Only detections inside this zone trigger events
   - **Exclude**: Detections inside this zone are ignored (useful for trees, roads)
6. Click **Save**

### Tips

- The snapshot auto-refreshes every 10 seconds so you can see the current camera view
- Press **Escape** or right-click to cancel the current polygon
- You can create multiple zones per camera
- Zone coordinates are normalized (0.0 to 1.0) so they work at any resolution

---

## 11. Settings

The **Settings** page lets you adjust system configuration without editing YAML files.

| Setting             | Description                                  | Restart required? |
|---------------------|----------------------------------------------|-------------------|
| Log Level           | debug, info, warn, error                     | No                |
| Hot Retention Days  | Days to keep recordings on fast storage      | No                |
| Cold Retention Days | Days to keep recordings on archival storage  | No                |
| Segment Duration    | Recording segment length in minutes          | No                |

Changes are applied immediately and persisted to `sentinel.yml`.

Storage paths, database path, and auth settings require editing `sentinel.yml` and restarting the container.

---

## 12. Production Deployment

For production, use the nginx-based frontend instead of the Vite dev server.

### Start with production compose

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

This builds the frontend as static files served by nginx on port 5173 (mapped to container port 80). Nginx handles:

- Gzip compression for all text assets
- Aggressive caching for Vite-hashed static assets (1 year)
- SPA fallback routing (`try_files` to `index.html`)
- API proxy to the backend (`/api/` forwarded to `sentinel:8099`)
- WebSocket upgrade for live streaming and SSE

### HTTPS

Sentinel does not include a built-in TLS terminator. For HTTPS:

1. Place a reverse proxy (Caddy, Traefik, or nginx) in front of the stack
2. Set `auth.secure_cookie: true` in `sentinel.yml` so auth cookies include the `Secure` flag
3. Update `auth.allowed_origins` to include your HTTPS domain

### Environment variables

| Variable                  | Description                          |
|---------------------------|--------------------------------------|
| `TZ`                      | Container timezone (e.g., `America/New_York`) |
| `SENTINEL_ADMIN_PASSWORD` | Override admin password on startup   |
| `GO2RTC_API`              | Override go2rtc API URL              |
| `GO2RTC_RTSP`             | Override go2rtc RTSP URL             |

---

## 13. Troubleshooting

### Camera stream not connecting

1. **Test the RTSP URL directly**: Use VLC or ffplay to verify the URL works outside Sentinel
   ```bash
   ffplay "rtsp://user:pass@192.168.1.100:554/stream1"
   ```
2. **Check go2rtc health**: Open `http://localhost:1984` (only accessible from the host machine)
3. **Check logs**: `docker compose logs sentinel | grep -i "camera\|stream\|error"`
4. **Use Test Stream**: In the web UI camera form, click **Test Stream** to verify connectivity through go2rtc

### Notifications not working

1. Verify `notifications.enabled: true` in `sentinel.yml`
2. Check the notification log at **Notifications** > **Delivery Log**
3. For webhooks, verify the URL is publicly reachable (private/loopback IPs are blocked)
4. For FCM, verify the service account JSON file is mounted into the container
5. Send a test notification from the web UI to isolate the issue

### Docker bind mount permissions (Linux)

If you see permission errors in logs:

```bash
# The sentinel container runs as UID 10001
sudo chown -R 10001:10001 media/
# Also ensure the config is readable
sudo chown 10001:10001 configs/sentinel.yml
```

If you previously ran with a different UID, remove the Docker volumes and rebuild:

```bash
docker compose down -v
docker compose up -d
```

### Database locked errors

Sentinel uses SQLite in WAL mode for concurrent reads. If you see "database is locked":

1. Ensure only one instance of Sentinel is running
2. Check that the data volume is not mounted by another container
3. Verify the filesystem supports file locking (some NFS mounts do not)

### High CPU usage

- Reduce `detection.frame_interval` (e.g., from 5 to 10 seconds)
- Use sub-streams for detection (lower resolution)
- If using a remote detection backend, check its resource usage separately

### ONVIF discovery returns empty results

This is expected in Docker bridge networking. Use the **Probe by IP** feature instead:
1. Enter the camera's IP address directly
2. Provide ONVIF credentials
3. Click **Probe** to retrieve stream profiles

### Resetting the admin password

If you forget the admin password:

```bash
docker compose exec sentinel ./sentinel -reset-password admin
```

Or set the `SENTINEL_ADMIN_PASSWORD` environment variable and restart the container.
