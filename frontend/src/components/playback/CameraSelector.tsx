/** CameraSelector — dropdown for choosing a recording-enabled camera (R6). */
import type { CameraDetail } from "../../api/client";

interface CameraSelectorProps {
  cameras: CameraDetail[];
  selected: string | null;
  onSelect: (cameraName: string) => void;
}

export default function CameraSelector({ cameras, selected, onSelect }: CameraSelectorProps) {
  const recordingCameras = cameras.filter((c) => c.record);

  return (
    <select
      value={selected || ""}
      onChange={(e) => onSelect(e.target.value)}
      className="bg-surface-base border border-border rounded-lg px-3 py-1.5 text-sm
                 text-white focus:outline-none focus:ring-1 focus:ring-sentinel-500
                 cursor-pointer min-w-[160px]"
    >
      <option value="" disabled>
        Select camera...
      </option>
      {recordingCameras.map((cam) => (
        <option key={cam.name} value={cam.name}>
          {cam.name}
        </option>
      ))}
    </select>
  );
}
