# Dev Prompt: Sentinel NVR v0.2 — Triage the Veteran Review and Ship

You are the Sentinel NVR engineering team. A veteran NVR user (3 years Blue Iris,
6 months Frigate, 12 cameras, Coral TPU) just submitted a month-long review of v0.1.0.

Read the review at `docs/v01-veteran-review.md`. Then:

1. **Triage every finding** into: fix now (v0.2), fix later (v0.3+), won't fix
2. **Identify the #1 thing** the reviewer said must change before they'd switch
3. **Build the v0.2 plan** — maximum 2 weeks of work
4. **Write the response** to the reviewer

---

## How to Triage

### Fix Now (v0.2) — things that make users say "this isn't ready"

Criteria:
- Reliability issues (recording drops, crashes, silent failures)
- Features that exist but are broken or incomplete
- UX gaps that make the product feel unfinished
- Things the reviewer called "unacceptable" or "dealbreaker"

### Fix Later (v0.3+) — things that are missing but not blocking

Criteria:
- Features competitors have that Sentinel doesn't yet
- Nice-to-have improvements the reviewer mentioned
- Performance optimizations that aren't urgent

### Won't Fix — things that don't matter

Criteria:
- Features the reviewer specifically said "not needed"
- Things Blue Iris has that nobody uses
- Scope creep disguised as feedback

---

## How to Prioritize v0.2

The veteran reviewer answered: "What is the single most important thing the team
should work on next?" Whatever they said, that's the headline for v0.2.

Then look at their "must-have for v1.0 launch" list. Those are the P1s.

Then look at their "unacceptable" list. Those are the bugs that must be fixed.

v0.2 should contain:
- The reviewer's #1 priority
- All "unacceptable" items fixed
- The top 3 "must-have for launch" items implemented
- No new features that weren't requested

---

## How to Respond

After triaging, write a response to the reviewer. Be honest about what you're
fixing, what you're deferring, and what you disagree with. The reviewer respects
directness — they've been giving NVR feedback for years and they can tell when
a team is dodging.

Include:
- What's shipping in v0.2 (with estimated timeline)
- What's planned for v0.3
- What you're not doing and why
- Questions back to the reviewer (anything unclear or needing more detail?)

---

## Execution

After writing the triage and response:

1. Create a `docs/v02-plan.md` with the specific implementation plan
2. Identify the files that need to change for each fix
3. Estimate effort per item
4. Order by: reliability fixes first, then UX, then features
5. Do NOT start coding in this session — plan first, then execute in the next session

The goal is to go from "interesting v0.1 project" to "I'm considering switching
from Blue Iris" in one release cycle.
