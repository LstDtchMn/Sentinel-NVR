/**
 * Models — AI model management page (R10).
 * Displays the curated model manifest alongside locally installed models.
 * Admins can download curated models, upload custom ONNX files, and delete models.
 */
import { useEffect, useState, useRef } from "react";
import { Box, Download, Upload, Trash2, CheckCircle, Loader2 } from "lucide-react";
import { api, ModelEntry } from "../api/client";
import { useAuth } from "../context/AuthContext";
import Toast from "../components/Toast";
import { useToast } from "../hooks/useToast";

function formatSize(bytes: number): string {
  if (bytes <= 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

export default function Models() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { toast, showToast, dismissToast } = useToast();
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Download state: track which model is being downloaded.
  const [downloading, setDownloading] = useState<string | null>(null);

  // Upload state.
  const [uploadFile, setUploadFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  async function loadModels(signal?: AbortSignal) {
    try {
      setLoading(true);
      setError(null);
      const data = await api.listModels(signal);
      setModels(data);
    } catch (err: any) {
      if (err?.name !== "AbortError") setError(err?.message ?? "Failed to load models");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    const ctrl = new AbortController();
    loadModels(ctrl.signal);
    return () => ctrl.abort();
  }, []);

  async function handleDownload(filename: string) {
    try {
      setDownloading(filename);
      setError(null);
      await api.downloadModel(filename);
      // TODO(review): L1 — loadModels post-mutation lacks AbortController/unmount guard
      await loadModels();
      showToast("Model downloaded", "success");
    } catch (err: any) {
      const msg = err?.message ?? "Download failed";
      setError(msg);
      showToast(msg, "error");
    } finally {
      setDownloading(null);
    }
  }

  async function handleUpload() {
    if (!uploadFile) return;
    try {
      setUploading(true);
      setUploadError(null);
      await api.uploadModel(uploadFile);
      setUploadFile(null);
      if (fileInputRef.current) fileInputRef.current.value = "";
      await loadModels();
      showToast("Model uploaded", "success");
    } catch (err: any) {
      const msg = err?.message ?? "Upload failed";
      setUploadError(msg);
      showToast(msg, "error");
    } finally {
      setUploading(false);
    }
  }

  async function handleDelete(filename: string) {
    if (!window.confirm(`Delete model "${filename}"? This cannot be undone.`)) return;
    try {
      setError(null);
      await api.deleteModel(filename);
      await loadModels();
      showToast("Model deleted", "success");
    } catch (err: any) {
      const msg = err?.message ?? "Delete failed";
      setError(msg);
      showToast(msg, "error");
    }
  }

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <div className="flex items-center gap-3 mb-6">
        <Box className="w-6 h-6 text-sentinel-500" />
        <h1 className="text-xl font-semibold">AI Models</h1>
      </div>

      <p className="text-sm text-muted mb-6">
        Manage ONNX models for object detection, face recognition, and audio classification.
        Download curated models or upload your own.
      </p>

      {error && (
        <div className="bg-red-500/10 border border-red-500/30 text-red-400 rounded-lg px-4 py-3 text-sm mb-4">
          {error}
        </div>
      )}

      {/* Model List */}
      {loading ? (
        <div className="flex items-center justify-center py-12 text-muted">
          <Loader2 className="w-5 h-5 animate-spin mr-2" />
          Loading models…
        </div>
      ) : (
        <div className="space-y-3 mb-8">
          {models.length === 0 && (
            <div className="flex flex-col items-center justify-center py-12 gap-3">
              <Box className="w-12 h-12 text-faint" />
              <p className="text-muted">No models available</p>
              {isAdmin && (
                <p className="text-sm text-faint">
                  Upload an ONNX model file below or download a curated model to get started.
                </p>
              )}
            </div>
          )}
          {models.map((m) => (
            <div
              key={m.filename}
              className="flex items-center justify-between bg-surface-raised border border-border rounded-lg px-4 py-3"
            >
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-sm text-white truncate">{m.name}</span>
                  {m.curated && (
                    <span className="text-[10px] font-medium bg-sentinel-500/15 text-sentinel-400 px-1.5 py-0.5 rounded">
                      Curated
                    </span>
                  )}
                  {m.installed && (
                    <CheckCircle className="w-4 h-4 text-green-400 flex-shrink-0" />
                  )}
                </div>
                {m.description && (
                  <p className="text-xs text-muted mt-0.5 truncate">{m.description}</p>
                )}
                <div className="flex items-center gap-3 mt-1 text-xs text-faint">
                  <span className="font-mono">{m.filename}</span>
                  {m.size_bytes > 0 && <span>{formatSize(m.size_bytes)}</span>}
                </div>
              </div>

              <div className="flex items-center gap-2 ml-4 flex-shrink-0">
                {/* Download button for curated models not yet installed */}
                {m.curated && !m.installed && isAdmin && (
                  <button
                    onClick={() => handleDownload(m.filename)}
                    disabled={downloading !== null}
                    className="flex items-center gap-1.5 px-3 py-1.5 bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white rounded-lg text-xs font-medium transition-colors"
                  >
                    {downloading === m.filename ? (
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                      <Download className="w-3.5 h-3.5" />
                    )}
                    {downloading === m.filename ? "Downloading…" : "Download"}
                  </button>
                )}

                {/* Delete button for installed models (admin only) */}
                {m.installed && isAdmin && (
                  <button
                    onClick={() => handleDelete(m.filename)}
                    className="p-1.5 text-red-400 hover:text-red-300 hover:bg-surface-overlay rounded-lg transition-colors"
                    title="Delete model"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Upload Section (admin only) */}
      {isAdmin && (
        <section className="bg-surface-raised border border-border rounded-lg p-5">
          <h2 className="text-sm font-medium text-muted mb-3">Upload Custom Model</h2>
          <p className="text-xs text-faint mb-4">
            Upload a custom ONNX model file (.onnx). The model will be available for configuration
            after upload.
          </p>

          {uploadError && (
            <p className="text-red-400 text-sm mb-3">{uploadError}</p>
          )}

          <div className="flex items-center gap-3">
            <input
              ref={fileInputRef}
              type="file"
              accept=".onnx"
              aria-label="Choose ONNX model file"
              onChange={(e) => setUploadFile(e.target.files?.[0] ?? null)}
              className="flex-1 text-sm text-muted file:mr-3 file:py-1.5 file:px-3 file:rounded-lg file:border-0 file:text-xs file:font-medium file:bg-surface-overlay file:text-white hover:file:bg-surface-base file:transition-colors file:cursor-pointer"
            />
            <button
              onClick={handleUpload}
              disabled={!uploadFile || uploading}
              className="flex items-center gap-1.5 bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-4 py-1.5 rounded-lg text-sm font-medium transition-colors"
            >
              {uploading ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Upload className="w-4 h-4" />
              )}
              {uploading ? "Uploading…" : "Upload"}
            </button>
          </div>
        </section>
      )}
      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}
    </div>
  );
}
