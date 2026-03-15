import { Circle } from "lucide-react";
import { CameraState } from "../../api/client";

const STATUS_COLORS: Record<CameraState, string> = {
  streaming: "text-green-400",
  recording: "text-blue-400",    // distinct from streaming — actively writing to disk
  connecting: "text-yellow-400",
  error: "text-red-400",
  idle: "text-faint",
  stopped: "text-faint",
};

export function StatusBadge({ status }: { status: CameraState }) {
  return (
    <span
      className={`flex items-center gap-1.5 text-xs font-medium ${STATUS_COLORS[status] || "text-faint"}`}
    >
      <Circle className="w-2 h-2 fill-current" />
      {status}
    </span>
  );
}
