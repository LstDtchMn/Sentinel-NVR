import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  Camera,
  Edit2,
  MapPin,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { api, CameraDetail } from "../../api/client";
import { StatusBadge } from "./StatusBadge";

function StreamIndicator({ label, active }: { label: string; active?: boolean }) {
  return (
    <span className="flex items-center gap-1 text-xs">
      <span className={`w-1.5 h-1.5 rounded-full ${active ? "bg-green-400" : "bg-faint"}`} />
      {label}
    </span>
  );
}

export function CameraCard({
  camera,
  onDelete,
  onEdit,
  onRestart,
}: {
  camera: CameraDetail;
  onDelete: (name: string) => void;
  onEdit: (camera: CameraDetail) => void;
  onRestart: (name: string) => void;
}) {
  const ps = camera.pipeline_status;
  const state = ps?.state || "idle";
  const showRestart = state === "error" || state === "recording" || state === "streaming";

  // Snapshot refresh: tick increments every 30s to cache-bust the thumbnail URL
  const [tick, setTick] = useState(0);
  const [snapshotError, setSnapshotError] = useState(false);

  useEffect(() => {
    const id = setInterval(() => {
      setTick((t) => t + 1);
      setSnapshotError(false); // retry on next tick
    }, 30_000);
    return () => clearInterval(id);
  }, []);

  return (
    <div className="bg-surface-raised border border-border rounded-lg overflow-hidden">
      {/* Snapshot thumbnail */}
      <div className="aspect-video w-full bg-surface-base relative">
        {!snapshotError ? (
          <img
            src={`${api.cameraSnapshotURL(camera.name)}?_cb=${tick}`}
            alt={`${camera.name} snapshot`}
            className="w-full h-full object-cover"
            onError={() => setSnapshotError(true)}
          />
        ) : (
          <div className="w-full h-full flex flex-col items-center justify-center gap-2">
            <Camera className="w-10 h-10 text-faint" />
            <span className="text-xs text-muted">Connecting...</span>
          </div>
        )}
      </div>

      <div className="p-5">
        <div className="flex items-center justify-between mb-3">
          <h3 className="font-medium break-words mr-2">{camera.name}</h3>
          <div className="flex items-center gap-2">
            <StatusBadge status={state} />
            {showRestart && (
              <button
                type="button"
                onClick={() => onRestart(camera.name)}
                className="text-faint hover:text-yellow-400 transition-colors p-1"
                aria-label={`Restart pipeline for ${camera.name}`}
                title="Restart pipeline"
              >
                <RefreshCw className="w-4 h-4" />
              </button>
            )}
            <button
              type="button"
              onClick={() => onEdit(camera)}
              className="text-faint hover:text-sentinel-400 transition-colors p-1"
              aria-label={`Edit camera ${camera.name}`}
              title="Edit camera"
            >
              <Edit2 className="w-4 h-4" />
            </button>
            <button
              type="button"
              onClick={() => onDelete(camera.name)}
              className="text-faint hover:text-red-400 transition-colors p-1"
              aria-label={`Delete camera ${camera.name}`}
              title="Delete camera"
            >
              <Trash2 className="w-4 h-4" />
            </button>
          </div>
        </div>
        <div className="space-y-1 text-sm text-muted">
          {!camera.enabled && (
            <p className="text-yellow-500">Disabled</p>
          )}
          <div className="flex items-center gap-3">
            <StreamIndicator label="Main" active={ps?.main_stream_active} />
            {camera.sub_stream && (
              <StreamIndicator label="Sub" active={ps?.sub_stream_active} />
            )}
          </div>
          <div className="flex items-center gap-3">
            <span>Record: {camera.record ? "On" : "Off"} · Detect: {camera.detect ? "On" : "Off"}</span>
            {ps?.recording && (
              <span className="text-xs font-bold text-red-400 animate-pulse">REC</span>
            )}
          </div>
          {ps?.last_error && (
            <p className="text-red-400 text-xs truncate" title={ps.last_error}>
              {ps.last_error}
            </p>
          )}
          {/* Zones link — only shown when detection is enabled (Phase 9) */}
          {camera.detect && (
            <div className="pt-1">
              <Link
                to={`/cameras/${encodeURIComponent(camera.name)}/zones`}
                className="inline-flex items-center gap-1 text-xs text-blue-400 hover:text-blue-300 transition-colors"
              >
                <MapPin className="w-3.5 h-3.5" />
                Zones {camera.zones?.length > 0 ? `(${camera.zones.length})` : ""}
              </Link>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
