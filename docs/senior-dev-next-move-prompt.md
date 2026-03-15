# Senior Developer Prompt: What's Your Next Move?

You are the senior developer and sole technical lead on **Sentinel NVR**. You've
shipped 4 releases in rapid succession. A veteran NVR user moved from 5/10 to 8.5/10
and is actively switching from Blue Iris. The code compiles, the architecture is
sound, and you have momentum.

Now stop and think. Not about features. About strategy.

---

## Where You Are

**What you've built:**
- Go backend with 15 packages (camera, detection, recording, auth, mqtt, notifications,
  storage, watchdog, eventbus, config, db, server, backup, detector, mqtt)
- React 19 frontend with 15 pages (LiveView, Playback, Events, Cameras, Settings,
  ZoneEditor, Faces, Models, Dashboard, Users, Login, Setup, Import,
  NotificationSettings, EventDetail)
- Flutter mobile app (code exists, not published)
- Docker deployment, MQTT bridge, REST API, WebSocket streaming
- 4 releases: v0.1.0, v0.2.0, v0.2.1, v0.3.0

**What your reviewer said:**
- 8.5/10, switching from Blue Iris as primary NVR
- "Ready for v1.0 for Docker-comfortable power users"
- v1.0 blockers: HA auto-discovery stub, user docs, release artifacts, security audit
- 6-month roadmap: docs → event dedup → mobile app → Coral TPU

**What's on the backlog (12 items):**
Consecutive frame confirmation, per-zone cooldown, object tracking, two-way audio,
PTZ controls, Coral TPU, published mobile app, native binaries, HA auto-discovery,
multi-camera sync playback, license plate recognition, continuous sub-stream detection

**What's NOT on any list but should concern you:**
- You have zero automated tests for the frontend
- You have no CI/CD pipeline running on every push
- You have no user documentation beyond code comments
- The mobile app has never been tested by a real user
- You have exactly ONE reviewer's opinion
- You haven't tagged a v1.0 yet

---

## The Questions You Need to Answer

### 1. Should you tag v1.0 now?

The reviewer said "ready for Docker-comfortable power users." But consider:

**Arguments for tagging v1.0 now:**
- The product works. 8.5/10 from a brutal reviewer.
- v1.0 is a marketing signal. "v0.3" says "experimental." "v1.0" says "use this."
- You've been iterating on pre-release versions. At some point you have to ship.
- Every day without v1.0 is a day potential users see "v0.x" and move on.

**Arguments against tagging v1.0 now:**
- The HA auto-discovery stub is a lie in the UI. A toggle that does nothing.
- Zero user documentation. README exists but no setup guide, no troubleshooting.
- No CI/CD. You're building and pushing manually. One mistake and you ship broken code.
- No security audit. The reviewer didn't test auth bypass, SSRF, path traversal.
- One reviewer is not validation. One person's 8.5 might be another's 4.

**Your call.** What do you do? Tag v1.0 with known gaps and fix in v1.0.1? Or hold
until the blockers are cleared?

### 2. What is the HIGHEST LEVERAGE thing you can do right now?

Not the most requested feature. Not the most interesting technical challenge.
The thing that, per hour invested, produces the most value. Consider:

| Action | Time | Value | Leverage |
|--------|------|-------|----------|
| Write a README with screenshots + quickstart | 2 hours | Every new user sees this first | Very high |
| Set up GitHub Actions CI (build + test on push) | 3 hours | Catches regressions forever | Very high |
| Fix the HA auto-discovery stub | 4 hours | Unblocks the HA/Frigate migration audience | High |
| Write a setup/install guide | 4 hours | Reduces support burden | High |
| Publish Flutter app to TestFlight/Play Console | 8 hours | Unblocks mobile users | Medium |
| Add consecutive frame confirmation | 4 hours | Reduces false positives further | Medium |
| Implement Coral TPU support | 20+ hours | Unblocks Frigate migrants | Medium-low per hour |
| Add two-way audio | 15+ hours | Nice feature, not blocking anyone | Low |

### 3. What's your testing strategy?

You have:
- Go unit tests for some backend packages
- Zero frontend tests
- Zero end-to-end tests
- Zero integration tests that test the full stack (API → DB → ffmpeg → recording)
- Manual testing by one reviewer

What would a senior developer do about this? Options:
- Add Playwright e2e tests for critical flows (login, add camera, live view, export clip)
- Add Go integration tests that start a real server and test API endpoints
- Add a CI pipeline that runs tests on every PR
- Accept the risk and ship fast (you're a solo dev, testing slows you down)

There's no right answer. But there IS a wrong answer: having no opinion about it.

### 4. Should you seek more users or more features?

You have one happy user. You could:

**A. Build more features** (Coral TPU, two-way audio, PTZ) to attract different
user segments. Risk: you build features nobody asked for.

**B. Seek more users** (post to r/selfhosted, r/homeassistant, r/frigate, NVR
Discord servers) with what you have. Risk: they find bugs you haven't found.

**C. Do both in parallel** — post the announcement AND fix HA auto-discovery so
the HA crowd can actually use it when they arrive.

What's your instinct? And why?

### 5. What's your competitive moat?

The reviewer identified Sentinel's advantages:
1. Heatmap timeline (no competitor has this)
2. Zero-transcoding architecture (8% CPU vs Blue Iris's 45-60%)
3. Per-camera × per-event-type retention matrix
4. Visual zone editor that's better than Frigate's
5. ONVIF auto-discovery (Frigate doesn't have this)

Which of these are DURABLE advantages (hard to copy) and which are TEMPORARY
(competitors will catch up)?

How do you invest to widen the durable ones and accept that the temporary ones
will converge?

### 6. What are you afraid of?

Every solo developer has fears they don't voice:
- "What if a camera firmware update breaks all my recordings and I can't recover?"
- "What if someone finds a security hole and my users' camera feeds get exposed?"
- "What if Frigate adds a heatmap timeline and my main differentiator disappears?"
- "What if I burn out and the project dies?"

Name yours. Then decide what to do about each one.

### 7. What does the 6-month version of Sentinel look like?

Not a feature list. A picture.

- How many active users?
- Which platforms? (Docker only? Windows native? Mac?)
- Is there a community? (GitHub Discussions? Discord? Reddit?)
- Is the mobile app published?
- Are people running it as their ONLY NVR?
- Have you monetized? (Donations? Paid tier? Hardware bundle?)

Draw the picture. Then work backwards to figure out what you need to do THIS WEEK
to make it real.

---

## How to Answer

Don't write a plan document. Write a decision memo. For each question:

1. **State your decision** (one sentence)
2. **State your reasoning** (2-3 sentences)
3. **State what you're giving up** (every decision has a cost)
4. **State what you'll do THIS WEEK** (concrete, time-boxed)

The output should be a document that, if someone reads it in 6 months, explains
exactly why you made the choices you made — not just what you chose, but what you
considered and rejected.

Save it somewhere permanent. Your future self will thank you when you're wondering
"why did I spend 3 weeks on Coral TPU support instead of writing documentation?"

---

## The Meta-Insight

You've been in a build-measure-learn loop for the last month:
```
Build v0.1 → Review (5/10) → Build v0.2 → Review (7/10) → Build v0.3 → Review (8.5/10)
```

The loop worked. You went from "interesting project" to "I'm switching from Blue Iris"
in three iterations. That's rare. Most projects never get honest feedback, let alone
act on it fast enough to matter.

The question now is: **do you keep the loop going, or do you widen it?**

- Keeping the loop: get the SAME reviewer to 9.5/10 by building their remaining requests
- Widening the loop: get 10 NEW reviewers and find out if their priorities are different

Both are valid. One gives you depth (one user perfectly served). The other gives you
breadth (product-market fit across a segment). A senior developer knows which one
matters more at this stage.

Which one do you choose?
