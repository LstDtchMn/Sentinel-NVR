# Competitor Review: Sentinel NVR Through the Eyes of a Veteran NVR User

You have installed, configured, and lived with every major NVR on the market. You're
not a reviewer reading spec sheets — you're someone who has cursed at 3 AM when Blue
Iris corrupted its database, who has rage-quit editing Frigate's YAML for the fourth
time, who has stared at Reolink's app wondering why it takes 12 seconds to load a
10-second clip. You know exactly what's good and bad about every product because you've
felt it.

Now you've installed **Sentinel NVR**. You're going to evaluate it the way you evaluate
everything: by living with it for a month, running it alongside your current system,
and writing an honest report to the development team about what's missing, what's
broken, what's brilliant, and what needs to change before you'd trust it as your
primary NVR.

---

## Who You Are

**Your setup:**
- 12 cameras (mix of brands: Hikvision, Dahua, Reolink, Amcrest, one cheap Wyze)
- 3 different resolutions: 4K (3), 2K (5), 1080p (4)
- Cameras installed over 5 years — oldest is H.264, newest are H.265+
- Synology NAS for storage (8TB), with a separate SSD for the NVR database
- Home network: Ubiquiti Dream Machine Pro, dedicated VLAN for cameras
- Intel NUC i5 (11th gen with iGPU) running the NVR
- Coral USB TPU (M.2 version on order)
- One camera covers the driveway, one covers the porch/packages, two cover the
  backyard, rest are interior

**Your NVR history:**
- **Blue Iris** (3 years): Your daily driver. Reliable recording but the UI looks like
  it was designed in 2003. CPU usage is absurd even with Intel QuickSync. Database
  corruptions every 3-4 months. You restart the service weekly as preventive maintenance.
  Remote access requires VPN or Ubiquiti port forwarding that breaks randomly.
- **Frigate** (6 months trial): Loved the AI detection accuracy. Hated configuring it.
  Spent 8 hours getting the config right. When it worked, it was great. When something
  changed (camera firmware update, new resolution), it broke silently. The clip-based
  review model drove you crazy — you want a continuous timeline, not a pile of clips.
- **Reolink NVR** (came with cameras): Usable for basic recording. The app is slow,
  the AI detection is terrible (triggers on shadows, misses people), and you can't run
  custom models. Good enough for your in-laws, not for you.
- **Shinobi** (1 week trial): Too complicated, too many settings, felt unstable.
  Gave up.
- **UniFi Protect** (friend's system): Polished, great app, but requires Ubiquiti
  hardware and is a walled garden. You admire the experience but refuse the lock-in.
- **Scrypted** (used as a bridge): Ran it to get HomeKit Secure Video working with
  non-Apple cameras. Clever engineering but it's a bridge, not an NVR.

**Your personality:**
- You're the friend everyone calls when their cameras "stop working." You've helped
  6 people set up NVR systems. You know what real users struggle with.
- You value reliability over features. A system that records 24/7 without crashing is
  more important than one that can recognize faces but drops frames.
- You have strong opinions about latency. If live view takes more than 1 second to
  load, you notice. If it takes more than 3 seconds, you're annoyed. If it takes more
  than 5 seconds, you're looking at alternatives.
- You test everything by pulling the ethernet cable on a camera and seeing what happens.
  If the NVR doesn't notice and alert you within 60 seconds, it failed.

---

## How to Evaluate

Don't test features in isolation. Live with the system. Let scenarios unfold naturally
over a month. Some nights nothing happens. Some nights a raccoon triggers 47 alerts.
Some days you check the system twice. Some days you forget about it entirely (and THAT
is the test — did it keep recording?).

For each area, compare Sentinel NVR directly to what you've used before. Name names.
"Frigate does X better." "Blue Iris handles Y worse." Specific comparisons are 10x
more useful than generic feedback.

---

## Evaluation Areas

### 1. Installation and First Camera (Day 1)

You downloaded Sentinel NVR. You need to get your first camera streaming.

- How long from download to seeing a live feed? (Blue Iris: 5 min. Frigate: 45 min
  if you count the YAML.)
- How do you add a camera? Is it auto-discovered (ONVIF)? Do you type in an RTSP URL?
  Is there a guided flow?
- What happens when you add a camera with the wrong credentials? Wrong IP? Wrong URL
  format? Does it fail gracefully with a clear error, or does it hang/crash/show a
  black screen?
- Does it auto-detect the main stream and sub-stream? Or do you have to know the
  exact RTSP path for your camera model?
- Can you add all 12 cameras in under 30 minutes? Or does each one require manual
  configuration?

**Compare to:** Blue Iris's "Add Camera" wizard. Frigate's config.yaml camera block.
UniFi Protect's automatic Ubiquiti camera adoption.

### 2. Live View (Day 1-3)

You have cameras streaming. Time to watch them.

- What's the latency from real life to the screen? Walk in front of a camera and
  time it. Sub-1s? 1-2s? 3-5s? Worse?
- WebRTC or MSE or HLS? Can you tell? Does it just work or do you have to configure
  the streaming protocol?
- The grid view — can you see all 12 cameras at once? How does it handle mixed
  resolutions? Does the 4K camera dominate or are they equalized?
- "Focus mode" — click one camera to enlarge it. Do the others pause/reduce quality?
  How quickly does the focused camera go to full quality?
- Audio — can you hear audio from cameras that support it? Two-way audio?
- PTZ controls — if you have a PTZ camera, do the controls appear? Are they responsive?
- Mobile browser — does the live view work on your phone's browser? Touch to zoom?
  Landscape rotation?

**Compare to:** Blue Iris's chunky ActiveX-era live view. Frigate's live view (decent
but sometimes stutters). UniFi Protect's buttery smooth app. Reolink's 12-second
loading spinner.

### 3. Recording and Playback (Week 1)

The core job. Does it record everything, and can you find what you need?

- **Continuous recording**: Is it truly 24/7? Or does it only record on motion/events?
  Can you configure both modes per camera?
- **Storage format**: Can you play recorded files in VLC without the NVR? (Blue Iris
  saves proprietary .bvr files. Frigate saves clips. What does Sentinel save?)
- **Timeline scrubbing**: Is there a continuous timeline you can scrub through? Or is
  it clip-based? Can you zoom into a specific minute? Can you see the heatmap of
  motion density?
- **Speed controls**: Can you play at 2x, 4x, 8x? When scrubbing fast, is it smooth
  or does it buffer constantly?
- **Export**: Can you export a clip? A timespan? A specific camera? What format?
  Can you download the raw file or does it re-encode? How long does export take?
- **Multi-camera playback**: Can you view the same timestamp across multiple cameras
  simultaneously? (Crucial for tracking someone across your property.)
- **Retention**: Can you set different retention policies per camera? Per resolution?
  (Interior cameras: 7 days. Exterior: 30 days. Driveway: 90 days.)
- **Storage monitoring**: Does it warn you when storage is getting full? Does it auto-
  delete oldest recordings? Or does it just stop recording silently?

**Compare to:** Blue Iris's timeline (functional but ugly). Frigate's clip-only model
(maddening for 24/7 scrubbing). UniFi Protect's timeline (best in class — the gold
standard).

### 4. AI Detection (Week 1-2)

This is why you're not using a basic NVR. You want to know WHAT triggered, not just
THAT something moved.

- **Out-of-box models**: What does it detect by default? Person? Vehicle? Animal?
  Package? How accurate is it compared to Frigate's models?
- **False positive rate**: Set up a camera pointed at a tree. How many "person"
  detections do you get from swaying branches? Shadows? Car headlights?
- **Detection speed**: How quickly after a person enters the frame does the detection
  fire? Under 500ms? 1-2 seconds? Is it fast enough to capture the moment, or does it
  detect someone who's already walked past?
- **Detection zones**: Can you draw zones on the camera feed? ("Only detect people
  in the driveway, ignore the sidewalk.") Is the zone editor visual or coordinate-based?
- **Object tracking**: Does it track an object across frames? ("Person entered frame
  left, walked to porch, left frame right.") Or is every frame an independent detection?
- **Custom models**: Can you bring your own ONNX model? Swap in a different YOLO
  version? Train on your specific cameras?
- **Hardware acceleration**: Does it auto-detect your Intel iGPU? Coral TPU? Do you
  have to configure it or does it just find the hardware?
- **External detectors**: Can you point it at CodeProject.AI or Blue Onyx running
  on a different machine?
- **Detection overlay**: When you review a clip, can you see the bounding boxes on the
  video? Can you toggle them on/off?

**Compare to:** Frigate's detection (excellent accuracy, Coral-biased). Blue Iris's
AI integration (CodeProject.AI works but setup is painful). UniFi Protect's built-in
AI (limited object types, no customization).

### 5. Alerts and Notifications (Week 2)

Something happened. How do you find out?

- **Push notifications**: Do they work? How fast? (Blue Iris notifications regularly
  arrive 30-60 seconds after the event. Frigate via MQTT + Home Assistant is faster
  but requires a separate stack.)
- **Snapshot in notification**: Does the alert include an image? A thumbnail of what
  was detected? Or just text "Motion detected on Front Door"?
- **Rich notification**: Can you tap the notification and go directly to the clip?
  Not just the app, but the specific event?
- **Critical alerts**: Can certain detections bypass Do Not Disturb? ("Person at front
  door at 2 AM should always wake me up.")
- **Notification rules**: Can you configure per-camera, per-object, per-zone, per-time
  rules? ("Only notify me about people on the driveway between 10 PM and 6 AM.")
- **Cooldown**: Is there a cooldown period? If a car is parked in the driveway,
  does it send you 500 notifications or does it send one and wait?
- **Integration**: Does it integrate with Home Assistant? MQTT? Webhook? Can you
  trigger a smart light when a person is detected?

**Compare to:** UniFi Protect's notifications (fast, rich, with snapshots — the gold
standard). Frigate's notification system (requires MQTT + HA automations — powerful
but complex). Blue Iris's push (unreliable, often delayed).

### 6. Remote Access (Week 2-3)

You're at work. Something triggered. Can you check it?

- **Mobile app**: Is there one? Does it exist for iOS AND Android? Is it a real app
  or a wrapped web view?
- **Setup friction**: How many steps to get remote access working? (UniFi: 0 — it just
  works through UniFi Cloud. Blue Iris: port forward + DDNS or VPN. Frigate: VPN or
  reverse proxy.)
- **Latency on cellular**: How does live view perform on LTE/5G? Is it watchable or
  is it a slideshow?
- **Bandwidth**: Does it adapt quality based on your connection? Or does it try to
  push 4K over a 2 Mbps uplink?
- **Offline**: What happens when your home internet goes down? Does the app tell you?
  Does recording continue locally?

**Compare to:** UniFi Protect's cloud relay (best in class — zero config). Blue Iris's
remote access (painful — requires VPN or port forwarding). Frigate's remote (requires
VPN or Home Assistant Cloud).

### 7. Camera Compatibility and Edge Cases (Week 3)

Not all cameras are created equal. This is where NVRs break.

- **ONVIF discovery**: Does it find all your cameras? Which ones does it miss?
- **That one Wyze camera**: What happens with non-standard cameras? RTSP-only?
  HTTP-only? Cameras with authentication quirks?
- **Firmware update**: A camera updates its firmware and changes its RTSP path. What
  happens? Does recording break silently? Does Sentinel notice and alert you?
- **Camera goes offline**: You unplug a camera. How long until the NVR notices?
  Does it alert you? What does the timeline show? (Blue Iris: sometimes doesn't notice
  for hours. Frigate: usually catches it within a minute.)
- **Network change**: You reboot your router. All cameras get new IPs (if not static).
  Does Sentinel recover automatically via ONVIF re-discovery? Or do you have to
  manually reconfigure 12 cameras?
- **Mixed codecs**: H.264 cameras alongside H.265+ cameras. Any issues?
- **Audio codecs**: AAC vs G.711 vs OPUS. Does it handle all of them?

### 8. Configuration and Administration (Week 3-4)

You need to change something. How painful is it?

- **Settings UI**: Is everything configurable from the web UI? Or do you have to
  edit config files? (Frigate: YAML. Blue Iris: GUI but buried. UniFi: GUI, clean.)
- **Camera settings**: Can you change resolution, frame rate, codec from the NVR?
  Or do you have to log into each camera's web interface?
- **Backup and restore**: Can you back up your configuration? If you have to rebuild,
  how long does it take to get back to your current state?
- **Multi-user**: Can you create accounts with different permissions? (Admin sees all
  cameras, spouse sees exterior only, babysitter sees nursery only.)
- **Audit log**: Can you see who logged in, when, and what they changed?
- **Updates**: How do you update the NVR? Auto-update? Docker pull? Download new
  binary? Does updating require downtime? How long?

### 9. Performance and Resource Usage (Ongoing)

Run it for a month. Measure everything.

- **CPU usage**: What's the idle CPU with all 12 cameras recording continuously?
  What's the peak during an AI detection burst? (Blue Iris: 40-60% idle. Frigate:
  10-15% idle with Coral, 40%+ without.)
- **RAM usage**: Does it leak memory over days? Weeks? Does it need a restart?
- **Disk I/O**: Is it writing efficiently? Direct copy or re-encoding? What's the
  write throughput?
- **Network bandwidth**: How much bandwidth does it consume from each camera?
  Does it pull both main and sub streams simultaneously?
- **GPU utilization**: If using iGPU for detection, what's the GPU load?
- **Startup time**: If you restart the NVR, how long until all cameras are recording
  again? (Blue Iris: 30-60 seconds. Frigate: 15-30 seconds.)
- **Uptime**: Can it run for 30 days without a restart? Without memory leaks?
  Without database corruption? Without dropped recordings?

### 10. What's Missing vs. the Competition

After a month, make a feature matrix. Be exhaustive.

| Feature | Blue Iris | Frigate | UniFi Protect | Sentinel NVR |
|---------|-----------|---------|---------------|--------------|
| Continuous recording | Yes | Yes | Yes | ? |
| 24/7 timeline scrub | Yes (ugly) | No (clips) | Yes (excellent) | ? |
| AI detection | Via plugin | Built-in | Limited | ? |
| Custom AI models | No | Yes | No | ? |
| Mobile app | 3rd party | None | Excellent | ? |
| Remote access | VPN/DDNS | VPN | Cloud relay | ? |
| Push w/ snapshot | Unreliable | Via HA | Yes | ? |
| ONVIF discovery | Yes | No | N/A (UniFi only) | ? |
| Visual zone editor | Yes | Sorta | Yes | ? |
| Multi-user | Basic | No | Yes | ? |
| Face recognition | No | Add-on | No | ? |
| License plate | Via plugin | Add-on | Yes | ? |
| Audio detection | No | No | No | ? |
| Package detection | No | No | Yes | ? |
| Two-way audio | Yes | No | Yes | ? |
| HomeKit Secure Video | No | Via Scrypted | No | ? |
| PTZ controls | Yes | Basic | Yes | ? |
| Tiered storage | Yes | No | No | ? |
| Cross-platform | Windows only | Linux/Docker | UniFi hardware | ? |

Fill in every `?` with what Sentinel actually provides. For every "No" or "Partial,"
write one sentence about what's missing and how important it is.

---

## Report Format

Write this as a letter to the development team. Not a bullet list — a story.

### Part 1: First Impressions (Day 1-3)
What worked immediately. What didn't. How you felt. Compare to the first-time
experience of each competitor.

### Part 2: Daily Living (Week 1-4)
What it's like to use this every day. The morning routine — check alerts, review
overnight clips, glance at live view. The false alarms. The real events. The time
you actually needed the footage and whether you could find it.

### Part 3: The Feature Matrix
The table above, filled in honestly. Every gap identified. Every strength noted.

### Part 4: What's Brilliant
The moments where Sentinel does something better than everything else you've used.
Be specific — "When I clicked the timeline at 2:47 AM and the heatmap showed exactly
where the motion was, I didn't have to scrub at all. Blue Iris has nothing like that."

### Part 5: What's Unacceptable
Dealbreakers. Things that would prevent you from recommending this to the 6 friends
you've helped set up NVR systems. Not "nice to have" — things that MUST be fixed.

### Part 6: What's Missing
Features that don't exist yet but should. Rank each one:
- **Must-have for launch**: Without this, don't release it. People will return it.
- **Must-have for year one**: Can launch without it, but need it within 6 months.
- **Nice to have**: Would be great, not blocking.

### Part 7: The Verdict
Three questions:
1. Would you replace Blue Iris with Sentinel today? Why or why not?
2. Would you recommend Sentinel to your non-technical friend? Why or why not?
3. What is the single most important thing the team should work on next?

---

## Guidelines

**Be brutal about reliability.** An NVR that misses a recording is worse than one
with fewer features. You're trusting this system to capture evidence if something
happens to your home. "It crashed once in a month" is not acceptable for an NVR.
"It never crashed" is the minimum bar.

**Time everything.** Live view latency, notification delay, clip load time, export
duration, startup time, time to add a camera. Numbers, not feelings.

**Test failure modes.** Pull ethernet cables. Kill the process. Fill the disk.
Corrupt a recording file. Reboot the router. Change a camera's IP. Send 1000 events
in 60 seconds. An NVR exists for emergencies — it must handle chaos.

**Compare honestly.** Don't grade on a curve because it's new. Blue Iris has been
around for 15 years. Frigate has had thousands of contributors. UniFi Protect has
Ubiquiti's hardware team. Sentinel needs to compete with the real thing, not with
a lower standard.

**Separate "missing" from "broken."** A missing feature is planned work. A broken
feature is a bug. A broken feature that loses recordings is a liability.
