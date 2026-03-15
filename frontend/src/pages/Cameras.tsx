/**
 * Cameras — camera management page with add/edit/delete and live status.
 * Polls /api/v1/cameras every 5s for real-time go2rtc stream health.
 * Phase 9: added Edit form and Zones link per camera card.
 */
import { useEffect, useState, useCallback, useRef } from "react";
import {
  api,
  CameraDetail,
} from "../api/client";
import {
  Camera,
  Plus,
  Wifi,
} from "lucide-react";
import Toast from "../components/Toast";
import { useToast } from "../hooks/useToast";
import { CameraCard } from "../components/cameras/CameraCard";
import { AddCameraForm, EditCameraForm, CameraPrefill } from "../components/cameras/CameraForm";
import { DiscoveryPanel } from "../components/cameras/DiscoveryPanel";

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

  const handleRestart = async (name: string) => {
    manualCtrlRef.current?.abort();
    const ctrl = new AbortController();
    manualCtrlRef.current = ctrl;
    try {
      await api.restartCamera(name, ctrl.signal);
      if (ctrl.signal.aborted) return;
      showToast("Pipeline restarted", "success");
      manualCtrlRef.current?.abort();
      manualCtrlRef.current = new AbortController();
      fetchCameras(manualCtrlRef.current.signal);
    } catch (err) {
      if (ctrl.signal.aborted) return;
      showToast(err instanceof Error ? err.message : "Restart failed", "error");
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
    showToast("Camera updated", "success");
    manualCtrlRef.current?.abort();
    manualCtrlRef.current = new AbortController();
    fetchCameras(manualCtrlRef.current.signal);
  };

  return (
    <div className="p-8">
      <div className="flex items-center justify-between mb-6 flex-wrap gap-3">
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
              onRestart={handleRestart}
            />
          ))}
        </div>
      )}
      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}
    </div>
  );
}
