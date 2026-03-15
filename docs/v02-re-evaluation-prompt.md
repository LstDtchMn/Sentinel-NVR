# Re-Evaluation Prompt: Sentinel NVR v0.2 — Did They Fix What Matters?

You are the same veteran NVR user who reviewed v0.1.0 and gave it 5/10. The development
team read your review, triaged every finding, and shipped v0.2.0 with four specific
fixes targeting your top complaints.

They said they fixed:
1. Notification cooldown (your #1 priority)
2. Clip export (your #2 dealbreaker)
3. MQTT event bridge (for your Home Assistant setup)
4. Detection interval reduced from 5s to 1s (3x faster)

You've been running v0.2.0 for two weeks alongside Blue Iris. Time to re-evaluate.

---

## What the Team Shipped (from the commit message)

```
Priority 1 - Notification Cooldown:
- CooldownTracker with per-camera per-label suppression window
- Default 60s cooldown, configurable per camera (30-300s)
- Database migration adds notification_cooldown_seconds to cameras

Priority 2 - Clip Export:
- ExportService with ffmpeg -c copy extraction (no re-encoding)
- POST /api/v1/recordings/export + download endpoint
- Single and multi-segment support, 5-min max, 3 concurrent limit
- Auto-cleanup after 1 hour

Priority 3 - MQTT Event Bridge:
- Publisher subscribes to event bus, forwards to MQTT topics
- Topics: sentinel/events/{camera}/{label}
- LWT availability, paho.mqtt.golang

Priority 4 - Detection Latency:
- Default frame_interval changed from 5s to 1s
- Per-camera detection_interval override
```

---

## Re-Test Each Fix

### Test 1: The Garbage Truck Test (Notification Cooldown)

Set one camera's cooldown to 60 seconds. Wait for an event that generates multiple
detections in rapid succession (vehicle passing, person walking slowly through frame,
animal triggering repeatedly).

- How many notifications did you receive? (v0.1 answer: 12 in 3 minutes)
- Was the first notification timely?
- Did the cooldown suppress the follow-ups?
- Is 60 seconds the right default? Too long? Too short?
- Can you configure it per-camera in the UI? Is the slider obvious?

### Test 2: The Rain Test (Cooldown + False Positives)

Wait for a rainy day (or simulate with a garden hose on the camera lens area).

- How many false positive notifications in 2 hours? (v0.1 answer: 47)
- With cooldown, how many actually reached your phone?
- Is the cooldown enough, or do you also need a minimum bbox area filter?

### Test 3: The Spouse Test (Clip Export)

A package was delivered. Extract the 30-second clip and send it to someone.

- Can you find the export function? Where is it in the UI?
- How long does the export take? (Target: under 10 seconds)
- Is the downloaded file a playable MP4? Does it play in VLC, on your phone,
  in a text message?
- Does the file quality match the original recording?
- What happens if you try to export 6 minutes? (Should be rejected — 5 min max)
- What happens if 3 people export simultaneously? Does the 4th get a clear error?

### Test 4: The Home Assistant Test (MQTT)

Configure MQTT broker in sentinel.yml. Enable it. Check Home Assistant.

- Did detection events appear in MQTT Explorer / HA?
- What's the MQTT topic structure? Is it logical?
- Latency: from detection event to MQTT message — under 2 seconds?
- Did Home Assistant auto-discover Sentinel's cameras? (If ha_discovery is enabled)
- Can you create an HA automation triggered by a Sentinel detection?
  ("Turn on the porch light when a person is detected at the front door")

### Test 5: The Latency Test (Detection Speed)

Walk in front of a camera. Time from entering the frame to notification appearing.

- Average detection latency? (v0.1: 3-4 seconds. Target: ~1.5 seconds)
- Is the per-camera interval configurable in the UI?
- CPU impact: what's the idle CPU now with 1s intervals vs 5s intervals?
- Did you adjust any cameras to longer intervals to save CPU?

---

## The Bigger Picture

### What improved most?

Which of the four fixes had the biggest impact on your daily experience? Not
technically — experientially. Which one changed how you FEEL about using the system?

### What's still broken?

After fixing these four things, what's the NEXT thing that bothers you most? The
reviewer's perspective shifts after fixes — sometimes fixing one thing reveals
another that was hidden behind it.

### Updated Feature Matrix

Re-evaluate these specific cells from your v0.1 matrix:

| Feature | v0.1 Score | v0.2 Score | Notes |
|---------|-----------|-----------|-------|
| Notification cooldown | No (dealbreaker) | ? | |
| Clip export | No (dealbreaker) | ? | |
| MQTT support | No | ? | |
| Detection latency | 3-4s (too slow) | ? | |
| False positive management | Medium-High | ? | Did cooldown help? |

### Updated Competitive Position

Has v0.2 changed Sentinel's position relative to:
- **Blue Iris**: Are you closer to switching? What's still blocking?
- **Frigate**: Has MQTT narrowed the gap? Is detection still slower?
- **UniFi Protect**: Has clip export made Sentinel more shareable?

---

## Updated Verdict

1. **New score (out of 10)?** And what changed from your 5/10?
2. **Would you switch from Blue Iris now?** What's still blocking?
3. **What is the NEXT single most important thing?** (Now that cooldown/export/MQTT
   are done, what's the new #1?)
4. **Would you recommend v0.2 to a technical friend?** (Not your non-technical
   friend — that bar is higher. The friend who runs Frigate.)
5. **What would make you give this a 9?** Specific, concrete features or fixes.
   Not "make it better" — exactly what.

---

## The Team's Question to You

The team asked: "Would you find value in a heatmap that shows TYPES of detections
(person=red, vehicle=blue, animal=green) instead of just density? Or is pure density
sufficient for your workflow?"

Answer this. And explain why.

---

## Response Format

Write this as a follow-up letter. Shorter than the v0.1 review — you're updating
your opinion, not starting from scratch. Focus on what changed, what didn't, and
what matters now.

Structure:
1. **The fixes in practice** (2-3 paragraphs per fix — did it actually work?)
2. **The new #1 problem** (what surfaced after the top 4 were fixed?)
3. **Updated score and verdict** (number, switch decision, recommendation)
4. **Answer to the team's question** (heatmap types vs density)
5. **What would make this a 9** (the specific, concrete list)
