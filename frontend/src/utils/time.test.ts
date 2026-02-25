// Unit tests for src/utils/time.ts — pure functions only, no network/DOM required.
// Run with: npm test  (vitest run)

import { describe, it, expect } from "vitest";
import {
  isoToSecondsSinceMidnight,
  formatWallClock,
  formatSecondsAsTime,
  formatHour,
  getDaysInMonth,
  getFirstDayOfMonth,
} from "./time";

// ─── formatSecondsAsTime ─────────────────────────────────────────────────────

describe("formatSecondsAsTime", () => {
  it("formats midnight (0s) as 00:00:00", () => {
    expect(formatSecondsAsTime(0)).toBe("00:00:00");
  });

  it("formats 3661s as 01:01:01", () => {
    expect(formatSecondsAsTime(3661)).toBe("01:01:01");
  });

  it("formats last second of day (86399s) as 23:59:59", () => {
    expect(formatSecondsAsTime(86399)).toBe("23:59:59");
  });

  it("pads single-digit components with leading zeros", () => {
    // 2h 3m 4s = 7384s
    expect(formatSecondsAsTime(7384)).toBe("02:03:04");
  });
});

// ─── formatHour ──────────────────────────────────────────────────────────────

describe("formatHour", () => {
  it("formats 0 seconds as 00:00", () => {
    expect(formatHour(0)).toBe("00:00");
  });

  it("formats noon (43200s) as 12:00", () => {
    expect(formatHour(43200)).toBe("12:00");
  });

  it("formats 23h (82800s) as 23:00", () => {
    expect(formatHour(82800)).toBe("23:00");
  });
});

// ─── getDaysInMonth ───────────────────────────────────────────────────────────

describe("getDaysInMonth", () => {
  it("returns 31 for January", () => {
    expect(getDaysInMonth(2024, 1)).toBe(31);
  });

  it("returns 28 for February in a non-leap year", () => {
    expect(getDaysInMonth(2023, 2)).toBe(28);
  });

  it("returns 29 for February in a leap year", () => {
    expect(getDaysInMonth(2024, 2)).toBe(29);
  });

  it("returns 30 for April", () => {
    expect(getDaysInMonth(2024, 4)).toBe(30);
  });

  it("returns 31 for December", () => {
    expect(getDaysInMonth(2024, 12)).toBe(31);
  });
});

// ─── getFirstDayOfMonth ───────────────────────────────────────────────────────

describe("getFirstDayOfMonth", () => {
  // Jan 1, 2024 = Monday → day-of-week 1
  it("returns 1 (Monday) for January 2024", () => {
    expect(getFirstDayOfMonth(2024, 1)).toBe(1);
  });

  // Feb 1, 2024 = Thursday → day-of-week 4
  it("returns 4 (Thursday) for February 2024", () => {
    expect(getFirstDayOfMonth(2024, 2)).toBe(4);
  });

  // Jan 1, 2023 = Sunday → day-of-week 0
  it("returns 0 (Sunday) for January 2023", () => {
    expect(getFirstDayOfMonth(2023, 1)).toBe(0);
  });
});

// ─── isoToSecondsSinceMidnight ────────────────────────────────────────────────
// Uses local-time construction to stay timezone-independent.

describe("isoToSecondsSinceMidnight", () => {
  it("converts local midnight to 0 seconds", () => {
    const d = new Date();
    d.setHours(0, 0, 0, 0);
    expect(isoToSecondsSinceMidnight(d.toISOString())).toBe(0);
  });

  it("converts local noon to 43200 seconds", () => {
    const d = new Date();
    d.setHours(12, 0, 0, 0);
    expect(isoToSecondsSinceMidnight(d.toISOString())).toBe(43200);
  });

  it("converts end-of-day to 86399 seconds", () => {
    const d = new Date();
    d.setHours(23, 59, 59, 0);
    expect(isoToSecondsSinceMidnight(d.toISOString())).toBe(86399);
  });
});

// ─── formatWallClock ─────────────────────────────────────────────────────────

describe("formatWallClock", () => {
  it("returns start time when video position is 0", () => {
    const ref = new Date();
    ref.setHours(10, 0, 0, 0);
    expect(formatWallClock(ref.toISOString(), 0)).toBe("10:00:00");
  });

  it("advances clock by video playback position", () => {
    const ref = new Date();
    ref.setHours(10, 0, 0, 0);
    // 90 seconds = 1 minute 30 seconds
    expect(formatWallClock(ref.toISOString(), 90)).toBe("10:01:30");
  });

  it("handles hour wrap-around correctly", () => {
    const ref = new Date();
    ref.setHours(23, 58, 0, 0);
    // 2 minutes later = 00:00:00 next day — formatWallClock only formats
    // local hour/minute/second so it wraps to 00:00:00
    expect(formatWallClock(ref.toISOString(), 120)).toBe("00:00:00");
  });
});
