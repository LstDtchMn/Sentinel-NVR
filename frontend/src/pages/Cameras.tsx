/**
 * Cameras — camera management page with add/delete and live status.
 * Polls /api/v1/cameras every 5s for real-time go2rtc stream health.
 */
import { useEffect, useState, useCallback } from "react";
import { api, CameraDetail, CameraState, CameraInput } from "../api/client";
import { Camera, Circle, Plus, Trash2, X } from "lucide-react";

const STATUS_COLORS: Record<CameraState, string> = {
  streaming: "text-green-400",
  recording: "text-blue-400",    // distinct from streaming — actively writing to disk
  connecting: "text-yellow-400",
  error: "text-red-400",
  idle: "text-faint",
  stopped: "text-faint",
};

export default function Cameras() {
  const [cameras, setCameras] = useState<CameraDetail[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);

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
    const interval = setInterval(poll, 5_000);

    return () => {
      unmounted = true;
      currentController?.abort();
      clearInterval(interval);
    };
  }, [fetchCameras]);

  const handleDelete = async (name: string) => {
    if (!window.confirm(`Delete camera "${name}"? This cannot be undone.`)) return;
    try {
      await api.deleteCamera(name);
      fetchCameras();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Delete failed");
    }
  };

  const handleAdded = () => {
    setShowForm(false);
    fetchCameras();
  };

  return (
    <div className="p-8">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold">Cameras</h1>
        {!showForm && (
          <button
            onClick={() => setShowForm(true)}
            className="flex items-center gap-2 bg-sentinel-500 hover:bg-sentinel-600 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
          >
            <Plus className="w-4 h-4" />
            Add Camera
          </button>
        )}
      </div>

      {error && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4 mb-6">
          <p className="text-red-400 text-sm">{error}</p>
        </div>
      )}

      {showForm && (
        <AddCameraForm
          onSuccess={handleAdded}
          onCancel={() => setShowForm(false)}
        />
      )}

      {cameras === null && !error && (
        <p className="text-muted animate-pulse">Loading cameras...</p>
      )}

      {cameras !== null && cameras.length === 0 && !error && !showForm && (
        <div className="text-center py-16">
          <Camera className="w-12 h-12 text-faint mx-auto mb-4" />
          <p className="text-muted">No cameras configured</p>
          <button
            onClick={() => setShowForm(true)}
            className="mt-4 text-sentinel-400 hover:text-sentinel-300 text-sm font-medium"
          >
            Add your first camera
          </button>
        </div>
      )}

      {cameras !== null && cameras.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {cameras.map((cam) => (
            <CameraCard key={cam.id} camera={cam} onDelete={handleDelete} />
          ))}
        </div>
      )}
    </div>
  );
}

function CameraCard({
  camera,
  onDelete,
}: {
  camera: CameraDetail;
  onDelete: (name: string) => void;
}) {
  const ps = camera.pipeline_status;
  const state = ps?.state || "idle";

  return (
    <div className="bg-surface-raised border border-border rounded-lg p-5">
      <div className="flex items-center justify-between mb-3">
        <h3 className="font-medium truncate mr-2">{camera.name}</h3>
        <div className="flex items-center gap-2">
          <StatusBadge status={state} />
          <button
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
      </div>
    </div>
  );
}

function StreamIndicator({ label, active }: { label: string; active?: boolean }) {
  return (
    <span className="flex items-center gap-1 text-xs">
      <span className={`w-1.5 h-1.5 rounded-full ${active ? "bg-green-400" : "bg-faint"}`} />
      {label}
    </span>
  );
}

function StatusBadge({ status }: { status: CameraState }) {
  return (
    <span
      className={`flex items-center gap-1.5 text-xs font-medium ${STATUS_COLORS[status] || "text-faint"}`}
    >
      <Circle className="w-2 h-2 fill-current" />
      {status}
    </span>
  );
}

function AddCameraForm({
  onSuccess,
  onCancel,
}: {
  onSuccess: () => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState("");
  const [mainStream, setMainStream] = useState("");
  const [subStream, setSubStream] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [record, setRecord] = useState(true);
  const [detect, setDetect] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !mainStream.trim()) return;

    setSubmitting(true);
    setFormError(null);

    try {
      const input: CameraInput = {
        name: name.trim(),
        main_stream: mainStream.trim(),
        sub_stream: subStream.trim() || undefined,
        enabled,
        record,
        detect,
      };
      await api.createCamera(input);
      onSuccess();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Failed to add camera");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="bg-surface-raised border border-border rounded-lg p-6 mb-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-medium">Add Camera</h2>
        <button onClick={onCancel} className="text-muted hover:text-white p-1" aria-label="Close form">
          <X className="w-5 h-5" />
        </button>
      </div>

      {formError && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-3 mb-4">
          <p className="text-red-400 text-sm">{formError}</p>
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label htmlFor="cam-name" className="block text-sm text-muted mb-1">Name *</label>
            <input
              id="cam-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Front Door"
              required
              className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
            />
          </div>
          <div>
            <label htmlFor="cam-main-stream" className="block text-sm text-muted mb-1">Main Stream URL *</label>
            <input
              id="cam-main-stream"
              type="text"
              value={mainStream}
              onChange={(e) => setMainStream(e.target.value)}
              placeholder="rtsp://user:pass@192.168.1.100:554/stream1"
              required
              className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
            />
          </div>
        </div>

        <div>
          <label htmlFor="cam-sub-stream" className="block text-sm text-muted mb-1">Sub Stream URL (optional)</label>
          <input
            id="cam-sub-stream"
            type="text"
            value={subStream}
            onChange={(e) => setSubStream(e.target.value)}
            placeholder="rtsp://user:pass@192.168.1.100:554/stream2"
            className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
          />
        </div>

        <div className="flex items-center gap-6">
          <Toggle label="Enabled" checked={enabled} onChange={setEnabled} />
          <Toggle label="Record" checked={record} onChange={setRecord} />
          <Toggle label="Detect" checked={detect} onChange={setDetect} />
        </div>

        <div className="flex justify-end gap-3 pt-2">
          <button
            type="button"
            onClick={onCancel}
            className="px-4 py-2 text-sm text-muted hover:text-white transition-colors"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={submitting || !name.trim() || !mainStream.trim()}
            className="bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
          >
            {submitting ? "Adding..." : "Add Camera"}
          </button>
        </div>
      </form>
    </div>
  );
}

function Toggle({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-2 text-sm cursor-pointer select-none">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="sr-only peer"
        role="switch"
        aria-checked={checked}
      />
      <div
        className={`w-8 h-5 rounded-full relative transition-colors cursor-pointer ${
          checked ? "bg-sentinel-500" : "bg-border"
        }`}
      >
        <div
          className={`absolute top-[3px] w-3.5 h-3.5 rounded-full bg-white transition-transform ${
            checked ? "translate-x-[14px]" : "translate-x-[3px]"
          }`}
        />
      </div>
      <span className="text-muted">{label}</span>
    </label>
  );
}
