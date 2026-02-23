/** Time utility functions for the playback timeline (R6). */

/** Returns today's date as "YYYY-MM-DD" in local time. */
export function todayDateString(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** Returns the current month as "YYYY-MM" in local time. */
export function currentMonthString(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

/**
 * Converts an ISO 8601 timestamp to seconds since midnight (local time).
 *
 * IMPORTANT: Uses local time (getHours/getMinutes/getSeconds), which is correct
 * because the backend stores recording timestamps in local time via
 * time.ParseInLocation(..., time.Local). If the server and browser are in
 * different timezones, timeline positions will be offset. This is acceptable
 * for the typical NVR deployment where server and user are colocated.
 */
export function isoToSecondsSinceMidnight(isoString: string): number {
  const d = new Date(isoString);
  return d.getHours() * 3600 + d.getMinutes() * 60 + d.getSeconds();
}

/**
 * Computes the wall-clock time during recording playback.
 * @param segmentStartISO - Segment start time as ISO string
 * @param videoCurrentTime - Current playback position in seconds (from video element)
 * @returns Formatted "HH:MM:SS" string in local time
 */
export function formatWallClock(segmentStartISO: string, videoCurrentTime: number): string {
  const startMs = new Date(segmentStartISO).getTime();
  const wallMs = startMs + videoCurrentTime * 1000;
  const d = new Date(wallMs);
  return [d.getHours(), d.getMinutes(), d.getSeconds()]
    .map((n) => String(n).padStart(2, "0"))
    .join(":");
}

/** Formats a "YYYY-MM-DD" date string for display (e.g. "Feb 22, 2026"). */
export function formatDate(dateStr: string): string {
  const d = new Date(dateStr + "T00:00:00");
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });
}

/** Returns the number of days in a given month (1-indexed month). */
export function getDaysInMonth(year: number, month: number): number {
  return new Date(year, month, 0).getDate();
}

/** Returns the day-of-week (0=Sun, 6=Sat) for the first day of a month (1-indexed). */
export function getFirstDayOfMonth(year: number, month: number): number {
  return new Date(year, month - 1, 1).getDay();
}

/** Formats seconds as "HH:MM:SS" for timeline tick labels. */
export function formatSecondsAsTime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  return [h, m, s].map((n) => String(n).padStart(2, "0")).join(":");
}

/** Formats seconds as "HH:00" for compact 24h timeline labels. */
export function formatHour(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  return `${String(h).padStart(2, "0")}:00`;
}
