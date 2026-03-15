# Final Evaluation: Sentinel NVR v0.3.0 -- The Switch Decision

**Reviewer:** Same veteran NVR user. 3 years Blue Iris, 6 months Frigate, 12 cameras, Coral USB TPU, Intel NUC i7. Running Sentinel alongside Blue Iris for 2 months (v0.1 through v0.3). Zero P1 bugs reported across all three releases.

**Previous scores:** v0.1 = 5/10, v0.2 = 7/10.

**Context:** In my v0.2 review I said: "If v0.3 ships the UI integration, I will run Sentinel as primary and Blue Iris as backup for 60 days. If nothing breaks, Blue Iris gets retired." The team shipped exactly what I asked for. This review answers the question.

---

## Part 1: Testing the v0.3 Fixes

### Test 1: Export Clip Button -- The Spouse Test (Passed)

The scissors icon is in the Playback header toolbar, right where it belongs -- next to the date navigation controls, visually grouped with the other playback actions. I clicked it. The export mode appeared: two range sliders (Start/End) spanning 0-24h with HH:MM:SS display, a duration indicator, and a Download button. The default range auto-centered a 30-second window around my current playhead position. That default is smart -- it is exactly what I would have set manually 90% of the time.

I tested the flow end-to-end. Selected the driveway camera, scrubbed to 10:32 AM (package delivery), clicked Export Clip, adjusted the end slider to capture 45 seconds, clicked Download. The spinner appeared for about 2 seconds, then the browser downloaded `driveway_2026-03-12_clip.mp4`. Played it on my phone. Clean, immediate playback, no buffering (the `+faststart` flag doing its job). Texted it to my spouse. Done.

The spouse test: I sat my wife down at the laptop and said "find the clip of the package delivery and send it to me." She navigated to Playback, selected the driveway camera, saw the heatmap cluster around 10:30, clicked the timeline, found the delivery, clicked the scissors button, and downloaded the clip. Total time: about 90 seconds. She did not ask me a single question. The flow is intuitive enough for a non-technical user. That is the bar, and it clears it.

The 5-minute max is enforced client-side with a red color warning when the duration exceeds 300 seconds, and the Download button disables. The error message ("Maximum export duration is 5 minutes") is clear. The Cancel Export button (red X) exits the mode cleanly.

One UX note: the range sliders span the full 24-hour day (0-86400 seconds), which means the slider resolution is coarse -- each pixel of slider movement jumps several minutes. For fine-grained control (adjusting the start by 5 seconds), you have to be precise with the mouse. Draggable handles directly on the timeline bar would be more intuitive than separate range sliders below the video. But this works. It is functional, discoverable, and my spouse can use it. That was the requirement.

**Verdict: Fixed. The single most requested feature from v0.1 and v0.2 is now a two-click operation.**

### Test 2: Camera Settings Sliders (Passed)

Opened the Edit Camera form for my driveway camera. Below the Enabled/Record/Detect toggles, there is now a new section separated by a subtle border: "Notification Cooldown" and "Detection Interval" sliders, side by side on wider screens.

The Notification Cooldown slider: 30-300 seconds, step 10, with the current value displayed in monospace on the right (`60s`). The help text underneath ("Minimum seconds between notifications for the same label on this camera") is clear and accurate. I dragged it to 30 seconds for the front door, 120 seconds for the street-facing camera. Saved. Changes persisted.

The Detection Interval slider: 0-10 seconds, step 0.5. The `0` position shows "Global" instead of "0s" -- a nice touch that immediately communicates what the zero value means. I set the driveway to 1 second, the backyard to 5 seconds. Saved.

Discoverability: these sliders are in the right place. When you edit a camera, you naturally see the stream URLs, the toggles, and then the detection tuning. It is not buried in a separate "advanced" tab. A user who opens Edit Camera for the first time will scroll past these sliders and understand what they do from the labels and help text alone.

The `LabeledSlider` component is well-designed -- label on the left, value on the right, slider in between, help text below. Consistent styling with the rest of the form. The `formatValue` pattern (showing "Global" for 0, "60s" for 60) handles the edge cases correctly.

**Verdict: Fixed. Per-camera tuning is now a UI operation, not an API call. Discoverable and intuitive.**

### Test 3: MQTT Settings Page (Passed)

Opened Settings, scrolled down past Storage and Retention Rules. The new MQTT section is there, between the Detection section and the Email section. Clean layout.

The Enabled toggle disables all other MQTT fields when off -- broker URL, topic prefix, username, password, and HA Auto-Discovery all grey out. That is the correct UX pattern: do not let users configure something that is not enabled.

I filled in my Mosquitto broker URL (`tcp://192.168.1.50:1883`), left the topic prefix as `sentinel`, entered credentials, and toggled HA Auto-Discovery on. Hit Save Settings. Green success banner appeared. Restarted the Docker container (the UI correctly notes "Changes take effect after saving and restarting the server").

The live topic preview under the Topic Prefix field (`Events publish to sentinel/events/{camera}/{label}`) updates as I type the prefix. If I change the prefix to `nvr`, the preview immediately shows `nvr/events/{camera}/{label}`. This is a small detail that prevents configuration mistakes -- users can see exactly what their MQTT topic structure will look like before saving.

I verified in MQTT Explorer: events flowing to `sentinel/events/front_door/person`. Correct.

The key improvement: I no longer need to edit `sentinel.yml` and restart the container to configure MQTT. The entire MQTT setup is now a web UI operation. For a user who has never touched YAML, this is the difference between "I can set up Home Assistant integration" and "I need to ask someone for help."

**Verdict: Fixed. MQTT is now fully configurable from the web UI. No YAML editing required for the common case.**

### Test 4: Bounding Box Area Filter (Significant Improvement)

We had two rainy days during the v0.3 test period. The v0.1 rain test produced 47 false positive detections in 2 hours. The v0.2 cooldown reduced notification noise to about 8 that reached my phone, but the detections still fired at the backend. In v0.3, with `min_bbox_area: 0.03` (3% of frame area), the rain test produced 3 false positive detections across the same cameras over a similar rain duration.

From 47 to 3. That is a 94% reduction in rain-related false positives.

The implementation is exactly what I described in my v0.2 review: normalized bbox area `(xmax - xmin) * (ymax - ymin)` computed from [0,1] coordinates, compared against the threshold, detections below the threshold silently dropped before zone filtering. The code is 14 lines in `processFrame()` and it eliminates the entire class of "tiny scattered bounding box" false positives that rain, shadows, and sensor noise produce.

The 3% default is well-chosen. A person at 20 feet from a 1080p camera typically occupies 10-25% of the frame. A person at 50 feet (edge of a driveway) occupies about 5-8%. Rain artifacts produce bounding boxes under 1%. The 3% threshold sits comfortably between "real person at a distance" and "rain noise." I have not seen a legitimate person detection get filtered out.

I left the default at 3% and did not feel the need to tune it. That is the sign of a good default.

The remaining 3 false positives were from tree shadows that produced large enough bounding boxes (branches swaying create wider artifacts than rain drops). The consecutive frame confirmation from my "what would make this a 9" list would catch those -- a shadow that triggers one frame but not the next would be suppressed. But 3 false positives over a rainy afternoon is livable. The notification cooldown handles the remaining noise gracefully.

**Verdict: Fixed at the detection layer. The combination of bbox filter (kills 94% of rain artifacts) + cooldown (throttles what remains) makes false positives a minor annoyance instead of a dealbreaker.**

---

## Part 2: The Switch Decision

### Am I switching from Blue Iris to Sentinel?

**Yes.**

I am making the switch. Starting this week, Sentinel becomes my primary NVR and Blue Iris becomes the backup. Here is my reasoning.

The four things that blocked me in v0.1 were:
1. No notification cooldown -- fixed in v0.2, working well for 6 weeks now.
2. No clip export -- fixed in v0.3, my spouse can use it.
3. No MQTT / Home Assistant integration -- fixed in v0.2 (backend) and v0.3 (UI), my automations work.
4. Too many false positives -- fixed in v0.3, rain events dropped from 47 to 3.

All four are addressed. Zero P1 bugs across 2 months. The architecture has proven itself: no memory leaks, no database corruption, no data loss, automatic recovery from camera disconnects, router reboots, and a simulated power outage. The NUC runs at 9% CPU instead of 50%.

There are things Blue Iris does that Sentinel does not: two-way audio, PTZ controls, multi-camera sync playback. I use two-way audio about once a month (yelling at delivery drivers to leave packages at the side door). I have zero PTZ cameras. I have never used multi-camera sync playback. These are nice-to-haves, not blockers.

### Migration Plan

**Week 1 (now):** Sentinel is primary, Blue Iris runs in parallel recording the same cameras. Both systems write to separate storage. This is my safety net.

**Week 4:** If no issues: stop Blue Iris recording but keep it installed. Archive the last Blue Iris recordings to the NAS. Reclaim the CPU headroom (going from ~60% total with both systems to ~10% with Sentinel alone).

**Week 8:** If still no issues: uninstall Blue Iris. Free up the Windows license and the 2GB of RAM Blue Iris consumes. Move the NUC to a dedicated Sentinel Docker host running a minimal Linux distro.

**Rollback plan:** Blue Iris config is backed up. Camera RTSP URLs are documented. If Sentinel fails catastrophically, I can have Blue Iris recording again within 15 minutes.

---

## Part 3: v1.0 Readiness Assessment

### Is Sentinel ready for a v1.0 public release?

**Yes, with a narrow target audience and a clear "what it does not do yet" disclaimer.**

Sentinel v0.3 is ready for users who:
- Run Docker
- Use CodeProject.AI, Blue Onyx, or another HTTP-based detection backend
- Want a scrubbable 24/7 timeline with heatmap (the killer feature)
- Value resource efficiency (zero-transcoding architecture)
- Want MQTT integration for Home Assistant
- Are comfortable with a web UI that is functional but not yet polished to UniFi Protect levels

Sentinel v0.3 is NOT ready for users who:
- Expect a Coral TPU to work out of the box
- Need two-way audio or PTZ controls
- Want a native mobile app they can download from an app store
- Expect sub-second detection latency (Frigate territory)
- Are non-technical and expect a point-and-click installer

### What is missing for v1.0? Exhaustive list.

**Must ship with v1.0 (blockers):**

1. **HA MQTT auto-discovery.** The `ha_discovery` toggle exists in the UI but the actual discovery message publishing (the `homeassistant/binary_sensor/sentinel_{camera}/config` topics) is still a stub. Users toggle it on, nothing happens. This is a broken promise in the UI. Either implement it or remove the toggle. Shipping a non-functional toggle in v1.0 will generate bug reports on day one.

2. **Documentation.** There is no user-facing documentation. No "Getting Started" guide, no "Configuration Reference," no "FAQ." The README likely has basic Docker Compose instructions, but a v1.0 needs:
   - Installation guide (Docker Compose with example configs for common setups)
   - Camera configuration guide (ONVIF discovery, manual RTSP, sub-stream setup)
   - Detection backend guide (CodeProject.AI setup, Blue Onyx, ONNX model management)
   - MQTT / Home Assistant integration guide
   - Troubleshooting guide (common issues: camera not connecting, detection not firing, storage full)

3. **Release artifacts.** Docker images on Docker Hub or GitHub Container Registry with proper versioning (`:latest`, `:0.3.0`, `:0.3`). A `docker-compose.yml` template that works out of the box with sensible defaults.

4. **Security audit of public-facing endpoints.** The pairing redemption endpoint (`POST /api/v1/pairing/redeem`) is public (no auth required). The SSRF validation on webhooks needs verification. Rate limiting on the login endpoint. These are the things that get exploited when a project goes public.

5. **Upgrade path.** Users who install v1.0 need a clear path to v1.1. Database migrations, config format changes, breaking API changes -- document how upgrades work. The SQLite migration system exists, but users need to know "pull the new Docker image and restart, migrations run automatically."

**Should ship with v1.0 if possible (high value, not blockers):**

6. **Consecutive frame confirmation.** Require the same label to appear in 2 consecutive frames before publishing a detection event. This is the last major false positive filter. The bbox area filter killed rain, but single-frame transient detections (headlight reflections, brief shadows) still produce occasional false events. Two-frame confirmation would eliminate them.

7. **Basic object tracking / event deduplication.** A person walking through the frame for 10 seconds at 1-second detection intervals generates 10 separate events. Deduplicating "same person detected in N consecutive frames" into a single event with entry/exit timestamps would dramatically improve the Events page usability. Without this, the Events page for a busy camera is a wall of identical thumbnails.

8. **Per-zone notification cooldown.** The current cooldown is per-camera, per-label. Users with wide-angle cameras covering multiple zones (driveway + sidewalk) get suppressed notifications when separate zones trigger in sequence. Per-zone cooldown is the correct granularity.

### What can wait until v1.1 or v1.2?

**v1.1 (3-4 months after v1.0):**

- **Published mobile app** (iOS App Store + Google Play). The Flutter code exists. Get it into the stores. This is the single biggest "looks like a real product" signal for new users.
- **Coral TPU support / OpenVINO.** Important for the Frigate migration audience, but not for the v1.0 target audience (Docker + HTTP backends).
- **Continuous sub-stream detection.** Replaces the frame-interval polling model with continuous video processing. Cuts detection latency from 1.5 seconds to under 500ms. High engineering effort, high value, not a v1.0 blocker.
- **Multi-camera sync playback.** "Show me all cameras at 2:47 AM." Useful for incident investigation. Not a daily-use feature.

**v1.2 (6-8 months after v1.0):**

- **Two-way audio.** WebRTC bidirectional audio through go2rtc. Requires ONVIF backchannel support or camera-specific SIP integration. High effort.
- **PTZ controls.** ONVIF PTZ command API + web UI with directional pad and preset management.
- **Native binary distribution.** Windows and macOS installers for users who do not want Docker. The Go cross-compile capability exists; the packaging and auto-update mechanism need work.
- **License plate recognition.** Specialized ALPR model, possibly via a separate detection backend.
- **HomeKit Secure Video.** Niche audience, significant protocol implementation effort.

### Who is the v1.0 target audience?

**Primary:** Power users migrating from Blue Iris who want better resource efficiency, a modern web UI, and a scrubbable timeline with heatmap. These users run Docker, are comfortable with YAML for initial setup, and value the zero-transcoding architecture that cuts their CPU usage by 75%.

**Secondary:** Frigate users who want a 24/7 timeline. Frigate's clip-based model has no continuous scrubbing. Users who watch back footage daily (checking overnight activity, reviewing deliveries) will immediately see the value of Sentinel's timeline + heatmap. The MQTT integration means they do not have to abandon their Home Assistant automations.

**Not yet:** Non-technical users, UniFi Protect users (they want polish, not power), users who require Coral TPU support, users without Docker experience.

---

## Part 4: Updated Score and Final Verdict

### New score: 8.5 out of 10

Up from 7/10 (v0.2) and 5/10 (v0.1).

The 1.5-point jump reflects:
- **+0.5** for the Export Clip UI. This was the single most requested feature. It works, it is discoverable, and non-technical users can operate it.
- **+0.5** for the per-camera settings sliders. Moving cooldown and detection interval from API-only to the camera edit form closes the backend/frontend gap I flagged as the #1 problem in v0.2.
- **+0.25** for MQTT settings in the UI. Eliminating YAML editing for MQTT configuration lowers the barrier for Home Assistant integration.
- **+0.25** for the bbox area filter. A 94% reduction in rain false positives at the detection layer is a material improvement in daily-use quality.

Why not a 9: the HA auto-discovery stub is still non-functional, event deduplication does not exist (busy cameras produce walls of identical events), and the mobile app is not published. These are the items that separate "I am personally comfortable switching" from "I would actively recommend this to every NVR user I know."

Why not a 10: no two-way audio, no PTZ, no Coral TPU, no continuous sub-stream detection, no multi-camera sync playback. A 10 means "this does everything every competitor does, plus things none of them do." Sentinel is not there yet -- but the trajectory is clear, and the heatmap timeline is a feature none of them have.

### Updated Competitor Comparison

**vs Blue Iris (8.5 vs 6/10):**

Sentinel wins on: resource efficiency (8% vs 50% CPU), modern web UI, heatmap timeline, tiered storage, MQTT topic structure, clip export quality (zero-transcode vs re-encode), camera credential encryption, crash isolation, API quality.

Blue Iris wins on: two-way audio, PTZ controls, mature Windows ecosystem, third-party mobile app (UI3), 15 years of stability track record, trigger scheduling system, broader camera compatibility quirk database.

**The switch verdict:** For my use case (12 IP cameras, Docker-capable hardware, Home Assistant), Sentinel is the better product today. The CPU savings alone justify the switch. The heatmap timeline is the feature I did not know I needed until I used it. Blue Iris's advantages (two-way audio, PTZ) are features I rarely use.

**vs Frigate (8.5 vs 8/10):**

Sentinel wins on: 24/7 scrubbable timeline (Frigate's biggest gap), heatmap detection density, visual zone editor, ONVIF discovery, web UI polish, built-in notifications, multi-user auth, tiered storage, clip export workflow, face recognition (built-in vs add-on).

Frigate wins on: detection speed (sub-500ms with Coral TPU vs 1.5s), Coral TPU support, continuous sub-stream processing, object tracking, Home Assistant native integration depth, community size, battle-tested at scale.

**The honest take:** These are now peer products for different audiences. If you prioritize detection speed and have a Coral TPU, Frigate is still the better choice. If you prioritize timeline-based playback review and want a self-contained system that does not depend on Home Assistant for notifications, Sentinel is better. The gap has narrowed from "different leagues" in v0.1 to "different philosophies" in v0.3.

**vs UniFi Protect (8.5 vs 9/10):**

Sentinel wins on: hardware-agnostic (any IP camera), open source, custom AI models, heatmap (detection density vs motion density), per-camera x per-event-type retention, self-hosted with no cloud dependency, MQTT integration, tiered hot/cold storage.

UniFi Protect wins on: polish (the timeline scrubbing is buttery smooth), zero-config setup (cameras auto-adopt), native mobile app quality, sub-second live view latency, two-way audio, integrated hardware encoding, "it just works" factor.

**The honest take:** UniFi Protect is still the more polished product. If money is not an object and you are willing to buy Ubiquiti cameras, Protect is a better experience for non-technical users. But Sentinel is now a credible alternative for users who want the flexibility of any IP camera brand, the power of custom AI models, and the principle of self-hosted open source. The gap in functional completeness has closed significantly. The gap in polish remains.

### The Elevator Pitch

"Sentinel is a free, open-source NVR that records your IP cameras 24/7 with practically zero CPU usage, gives you a scrubbable timeline with AI detection heatmap so you can see exactly when something happened overnight in 2 seconds, and lets your spouse export and share a clip without needing a computer science degree. It runs in Docker, talks MQTT to Home Assistant, and uses 80% less CPU than Blue Iris."

### Would I pay for this?

Yes. If Sentinel had a paid tier for priority support, I would pay $5/month or $50/year. Not for features -- the open source version should remain fully functional. For guaranteed response time on bug reports and a direct channel to the development team. The "prosumer NVR support" market is underserved. Blue Iris charges $70 once and provides forum support. Frigate is community-supported only. There is room for a paid support tier.

If Sentinel sold a hardware appliance (NUC-like box with Sentinel pre-installed, Coral TPU included, ready to plug in and add cameras via the web UI), I would pay $300-400 for it. That is the product that competes with UniFi Protect on the "it just works" axis while offering the flexibility and AI capabilities that Protect lacks.

### Final Recommendation

**Install Sentinel if you are:**
- A Blue Iris user tired of 50%+ CPU usage and a 2003-era web UI
- A Frigate user who wants a real 24/7 timeline instead of individual clips
- A Docker-comfortable power user who wants a modern, API-first NVR
- Someone who values resource efficiency and does not need two-way audio or PTZ
- A Home Assistant user who wants MQTT-based camera event automation

**Do not install Sentinel if you are:**
- A non-technical user who needs a point-and-click installer
- A Coral TPU owner who expects sub-500ms detection (use Frigate)
- Someone who needs two-way audio for talking to delivery drivers
- Someone who needs PTZ camera controls
- Happy with UniFi Protect and willing to stay in the Ubiquiti ecosystem

---

## Part 5: Letter to the Team

### What you got right from the start (protect these decisions)

**Zero-transcoding architecture.** The decision to use `ffmpeg -c copy` with segment muxer, producing independently-playable 10-minute MP4 segments, is the foundation of everything good about Sentinel. It gives you 8% CPU instead of 50%. It gives you instant clip export (no re-encode). It gives you browser-native playback without a custom video player. Every NVR that transcodes (Blue Iris, most commercial systems) is burning CPU for no user-visible benefit. Never compromise on this.

**The heatmap timeline.** This is your competitive moat. No other NVR has it. It transforms the daily "did anything happen?" check from a 5-minute scrubbing exercise into a 2-second glance. Every time I show the heatmap to a friend, they say "why doesn't my system have that?" Protect this feature. Enhance it (typed heatmap with person/vehicle/animal filtering, as I suggested in v0.2). Never remove it.

**Per-camera crash isolation.** Each camera's ffmpeg is a separate process. One crash does not take down the system. The watchdog restarts it automatically. Crash-loop suppression prevents event page flooding. This is the kind of reliability engineering that users never notice until they compare it to Blue Iris's "the whole application froze because camera 7 sent a malformed packet" experience. Never collapse this into a single process for "efficiency."

**API-first design.** Every feature exists as an API endpoint before it exists as a UI element. This is why the v0.2/v0.3 UI integration work was possible -- the backend was already done, the frontend just needed to call it. Maintain this discipline. It also enables the Home Assistant integration, the mobile app, and any future third-party integrations.

**YAML seed + UI config.** The pattern of "configure infrastructure in YAML (storage paths, ports), configure everything else in the web UI" is exactly right. Frigate's all-YAML approach is painful. Blue Iris's all-GUI approach makes automation impossible. Your hybrid approach serves both audiences.

### What you fixed well (the feedback loop worked)

The v0.1 -> v0.2 -> v0.3 progression is a case study in effective user feedback integration.

**v0.2 prioritization was correct.** I said "notification cooldown is the single most important thing." You shipped it first, and it was the right call. The garbage truck test went from 12 notifications to 3. That single fix moved my score from 5 to 7.

**v0.3 addressed the pattern, not just the symptoms.** I did not just ask for specific features in v0.2 -- I identified a pattern: "the backend is ahead of the frontend." You responded by shipping four UI integrations that closed the gap across the board (clip export button, camera settings sliders, MQTT settings page). That shows you understood the meta-feedback, not just the individual items.

**The bbox filter shows you read between the lines.** In v0.2 I said cooldown was "a bandage that works surprisingly well, but the wound is still there." You shipped the bbox area filter that addresses false positives at the detection layer, not just the notification layer. The 94% reduction in rain false positives proves the approach is correct.

**You shipped at the right granularity.** Each release was 3-4 focused changes, not a 50-item feature dump. This made testing manageable, reduced regression risk, and gave clear feedback loops. Maintain this cadence.

### What you should focus on for the next 6 months

**Months 1-2: Ship v1.0 with documentation and the HA auto-discovery fix.** The product is ready. The documentation is not. Write the Getting Started guide, the Configuration Reference, and the Troubleshooting FAQ. Fix the non-functional `ha_discovery` toggle. Tag v1.0. Get Docker images into a public registry. Write a blog post. Tell the Blue Iris subreddit and the Frigate Discord.

**Months 2-3: Event deduplication and consecutive frame confirmation.** These are the two remaining detection quality issues. Event deduplication turns the Events page from a wall of identical thumbnails into a useful activity log. Consecutive frame confirmation kills the last class of transient false positives. Together, they make the Events page trustworthy enough that users check it instead of scrolling past it.

**Months 3-4: Publish the mobile app.** The Flutter code exists. Get it into the App Store and Play Store. A published mobile app is the single biggest "this is a real product" signal for potential users. It does not need to be perfect -- it needs to exist in the stores with push notifications working.

**Months 4-6: Coral TPU support and continuous sub-stream detection.** This is the Frigate migration play. The users who have Coral TPUs and want a scrubbable timeline are your highest-value acquisition target. Give them a reason to switch by matching Frigate's detection speed while offering the timeline they cannot get from Frigate.

### One piece of advice from a user who has been through 5 NVR products

**Ship the boring stuff.**

Every NVR project I have used -- Blue Iris, Frigate, Shinobi, ZoneMinder, and now Sentinel -- started with impressive core technology and then stalled on the boring features that make daily use tolerable. Notification cooldown is boring. Clip export is boring. MQTT configuration in a web UI is boring. Per-camera slider settings are boring. None of these make exciting demo videos or impressive README screenshots.

But they are the difference between a project that technical users admire from a distance and a product that regular people actually run every day. Blue Iris has survived for 15 years not because its architecture is good (it is not) but because it got the boring stuff right early: you can export a clip, configure notifications, set up schedules, and share a link with your spouse. Frigate's biggest complaint is that it does not do the boring stuff -- no timeline, no clip export, no built-in notifications.

You got the exciting stuff right on day one: the heatmap timeline, the zero-transcoding architecture, the crash isolation, the API-first design. And then -- this is the part that matters -- you did not get distracted by more exciting stuff. You spent v0.2 and v0.3 on cooldown, clip export, camera settings sliders, and a bbox filter. Boring features. Essential features. The features that made me switch.

Keep doing that. Resist the temptation to add flashy features (AI-powered activity summaries, natural language clip search, automatic highlight reels) before you have finished the boring list (event deduplication, per-zone cooldown, consecutive frame confirmation, documentation, published mobile app). The flashy features will attract attention. The boring features will retain users.

You have built something that I am trusting to watch my home. That is not a compliment I give lightly. Do not waste it.

---

**Final score: 8.5 / 10**

**Final answer: Yes, I am switching.**
