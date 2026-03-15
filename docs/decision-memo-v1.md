# Decision Memo: Post-v0.3 Strategy
**Date:** 2026-03-15
**Author:** Senior Developer / Sole Technical Lead
**Context:** Sentinel NVR v0.3.0 shipped. Reviewer score moved from 5/10 to 8.5/10. Reviewer is actively switching from Blue Iris. Zero P1 bugs across 3 releases. 14 backend packages, 15 frontend pages, 39 backend test files, 15 Playwright e2e spec files, CI pipeline running on every push.

---

## Decision 1: Should we tag v1.0 now?

**Decision:** No. Hold v1.0 for 2-3 weeks until the HA auto-discovery stub is either implemented or removed, and the getting-started documentation is validated.

**Reasoning:** The HA auto-discovery toggle is a non-functional UI element. Shipping v1.0 with a toggle that does nothing will generate bug reports within hours of any public announcement. That is the kind of first impression that poisons a project's reputation permanently. The documentation gap is less fatal but still problematic -- a v1.0 announcement on r/selfhosted without a clear setup guide means every interested user becomes a support ticket. The good news: CI already exists (lint, test, build, Docker build on every push; multi-arch release pipeline on tag). Release artifacts are automated. The security posture is reasonable (JWT auth, SSRF validation, rate limiting, encrypted credentials). These were the blockers I worried about, and they are already handled. The remaining blockers are small and time-boxed.

**What I'm giving up:** 2-3 weeks of "v1.0" marketing signal. Every day as v0.3 is a day someone sees "v0.x" and assumes it is experimental. But shipping a v1.0 that generates "your HA toggle is broken" issues on day one is worse than waiting.

**This week:**
- Remove the HA auto-discovery toggle from the MQTT settings UI entirely. Replace it with a "coming soon" note in the help text. This is a 30-minute change that eliminates the broken-promise risk.
- Review `docs/getting-started.md` for completeness against the reviewer's documentation checklist (install, camera config, detection backend, MQTT, troubleshooting). Fill gaps.
- Target: tag v1.0-rc1 by end of week. Tag v1.0 final after 1 week of rc1 soak.

---

## Decision 2: What is the highest leverage thing to do right now?

**Decision:** Write the user documentation (Getting Started guide + Configuration Reference) and remove the broken HA auto-discovery toggle.

**Reasoning:** The CI pipeline already exists and runs on every push -- backend lint, backend tests with race detector, frontend lint, frontend type check, frontend build, and Docker build. The release pipeline builds multi-arch Docker images and native binaries for 4 platforms on tag push. That was the infrastructure gap I expected to find, and it is already closed. So the highest leverage per-hour-invested action shifts to documentation. A `getting-started.md` file already exists in `docs/`, but it needs to be validated against the reviewer's checklist: Docker Compose setup for common configurations, camera setup (ONVIF + manual RTSP), detection backend configuration (CodeProject.AI / Blue Onyx), MQTT/HA integration, and troubleshooting common issues. Every new user who arrives from a Reddit post will land on this guide first. If it works, they stay. If it does not, they leave and never come back. The HA toggle removal is the other high-leverage action because it costs 30 minutes and prevents an entire class of bug reports.

**What I'm giving up:** Building features. Consecutive frame confirmation, event deduplication, and per-zone cooldown are all high-value detection quality improvements. But they do not matter if nobody can install the product.

**This week:**
- Audit and complete `docs/getting-started.md` (2-3 hours)
- Write `docs/configuration-reference.md` covering all `sentinel.yml` keys and web UI settings (2-3 hours)
- Write `docs/troubleshooting.md` with the 10 most likely setup failures (1-2 hours)
- Remove HA auto-discovery toggle, add "coming soon" note (30 minutes)

---

## Decision 3: What is the testing strategy?

**Decision:** The current test suite is sufficient for a v1.0 launch. Invest in Go integration tests for the 3 riskiest flows before the v1.1 cycle, not now.

**Reasoning:** The project already has more test coverage than I expected when I started this analysis. There are 39 Go test files across the backend (unit tests for auth, camera, config, db, detection, eventbus, notification, recording, server, storage, watchdog), including HTTP API integration tests (`integration_test.go` and `integration_routes_test.go` totaling 1,500 lines), and 15 Playwright e2e spec files covering auth, cameras, dashboard, events, faces, import, live view, models, navigation, notifications, playback, responsive layout, settings, and accessibility. There are also 2 frontend unit tests (`client.test.ts`, `time.test.ts`). CI runs `go test -race` and frontend lint/typecheck on every push. This is not zero coverage -- this is a real test suite. The gap is not "no tests" but "no integration tests that exercise the full video pipeline" (camera -> go2rtc -> ffmpeg -> segment -> playback). That gap is real but acceptable for v1.0 because the reviewer ran the full pipeline for 60+ days across 3 releases with zero P1 bugs. Manual testing by a hostile reviewer is, for now, a valid substitute for automated pipeline integration tests.

**What I'm giving up:** Confidence that a refactor of the recording pipeline will not break segment generation. The recording and camera packages have the most complex interaction with external processes (ffmpeg, go2rtc), and that interaction is the hardest to test automatically. If a regression slips through, it will be caught by the reviewer or by my own manual testing, not by CI. That is a risk I accept for now.

**This week:**
- Nothing. The test suite is adequate. Do not invest in testing this week. Invest in documentation.
- Backlog for v1.1 cycle: add Go integration tests for (1) recording segment creation end-to-end, (2) clip export via the export API, (3) detection pipeline with a mock HTTP backend. These are the three flows most likely to regress.

---

## Decision 4: Should we seek more users or more features?

**Decision:** Widen the loop. Seek 10 new users before building more features.

**Reasoning:** One reviewer's 8.5 might be another reviewer's 4. The current reviewer has 12 cameras, uses CodeProject.AI, runs Docker on an Intel NUC, and does not use Coral TPU. That is one profile out of the entire NVR user base. Before investing 20+ hours in Coral TPU support or 15+ hours in two-way audio, I need to know whether those features are actually the highest priority for the broader audience or just the features that seem obvious from a competitor feature matrix. The reviewer explicitly said Sentinel is ready for "Docker-comfortable power users." That is a real audience segment that can be reached today with what exists today. Posting to r/selfhosted and r/homeassistant with a solid getting-started guide will surface the real prioritization data: are people asking for Coral TPU, or are they asking for RTSP auto-detection, or are they asking for something I have not thought of?

**What I'm giving up:** Depth with the current reviewer. The path from 8.5 to 9.5 is clear (event dedup, consecutive frame confirmation, per-zone cooldown, published mobile app). By pausing feature work to seek users, I delay those improvements. But the reviewer is already switching -- they are committed. The marginal value of moving them from 8.5 to 9 is lower than the marginal value of learning whether 10 other people would give this a 6 or an 8.

**This week:**
- After documentation is complete (see Decision 2), draft a r/selfhosted announcement post. Format: problem statement ("Blue Iris uses 50% CPU, Frigate has no timeline"), solution ("Sentinel does X at 8% CPU"), screenshots (heatmap timeline, zone editor, clip export), getting started link, honest "what it does NOT do yet" section.
- Do NOT post until the getting-started docs are validated and the HA toggle is removed.
- Target: post within 10 days of today (by March 25).

---

## Decision 5: What is the competitive moat?

**Decision:** The heatmap timeline and the zero-transcoding architecture are durable advantages. ONVIF discovery, zone editor quality, and retention granularity are temporary advantages that competitors will eventually match.

**Reasoning:**

**Durable (hard to copy):**
- *Heatmap timeline.* This requires the 24/7 recording architecture (10-minute segments with detection density indexing) to exist before the heatmap can be overlaid. Frigate would need to fundamentally change its clip-based model to add this. That is an architectural change, not a feature addition. It would take them 6-12 months even if they started today. This is the moat to widen.
- *Zero-transcoding architecture.* The `ffmpeg -c copy` segment muxer approach is an architectural decision made at the foundation. Retrofitting this into Blue Iris (which transcodes everything) would require rewriting their video pipeline. Frigate already uses a similar approach for recording but still transcodes for detection. Sentinel's 8% CPU vs 50% is a structural advantage.

**Temporary (competitors will catch up):**
- *ONVIF auto-discovery.* This is straightforward WS-Discovery + SOAP. Frigate could add this in a sprint. It is convenient, not defensible.
- *Visual zone editor.* Frigate has been improving their mask editor. The gap will narrow.
- *Per-camera x per-event-type retention matrix.* This is a database schema decision. Any competitor could add this in a release. It is a nice differentiator today, not a moat.

**What I'm giving up:** Time spent on temporary advantages. Improving the zone editor or adding more retention granularity is low-moat work. Those features should be maintained, not invested in heavily.

**This week:**
- Begin design work on typed heatmap filtering (person/vehicle/animal layers on the timeline). The reviewer suggested this in v0.2. It deepens the durable moat by making the heatmap more useful than any competitor can match without rebuilding their architecture.
- Backlog: interactive heatmap drill-down (click a heatmap cluster to see the events that produced it). This turns the heatmap from a visualization into a navigation tool.

---

## Decision 6: What am I afraid of?

**Decision:** Name the fears, triage them, act on the ones that are both likely and high-impact.

**Fear 1: A camera firmware update breaks recordings and I lose footage.**
- Likelihood: Medium. It happened once in 60 days (camera reboot caused a 25-minute gap).
- Impact: Medium. Gaps are annoying but the watchdog recovers automatically.
- Action: Already handled. Pipeline auto-restart, crash isolation, and watchdog recovery are solid. The reviewer validated this with a simulated power outage and a router reboot. No additional investment needed.

**Fear 2: Someone finds a security hole and camera feeds get exposed.**
- Likelihood: Medium-High once the project goes public. The pairing redemption endpoint is public (no auth). Webhook URLs could be SSRF vectors. The login endpoint has rate limiting but no account lockout.
- Impact: Critical. Camera feeds are intimate. A security breach would kill the project.
- Action: Before the v1.0 announcement, do a focused 2-hour security audit of: (1) the pairing endpoint (`POST /api/v1/pairing/redeem`) -- ensure tokens expire and are single-use, (2) SSRF validation on webhook URLs, (3) path traversal on recording file serving, (4) login endpoint brute-force resistance. This is not a full penetration test, but it covers the highest-risk surfaces. Backlog a more thorough audit for v1.1.

**Fear 3: Frigate adds a heatmap timeline and my main differentiator disappears.**
- Likelihood: Low in the next 6 months. Frigate's architecture is clip-based. Adding a 24/7 timeline would require fundamental changes to their recording model.
- Impact: High if it happens. The heatmap is Sentinel's elevator pitch.
- Action: Widen the moat (typed heatmap, interactive drill-down) rather than defend against a threat that requires Frigate to rearchitect. Move faster than they can pivot.

**Fear 4: I burn out and the project dies.**
- Likelihood: Medium. Solo developer, shipping 4 releases in rapid succession, unpaid.
- Impact: Fatal.
- Action: The reviewer suggested a paid support tier ($5/month) and a hardware appliance ($300-400). Neither is viable at 1 user. But at 50+ users, a GitHub Sponsors page or a "priority support" tier becomes reasonable. The immediate action is: do not burn out this week. Ship documentation, not features. Documentation is lower-stress work than debugging ffmpeg edge cases. Alternate between creative work (features) and operational work (docs, CI, community) to avoid monotony burnout.

**This week:**
- Spend 2 hours on the security audit described above (pairing endpoint, SSRF, path traversal, login brute-force). Document findings. Fix anything critical before the v1.0 tag.

---

## Decision 7: What does the 6-month version of Sentinel look like?

**Decision:** The picture, then the backward plan.

**The picture (September 2026):**
- **Active users:** 50-100 running it daily. Not thousands. The target audience is narrow (Docker-comfortable power users migrating from Blue Iris or supplementing Frigate). 50 real users who file real bugs are worth more than 1,000 GitHub stars from people who never install it.
- **Platforms:** Docker only. Native binaries exist in the release pipeline but are not promoted. Docker is the tested, supported path. Expanding to native Windows/Mac installs would fracture testing and support across platforms I cannot validate alone.
- **Community:** GitHub Discussions enabled. Not Discord -- Discord fragments knowledge and makes answers unsearchable. GitHub Discussions keeps everything in one place, tied to the repo, and indexable by search engines. A Discord server can come later if the community outgrows Discussions.
- **Mobile app:** Published on TestFlight (iOS) and Google Play open beta. Not a polished App Store release -- an open beta that works for push notifications and live view. The Flutter code exists. Getting it into stores is an 8-hour packaging exercise, not a development project.
- **Primary NVR:** Yes. At least 10 users running Sentinel as their only NVR, with the reviewer being the reference case. The v1.0 announcement targets users who are willing to run it alongside their existing NVR for 30 days, then switch.
- **Monetization:** GitHub Sponsors page with a "buy me a coffee" tier. No paid features, no paid tier, no hardware bundle. At 50 users it is too early to monetize. The goal is adoption and feedback, not revenue. Revenue conversations start at 500+ users.
- **Feature state by September 2026:**
  - v1.0 (April): docs, HA toggle removed, security audit, v1.0 tag
  - v1.1 (May-June): consecutive frame confirmation, event deduplication, per-zone cooldown, typed heatmap filtering
  - v1.2 (July-August): published mobile app (TestFlight + Play open beta), Coral TPU support (the Frigate migration play)
  - v1.3 (September): continuous sub-stream detection (sub-500ms detection latency), HA MQTT auto-discovery (the real implementation, not the stub)

**What I'm giving up:** Two-way audio, PTZ controls, multi-camera sync playback, native binaries, license plate recognition, HomeKit Secure Video. All of these are real features that real users want. None of them are worth building before the product has 50 users who can validate whether they are actually the highest priority. The reviewer uses two-way audio once a month and has zero PTZ cameras. Until I hear from 10 more users, I do not know if that is representative.

**This week (working backwards from the picture):**
1. Complete getting-started and configuration-reference documentation (prerequisite for v1.0 announcement)
2. Remove HA auto-discovery toggle from UI (prerequisite for v1.0 tag)
3. Run 2-hour security audit of public endpoints (prerequisite for v1.0 tag)
4. Begin design sketch for typed heatmap filtering (moat-widening, can happen in parallel)
5. Enable GitHub Discussions on the repo (zero-cost community infrastructure)

---

## The Meta-Decision: Keep the loop or widen it?

**Decision:** Widen the loop.

**Reasoning:** The build-measure-learn loop with one reviewer has been extraordinarily productive. Three iterations, 5/10 to 8.5/10, a real switch decision. But the loop has extracted most of the value it can from a single reviewer. The remaining path from 8.5 to 9.5 is known (event dedup, consecutive frames, per-zone cooldown, mobile app). Those are execution tasks, not discovery tasks. The unknown is whether 10 different users would even agree with the current 8.5. Maybe they have cameras that expose ffmpeg bugs I have never seen. Maybe they run ARM64 and the Docker image has a build issue. Maybe their detection backend is Ollama and the remote detector has a timeout bug. The only way to find out is to widen the loop.

The depth play (one user at 9.5) gives me a perfect reference customer. The breadth play (10 users at 7-8) gives me product-market fit data. At this stage -- pre-v1.0, pre-announcement, solo developer -- breadth matters more. A perfect product for one person is a hobby project. An adequate product for 50 people is a real project.

**What I'm giving up:** The satisfaction of a 9.5 from the reviewer who has been with me since v0.1. That relationship is valuable and I will not abandon it -- they will continue to get releases and their feedback will continue to matter. But their priorities will no longer be the sole input to the roadmap. The next 10 users might have completely different priorities, and I need to hear them before I commit to the v1.1 feature set.

---

## Summary: This Week's Commitments

| Day | Task | Time | Blocks |
|-----|------|------|--------|
| Mon | Remove HA auto-discovery toggle, add "coming soon" note | 30 min | v1.0 tag |
| Mon-Tue | Audit and complete `docs/getting-started.md` | 3 hrs | v1.0 announcement |
| Tue-Wed | Write `docs/configuration-reference.md` | 3 hrs | v1.0 announcement |
| Wed | Write `docs/troubleshooting.md` | 2 hrs | v1.0 announcement |
| Thu | Security audit: pairing, SSRF, path traversal, login | 2 hrs | v1.0 tag |
| Thu | Enable GitHub Discussions | 15 min | Community |
| Fri | Draft r/selfhosted announcement post | 1 hr | User acquisition |
| Fri | Tag v1.0-rc1 | 30 min | v1.0 release |

**Total estimated time:** ~12 hours across 5 days. Deliberately light. The reviewer's best advice was "ship the boring stuff." Documentation is the boring stuff this week.
