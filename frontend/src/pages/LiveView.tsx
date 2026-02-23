/**
 * LiveView — live camera grid with Focus Mode (CG3, R7).
 * Fetches enabled cameras, renders a responsive grid of MSE video tiles.
 * Click a tile to enter Focus Mode (expands one camera, pauses others).
 * Press Escape or click the focused tile to return to grid view.
 */
import { useEffect, useRef, useState, useCallback } from "react";
import { api, CameraDetail, CameraState } from "../api/client";
import { Video, Maximize2, Minimize2 } from "lucide-react";
import VideoPlayer from "../components/VideoPlayer";

/** Cameras in these states have an active stream worth displaying. */
const STREAMABLE_STATES: CameraState[] = [
  "streaming",
  "recording",
  "connecting",
];

export default function LiveView() {
  const [cameras, setCameras] = useState<CameraDetail[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [focusedCamera, setFocusedCamera] = useState<string | null>(null);

  const fetchCameras = useCallback((signal?: AbortSignal) => {
    api
      .getCameras(signal)
      .then((data) => {
        setCameras(data);
        setError(null);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setError(err.message);
      });
  }, []);

  // Fetch cameras on mount, poll every 30s for status updates.
  // (30s is enough for status — the VideoPlayer handles its own real-time WS.)
  useEffect(() => {
    let unmounted = false;
    let currentController: AbortController | null = null;

    const poll = () => {
      if (unmounted) return;
      currentController?.abort();
      currentController = new AbortController();
      fetchCameras(currentController.signal);
    };

    poll();
    const interval = setInterval(poll, 30_000);

    return () => {
      unmounted = true;
      currentController?.abort();
      clearInterval(interval);
    };
  }, [fetchCameras]);

  // Keep a ref to focusedCamera so the Escape handler can read the current value
  // without being in the listener's dependency array — avoids tearing down and
  // re-adding the global keydown listener on every camera focus change.
  const focusedCameraRef = useRef<string | null>(null);
  useEffect(() => {
    focusedCameraRef.current = focusedCamera;
  }, [focusedCamera]);

  // Escape key exits Focus Mode — registered once on mount, never re-registered.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape" && focusedCameraRef.current) {
        setFocusedCamera(null);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  // Show all enabled cameras — VideoPlayer handles connection errors gracefully.
  // Cameras in error/idle state get a placeholder tile so users see them.
  const displayCameras = cameras?.filter((cam) => cam.enabled);

  // Grid columns based on camera count
  const gridClass = getGridClass(displayCameras?.length ?? 0);

  return (
    <div className="h-full flex flex-col">
      {/* Header — compact to maximize video space */}
      <div className="flex items-center justify-between px-6 py-3 border-b border-border">
        <h1 className="text-lg font-semibold">Live View</h1>
        {focusedCamera && (
          <button
            onClick={() => setFocusedCamera(null)}
            className="flex items-center gap-2 text-sm text-muted hover:text-white transition-colors"
          >
            <Minimize2 className="w-4 h-4" />
            Exit Focus
          </button>
        )}
      </div>

      {/* Error banner */}
      {error && (
        <div className="mx-6 mt-3 bg-red-900/20 border border-red-800 rounded-lg p-3">
          <p className="text-red-400 text-sm">{error}</p>
        </div>
      )}

      {/* Loading state */}
      {cameras === null && !error && (
        <div className="flex-1 flex items-center justify-center">
          <p className="text-muted animate-pulse">Loading cameras...</p>
        </div>
      )}

      {/* Empty state — no enabled cameras */}
      {displayCameras !== undefined && displayCameras.length === 0 && !error && (
        <div className="flex-1 flex flex-col items-center justify-center">
          <Video className="w-12 h-12 text-faint mb-4" />
          <p className="text-muted">No cameras to display</p>
          <p className="text-sm text-faint mt-1">
            Add cameras on the Cameras page to see live video here
          </p>
        </div>
      )}

      {/* Focus Mode — single camera expanded */}
      {focusedCamera && displayCameras && displayCameras.length > 0 && (() => {
        const cam = displayCameras.find((c) => c.name === focusedCamera);
        // Camera may have been disabled/deleted since focus — fall back to grid
        if (!cam) return null;
        return (
          <div className="flex-1 p-3">
            <FocusedTile camera={cam} onClose={() => setFocusedCamera(null)} />
          </div>
        );
      })()}

      {/* Grid Mode — all cameras in responsive grid */}
      {!focusedCamera && displayCameras && displayCameras.length > 0 && (
        <div className={`flex-1 p-3 ${gridClass} gap-2 overflow-auto`}>
          {displayCameras.map((cam) => (
            <CameraTile
              key={cam.id}
              camera={cam}
              onFocus={() => setFocusedCamera(cam.name)}
              active={true}
            />
          ))}
        </div>
      )}
    </div>
  );
}

/** Returns Tailwind grid classes based on camera count. */
function getGridClass(count: number): string {
  if (count <= 1) return "grid grid-cols-1";
  if (count <= 2) return "grid grid-cols-2";
  if (count <= 4) return "grid grid-cols-2";
  if (count <= 9) return "grid grid-cols-3";
  return "grid grid-cols-4";
}

/** Camera tile in the grid — video player with overlay controls. */
function CameraTile({
  camera,
  onFocus,
  active,
}: {
  camera: CameraDetail;
  onFocus: () => void;
  active: boolean;
}) {
  const ps = camera.pipeline_status;
  const state = ps?.state || "idle";
  const isStreamable = STREAMABLE_STATES.includes(state);

  return (
    <div
      className="relative bg-surface-raised border border-border rounded-lg overflow-hidden cursor-pointer group aspect-video"
      onClick={onFocus}
    >
      {isStreamable ? (
        <VideoPlayer
          cameraName={camera.name}
          active={active}
          className="w-full h-full"
        />
      ) : (
        <div className="w-full h-full flex flex-col items-center justify-center bg-surface-base">
          <Video className="w-8 h-8 text-faint mb-2" />
          <span className="text-xs text-faint capitalize">{state}</span>
        </div>
      )}

      {/* Bottom overlay bar */}
      <div className="absolute bottom-0 left-0 right-0 bg-gradient-to-t from-black/80 to-transparent px-3 py-2 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-white truncate max-w-[200px]">
            {camera.name}
          </span>
          <StatusDot state={state} />
        </div>
        <div className="flex items-center gap-2">
          {ps?.recording && (
            <span className="text-xs font-bold text-red-400 animate-pulse">
              REC
            </span>
          )}
          <Maximize2 className="w-4 h-4 text-white/60 opacity-0 group-hover:opacity-100 transition-opacity" />
        </div>
      </div>
    </div>
  );
}

/** Focused camera view — fills available space with controls. */
function FocusedTile({
  camera,
  onClose,
}: {
  camera: CameraDetail;
  onClose: () => void;
}) {
  const ps = camera.pipeline_status;
  const state = ps?.state || "idle";
  const isStreamable = STREAMABLE_STATES.includes(state);

  return (
    <div className="relative w-full h-full bg-surface-raised border border-border rounded-lg overflow-hidden">
      {isStreamable ? (
        <VideoPlayer
          cameraName={camera.name}
          active={true}
          className="w-full h-full"
        />
      ) : (
        <div className="w-full h-full flex flex-col items-center justify-center bg-surface-base">
          <Video className="w-12 h-12 text-faint mb-3" />
          <span className="text-muted capitalize">{state}</span>
          {ps?.last_error && (
            <span className="text-xs text-status-error mt-1 max-w-md text-center">
              {ps.last_error}
            </span>
          )}
        </div>
      )}

      {/* Top overlay bar with camera info */}
      <div className="absolute top-0 left-0 right-0 bg-gradient-to-b from-black/80 to-transparent px-4 py-3 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <span className="text-base font-medium text-white">
            {camera.name}
          </span>
          <StatusDot state={state} />
          {ps?.recording && (
            <span className="text-xs font-bold text-red-400 animate-pulse">
              REC
            </span>
          )}
        </div>
        <button
          onClick={(e) => {
            e.stopPropagation();
            onClose();
          }}
          className="flex items-center gap-1 text-sm text-white/70 hover:text-white transition-colors"
        >
          <Minimize2 className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
}

/** Small colored dot indicating camera state. */
function StatusDot({ state }: { state: CameraState }) {
  const colorClass =
    state === "streaming" || state === "recording"
      ? "bg-status-ok"
      : state === "connecting"
        ? "bg-status-warn"
        : state === "error"
          ? "bg-status-error"
          : "bg-faint";

  return (
    <span
      className={`inline-block w-2 h-2 rounded-full ${colorClass}`}
      title={state}
    />
  );
}
