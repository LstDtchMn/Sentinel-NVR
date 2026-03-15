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

/** Compact event timestamp — "Feb 22, 10:34:12 AM" (used in event cards). */
export function formatEventTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

/** Verbose event timestamp — "Sat, Feb 22, 2026, 10:34:12 AM" (used in event detail). */
export function formatEventTimeLong(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    weekday: "short",
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

/** Convert Go duration string (e.g. "121h33m56s") to human-friendly "5d 1h 33m". */
export function formatUptime(raw: string): string {
  const h = raw.match(/(\d+)h/);
  const m = raw.match(/(\d+)m/);
  const s = raw.match(/(\d+)s/);
  const hours = h ? parseInt(h[1], 10) : 0;
  const mins = m ? parseInt(m[1], 10) : 0;
  const secs = s ? parseInt(s[1], 10) : 0;
  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const remH = hours % 24;
    return remH > 0 ? `${days}d ${remH}h ${mins}m` : `${days}d ${mins}m`;
  }
  if (hours >= 1) return `${hours}h ${mins}m`;
  if (mins >= 1) return `${mins}m ${secs}s`;
  return `${secs}s`;
}

/** Format an ISO timestamp as a relative time string (e.g. "2h ago"). */
export function formatRelativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  if (diff < 0) return "just now";
  const secs = Math.floor(diff / 1000);
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}
