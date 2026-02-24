/**
 * Import — migration tool for importing cameras from Blue Iris or Frigate (Phase 14, R15).
 * Upload a Blue Iris .reg export or Frigate config.yml to preview and import cameras.
 * Admin only — non-admins see a permission message.
 */
import { useState, useRef, useEffect } from "react";
import { Upload, FileUp, CheckCircle, AlertTriangle, XCircle } from "lucide-react";
import { api, ImportResult, ImportExecuteResult } from "../api/client";
import { useAuth } from "../context/AuthContext";

type ImportFormat = "blue_iris" | "frigate";

export default function Import() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  const [format, setFormat] = useState<ImportFormat>("blue_iris");
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<ImportResult | null>(null);
  const [result, setResult] = useState<ImportExecuteResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ctrlRef = useRef<AbortController | null>(null);

  // Abort in-flight requests on unmount.
  useEffect(() => () => ctrlRef.current?.abort(), []);

  async function handlePreview() {
    if (!file) return;
    ctrlRef.current?.abort();
    const ctrl = new AbortController();
    ctrlRef.current = ctrl;

    setLoading(true);
    setError(null);
    setPreview(null);
    setResult(null);

    try {
      const res = await api.importPreview(format, file, ctrl.signal);
      if (ctrl.signal.aborted) return;
      setPreview(res);
      setLoading(false);
    } catch (err) {
      if (ctrl.signal.aborted) return;
      if (err instanceof DOMException && err.name === "AbortError") return;
      setError(err instanceof Error ? err.message : "Preview failed");
      setLoading(false);
    }
  }

  async function handleImport() {
    if (!file) return;
    ctrlRef.current?.abort();
    const ctrl = new AbortController();
    ctrlRef.current = ctrl;

    setLoading(true);
    setError(null);
    setResult(null);

    try {
      const res = await api.importExecute(format, file, ctrl.signal);
      if (ctrl.signal.aborted) return;
      setResult(res);
      setPreview(null);
      setLoading(false);
    } catch (err) {
      if (ctrl.signal.aborted) return;
      if (err instanceof DOMException && err.name === "AbortError") return;
      setError(err instanceof Error ? err.message : "Import failed");
      setLoading(false);
    }
  }

  if (!isAdmin) {
    return (
      <div className="p-8 flex items-center justify-center min-h-full">
        <p className="text-muted">Only administrators can import cameras.</p>
      </div>
    );
  }

  return (
    <div className="p-8 flex flex-col gap-6 max-w-3xl">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Upload className="w-6 h-6 text-sentinel-500" />
        <h1 className="text-2xl font-semibold">Import Cameras</h1>
      </div>

      <p className="text-sm text-muted">
        Migrate camera configurations from Blue Iris (.reg export) or Frigate (config.yml).
        Upload the file, preview what will be imported, then confirm.
      </p>

      {/* Format + File selection */}
      <div className="bg-surface-raised border border-border rounded-lg p-6 space-y-4">
        <div>
          <label className="block text-sm text-muted mb-2">Source NVR</label>
          <div className="flex gap-4">
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="format"
                value="blue_iris"
                checked={format === "blue_iris"}
                onChange={() => { setFormat("blue_iris"); setPreview(null); setResult(null); }}
                className="accent-sentinel-500"
              />
              Blue Iris (.reg)
            </label>
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="format"
                value="frigate"
                checked={format === "frigate"}
                onChange={() => { setFormat("frigate"); setPreview(null); setResult(null); }}
                className="accent-sentinel-500"
              />
              Frigate (config.yml)
            </label>
          </div>
        </div>

        <div>
          <label className="block text-sm text-muted mb-2">Configuration file</label>
          <label className="flex items-center gap-3 cursor-pointer bg-surface-base border border-border
                            rounded-lg px-4 py-3 text-sm hover:border-sentinel-500 transition-colors">
            <FileUp className="w-5 h-5 text-muted" />
            <span className="text-muted">
              {file ? file.name : format === "blue_iris" ? "Choose .reg file..." : "Choose config.yml..."}
            </span>
            <input
              type="file"
              accept={format === "blue_iris" ? ".reg" : ".yml,.yaml"}
              onChange={(e) => {
                setFile(e.target.files?.[0] ?? null);
                setPreview(null);
                setResult(null);
              }}
              className="hidden"
            />
          </label>
        </div>

        <div className="flex gap-3 pt-2">
          <button
            onClick={handlePreview}
            disabled={!file || loading}
            className="bg-surface-overlay hover:bg-surface-base border border-border text-sm px-4 py-2
                       rounded-lg transition-colors disabled:opacity-50"
          >
            {loading && !preview && !result ? "Parsing..." : "Preview"}
          </button>
          {preview && preview.cameras.length > 0 && (
            <button
              onClick={handleImport}
              disabled={loading}
              className="bg-sentinel-500 hover:bg-sentinel-600 text-white text-sm px-4 py-2
                         rounded-lg font-medium transition-colors disabled:opacity-50"
            >
              {loading ? "Importing..." : `Import ${preview.cameras.length} camera${preview.cameras.length === 1 ? "" : "s"}`}
            </button>
          )}
        </div>
      </div>

      {error && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4 flex items-start gap-3">
          <XCircle className="w-5 h-5 text-red-400 shrink-0 mt-0.5" />
          <p className="text-red-400 text-sm">{error}</p>
        </div>
      )}

      {/* Preview results */}
      {preview && (
        <div className="space-y-4">
          <h2 className="text-lg font-medium">Preview</h2>

          {preview.cameras.length > 0 && (
            <div className="bg-surface-raised border border-border rounded-lg divide-y divide-border">
              {preview.cameras.map((cam, i) => (
                <div key={i} className="px-4 py-3 text-sm">
                  <div className="flex items-center gap-3">
                    <span className="font-medium">{cam.name}</span>
                    <span className="text-xs text-faint">
                      {cam.enabled ? "Enabled" : "Disabled"}
                      {cam.record ? " · Record" : ""}
                      {cam.detect ? " · Detect" : ""}
                    </span>
                  </div>
                  <p className="text-xs text-muted mt-1 font-mono truncate">{cam.main_stream}</p>
                  {cam.sub_stream && (
                    <p className="text-xs text-faint font-mono truncate">Sub: {cam.sub_stream}</p>
                  )}
                </div>
              ))}
            </div>
          )}

          {preview.warnings.length > 0 && (
            <div className="space-y-1">
              {preview.warnings.map((w, i) => (
                <div key={i} className="flex items-start gap-2 text-xs text-status-warn">
                  <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                  {w}
                </div>
              ))}
            </div>
          )}

          {preview.errors.length > 0 && (
            <div className="space-y-1">
              {preview.errors.map((e, i) => (
                <div key={i} className="flex items-start gap-2 text-xs text-status-error">
                  <XCircle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                  {e}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Import results */}
      {result && (
        <div className="bg-surface-raised border border-border rounded-lg p-6 space-y-3">
          <div className="flex items-center gap-3">
            <CheckCircle className="w-6 h-6 text-green-400" />
            <h2 className="text-lg font-medium">Import Complete</h2>
          </div>
          <div className="text-sm text-muted space-y-1">
            <p>Imported: <span className="text-white font-medium">{result.imported}</span></p>
            {result.skipped > 0 && (
              <p>Skipped (already exist): <span className="text-status-warn font-medium">{result.skipped}</span></p>
            )}
            {result.errors.length > 0 && (
              <p>Errors: <span className="text-status-error font-medium">{result.errors.length}</span></p>
            )}
          </div>

          {result.warnings.length > 0 && (
            <div className="pt-2 space-y-1">
              {result.warnings.map((w, i) => (
                <p key={i} className="text-xs text-status-warn">{w}</p>
              ))}
            </div>
          )}
          {result.errors.length > 0 && (
            <div className="pt-2 space-y-1">
              {result.errors.map((e, i) => (
                <p key={i} className="text-xs text-status-error">{e}</p>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
