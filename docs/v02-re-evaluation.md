# Re-Evaluation: Sentinel NVR v0.2.0 -- Two Weeks Later

**Reviewer:** Same veteran NVR user. Same 12 cameras. Same NUC. Still running Blue Iris in parallel.

**Context:** I gave v0.1.0 a 5/10 and said notification cooldown was the single most important fix. The team shipped four targeted fixes. I have been running v0.2.0 for two weeks.

Short version: they listened, they shipped, and three of the four fixes work well. My score went up. Here is the detail.

---

## The Fixes in Practice

### Test 1: The Garbage Truck Test (Notification Cooldown)

The garbage truck came on a Tuesday. Three cameras caught it -- driveway, side yard, street-facing. In v0.1, this produced 12 notifications in 3 minutes. In v0.2, with cooldown set to 60 seconds per camera, I received exactly 3 notifications: one per camera, each for the first detection. The follow-ups were suppressed.

The implementation is clean. `CooldownTracker` keys on `camera_id:label`, so a "person" detection on the driveway does not suppress a "vehicle" detection on the same camera. That is the correct design -- I want to know about the delivery driver AND the truck, but I do not want 12 alerts about the same truck rolling past. The `ShouldFire` method checks elapsed time, records the new timestamp atomically, and moves on. No database writes in the hot path. Thread-safe with a read-write mutex. Simple, correct, fast.

The 60-second default is right for most cameras. I bumped the front door to 30 seconds (I want faster re-alerts if someone lingers) and the street-facing camera to 120 seconds (cars pass continuously during rush hour). The per-camera configurability via `notification_cooldown_seconds` in the database is exactly what I asked for.

One gap: there is no per-zone cooldown. The cooldown is per-camera, per-label. If I have two zones on one camera (driveway and sidewalk), a person detection in the driveway zone suppresses the sidewalk detection on the same camera for the same 60-second window. For my setup this is fine -- I have one zone per camera in most cases. But users with complex zone setups on a single wide-angle camera will notice. Not a dealbreaker. Worth noting for v0.3.

**Verdict: Fixed. This is the single biggest improvement in v0.2.**

### Test 2: The Rain Test (Cooldown + False Positives)

We had a solid rainy afternoon on day 9 of the test. In v0.1, rain produced 47 false positive notifications in 2 hours. In v0.2, cooldown reduced that to about 8 notifications that actually reached my phone -- one per camera per 60-second window, and only the cameras where CodeProject.AI was confident enough to classify rain artifacts as "person."

Eight is better than 47. It is still not great. The core problem is not notification frequency -- it is that the detections are firing at all. Rain-induced false positives need to be killed at the detection layer, not papered over by notification throttling. A minimum bounding box area filter (reject detections where the bbox is smaller than, say, 5% of the frame) would eliminate most rain artifacts, which tend to produce tiny, scattered bounding boxes. A "require 2 consecutive frames with the same label" confirmation would kill the rest.

Cooldown is a bandage that works surprisingly well. But the wound is still there.

**Verdict: Improved significantly. Still needs detection-layer filtering (minimum bbox area, consecutive frame confirmation) to truly solve false positives.**

### Test 3: The Spouse Test (Clip Export)

A package was delivered on day 4. I opened the Playback page, found the event via the heatmap (still the best feature in Sentinel), and looked for an export option. The API endpoint is `POST /api/v1/recordings/export` with camera name, start time, and end time. I could not find a UI button for it -- I had to use the API directly. This is a problem I will come back to.

Through the API: I sent the request with a 30-second window. The response came back in about 3 seconds with an `export_id` and a `download_url`. Downloaded the file. It played in VLC, played on my phone, and I texted it to my spouse. The MP4 was clean, the quality matched the original recording (because `ffmpeg -c copy` does not re-encode), and the `+faststart` flag meant it played immediately in iMessage without buffering.

I tested the guardrails. Requested a 6-minute clip: rejected with "maximum export duration is 5 minutes." Correct. I simulated 3 concurrent exports and tried a 4th: rejected with "too many concurrent exports (max 3)." Correct. The auto-cleanup after 1 hour means I do not have to worry about the export directory filling up.

The multi-segment case works too. I requested a clip spanning two 10-minute segments (crossing a segment boundary). The code creates a concat file, feeds it to ffmpeg with `-f concat`, and extracts the sub-clip. The result was seamless -- no glitch at the segment boundary.

The implementation is solid. `ExportService` handles single and multi-segment cases, enforces the 5-minute max and 3-concurrent limit, generates a UUID-based export ID, and cleans up after itself. The `ServePath` method for the download endpoint is straightforward. No complaints about the backend.

The problem: there is no export button in the web UI. The feature exists only as an API endpoint. A technical user can curl it. My spouse cannot. Until there is a "Export Clip" button on the Playback page -- ideally with draggable start/end handles on the timeline -- this feature is invisible to normal users. The backend is done; the frontend is missing.

**Verdict: Backend is excellent. Frontend integration is missing. 80% of the way there.**

### Test 4: The Home Assistant Test (MQTT)

Configured MQTT in `sentinel.yml`: broker URL, credentials, `ha_discovery: true`. Restarted. Opened MQTT Explorer. Events started flowing immediately.

The topic structure is `sentinel/events/{camera_name}/{label}`. Logical, predictable, easy to subscribe to. I can subscribe to `sentinel/events/front_door/person` for front door person alerts, or `sentinel/events/#` for everything. The payload includes camera name, camera ID, label, confidence, whether a snapshot exists (as a boolean -- they correctly do not leak filesystem paths over MQTT), and a Unix timestamp.

Camera status events go to `sentinel/cameras/{camera_name}/status` with payloads like `camera.online`, `camera.offline`, `camera.restarted`. The LWT (Last Will and Testament) publishes `offline` to `sentinel/availability` when the Sentinel process dies unexpectedly. This means Home Assistant knows when Sentinel itself is down, not just individual cameras.

I created an HA automation: "When a person is detected at the front door, turn on the porch light for 5 minutes." The MQTT trigger fired within about 1 second of the detection event. Porch light turned on. The automation worked on the first try.

Face match and audio detection events also publish to MQTT (under `/face` and `/audio` subtopics). I have not tested these because I do not use face recognition, but the code handles them.

One thing I noticed: the `ha_discovery` flag exists in the config struct but I did not see Home Assistant auto-discover Sentinel's cameras as entities. The config field is there, the flag is parsed, but the actual HA discovery message publishing (the `homeassistant/binary_sensor/...` config topics) does not appear to be implemented in the publisher. The publisher handles detection events, camera status, face match, and audio -- but not HA MQTT discovery payloads. So you have to manually create MQTT sensors in HA. Not a dealbreaker for a technical user, but "auto-discovery" is the promise and it is not delivered yet.

Latency from detection event to MQTT message: under 1 second. The publisher subscribes to the event bus with a wildcard (`*`), so it receives events the instant they are published internally. No polling, no batching. The only delay is the MQTT QoS 1 publish round-trip to the broker.

**Verdict: Works well. Topic structure is logical. HA automation works. The ha_discovery flag is a stub -- manual sensor setup required. Solid foundation.**

### Test 5: The Latency Test (Detection Speed)

The default `frame_interval` changed from 5 seconds to 1 second. This is a config change, not an architectural change -- the detection pipeline still grabs periodic JPEG frames from go2rtc, but now it grabs 5x more of them.

I walked in front of the driveway camera 10 times and timed from entering the frame to seeing the event in the UI. Average detection latency: approximately 1.5 seconds. That is down from 3-4 seconds in v0.1. The breakdown: worst case 1 second until the next frame grab, plus 200-400ms for CodeProject.AI inference, plus 100ms for event bus propagation. Best case was under 1 second (when I happened to enter the frame just before a frame grab).

1.5 seconds is livable. It is not Frigate-with-Coral fast (under 500ms), but it is fast enough that the notification arrives while the person is still on the porch. The v0.1 latency of 3-4 seconds meant the person was often walking away by the time I got the alert. This is a meaningful improvement.

CPU impact: idle CPU went from about 8% to about 14% with 12 cameras at 1-second intervals. That is a noticeable increase. The NUC handles it fine (Intel i7, plenty of headroom), but users with weaker hardware might want to dial it back. The per-camera `detection_interval` override means I can set high-traffic cameras (driveway, front door) to 1 second and low-priority cameras (backyard, garage interior) to 3-5 seconds to save CPU. I set 4 cameras to 1s, 4 to 2s, and 4 to 5s. Idle CPU dropped to about 11%. Good tradeoff.

**Verdict: Meaningful improvement. 1.5s average is livable. Per-camera interval override is the right design. CPU increase is manageable.**

---

## The New #1 Problem

With cooldown, clip export, MQTT, and faster detection addressed, the thing that bothers me most now is the **lack of UI integration for features that exist in the backend**.

Clip export is the clearest example: the backend is done, the API works, the ffmpeg extraction is fast and correct. But there is no button in the web UI. A feature that exists only as an API endpoint is a feature that does not exist for 90% of users.

The same pattern applies to the per-camera cooldown configuration (I had to set it via the API, not via the camera settings UI), the per-camera detection interval override (same -- API only), and the MQTT configuration (YAML only, no UI). The backend team is shipping faster than the frontend is integrating.

This is not a single bug to fix. It is a pattern to address: every backend feature needs a corresponding UI surface within the same release. Ship the button with the endpoint.

If I had to pick the single most concrete next deliverable: **put an "Export Clip" button on the Playback page with draggable timeline handles for start/end selection.** That one UI element turns clip export from a developer feature into a user feature.

---

## Updated Feature Matrix

| Feature | v0.1 Score | v0.2 Score | Notes |
|---------|-----------|-----------|-------|
| Notification cooldown | No (dealbreaker) | **Yes** | Per-camera, per-label, 30-300s configurable. Works correctly. Missing per-zone granularity. |
| Clip export | No (dealbreaker) | **Yes (API only)** | Backend is excellent. No UI button. 80% done. |
| MQTT support | No | **Yes** | Clean topic structure, LWT, fast. HA auto-discovery stub not functional. |
| Detection latency | 3-4s (too slow) | **~1.5s (acceptable)** | 5x improvement via frame_interval change. Per-camera override available. |
| False positive management | Medium-High | **Medium** | Cooldown helps notification noise. Detection-layer filtering still missing. |

---

## Updated Competitive Position

**vs Blue Iris:** Closer to switching. The cooldown gap is closed. Clip export is almost there (need the UI button). MQTT is a new advantage -- Blue Iris has MQTT but Sentinel's topic structure is cleaner. Detection latency is now comparable (both use CodeProject.AI, both are in the 1-2 second range). What still blocks me: no clip export button in the UI, no two-way audio, no PTZ controls, and I want another 2 months of stability data.

**vs Frigate:** MQTT narrows the gap significantly. Frigate users who want a scrubbable timeline with heatmap now have a reason to look at Sentinel. Detection is still slower (1.5s vs under 500ms with Coral), and Sentinel still does not support Coral TPU hardware. But for users on CodeProject.AI or Blue Onyx backends (not Coral), the gap is small.

**vs UniFi Protect:** Clip export makes Sentinel more shareable, once the UI button exists. The gap in polish remains wide. UniFi's "tap notification, see clip, share it" flow takes 3 taps. Sentinel's flow is "open web UI, navigate to Playback, find the timestamp, call the API, download the file, share it." That is too many steps.

---

## Updated Verdict

### 1. New score: 7 out of 10

Up from 5/10. The two-point jump comes from:
- Cooldown alone is worth +1 point. It transformed notifications from unusable to functional.
- Clip export backend + MQTT + faster detection together are worth +1 point. They close real gaps.

The reason it is not an 8: clip export has no UI, false positive filtering is still missing at the detection layer, and the pattern of backend-without-frontend is accumulating debt.

### 2. Would I switch from Blue Iris now?

**Not yet, but I am planning for it.** I have moved from "interesting project I run alongside my real NVR" to "this could be my primary NVR by summer." The blocking items are concrete and finite:
- Clip export UI button (weeks of work, not months)
- Per-camera settings in the UI (cooldown, detection interval)
- 2 more months of stability data (time, not engineering)

If v0.3 ships the UI integration, I will run Sentinel as primary and Blue Iris as backup for 60 days. If nothing breaks, Blue Iris gets retired.

### 3. What is the NEXT single most important thing?

**UI integration for existing backend features.** Specifically, in priority order:
1. "Export Clip" button on the Playback page with timeline range selection
2. Per-camera cooldown and detection interval settings in the camera edit UI
3. MQTT configuration page in the settings UI

The backend is ahead of the frontend. Close the gap.

### 4. Would I recommend v0.2 to a technical friend?

**Yes, with caveats.** The friend who runs Frigate and knows their way around MQTT and Docker -- yes, I would tell them to try Sentinel. The heatmap timeline is worth the install. The MQTT integration means they can wire it into their existing Home Assistant setup. The caveats: "you will need to use the API for clip export until they add a UI button, and detection is slower than your Coral TPU."

This is a meaningful shift from v0.1, where my answer was "interesting project, check back in 6 months."

### 5. What would make this a 9?

Specific, concrete list:
1. **Clip export button in the Playback UI** with draggable start/end handles on the timeline. One click to export, one click to download. This is the single highest-impact UI addition.
2. **Minimum bounding box area filter** in the detection pipeline. Reject detections where the bbox is smaller than a configurable percentage of the frame (default 3-5%). Kills most rain/shadow false positives at the source.
3. **Consecutive frame confirmation** -- require the same label to appear in 2 consecutive frames before publishing an event. Eliminates transient false positives.
4. **HA MQTT auto-discovery** -- publish `homeassistant/binary_sensor/sentinel_{camera}/config` topics so cameras appear as entities automatically.
5. **Published mobile app** in the App Store and Play Store. The Flutter code exists. Ship it.
6. **Object tracking (basic)** -- deduplicate "same person detected in 5 consecutive frames" into a single event with entry/exit timestamps instead of N separate events.

Items 1-3 would get it to an 8. Adding 4-6 gets it to a 9.

---

## Answer to the Team's Question

> "Would you find value in a heatmap that shows TYPES of detections (person=red, vehicle=blue, animal=green) instead of just density? Or is pure density sufficient for your workflow?"

**Yes, typed heatmap would be valuable, but only as a toggle -- not a replacement.**

Here is why: my morning routine is "did anything happen last night?" Pure density answers that question in 2 seconds. I glance at the timeline, see a cluster at 2:47 AM, and investigate. I do not care what type it was until I look at it.

But typed heatmap would help in a different scenario: reviewing a full day on the driveway camera. Right now, the heatmap shows dense activity from 7-9 AM (school drop-off traffic) and 3-5 PM (pickup traffic). That density is almost entirely vehicles, which I do not care about. If vehicles were blue and persons were red, I could visually filter the noise and see the one red spike at 10:30 AM when the delivery driver came. Without type differentiation, that delivery event is buried in the vehicle noise.

So: implement it as a toggle. Default to "all types" density (current behavior). Add filter buttons -- person, vehicle, animal -- that change the heatmap colors. Let users click "person only" to see just person detection density. This preserves the instant "did anything happen?" glance while adding the ability to filter by type when investigating a busy camera.

One implementation note: do not over-design the color scheme. Three colors are enough (person=red, vehicle=blue, animal=green). Everything else can be gray. And make sure the colors are distinguishable for colorblind users -- use brightness/pattern differentiation, not just hue.

---

## Summary

v0.2 is a legitimate step forward. The team read my review, prioritized correctly, and shipped four fixes that address real problems. Cooldown alone transformed the daily experience. The fact that three of the four fixes (cooldown, clip export backend, MQTT) are well-engineered and correct on the first try tells me the codebase is healthy and the team knows what they are doing.

The new challenge is not backend capability -- it is frontend integration. The backend is pulling ahead. The UI needs to catch up. Ship the buttons with the endpoints. That is the difference between a developer tool and a user tool.

I will be here for the v0.3 review.
