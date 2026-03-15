# Veteran NVR User Review: One Month with Sentinel NVR v0.1

You are the person from the competitor review prompt. You've run Blue Iris for 3 years,
tried Frigate for 6 months, played with Shinobi, admired UniFi Protect from a distance,
and helped 6 friends set up camera systems. You have 12 cameras, a Coral TPU, an Intel
NUC with iGPU, and very specific opinions about what makes an NVR worth using.

You installed Sentinel NVR v0.1.0 a month ago. You've been running it alongside Blue
Iris (which still handles your actual security recordings — you're not crazy enough to
trust a v0.1 with your only copy). This is your report.

You know what Sentinel has built (from the changelog):
- Go backend, SQLite/WAL, event bus, watchdog
- go2rtc for live streaming, ffmpeg for recording (10-min MP4 segments)
- React web UI with live view, timeline with heatmap, zone editor, event feed
- Remote HTTP detection (Blue Onyx, CodeProject.AI), face recognition stub
- Hot/cold tiered storage, per-camera retention
- JWT auth, encrypted camera credentials, notifications (FCM/APNs/webhook)
- Flutter mobile app with WebRTC, QR pairing, push deep links

Now tell the team what you actually experienced.

---

## Part 1: First Impressions (Day 1-3)

Write about your first 72 hours. Cover:

**Installation:**
- Docker Compose or native binary — which did you try? How long to get it running?
- The `sentinel.yml` config — is it clean and obvious or did you spend time guessing?
- First camera added — was ONVIF auto-discovery available or did you manually enter
  RTSP URLs? How long for camera #1? Camera #12?
- The go2rtc sidecar — did it start automatically? Did you have to configure it
  separately? Did the camera streams just appear or did you fight RTSP paths?

**First live view:**
- Time from clicking "Live View" to seeing video. Measure it.
- The grid — all 12 cameras at once. Performance? Dropped frames? CPU spike?
- Focus mode — click one camera. How fast does it go full quality? Click another.
  Smooth or janky?
- Latency — walk in front of a camera, time the delay on screen.
- Compare this moment to the first time you saw Blue Iris's live view, Frigate's,
  and UniFi Protect's (at a friend's house).

**First recording playback:**
- Found the timeline. Scrubbed to 2 hours ago. How did it feel?
- The heatmap — did it actually show you where events happened? Or was it empty/
  inaccurate?
- Played back a recording. Quality? Smooth? Stuttering? Artifacts?
- Tried to export a 30-second clip. Could you? How?

**First detection event:**
- Connected to Blue Onyx (or CodeProject.AI). How many steps?
- Someone walked past a camera. Did Sentinel detect them? How fast?
- Was the bounding box accurate? Did it follow the person across frames?
- False positives in the first 24 hours — how many? From what?

**The mobile app:**
- Downloaded the Flutter app. Scanned the QR code. Did pairing work?
- Opened live view on your phone over WiFi. Latency? Quality?
- Switched to cellular. Still watchable?
- Got a push notification. Tapped it. Did it go to the right event?

**Your gut feeling after 72 hours:**
Not features — feeling. Do you trust it? Are you anxious? Excited? Frustrated?
Would you show this to one of the 6 friends you've helped? Or would you wait?

---

## Part 2: Daily Living (Week 1-4)

### The Morning Routine

Every morning you check your cameras. With Blue Iris, that's: open the web UI, check
the alert list, scrub the driveway camera from midnight to 6 AM, glance at the front
porch.

Describe the same routine with Sentinel. Is it faster? Slower? More informative?
What do you look at first? How many clicks to get to "did anything happen last night?"

### The Events That Mattered

Over a month, things happen:
- A package was delivered. Did Sentinel catch it? Could you find the clip easily?
  Could you show it to your spouse?
- The neighbor's dog got into your yard. Did the AI call it "animal" or "person"
  or nothing? Did you get notified?
- A car pulled into your driveway at midnight. Detection? Alert? How fast?
- The garbage truck triggered 12 events in 3 minutes. Did the cooldown work?
  Or did you get 12 notifications?
- It rained hard for 2 hours. How many false positives? Trees swaying, water
  droplets on the lens — did the AI handle it or was it a notification storm?
- Nothing happened for 3 days straight. Did you check the system? Did you trust
  that it was recording? How did you verify?

### The Failures

Every NVR fails eventually. What happened with Sentinel?
- Did any camera stop recording without alerting you?
- Did the go2rtc sidecar crash? Did ffmpeg crash? How did recovery work?
- Did the database grow unexpectedly? Any performance degradation over time?
- Did storage management work? Did old recordings get purged correctly?
- Did the NVR survive a router reboot? A power outage? A camera firmware update?

### Resource Usage Over Time

| Metric | Day 1 | Day 7 | Day 14 | Day 30 |
|--------|-------|-------|--------|--------|
| CPU idle (12 cameras recording) | ? | ? | ? | ? |
| CPU peak (detection burst) | ? | ? | ? | ? |
| RAM usage | ? | ? | ? | ? |
| Disk write rate | ? | ? | ? | ? |
| Database size | ? | ? | ? | ? |

Did it leak memory? Did CPU creep up? Was day 30 the same as day 1?

---

## Part 3: Feature Matrix — Filled In

Go through every row. Be honest.

| Feature | Blue Iris | Frigate | UniFi Protect | Sentinel NVR |
|---------|-----------|---------|---------------|--------------|
| Continuous 24/7 recording | Yes | Yes | Yes | |
| 24/7 timeline scrub | Yes (ugly) | No (clips) | Yes (excellent) | |
| Heatmap on timeline | No | No | Motion only | |
| AI object detection | Via CodeProject.AI | Built-in (excellent) | Limited objects | |
| Detection accuracy (person) | Good (with CPAI) | Excellent | Good | |
| False positive rate | Medium | Low | Medium | |
| Custom AI models (ONNX) | No | YOLO variants | No | |
| Face recognition | No | Double Take addon | No | |
| License plate recognition | Via plugin | Via addon | Yes (limited) | |
| Audio detection (glass break) | No | No | No | |
| Package detection | No | No | Yes | |
| Object tracking across frames | No | Yes | Basic | |
| Visual zone editor | Yes (clunky) | Partial (mask editor) | Yes (excellent) | |
| Exclusion zones | Yes | Yes (mask only) | Yes | |
| ONVIF auto-discovery | Yes | No | N/A | |
| Camera health monitoring | Basic | Basic | Yes | |
| Camera offline alert | Delayed | ~1 min | ~30 sec | |
| Mobile app (native) | 3rd party (paid) | None official | Excellent | |
| Remote access setup | VPN/port forward | VPN/HA Cloud | Zero config | |
| Push with snapshot | Unreliable | Via HA | Yes (fast) | |
| Critical alerts (bypass DND) | No | No | Yes (iOS) | |
| Notification cooldown | Basic | Via HA | Per-zone | |
| Two-way audio | Yes | No | Yes | |
| PTZ controls | Yes | Basic | Yes | |
| Multi-user with permissions | Basic | No | Yes | |
| Tiered storage (hot/cold) | Manual | No | No | |
| Auto storage migration | No | No | No | |
| Storage space warnings | Yes | Basic | Yes | |
| Export clips | Yes (re-encodes) | Yes (clip files) | Yes (fast) | |
| Multi-camera sync playback | Yes | No | Yes | |
| Playback speed (2x/4x/8x) | Yes | Basic | Yes | |
| Per-camera retention policy | Yes | Global only | Per-camera | |
| HomeKit Secure Video | No | Via Scrypted | No | |
| Home Assistant integration | Via MQTT | Native | Via HACS | |
| MQTT support | Yes | Yes | No | |
| Webhook notifications | Yes | Via HA | No | |
| Docker deployment | No | Yes | No | |
| Cross-platform (Win/Lin/Mac) | Windows only | Linux/Docker | Proprietary HW | |
| API for automation | Basic | REST | REST (limited) | |
| WebSocket events | No | MQTT | No | |
| Web UI quality | Dated (2003) | Functional | Modern | |
| Startup time (to recording) | 30-60 sec | 15-30 sec | 10 sec | |
| Crash recovery | Manual restart | Auto-restart | Auto-restart | |
| Config method | GUI (deep) | YAML (painful) | GUI (clean) | |
| Update process | Download .exe | Docker pull | Auto-update | |
| Price | $70 (one-time) | Free | $200+ (hardware) | |

For every cell you fill in for Sentinel, add a brief note: is it good enough?
Better than the competition? Noticeably worse?

---

## Part 4: What's Brilliant

Specific moments where Sentinel did something no other NVR has done for you.
Not "it's nice" — the exact moment, the exact feature, why it mattered.

Examples of what I'm looking for:
- "The heatmap timeline at 3 AM — I could see exactly when and where motion happened
  without scrubbing through hours of footage. Blue Iris has nothing like this."
- "QR code pairing on the mobile app. I scanned it and had remote access in 10 seconds.
  With Blue Iris I spent 2 hours setting up a VPN."
- "Camera credential encryption. I've always been uneasy about Blue Iris storing my
  camera passwords in plain text."

---

## Part 5: What's Unacceptable

Dealbreakers. Things that must be fixed before you'd consider this for real use (not
just a side-by-side test). Be brutal.

Examples:
- "Recording dropped for 45 minutes on camera 7 and nothing alerted me."
- "The mobile app crashed 3 times in a week. I can't trust it."
- "Detection latency is 4 seconds. By the time it alerts me, the person has already
  left the frame. Frigate detects in under 500ms."
- "No ONVIF discovery. I had to manually find RTSP URLs for all 12 cameras. That's
  a non-starter for anyone who isn't technical."

---

## Part 6: What's Missing

Rank every missing feature:

**Must-have for v1.0 launch:**
Things that users will immediately notice are missing and will prevent adoption.
The features that make someone say "this isn't ready" and go back to Blue Iris.

**Must-have for year one:**
Can launch without these, but they need to come within 6 months or users will leave
for something else.

**Nice to have:**
Would be great, won't lose users without them.

**Not needed:**
Features competitors have that you genuinely don't care about. Be honest — not every
feature matters. Some things Blue Iris has that nobody uses.

---

## Part 7: The Competitive Gap Analysis

For each major competitor, answer:

### vs Blue Iris
- What does Sentinel already do better?
- What must Sentinel match before a Blue Iris user would switch?
- What can Sentinel skip that Blue Iris has but nobody cares about?

### vs Frigate
- What does Sentinel already do better?
- What must Sentinel match before a Frigate user would switch?
- Where is Frigate genuinely superior and likely to stay ahead?

### vs UniFi Protect
- What does Sentinel already do better?
- What aspects of the UniFi experience should Sentinel aspire to?
- What's impossible to match without Ubiquiti's hardware integration?

---

## Part 8: The Verdict

1. **Would you replace Blue Iris with Sentinel today?** (Yes/No and exactly why)
2. **What needs to happen for you to switch?** (Specific list, in priority order)
3. **Would you recommend Sentinel to a non-technical friend?** (The friend who
   calls you when their cameras "stop working")
4. **What is the single most important thing the team should work on next?**
   Not a feature list — one thing. The thing that, if fixed, moves Sentinel from
   "interesting project" to "my daily driver."
5. **On a scale of 1-10, how ready is this for public release?** And what score
   does it need to reach before you'd trust it as your only NVR?

---

## Guidelines

**Run it alongside Blue Iris for the full month.** Don't switch over. Compare
recordings, compare detection, compare reliability. When something happens at
3 AM, check both systems — did they both catch it? Did one miss it?

**Test the chaos scenarios.** Power outage recovery. Camera offline detection.
Full disk behavior. Router reboot. These aren't edge cases — they're Tuesday.

**Involve your spouse/family.** Have them use the mobile app for a week without
help. Can they check the cameras? Find a clip? Understand an alert? If they
can't, the UI has failed — because most NVR users are not the person who
installed it.

**Be specific about latency and timing.** "It's fast" means nothing. "Live view
loaded in 1.2 seconds on WiFi, 3.8 seconds on LTE, and 6+ seconds on a weak
cellular signal" means everything.

**Don't grade on a curve.** v0.1 is an explanation, not an excuse. If detection
latency is 4 seconds, it's 4 seconds — it doesn't matter that it's the first
release. The development team needs the real number so they can fix it, not a
padded score so they feel good.
