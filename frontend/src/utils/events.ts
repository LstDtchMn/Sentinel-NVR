/** Shared event display helpers (extracted from EventCard + EventDetail). */

/** Returns badge colors and a display label for a given event type. */
export function eventTypeBadge(type: string): { bg: string; text: string; label: string } {
  const label = type.replace(/[._]/g, " ").replace(/^\w/, (c) => c.toUpperCase());
  switch (type) {
    case "detection":
      return { bg: "bg-blue-500/20", text: "text-blue-400", label };
    case "face_match":
      return { bg: "bg-purple-500/20", text: "text-purple-400", label };
    case "audio_detection":
      return { bg: "bg-amber-500/20", text: "text-amber-400", label };
    case "camera.connected":
    case "camera.disconnected":
      return { bg: "bg-orange-500/20", text: "text-orange-400", label };
    case "recording.started":
    case "recording.stopped":
      return { bg: "bg-green-500/20", text: "text-green-400", label };
    default:
      return { bg: "bg-gray-500/20", text: "text-gray-400", label };
  }
}

/** Returns Tailwind classes for a confidence value (green >= 0.8, yellow >= 0.5, red below). */
export function confidenceColor(c: number): string {
  if (c >= 0.8) return "bg-green-500/20 text-green-400";
  if (c >= 0.5) return "bg-yellow-500/20 text-yellow-400";
  return "bg-red-500/20 text-red-400";
}
