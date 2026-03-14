/**
 * Cameras — camera management page with add/edit/delete and live status.
 * Polls /api/v1/cameras every 5s for real-time go2rtc stream health.
 * Phase 9: added Edit form and Zones link per camera card.
 */
import { useEffect, useState, useCallback, useRef } from "react";
import { Link } from "react-router-dom";
import {
  api,
  CameraDetail,
  CameraState,
  CameraInput,
  DiscoveredCamera,
  ProbeResult,
} from "../api/client";
import {
  Camera,
  Circle,
  Edit2,
  Loader2,
  MapPin,
  Monitor,
  Plus,
  Radio,
  Search,
  Trash2,
  Wifi,
  X,
} from "lucide-react";
import Toast from "../components/Toast";
import { useToast } from "../hooks/useToast";

const STATUS_COLORS: Record<CameraState, string> = {
  streaming: "text-green-400",
  recording: "text-blue-400",    // distinct from streaming — actively writing to disk
  connecting: "text-yellow-400",
  error: "text-red-400",
  idle: "text-faint",
  stopped: "text-faint",
};

/** Pre-filled camera values passed from discovery/probe to the Add Camera form. */
interface CameraPrefill {
  name: string;
  main_stream: string;
  sub_stream?: string;
  onvif_host?: string;
  onvif_port?: number;
  onvif_user?: string;
  onvif_pass?: string;
}

export default function Cameras() {
  const [cameras, setCameras] = useState<CameraDetail[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [editingCamera, setEditingCamera] = useState<CameraDetail | null>(null);
  const [showDiscovery, setShowDiscovery] = useState(false);
  const [prefill, setPrefill] = useState<CameraPrefill | null>(null);
  // Tracks fire-and-forget manual refresh controllers so they can be cancelled on unmount.
  const manualCtrlRef = useRef<AbortController | null>(null);
  // Toast feedback
  const { toast, showToast, dismissToast } = useToast();

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

  // Abort manual refresh requests on unmount.
  useEffect(() => () => manualCtrlRef.current?.abort(), []);

  const handleDelete = async (name: string) => {
    if (!window.confirm(`Delete camera "${name}"? This will remove all associated recordings.`)) return;
    // Register this controller in manualCtrlRef so the unmount cleanup aborts it.
    // ctrl.signal.aborted then serves as the mounted-check for post-await code.
    manualCtrlRef.current?.abort();
    const ctrl = new AbortController();
    manualCtrlRef.current = ctrl;
    try {
      await api.deleteCamera(name, ctrl.signal);
      if (ctrl.signal.aborted) return; // component unmounted while DELETE was in-flight
      showToast("Camera deleted", "success");
      manualCtrlRef.current?.abort();
      manualCtrlRef.current = new AbortController();
      fetchCameras(manualCtrlRef.current.signal);
    } catch (err) {
      if (ctrl.signal.aborted) return; // ignore post-unmount errors
      setError(err instanceof Error ? err.message : "Delete failed");
    }
  };

  const handleAdded = () => {
    setShowForm(false);
    setPrefill(null);
    showToast("Camera added", "success");
    manualCtrlRef.current?.abort();
    manualCtrlRef.current = new AbortController();
    fetchCameras(manualCtrlRef.current.signal);
  };

  /** Called from DiscoveryPanel when user clicks "Add" on a discovered/probed camera. */
  const handleDiscoveryAdd = (p: CameraPrefill) => {
    setPrefill(p);
    setShowDiscovery(false);
    setEditingCamera(null);
    setShowForm(true);
  };

  const handleEdited = () => {
    setEditingCamera(null);
    manualCtrlRef.current?.abort();
    manualCtrlRef.current = new AbortController();
    fetchCameras(manualCtrlRef.current.signal);
  };

  return (
    <div className="p-8">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold">Cameras</h1>
        {!showForm && !editingCamera && (
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => { setShowDiscovery(true); setShowForm(false); setEditingCamera(null); }}
              className="flex items-center gap-2 border border-border hover:border-sentinel-500 text-muted hover:text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
            >
              <Wifi className="w-4 h-4" />
              Scan Network
            </button>
            <button
              type="button"
              onClick={() => { setShowForm(true); setPrefill(null); setShowDiscovery(false); }}
              className="flex items-center gap-2 bg-sentinel-500 hover:bg-sentinel-600 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
            >
              <Plus className="w-4 h-4" />
              Add Camera
            </button>
          </div>
        )}
      </div>

      {error && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4 mb-6">
          <p className="text-red-400 text-sm">{error}</p>
        </div>
      )}

      {showDiscovery && (
        <DiscoveryPanel
          onAdd={handleDiscoveryAdd}
          onClose={() => setShowDiscovery(false)}
        />
      )}

      {showForm && (
        <AddCameraForm
          prefill={prefill}
          onSuccess={handleAdded}
          onCancel={() => { setShowForm(false); setPrefill(null); }}
        />
      )}

      {editingCamera && (
        <EditCameraForm
          key={editingCamera.id}
          camera={editingCamera}
          onSuccess={handleEdited}
          onCancel={() => setEditingCamera(null)}
        />
      )}

      {cameras === null && !error && (
        <p className="text-muted animate-pulse">Loading cameras...</p>
      )}

      {cameras !== null && cameras.length === 0 && !error && !showForm && !editingCamera && (
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
            <CameraCard
              key={cam.id}
              camera={cam}
              onDelete={handleDelete}
              onEdit={(cam) => { setShowForm(false); setEditingCamera(cam); }}
            />
          ))}
        </div>
      )}
      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}
    </div>
  );
}

function CameraCard({
  camera,
  onDelete,
  onEdit,
}: {
  camera: CameraDetail;
  onDelete: (name: string) => void;
  onEdit: (camera: CameraDetail) => void;
}) {
  const ps = camera.pipeline_status;
  const state = ps?.state || "idle";

  return (
    <div className="bg-surface-raised border border-border rounded-lg p-5">
      <div className="flex items-center justify-between mb-3">
        <h3 className="font-medium break-words mr-2">{camera.name}</h3>
        <div className="flex items-center gap-2">
          <StatusBadge status={state} />
          <button
            onClick={() => onEdit(camera)}
            className="text-faint hover:text-sentinel-400 transition-colors p-1"
            aria-label={`Edit camera ${camera.name}`}
            title="Edit camera"
          >
            <Edit2 className="w-4 h-4" />
          </button>
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
  prefill,
  onSuccess,
  onCancel,
}: {
  prefill?: CameraPrefill | null;
  onSuccess: () => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(prefill?.name || "");
  const [mainStream, setMainStream] = useState(prefill?.main_stream || "");
  const [subStream, setSubStream] = useState(prefill?.sub_stream || "");
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
        onvif_host: prefill?.onvif_host,
        onvif_port: prefill?.onvif_port,
        onvif_user: prefill?.onvif_user,
        onvif_pass: prefill?.onvif_pass,
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
              autoComplete="off"
              value={mainStream}
              onChange={(e) => setMainStream(e.target.value)}
              placeholder="rtsp://user:pass@192.168.1.100:554/stream1"
              required
              className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
            />
            <p className="mt-1 text-xs text-faint">
              Supports rtsp://, rtsps://, rtmp://, and http:// / https:// (MJPEG)
            </p>
          </div>
        </div>

        <div>
          <label htmlFor="cam-sub-stream" className="block text-sm text-muted mb-1">Sub Stream URL (optional)</label>
          <input
            id="cam-sub-stream"
            type="text"
            autoComplete="off"
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

function EditCameraForm({
  camera,
  onSuccess,
  onCancel,
}: {
  camera: CameraDetail;
  onSuccess: () => void;
  onCancel: () => void;
}) {
  const [mainStream, setMainStream] = useState(camera.main_stream);
  const [subStream, setSubStream] = useState(camera.sub_stream || "");
  const [enabled, setEnabled] = useState(camera.enabled);
  const [record, setRecord] = useState(camera.record);
  const [detect, setDetect] = useState(camera.detect);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const saveCtrlRef = useRef<AbortController>(null);

  // Abort in-flight save on unmount (e.g. user clicks Cancel or switches cameras)
  useEffect(() => () => saveCtrlRef.current?.abort(), []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!mainStream.trim()) return;

    setSubmitting(true);
    setFormError(null);

    saveCtrlRef.current?.abort();
    const ctrl = new AbortController();
    saveCtrlRef.current = ctrl;
    try {
      // zones are intentionally omitted — server preserves existing zones on update
      const input: CameraInput = {
        name: camera.name,
        main_stream: mainStream.trim(),
        sub_stream: subStream.trim() || undefined,
        enabled,
        record,
        detect,
      };
      await api.updateCamera(camera.name, input, ctrl.signal);
      onSuccess();
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") return;
      setFormError(err instanceof Error ? err.message : "Failed to update camera");
      setSubmitting(false);
    }
  };

  return (
    <div className="bg-surface-raised border border-border rounded-lg p-6 mb-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-medium">Edit Camera</h2>
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
            <label className="block text-sm text-muted mb-1">
              Name
              <span className="ml-2 text-xs text-faint">(read-only — rename via delete + recreate)</span>
            </label>
            <p className="text-sm text-white/60 font-mono py-2">{camera.name}</p>
          </div>
          <div>
            <label htmlFor="edit-main-stream" className="block text-sm text-muted mb-1">Main Stream URL *</label>
            <input
              id="edit-main-stream"
              type="text"
              autoComplete="off"
              value={mainStream}
              onChange={(e) => setMainStream(e.target.value)}
              placeholder="rtsp://user:pass@192.168.1.100:554/stream1"
              required
              className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
            />
            <p className="mt-1 text-xs text-faint">
              Supports rtsp://, rtsps://, rtmp://, and http:// / https:// (MJPEG)
            </p>
          </div>
        </div>

        <div>
          <label htmlFor="edit-sub-stream" className="block text-sm text-muted mb-1">Sub Stream URL (optional)</label>
          <input
            id="edit-sub-stream"
            type="text"
            autoComplete="off"
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
            disabled={submitting || !mainStream.trim()}
            className="bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
          >
            {submitting ? "Saving..." : "Save Changes"}
          </button>
        </div>
      </form>
    </div>
  );
}

// ---------------------------------------------------------------------------
// ONVIF Discovery Panel
// ---------------------------------------------------------------------------

function DiscoveryPanel({
  onAdd,
  onClose,
}: {
  onAdd: (prefill: CameraPrefill) => void;
  onClose: () => void;
}) {
  const [scanning, setScanning] = useState(false);
  const [scanned, setScanned] = useState(false);
  const [discovered, setDiscovered] = useState<DiscoveredCamera[]>([]);
  const [scanWarning, setScanWarning] = useState<string | null>(null);
  const [scanError, setScanError] = useState<string | null>(null);

  // Manual probe state
  const [probeHost, setProbeHost] = useState("");
  const [probePort, setProbePort] = useState("80");
  const [probeUser, setProbeUser] = useState("");
  const [probePass, setProbePass] = useState("");
  const [probing, setProbing] = useState(false);
  const [probeResult, setProbeResult] = useState<ProbeResult | null>(null);
  const [probeError, setProbeError] = useState<string | null>(null);

  // Per-discovered-camera probe state (for getting stream URIs before adding)
  const [probingCamera, setProbingCamera] = useState<string | null>(null); // xaddr being probed
  const [cameraProbeResult, setCameraProbeResult] = useState<Record<string, ProbeResult>>({});
  const [cameraProbeUser, setCameraProbeUser] = useState<Record<string, string>>({});
  const [cameraProbePass, setCameraProbePass] = useState<Record<string, string>>({});
  const [cameraProbeError, setCameraProbeError] = useState<Record<string, string>>({});

  const scanCtrlRef = useRef<AbortController | null>(null);
  const probeCtrlRef = useRef<AbortController | null>(null);
  const cameraProbCtrlRef = useRef<AbortController | null>(null);

  // Abort all in-flight requests on unmount
  useEffect(() => () => {
    scanCtrlRef.current?.abort();
    probeCtrlRef.current?.abort();
    cameraProbCtrlRef.current?.abort();
  }, []);

  const handleScan = () => {
    scanCtrlRef.current?.abort();
    const ctrl = new AbortController();
    scanCtrlRef.current = ctrl;
    setScanning(true);
    setScanError(null);
    setScanWarning(null);
    setDiscovered([]);
    setScanned(false);

    api
      .discoverCameras(ctrl.signal)
      .then((res) => {
        if (ctrl.signal.aborted) return;
        setDiscovered(res.cameras || []);
        setScanWarning(res.warning || null);
        setScanned(true);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        if (ctrl.signal.aborted) return;
        const msg = err instanceof Error ? err.message : "Network scan failed";
        if (msg.includes("403")) {
          setScanError("Admin access required to scan the network.");
        } else {
          setScanError(msg);
        }
      })
      .finally(() => {
        if (ctrl.signal.aborted) return;
        setScanning(false);
      });
  };

  const handleProbe = (e: React.FormEvent) => {
    e.preventDefault();
    if (!probeHost.trim()) return;

    probeCtrlRef.current?.abort();
    const ctrl = new AbortController();
    probeCtrlRef.current = ctrl;
    setProbing(true);
    setProbeError(null);
    setProbeResult(null);

    api
      .probeCamera(
        {
          host: probeHost.trim(),
          port: parseInt(probePort, 10) || 80,
          username: probeUser,
          password: probePass,
        },
        ctrl.signal,
      )
      .then((res) => {
        if (ctrl.signal.aborted) return;
        setProbeResult(res);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        if (ctrl.signal.aborted) return;
        setProbeError(err instanceof Error ? err.message : "Probe failed");
      })
      .finally(() => {
        if (ctrl.signal.aborted) return;
        setProbing(false);
      });
  };

  /** Probe a discovered camera to get its stream URIs before adding. */
  const handleCameraProbe = (cam: DiscoveredCamera) => {
    const key = cam.endpoint_ref || cam.xaddr;
    cameraProbCtrlRef.current?.abort();
    const ctrl = new AbortController();
    cameraProbCtrlRef.current = ctrl;
    setProbingCamera(key);
    setCameraProbeError((prev) => ({ ...prev, [key]: "" }));

    const user = cameraProbeUser[key] || "";
    const pass = cameraProbePass[key] || "";

    api
      .probeCamera(
        { host: cam.ip, port: cam.port, username: user, password: pass },
        ctrl.signal,
      )
      .then((res) => {
        if (ctrl.signal.aborted) return;
        setCameraProbeResult((prev) => ({ ...prev, [key]: res }));
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        if (ctrl.signal.aborted) return;
        setCameraProbeError((prev) => ({
          ...prev,
          [key]: err instanceof Error ? err.message : "Probe failed",
        }));
      })
      .finally(() => {
        if (ctrl.signal.aborted) return;
        setProbingCamera(null);
      });
  };

  /** Build prefill from a ProbeResult and pass it to the Add Camera form. */
  const addFromProbe = (result: ProbeResult, host: string, port: number, user: string, pass: string) => {
    const mainUri = result.streams?.[0]?.stream_uri || "";
    const subUri = result.streams?.[1]?.stream_uri || "";
    const name = sanitizeName(result.device.model || result.device.manufacturer || "Camera");
    onAdd({
      name,
      main_stream: mainUri,
      sub_stream: subUri || undefined,
      onvif_host: host,
      onvif_port: port,
      onvif_user: user || undefined,
      onvif_pass: pass || undefined,
    });
  };

  /** Quick-add from discovered camera without probe (uses xaddr-derived guess). */
  const addFromDiscovery = (cam: DiscoveredCamera) => {
    const key = cam.endpoint_ref || cam.xaddr;
    const probed = cameraProbeResult[key];
    if (probed) {
      const user = cameraProbeUser[key] || "";
      const pass = cameraProbePass[key] || "";
      addFromProbe(probed, cam.ip, cam.port, user, pass);
      return;
    }
    // Fallback: no probe result, guess RTSP from IP
    const name = sanitizeName(cam.hardware || cam.name || "Camera");
    onAdd({
      name,
      main_stream: `rtsp://${cam.ip}:554/stream1`,
      onvif_host: cam.ip,
      onvif_port: cam.port,
    });
  };

  return (
    <div className="bg-surface-raised border border-border rounded-lg p-6 mb-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-medium flex items-center gap-2">
          <Radio className="w-5 h-5 text-sentinel-400" />
          ONVIF Camera Discovery
        </h2>
        <button type="button" onClick={onClose} className="text-muted hover:text-white p-1" aria-label="Close discovery panel">
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* Scan button */}
      {!scanning && !scanned && (
        <div className="text-center py-6">
          <p className="text-muted text-sm mb-4">
            Scan your local network for ONVIF-compatible IP cameras.
          </p>
          <button
            type="button"
            onClick={handleScan}
            className="flex items-center gap-2 mx-auto bg-sentinel-500 hover:bg-sentinel-600 text-white px-5 py-2.5 rounded-lg text-sm font-medium transition-colors"
          >
            <Search className="w-4 h-4" />
            Start Scan
          </button>
        </div>
      )}

      {/* Scanning spinner */}
      {scanning && (
        <div className="flex items-center justify-center gap-3 py-8">
          <Loader2 className="w-5 h-5 text-sentinel-400 animate-spin" />
          <span className="text-muted text-sm">Scanning for cameras...</span>
        </div>
      )}

      {/* Scan error */}
      {scanError && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-3 mb-4">
          <p className="text-red-400 text-sm">{scanError}</p>
          <button
            type="button"
            onClick={handleScan}
            className="text-red-400 hover:text-red-300 text-xs mt-2 underline"
          >
            Retry
          </button>
        </div>
      )}

      {/* Scan warning */}
      {scanWarning && (
        <div className="bg-yellow-900/20 border border-yellow-800 rounded-lg p-3 mb-4">
          <p className="text-yellow-400 text-sm">{scanWarning}</p>
        </div>
      )}

      {/* Discovery results */}
      {scanned && !scanning && (
        <div className="mb-4">
          {discovered.length > 0 ? (
            <>
              <p className="text-sm text-muted mb-3">
                Found {discovered.length} camera{discovered.length !== 1 ? "s" : ""} on the network:
              </p>
              <div className="space-y-2 max-h-64 overflow-y-auto">
                {discovered.map((cam) => {
                  const key = cam.endpoint_ref || cam.xaddr;
                  const probed = cameraProbeResult[key];
                  const probeErr = cameraProbeError[key];
                  const isProbing = probingCamera === key;
                  return (
                    <DiscoveredCameraRow
                      key={key}
                      camera={cam}
                      probeResult={probed}
                      probeError={probeErr}
                      isProbing={isProbing}
                      probeUser={cameraProbeUser[key] || ""}
                      probePass={cameraProbePass[key] || ""}
                      onProbeUserChange={(v) => setCameraProbeUser((prev) => ({ ...prev, [key]: v }))}
                      onProbePassChange={(v) => setCameraProbePass((prev) => ({ ...prev, [key]: v }))}
                      onProbe={() => handleCameraProbe(cam)}
                      onAdd={() => addFromDiscovery(cam)}
                    />
                  );
                })}
              </div>
            </>
          ) : (
            <div className="bg-surface-base border border-border rounded-lg p-4 text-center">
              <Wifi className="w-8 h-8 text-faint mx-auto mb-2" />
              <p className="text-muted text-sm mb-1">No cameras found via network scan.</p>
              <p className="text-faint text-xs">
                This can happen in Docker bridge mode or if cameras don't support WS-Discovery.
              </p>
            </div>
          )}

          <button
            type="button"
            onClick={handleScan}
            className="text-sentinel-400 hover:text-sentinel-300 text-xs mt-3 underline"
          >
            Scan again
          </button>
        </div>
      )}

      {/* Manual probe section — always visible after scan or as fallback */}
      <div className="border-t border-border pt-4 mt-4">
        <h3 className="text-sm font-medium text-muted mb-3 flex items-center gap-2">
          <Monitor className="w-4 h-4" />
          Manual Probe
        </h3>
        <p className="text-faint text-xs mb-3">
          Enter the IP address of a camera to probe it directly for stream information.
        </p>
        <form onSubmit={handleProbe} className="space-y-3">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <div>
              <label htmlFor="probe-host" className="block text-xs text-faint mb-1">IP Address *</label>
              <input
                id="probe-host"
                type="text"
                value={probeHost}
                onChange={(e) => setProbeHost(e.target.value)}
                placeholder="192.168.1.100"
                required
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
              />
            </div>
            <div>
              <label htmlFor="probe-port" className="block text-xs text-faint mb-1">Port</label>
              <input
                id="probe-port"
                type="number"
                value={probePort}
                onChange={(e) => setProbePort(e.target.value)}
                placeholder="80"
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
              />
            </div>
            <div>
              <label htmlFor="probe-user" className="block text-xs text-faint mb-1">Username</label>
              <input
                id="probe-user"
                type="text"
                value={probeUser}
                onChange={(e) => setProbeUser(e.target.value)}
                placeholder="admin"
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
              />
            </div>
            <div>
              <label htmlFor="probe-pass" className="block text-xs text-faint mb-1">Password</label>
              <input
                id="probe-pass"
                type="password"
                value={probePass}
                onChange={(e) => setProbePass(e.target.value)}
                placeholder="password"
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
              />
            </div>
          </div>
          <div className="flex items-center gap-3">
            <button
              type="submit"
              disabled={probing || !probeHost.trim()}
              className="flex items-center gap-2 bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
            >
              {probing ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Search className="w-4 h-4" />
              )}
              {probing ? "Probing..." : "Probe Camera"}
            </button>
          </div>
        </form>

        {/* Probe error */}
        {probeError && (
          <div className="bg-red-900/20 border border-red-800 rounded-lg p-3 mt-3">
            <p className="text-red-400 text-sm">{probeError}</p>
          </div>
        )}

        {/* Probe result */}
        {probeResult && (
          <div className="bg-surface-base border border-border rounded-lg p-4 mt-3">
            <div className="flex items-center justify-between mb-3">
              <div>
                <p className="text-sm font-medium text-white">
                  {probeResult.device.manufacturer} {probeResult.device.model}
                </p>
                <p className="text-xs text-faint">
                  FW: {probeResult.device.firmware_version || "N/A"}
                  {probeResult.device.serial_number ? ` | S/N: ${probeResult.device.serial_number}` : ""}
                </p>
              </div>
              <button
                type="button"
                onClick={() =>
                  addFromProbe(probeResult, probeHost.trim(), parseInt(probePort, 10) || 80, probeUser, probePass)
                }
                className="flex items-center gap-1.5 bg-green-600 hover:bg-green-700 text-white px-3 py-1.5 rounded-lg text-sm font-medium transition-colors"
              >
                <Plus className="w-3.5 h-3.5" />
                Add
              </button>
            </div>
            {probeResult.streams && probeResult.streams.length > 0 && (
              <div className="space-y-1.5">
                <p className="text-xs text-muted font-medium">Stream Profiles:</p>
                {probeResult.streams.map((s, i) => (
                  <div
                    key={s.token || i}
                    className="flex items-center gap-3 text-xs bg-surface-raised rounded px-3 py-2"
                  >
                    <span className="text-sentinel-400 font-medium min-w-[60px]">
                      {s.name || `Profile ${i + 1}`}
                    </span>
                    <span className="text-muted">{s.resolution}</span>
                    <span className="text-faint">{s.encoding}</span>
                    <span className="text-faint truncate flex-1 font-mono" title={s.stream_uri}>
                      {s.stream_uri}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/** A single discovered camera row with inline credential inputs and probe/add buttons. */
function DiscoveredCameraRow({
  camera,
  probeResult,
  probeError,
  isProbing,
  probeUser,
  probePass,
  onProbeUserChange,
  onProbePassChange,
  onProbe,
  onAdd,
}: {
  camera: DiscoveredCamera;
  probeResult?: ProbeResult;
  probeError?: string;
  isProbing: boolean;
  probeUser: string;
  probePass: string;
  onProbeUserChange: (v: string) => void;
  onProbePassChange: (v: string) => void;
  onProbe: () => void;
  onAdd: () => void;
}) {
  const [showCreds, setShowCreds] = useState(false);

  return (
    <div className="bg-surface-base border border-border rounded-lg p-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3 min-w-0">
          <Camera className="w-5 h-5 text-sentinel-400 flex-shrink-0" />
          <div className="min-w-0">
            <p className="text-sm font-medium text-white truncate">
              {camera.name || camera.hardware || "Unknown Camera"}
            </p>
            <p className="text-xs text-faint">
              {camera.ip}:{camera.port}
              {camera.hardware && camera.name ? ` | ${camera.hardware}` : ""}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          {!probeResult && (
            <button
              type="button"
              onClick={() => setShowCreds(!showCreds)}
              className="text-xs text-faint hover:text-muted transition-colors px-2 py-1"
              title="Enter credentials to probe for stream URLs"
            >
              {showCreds ? "Hide" : "Credentials"}
            </button>
          )}
          {!probeResult && (
            <button
              type="button"
              onClick={onProbe}
              disabled={isProbing}
              className="flex items-center gap-1.5 border border-border hover:border-sentinel-500 text-muted hover:text-white px-3 py-1.5 rounded-lg text-xs font-medium transition-colors disabled:opacity-50"
            >
              {isProbing ? (
                <Loader2 className="w-3 h-3 animate-spin" />
              ) : (
                <Search className="w-3 h-3" />
              )}
              Probe
            </button>
          )}
          <button
            type="button"
            onClick={onAdd}
            className="flex items-center gap-1.5 bg-green-600 hover:bg-green-700 text-white px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
          >
            <Plus className="w-3 h-3" />
            Add
          </button>
        </div>
      </div>

      {/* Inline credentials for probing */}
      {showCreds && !probeResult && (
        <div className="grid grid-cols-2 gap-2 mt-2 pt-2 border-t border-border">
          <input
            type="text"
            value={probeUser}
            onChange={(e) => onProbeUserChange(e.target.value)}
            placeholder="Username"
            className="bg-surface-raised border border-border rounded px-2 py-1.5 text-xs text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
          />
          <input
            type="password"
            value={probePass}
            onChange={(e) => onProbePassChange(e.target.value)}
            placeholder="Password"
            className="bg-surface-raised border border-border rounded px-2 py-1.5 text-xs text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
          />
        </div>
      )}

      {/* Probe error */}
      {probeError && (
        <p className="text-red-400 text-xs mt-2">{probeError}</p>
      )}

      {/* Probe result (inline) */}
      {probeResult && (
        <div className="mt-2 pt-2 border-t border-border">
          <p className="text-xs text-muted">
            {probeResult.device.manufacturer} {probeResult.device.model}
            {probeResult.streams?.length ? ` | ${probeResult.streams.length} stream profile${probeResult.streams.length !== 1 ? "s" : ""}` : ""}
          </p>
          {probeResult.streams?.map((s, i) => (
            <p key={s.token || i} className="text-xs text-faint font-mono truncate mt-0.5" title={s.stream_uri}>
              {s.name || `Profile ${i + 1}`}: {s.resolution} {s.encoding} — {s.stream_uri}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}

/** Sanitize a device name for use as a camera name (alphanumeric, spaces, hyphens, underscores). */
function sanitizeName(raw: string): string {
  return raw
    .replace(/[^a-zA-Z0-9 _-]/g, "")
    .trim()
    .slice(0, 64)
    .trim() || "Camera";
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
