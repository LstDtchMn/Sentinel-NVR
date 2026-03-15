# Veteran NVR User Review: One Month with Sentinel NVR v0.1.0

**Reviewer background:** 3 years Blue Iris, 6 months Frigate, dabbled with Shinobi, admired UniFi Protect at a friend's house. 12 cameras (mix of Amcrest, Reolink, Dahua), Coral USB TPU, Intel NUC i7 with Intel UHD 630 iGPU, 1TB NVMe hot storage + 8TB NAS cold. Helped 6 non-technical friends set up camera systems.

**Test setup:** Sentinel v0.1.0 running in Docker Compose alongside Blue Iris on the same NUC. Both systems recording the same 12 cameras simultaneously for 30 days. CodeProject.AI running as the detection backend for both.

---

## Part 1: First Impressions (Day 1-3)

### Installation

I went Docker Compose. Pulled the images, copied the example `sentinel.yml`, and had the containers booting within about 10 minutes. That is genuinely fast -- faster than my first Blue Iris install (which took an hour of Windows firewall wrangling) and comparable to Frigate's Docker setup.

The `sentinel.yml` config is clean. Storage paths, go2rtc URL, detection backend URL -- it is obvious what goes where. The YAML structure is shallow and well-commented. This is a massive improvement over Frigate's YAML, which requires you to understand the entire detection pipeline before you can type a single line. Sentinel's approach of "configure the basics in YAML, do the rest in the UI" is exactly right.

First camera: I was pleasantly surprised to find that ONVIF auto-discovery actually exists. There is a `POST /api/v1/cameras/discover` endpoint, and the web UI has camera add/edit forms. I ran the discovery, it found 9 of my 12 cameras (the three it missed were older Amcrest units with spotty ONVIF implementations). For those three, I manually entered the RTSP URLs through the web UI's camera creation form. Camera #1 was streaming in about 2 minutes. Camera #12 took about 15 minutes total -- mostly me looking up RTSP paths for the Amcrest oddballs.

The go2rtc sidecar started automatically via Docker Compose. Streams appeared in go2rtc's admin page. The camera pipeline code (`camera/pipeline.go`) monitors go2rtc stream health and manages ffmpeg recording automatically -- I did not have to configure go2rtc separately at all. This is how it should work.

### First Live View

Time from clicking "Live View" to seeing video: approximately 1.5 seconds on the local network. The MSE WebSocket player (`VideoPlayer.tsx`) connects to go2rtc via a backend WebSocket proxy, negotiates codecs (H.264/H.265/AAC), and starts rendering fragmented MP4 frames. The implementation is solid -- it probes for codec support, handles reconnection with exponential backoff, and has a 30-second "unavailable" timeout before giving up.

The grid with all 12 cameras: performance was acceptable. The grid auto-layouts based on camera count (the `getGridClass` function switches between 1, 2, 3, and 4-column layouts). With 12 cameras, I got a 4-column grid on my 1440p monitor. CPU on the NUC spiked to about 35% rendering all 12 MSE streams in Chrome. No dropped frames that I could see, but there was occasional stuttering on two cameras that have high-bitrate 4K streams. Not a dealbreaker.

Focus Mode -- click a camera to expand it. Transition is instant (React state change, no animation delay). The code sets `focusedCamera` state and conditionally renders either the grid or the focused `FocusedTile` component. Click another camera from the grid means exiting focus first, then clicking the new one. There is no "click another tile while focused to switch" -- you have to exit focus mode first (Escape key or the Exit Focus button), then click the new camera. This is mildly annoying but functional.

Latency: I walked in front of my driveway camera and timed the delay on screen. Approximately 1.0-1.5 seconds with MSE. This is comparable to Blue Iris's web UI (1-2 seconds) and better than Frigate's JSMPEG player (2-3 seconds). UniFi Protect at my friend's house felt like 0.5 seconds, but they have purpose-built hardware and a native app, so that is not a fair comparison. The WebRTC option (available in the Flutter mobile app) would presumably be lower latency, but the web UI uses MSE only.

### First Recording Playback

The timeline is genuinely impressive. The `Playback.tsx` page has a camera selector, date picker with prev/next day navigation, and a 24-hour timeline bar with 4 zoom levels (24h, 6h, 2h, 30min). The heatmap overlay actually works -- it fetches detection density from `GET /api/v1/events/heatmap` and renders colored bars on the timeline showing where events clustered.

Scrubbing to 2 hours ago: I selected the camera, clicked the timeline at the approximate position. The `handleTimelineSeek` function finds the correct 10-minute segment, calculates the offset within that segment, and seeks the HTML5 video player. It worked. The seek was not instant -- there is a brief loading pause as the browser fetches the MP4 segment from the server, but once loaded, seeking within the segment is smooth.

The heatmap showed me exactly where motion events happened. At 3 AM there was a cluster of red dots -- turned out to be a cat in the driveway. I could see the density pattern without scrubbing through hours of nothing. Blue Iris has nothing like this. Frigate has no timeline at all -- just individual clips. This is Sentinel's killer feature right now.

Playback quality: excellent. The segments are direct copies from the camera (zero transcoding, `"-c", "copy"` in the ffmpeg args), so quality is identical to the source. Playback speed controls work (1x/2x/4x/8x via the `playbackRate` state). Auto-advance between segments works -- when one 10-minute segment ends, it automatically loads the next one.

Exporting a 30-second clip: I could not find a way to do this. There is no clip export feature in the UI. The `GET /api/v1/recordings/:id/play` endpoint serves full 10-minute segments for browser playback, and `DELETE /api/v1/recordings/:id` deletes them, but there is no endpoint or UI to extract a sub-clip. I had to manually download the full 10-minute MP4 and trim it in VLC. This is a significant gap.

### First Detection Event

Connected to CodeProject.AI: 3 steps. Set `detection.enabled: true` and `detection.backend: remote` and `detection.remote_url: http://codeproject-ai:32168` in `sentinel.yml`. Restarted the container. Detection started on the next frame grab.

Someone walked past the front door camera. Sentinel detected them. The detection pipeline (`detection/pipeline.go`) grabs a JPEG frame from go2rtc every N seconds (configurable `frame_interval`), sends it to CodeProject.AI via the `RemoteDetector`, filters results by confidence threshold and zone polygons, saves a snapshot, and publishes a detection event to the event bus. The event appeared in the Events page within about 3 seconds of the person entering the frame.

Detection latency deserves specific numbers: the frame interval is the dominant factor. At the default 5-second interval, worst case is nearly 5 seconds before the frame is even grabbed, plus about 200-400ms for CodeProject.AI inference, plus about 100ms for event bus propagation and SSE delivery to the browser. Average detection latency from a person entering the frame to seeing the event in the UI: approximately 3-4 seconds. This is noticeably slower than Frigate (under 500ms with Coral TPU, because Frigate processes every frame from the sub-stream). It is comparable to Blue Iris with CodeProject.AI.

Bounding box accuracy was good -- CodeProject.AI's model handles person detection well. But there is no object tracking across frames. Each frame is processed independently. Sentinel detects "person present in frame" but does not track the person's movement or maintain object identity between frames.

False positives in the first 24 hours: 8 total. Mostly from tree shadows on the driveway camera and headlight reflections on the garage camera. Once I set up exclusion zones using the visual zone editor (more on that later), false positives dropped to about 1-2 per day. Not bad.

### The Mobile App

I did not build and install the Flutter app from source. The mobile app exists in the codebase as a Flutter project with WebRTC live streaming, QR code pairing, push notification deep links, and biometric unlock. The pairing flow uses a `POST /api/v1/pairing/qr` endpoint that generates a QR code, and `POST /api/v1/pairing/redeem` (public, no auth required) that the mobile app calls to redeem the pairing token and get credentials.

The architecture is sound: cloud relay brokers the WebRTC connection, with TURN relay fallback. The `GET /api/v1/relay/ice-servers` endpoint provides STUN/TURN configuration to the app. But since there is no published app in the iOS App Store or Google Play, this is untestable for a real user. I can review the code but I cannot review the experience.

This is a v0.1 problem, not a design problem. The mobile app implementation looks thoughtful.

### Gut Feeling After 72 Hours

Cautiously excited. The architecture is right. The timeline with heatmap is something I have wanted from every NVR I have used. The live view works. Recording works. Detection works. The code quality is genuinely good -- proper error handling, context cancellation, crash isolation, structured logging throughout.

But I would not show this to any of my 6 friends yet. The missing clip export alone would confuse them. No notification cooldown means their phone would blow up. And the detection latency (3-4 seconds) means by the time you get an alert, the delivery driver is already walking away.

I trust it to record. I do not yet trust it to alert me reliably.

---

## Part 2: Daily Living (Week 1-4)

### The Morning Routine

With Blue Iris: open the web UI, check the alert list (sorted by time), scrub the driveway camera from midnight to 6 AM, glance at the front porch. Takes about 3 minutes.

With Sentinel: open the web UI, click Events. The SSE live connection means new events appear without polling -- there is a green "Live" indicator in the header when connected. Filter by date (today), scan the event cards. Each card shows a thumbnail, label, confidence percentage, camera name, and timestamp. Click a card to see the event detail page. From the detail page, there is a "Jump to Recording" link that deep-links to the Playback page with the correct camera, date, and timestamp. That deep-link works well -- it pre-populates the URL query params (`?camera=front_door&date=2026-02-18&time=14523`) and auto-seeks to the right segment.

The morning routine with Sentinel takes about 2-3 minutes. Slightly faster than Blue Iris because the heatmap on the timeline instantly shows me where activity happened -- I do not need to manually scrub through empty hours. The "did anything happen last night?" question is answered by glancing at the heatmap, not by scrolling through an alert list.

### The Events That Mattered

**Package delivery:** Sentinel caught it. A "person" detection fired when the UPS driver walked up. The snapshot showed the driver clearly with a bounding box. I could find the clip by clicking the event card, then "Jump to Recording," and scrubbing to the exact moment. Could I show it to my spouse? I could show the event card with the snapshot. I could not easily share a 30-second clip because there is no clip export. My spouse would need access to the web UI to watch the full recording.

**Neighbor's dog in the yard:** CodeProject.AI detected it as "dog" (not "person" and not "nothing"). The event appeared in the feed. I got notified because I had webhook notifications configured for all detection events. The label was correct. This was good.

**Car in the driveway at midnight:** Detected. CodeProject.AI labeled it "car" and "person" (the driver getting out). Two separate events, a few seconds apart. Detection happened within about 4 seconds of the car pulling in. The snapshot was clear (IR illumination from the camera). Alert came through the webhook.

**Garbage truck -- 12 events in 3 minutes:** This is where the lack of notification cooldown became painful. There is no cooldown mechanism anywhere in the codebase. I searched for "cooldown" across the entire backend -- zero results. The notification service (`notification/service.go`) subscribes to all detection events on the event bus and fires a notification for every single one that matches the user's preferences. So yes, I got 12 webhook notifications in 3 minutes. This is unacceptable for daily use.

**Heavy rain for 2 hours:** 47 false positive detections. Trees swaying, water droplets, shifting shadows. The exclusion zones helped (I had already drawn zones around the tree line), but rain creates motion artifacts across the entire frame, not just in predictable areas. Without a motion-threshold or minimum-object-size filter, every rain-induced artifact that CodeProject.AI classified as "person" with confidence above the threshold triggered an event. This would be improved by a "minimum bbox area" filter or a "consecutive frame confirmation" requirement, neither of which exist.

**Nothing for 3 days:** I got nervous. Checked the Dashboard page -- it auto-refreshes every 10 seconds via `GET /api/v1/health`. The health endpoint returns uptime, database status, go2rtc status, and camera count. The per-camera health table on the Dashboard shows each camera's pipeline state (recording/streaming/connecting/error/idle), whether it is recording, and the last error. All 12 cameras showed "recording" with green status dots. The watchdog was running (it publishes `system.restart` events on startup and checks camera pipelines every health interval). I trusted it was recording.

### The Failures

**Camera recording gap:** Camera 4 (backyard Reolink) stopped recording for approximately 25 minutes on day 11. The ffmpeg process exited (RTSP connection dropped when the camera rebooted after a firmware update). The pipeline's health check loop detected `IsActive()=false` and restarted the recorder. But there is a gap in the timeline for those 25 minutes. The watchdog published a `camera.restarted` event when it restarted the pipeline, which appeared in the event feed. So I was not completely unaware -- but there was no push notification specifically for "camera offline." The watchdog publishes events to the event bus, but the notification service only fires on `detection` and `face_match` events by default. A "camera offline" push notification would require configuring notification preferences for `camera.offline` event types, which I did not do initially.

**go2rtc sidecar:** Stayed up for the full 30 days. No crashes. This is mature software.

**ffmpeg crashes:** Two crashes total across 12 cameras over 30 days. Both were on the same camera (a finicky Amcrest unit with an unreliable RTSP implementation). The recorder has proper crash isolation -- each camera's ffmpeg is a separate OS process managed by a goroutine. One camera's ffmpeg crashing does not affect the other 11. The pipeline restarted ffmpeg automatically within seconds. The code even suppresses crash-loop events (sub-2-second runs) to avoid flooding the event page. Well-engineered.

**Database growth:** SQLite WAL mode. The database grew from about 500KB to about 12MB over 30 days with 12 cameras. No performance degradation. Queries remained fast. The event cleanup runs on a configurable interval and purges events older than the retention period. No issues here.

**Storage management:** Hot/cold tiered storage worked. The storage manager (`storage/storage.go`) runs a migrator (hot -> cold after `hot_retention_days`) and a cleaner (delete cold segments older than `cold_retention_days`). I configured 3 days hot, 30 days cold. Segments moved correctly. File deletion worked. The code even handles cross-device moves (SSD -> NAS) with a copy+remove fallback when `os.Rename` fails across filesystems. Solid.

**Router reboot:** I rebooted my router on day 15. All 12 cameras disconnected. go2rtc reconnected them all within about 30 seconds. ffmpeg recording resumed automatically. The pipelines transitioned through connecting -> streaming -> recording states correctly. The watchdog logged the recovery. No manual intervention needed. This is production-grade behavior.

**Power outage:** Simulated a power outage on day 22 (unplugged the NUC for 30 seconds). Docker containers restarted automatically (restart policy). Sentinel booted, the watchdog published a `system.restart` event, cameras reconnected, recording resumed. Total downtime: about 45 seconds from power restoration to all 12 cameras recording. SQLite WAL mode prevented database corruption. No data loss except the 45-second gap.

### Resource Usage Over Time

| Metric | Day 1 | Day 7 | Day 14 | Day 30 |
|--------|-------|-------|--------|--------|
| CPU idle (12 cameras recording) | ~8% | ~8% | ~9% | ~9% |
| CPU peak (detection burst) | ~35% | ~35% | ~36% | ~35% |
| RAM usage (sentinel container) | ~180MB | ~195MB | ~200MB | ~210MB |
| Disk write rate (hot storage) | ~15 MB/min | ~15 MB/min | ~15 MB/min | ~15 MB/min |
| Database size | 500KB | 3MB | 7MB | 12MB |

No memory leak. CPU did not creep up. Day 30 was essentially the same as day 1. The ~30MB RAM increase over 30 days is normal Go runtime behavior (GC settling). The zero-transcoding architecture (ffmpeg copies streams directly from camera to disk) means CPU is dominated by go2rtc stream management and the periodic detection frame grabs, not by video processing. This is a massive win over Blue Iris, which transcodes everything and runs my NUC at 45-60% CPU with the same 12 cameras.

---

## Part 3: Feature Matrix -- Filled In

| Feature | Blue Iris | Frigate | UniFi Protect | Sentinel NVR |
|---------|-----------|---------|---------------|--------------|
| Continuous 24/7 recording | Yes | Yes | Yes | **Yes** -- ffmpeg direct-to-disk, 10-min segments. Works well. |
| 24/7 timeline scrub | Yes (ugly) | No (clips) | Yes (excellent) | **Yes** -- 4 zoom levels, smooth seek across segments. Better than Blue Iris, approaching UniFi. |
| Heatmap on timeline | No | No | Motion only | **Yes** -- detection density overlay. Best in class. |
| AI object detection | Via CodeProject.AI | Built-in (excellent) | Limited objects | **Yes** -- remote HTTP backends (CodeProject.AI, Blue Onyx, Ollama) + local ONNX. Functional but frame-interval based, not continuous. |
| Detection accuracy (person) | Good (with CPAI) | Excellent | Good | **Good** -- depends on the backend. Same as Blue Iris when both use CodeProject.AI. |
| False positive rate | Medium | Low | Medium | **Medium-High** -- no cooldown, no minimum bbox area filter, no consecutive-frame confirmation. Needs work. |
| Custom AI models (ONNX) | No | YOLO variants | No | **Yes** -- ONNX model upload/download via API. Model management page in UI. Better than competitors. |
| Face recognition | No | Double Take addon | No | **Yes (stub)** -- ArcFace embeddings + cosine similarity matching. API and pipeline exist. Enrollment via JPEG upload. More built-in than Frigate. |
| License plate recognition | Via plugin | Via addon | Yes (limited) | **No** -- not implemented. |
| Audio detection (glass break) | No | No | No | **Yes (stub)** -- YAMNet classification pipeline exists in code. AudioPipeline extracts PCM via ffmpeg, sends to classifier. Ahead of all competitors. |
| Package detection | No | No | Yes | **No** -- model listed in requirements but not shipped. |
| Object tracking across frames | No | Yes | Basic | **No** -- each frame processed independently. |
| Visual zone editor | Yes (clunky) | Partial (mask editor) | Yes (excellent) | **Yes** -- canvas polygon drawing, include/exclude zones, snapshot background refresh. Clean implementation. Better than Blue Iris and Frigate. |
| Exclusion zones | Yes | Yes (mask only) | Yes | **Yes** -- polygon-based, per-camera. Zone filtering uses ray-casting algorithm. Works well. |
| ONVIF auto-discovery | Yes | No | N/A | **Yes** -- `pkg/onvif/discovery.go` with WS-Discovery + device probe. Found 9 of 12 cameras. Better than Frigate. |
| Camera health monitoring | Basic | Basic | Yes | **Yes** -- per-camera pipeline state machine, Dashboard health table, 10-second refresh. Good. |
| Camera offline alert | Delayed | ~1 min | ~30 sec | **~30 sec via watchdog** -- watchdog health check interval is configurable. Publishes `camera.restarted` events. Comparable to UniFi. |
| Mobile app (native) | 3rd party (paid) | None official | Excellent | **Yes (Flutter)** -- code exists, not published to app stores. Cannot evaluate real-world experience. |
| Remote access setup | VPN/port forward | VPN/HA Cloud | Zero config | **QR code pairing** -- cloud relay + WebRTC P2P. Architecture is right. Untestable without published app. |
| Push with snapshot | Unreliable | Via HA | Yes (fast) | **Yes** -- FCM/APNs with snapshot path. Webhook also works. Delivery log with status tracking. |
| Critical alerts (bypass DND) | No | No | Yes (iOS) | **Yes** -- APNs critical alert support in code. Ahead of Blue Iris and Frigate. |
| Notification cooldown | Basic | Via HA | Per-zone | **No** -- zero cooldown. Every detection fires a notification. Dealbreaker for daily use. |
| Two-way audio | Yes | No | Yes | **No** -- not implemented. |
| PTZ controls | Yes | Basic | Yes | **No** -- ONVIF config struct has PTZ fields but no PTZ control API or UI. |
| Multi-user with permissions | Basic | No | Yes | **Yes** -- JWT auth, admin/viewer roles, user CRUD API. Better than Blue Iris and Frigate. |
| Tiered storage (hot/cold) | Manual | No | No | **Yes** -- automatic migration with configurable retention. Best in class. |
| Auto storage migration | No | No | No | **Yes** -- background migrator moves segments hot->cold on schedule. Cross-device aware. |
| Storage space warnings | Yes | Basic | Yes | **Yes** -- watchdog checks disk usage, publishes `storage.almost_full` events at 90%. |
| Export clips | Yes (re-encodes) | Yes (clip files) | Yes (fast) | **No** -- can download full 10-min segments via API. No sub-clip extraction. |
| Multi-camera sync playback | Yes | No | Yes | **No** -- single camera playback only. |
| Playback speed (2x/4x/8x) | Yes | Basic | Yes | **Yes** -- HTML5 playbackRate control. Works. |
| Per-camera retention policy | Yes | Global only | Per-camera | **Yes** -- per-camera x per-event-type retention matrix. More granular than any competitor. |
| HomeKit Secure Video | No | Via Scrypted | No | **No** -- not implemented. |
| Home Assistant integration | Via MQTT | Native | Via HACS | **No** -- no MQTT, no HA integration. |
| MQTT support | Yes | Yes | No | **No** -- not implemented. |
| Webhook notifications | Yes | Via HA | No | **Yes** -- native webhook sender with SSRF validation. |
| Docker deployment | No | Yes | No | **Yes** -- Docker Compose primary, multi-stage nginx build, health checks, resource limits. |
| Cross-platform (Win/Lin/Mac) | Windows only | Linux/Docker | Proprietary HW | **Yes (planned)** -- Go cross-compile in build matrix, but Docker is the tested path. |
| API for automation | Basic | REST | REST (limited) | **Yes** -- comprehensive REST API. Every feature is API-first. Best in class. |
| WebSocket events | No | MQTT | No | **SSE** -- Server-Sent Events for live event stream. Plus WebSocket for MSE video. |
| Web UI quality | Dated (2003) | Functional | Modern | **Modern** -- React 19, dark-first Tailwind, responsive. Closer to UniFi Protect than Blue Iris. |
| Startup time (to recording) | 30-60 sec | 15-30 sec | 10 sec | **~20-30 sec** -- Docker containers start, go2rtc connects cameras, ffmpeg starts recording. Good. |
| Crash recovery | Manual restart | Auto-restart | Auto-restart | **Auto-restart** -- watchdog supervisor, per-camera process isolation, crash-loop suppression. Well-engineered. |
| Config method | GUI (deep) | YAML (painful) | GUI (clean) | **GUI + YAML seed** -- YAML for initial setup, everything else via web UI. Best of both worlds. |
| Update process | Download .exe | Docker pull | Auto-update | **Docker pull** -- same as Frigate. |
| Price | $70 (one-time) | Free | $200+ (hardware) | **Free** -- open source. |

---

## Part 4: What's Brilliant

**The heatmap timeline.** This is the single feature that made me think "this team understands what NVR users actually need." At 3 AM I can glance at the timeline and see exactly when and where detection events clustered. I do not need to scrub through hours of empty footage. I do not need to scroll through an alert list. The red density bars on the timeline tell me "something happened here at 2:47 AM and here at 4:12 AM, and nothing happened in between." Blue Iris has nothing like this. Frigate has nothing like this. UniFi Protect has motion density but not AI detection density. This alone would make me recommend Sentinel to power users.

**Zero-transcoding architecture.** My NUC runs 12 cameras at 8-9% CPU idle. Blue Iris runs the same 12 cameras at 45-60% CPU because it transcodes everything. Sentinel's ffmpeg `-c copy` with segment muxer is the correct architectural decision. The fact that every 10-minute MP4 segment is independently playable (plays in VLC, plays in a browser, no proprietary container format) is a detail that shows engineering maturity.

**The camera pipeline crash isolation.** Each camera's ffmpeg is a separate OS process. One crashes, the other 11 keep recording. The pipeline detects the crash, publishes an event, and restarts. It even has crash-loop suppression (sub-2-second runs do not flood the event page). This is the kind of reliability engineering that Blue Iris completely lacks -- Blue Iris's database corruption issues are legendary.

**Per-camera x per-event-type retention matrix.** "Keep all events for 7 days, but keep person detections on the front door for 90 days." No other NVR I have used offers this level of granularity. Frigate has global retention only. Blue Iris has per-camera but not per-event-type. Sentinel's `retention_rules` table with the priority cascade (specific > camera-wildcard > type-wildcard > global) is thoughtful.

**ONVIF auto-discovery with probe.** It actually found my cameras. The `pkg/onvif/discovery.go` sends WS-Discovery multicast probes and parses the SOAP responses. The probe endpoint lets you test credentials and fetch stream URIs before committing. Frigate does not have this at all.

**The visual zone editor.** Drawing polygons directly on a live camera snapshot. Include zones and exclude zones with different colors (blue vs red). The `pointInPolygon` ray-casting algorithm correctly handles concave polygons. The code normalizes coordinates to [0,1] so zones survive resolution changes. This is better than Blue Iris's zone editor and far better than Frigate's YAML mask coordinates.

---

## Part 5: What's Unacceptable

**No notification cooldown.** The garbage truck incident (12 notifications in 3 minutes) is a dealbreaker. The rain incident (47 false positive notifications in 2 hours) would make any normal person disable notifications entirely. Without per-camera, per-zone cooldown periods (e.g., "after a person detection on the driveway, suppress further driveway-person notifications for 60 seconds"), Sentinel is unusable as a daily notification system. This is not a nice-to-have -- it is table stakes. Every competitor has some form of cooldown.

**No clip export.** I cannot extract a 30-second clip to share. When my spouse asks "can you send me that clip of the package being delivered," the answer is "no, but you can log into the web UI and scrub through a 10-minute recording." This is a non-starter for real-world use. Even Frigate generates downloadable clips. Blue Iris re-encodes but at least produces a shareable file.

**Detection latency is frame-interval bound.** At 5-second intervals, worst-case detection latency is ~5 seconds. Average is ~3-4 seconds. Frigate processes the sub-stream continuously and detects in under 500ms with a Coral TPU. For security use -- "someone is at my door right now" -- 3-4 seconds is too slow. By the time the notification arrives, the person may have already left the porch. The Coral TPU on my desk is completely unused because Sentinel only supports remote HTTP detection backends and a local ONNX subprocess, not direct TPU inference.

**No Coral TPU support.** The requirements document (CG10) lists Coral TPU as a supported accelerator with "auto-convert ONNX to TFLite at first startup." In reality, the code has a `LocalDetector` that manages a `sentinel-infer` subprocess (ONNX Runtime) and a `RemoteDetector` for HTTP backends. There is no TFLite conversion, no Coral TPU code, no `edgetpu` anything. For the large community of Frigate users who bought Coral TPUs specifically for NVR use, this is a missing feature that the requirements document promises.

**No MQTT support.** No Home Assistant integration. This cuts off the entire home automation audience. Frigate's tight integration with Home Assistant via MQTT is one of its biggest advantages. Sentinel has a clean event bus internally but does not expose it over MQTT or any external pub/sub protocol.

---

## Part 6: What's Missing

### Must-Have for v1.0 Launch

1. **Notification cooldown / rate limiting.** Per-camera, per-label, configurable suppression window. Without this, notifications are unusable.

2. **Clip export.** Server-side ffmpeg sub-clip extraction with a shareable download link. Users need to extract and share specific moments, not download 10-minute segments.

3. **Coral TPU / hardware accelerator support.** The target audience (Frigate users, home automation enthusiasts) expects this. OpenVINO for Intel iGPU at minimum.

4. **MQTT event publishing.** Bridge the event bus to an MQTT topic. Home Assistant integration depends on this.

5. **Notification cooldown per zone.** Even within a single camera, different zones need different cooldown periods (driveway: 60s, front door: 30s).

6. **Object tracking across frames (basic).** At minimum, deduplicate "the same person detected in 5 consecutive frames" into a single event with an entry/exit time range. Without this, a person walking through the frame generates N separate events where N = (time in frame / frame interval).

### Must-Have for Year One

7. **Two-way audio.** Users talking to delivery drivers and scaring off porch pirates. This is increasingly expected.

8. **PTZ controls.** The ONVIF config struct has PTZ fields but no control API. Users with PTZ cameras need pan/tilt/zoom in the web UI.

9. **Continuous sub-stream detection** (instead of frame-interval polling). This is what makes Frigate's detection so fast. Process the sub-stream as a video, not as periodic snapshots.

10. **Multi-camera sync playback.** "Show me all cameras at 2:47 AM." Incident investigation requires seeing multiple angles simultaneously.

11. **Home Assistant add-on / HACS integration.** Package Sentinel as a HA add-on for the home automation crowd.

12. **Native binary distribution.** Docker is fine for 80% of users, but Windows and macOS native binaries (as promised in CG5) would capture the Blue Iris migration audience.

13. **Published mobile apps.** The Flutter app exists in code but is not available in app stores. Until it is published, "native mobile app" is a checkbox on a feature list, not a real feature.

### Nice to Have

14. **License plate recognition** (ALPR model).
15. **Package detection model** (fine-tuned YOLO).
16. **Multi-NVR federation** (view multiple Sentinel instances from one UI).
17. **HomeKit Secure Video support** (via Scrypted bridge or native).
18. **Custom notification sounds.**
19. **Dark/light theme toggle** (currently dark-only, which is the right default).
20. **Audit log** (who changed what settings when).

### Not Needed

- **Audio detection** beyond glass break. The YAMNet stub is fine as a future feature. Nobody is switching NVRs because of baby cry detection.
- **OIDC/OAuth2 SSO.** The code already supports it. Power users who want Authelia/Keycloak can configure it. Not a launch requirement.
- **PWA manifest.** Already present. "Add to Home Screen" works. Not important for v1.0 marketing.

---

## Part 7: The Competitive Gap Analysis

### vs Blue Iris

**What Sentinel already does better:**
- Resource efficiency: 8% CPU vs 45-60% for the same 12 cameras. This is the single biggest complaint about Blue Iris.
- Heatmap timeline. Blue Iris has nothing comparable.
- Cross-platform. Blue Iris is Windows-only. Sentinel runs anywhere Docker runs.
- Modern web UI. Blue Iris looks like it was designed in 2003 because it was.
- Camera credential encryption. Blue Iris stores passwords in plain text in the registry.
- Tiered storage with automatic migration. Blue Iris requires manual file management.
- API-first architecture. Blue Iris's API is an afterthought.

**What Sentinel must match before a Blue Iris user would switch:**
- Clip export (Blue Iris users share clips constantly).
- Notification cooldown (Blue Iris has basic cooldown).
- Two-way audio (Blue Iris supports it).
- PTZ controls (Blue Iris has full PTZ support).
- Stable, published mobile app (Blue Iris users rely on the third-party UI3 app).

**What Sentinel can skip:**
- Blue Iris's complex trigger system with schedules, macros, and DIO. Nobody uses 90% of those features.
- Audio-triggered recording (decibel-based). Low value, high false positive rate.
- Built-in DDNS. Users have better options.

### vs Frigate

**What Sentinel already does better:**
- 24/7 scrubbable timeline with heatmap. Frigate has clips, not a timeline. This is Sentinel's biggest advantage.
- Visual zone editor. Frigate requires YAML coordinates or a basic mask editor.
- ONVIF auto-discovery. Frigate does not discover cameras.
- Web UI quality. Sentinel's React UI is modern and responsive. Frigate's UI is functional but sparse.
- Notification delivery (FCM/APNs/webhook with delivery log). Frigate depends on Home Assistant for notifications.
- Multi-user authentication. Frigate has no auth.
- Tiered storage. Frigate has single-path storage with global retention.
- Face recognition (built-in). Frigate requires the third-party Double Take add-on.
- Migration importers (Blue Iris + Frigate config parsers). Smart onboarding.

**What Sentinel must match before a Frigate user would switch:**
- Detection latency. Frigate with Coral TPU detects in under 500ms. Sentinel's 3-4 second frame-interval approach is not competitive.
- MQTT integration. Frigate's MQTT events are the backbone of home automation workflows.
- Coral TPU support. The Frigate user base has invested in Coral hardware.
- Object tracking. Frigate tracks objects across frames and maintains object identity.
- Home Assistant native integration. This is non-negotiable for the Frigate audience.

**Where Frigate is genuinely superior and likely to stay ahead:**
- Detection speed with dedicated hardware (Coral TPU). Frigate processes the sub-stream continuously. Sentinel's frame-interval approach is architecturally slower even with the same hardware.
- Frigate's community momentum and Home Assistant ecosystem integration. This is a network effect that takes years to build.

### vs UniFi Protect

**What Sentinel already does better:**
- Open source and hardware-agnostic. UniFi Protect requires Ubiquiti hardware ($200+ for a Cloud Key or Dream Machine).
- Heatmap timeline. UniFi has motion density but not AI detection density.
- Custom AI models. UniFi's detection is fixed to what Ubiquiti ships.
- Per-camera x per-event-type retention. UniFi has per-camera only.
- Self-hosted with no cloud dependency. UniFi Protect requires a Ubiquiti account.
- Tiered hot/cold storage. UniFi uses the internal drive only.

**What aspects of UniFi Protect Sentinel should aspire to:**
- The polish. UniFi Protect's timeline scrubbing is buttery smooth. The mobile app is fast and intuitive. The notification flow (alert -> tap -> video) takes 2 taps. Sentinel should aspire to this level of UX polish.
- Zero-config setup. UniFi cameras auto-adopt. Sentinel's ONVIF discovery is a step in this direction but needs refinement.
- Sub-second live view latency. UniFi achieves this with purpose-built hardware and protocol optimization.

**What is impossible to match without Ubiquiti's hardware integration:**
- Camera auto-adoption with no credentials needed (UniFi cameras trust the controller implicitly).
- PoE power management and camera reboot from the controller.
- Integrated hardware encoding on the controller for smooth playback.
- The "it just works" factor of a vertically integrated system.

---

## Part 8: The Verdict

### 1. Would you replace Blue Iris with Sentinel today?

**No.** The architecture is better. The resource efficiency is better. The timeline with heatmap is better. But the missing clip export, missing notification cooldown, and 3-4 second detection latency mean I cannot use Sentinel as my only NVR. I would miss clips from real events because I cannot easily share them. I would either drown in notifications or disable them entirely. And by the time a detection alert arrives, the event may already be over.

### 2. What needs to happen for you to switch?

In priority order:

1. **Notification cooldown** -- per-camera, per-label, configurable window (30-300 seconds).
2. **Clip export** -- select a time range on the timeline, download an MP4.
3. **Detection latency improvement** -- either continuous sub-stream processing or a configurable frame interval down to 0.5 seconds with hardware acceleration.
4. **MQTT event bridge** -- publish detection events to MQTT topics for Home Assistant.
5. **Published mobile app** -- available in the App Store and Play Store with push notifications.
6. **3 months of proven stability** -- v0.1 ran for 30 days without data loss, but I need a longer track record before I trust it as my only NVR.

### 3. Would you recommend Sentinel to a non-technical friend?

**Not yet.** My friend who calls me when their cameras "stop working" would be confused by Docker Compose, bewildered by YAML configuration, and frustrated by the lack of clip export. The web UI is clean and modern, but the setup process assumes a technical user. When Sentinel has a one-click installer, a published mobile app, and clip export, I would consider recommending it. Until then, I would tell them to buy a Reolink NVR or a UniFi system.

### 4. What is the single most important thing the team should work on next?

**Notification cooldown.**

Not clip export (annoying but workaround exists -- download the full segment). Not detection latency (Blue Iris has the same latency with CodeProject.AI, and people live with it). Not MQTT (only matters for the HA crowd).

Notification cooldown is the thing that, if fixed, moves Sentinel from "interesting project I run alongside my real NVR" to "this is my daily driver." Without cooldown, notifications are either too noisy (every detection fires an alert) or disabled entirely (useless). With cooldown, Sentinel becomes a system I can trust to alert me when something matters and stay quiet when it does not. That is the difference between a monitoring tool and an annoyance.

### 5. Readiness Score

**5 out of 10.**

The architecture is a 9. The implementation quality is an 8. The feature completeness is a 4. The daily-use readiness is a 3.

The bones are excellent. The core video pipeline, the event bus, the storage management, the watchdog, the crash isolation -- this is well-engineered software. But NVR users do not evaluate architecture. They evaluate: "did it catch the person on my porch, did it alert me in time, and can I show my spouse the clip?" Right now, Sentinel partially answers the first question, poorly answers the second, and cannot answer the third.

To reach a 7 (the score where I would trust it as my only NVR): notification cooldown, clip export, and 3 months of stability data.

To reach a 9 (the score where I would actively recommend it to others): add MQTT, published mobile app, continuous sub-stream detection, and the polish that makes non-technical users comfortable.

The team has built something real. The timeline heatmap alone is worth the price of admission (which is free). The zero-transcoding architecture solves Blue Iris's biggest problem. The visual zone editor solves Frigate's biggest UX pain point. This is not vaporware -- it is a v0.1 that demonstrates genuine understanding of what NVR users need. It just needs the last-mile features that make it livable.
