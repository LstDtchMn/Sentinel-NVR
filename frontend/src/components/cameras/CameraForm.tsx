import { useState, useEffect, useRef } from "react";
import { X } from "lucide-react";
import { api, CameraInput, CameraDetail } from "../../api/client";

/** Pre-filled camera values passed from discovery/probe to the Add Camera form. */
export interface CameraPrefill {
  name: string;
  main_stream: string;
  sub_stream?: string;
  onvif_host?: string;
  onvif_port?: number;
  onvif_user?: string;
  onvif_pass?: string;
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

export function AddCameraForm({
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

export function EditCameraForm({
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
