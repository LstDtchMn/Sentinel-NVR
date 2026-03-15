/** Shared string utility functions. */

/** Sanitize a device name for use as a camera name (alphanumeric, spaces, hyphens, underscores). */
export function sanitizeName(raw: string): string {
  return raw
    .replace(/[^a-zA-Z0-9 _-]/g, "")
    .trim()
    .slice(0, 64)
    .trim() || "Camera";
}
