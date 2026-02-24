/**
 * Settings — editable system configuration page (Phase 9, R5).
 * Allows admins to update log level, retention settings, and segment duration.
 * Storage paths are read-only (require restart to change).
 * Shows live storage usage stats from GET /api/v1/storage/stats (Phase 10, R13).
 */
import { useEffect, useRef, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { api, SystemConfig, StorageStats, PairingCode } from "../api/client";
import { Settings as SettingsIcon } from "lucide-react";

/** Format bytes as human-readable string (GiB / MiB / KiB). */
function formatBytes(bytes: number): string {
  if (bytes >= 1024 ** 3) return (bytes / 1024 ** 3).toFixed(1) + " GiB";
  if (bytes >= 1024 ** 2) return (bytes / 1024 ** 2).toFixed(1) + " MiB";
  if (bytes >= 1024) return (bytes / 1024).toFixed(1) + " KiB";
  return bytes + " B";
}

type LogLevel = "debug" | "info" | "warn" | "error";

export default function Settings() {
  const [config, setConfig] = useState<SystemConfig | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [storageStats, setStorageStats] = useState<StorageStats | null>(null);

  // Form state — initialised from config on load
  const [logLevel, setLogLevel] = useState<LogLevel>("info");
  const [hotRetention, setHotRetention] = useState(7);
  const [coldRetention, setColdRetention] = useState(30);
  const [segmentDuration, setSegmentDuration] = useState(10);

  // Submission state
  const [submitting, setSubmitting] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);

  // QR pairing state (Phase 12, CG11)
  const [pairingCode, setPairingCode] = useState<string | null>(null);
  const [pairingExpiry, setPairingExpiry] = useState<string | null>(null);
  const [pairingUrl, setPairingUrl] = useState(
    typeof window !== "undefined" ? window.location.origin : ""
  );
  const [pairingLoading, setPairingLoading] = useState(false);
  const [pairingError, setPairingError] = useState<string | null>(null);

  // Refs for cleanup on unmount
  const successTimerRef = useRef<ReturnType<typeof setTimeout>>(null);
  const saveCtrlRef = useRef<AbortController>(null);
  const pairingCtrlRef = useRef<AbortController>(null);

  useEffect(() => {
    const controller = new AbortController();

    api
      .getConfig(controller.signal)
      .then((cfg) => {
        setConfig(cfg);
        setLogLevel((cfg.server.log_level as LogLevel) || "info");
        setHotRetention(cfg.storage.hot_retention_days);
        // When cold storage is disabled (cold_path empty), backend returns 0.
        // Use Math.max(1, ...) so the form input is never below its min={1} constraint.
        setColdRetention(Math.max(1, cfg.storage.cold_retention_days));
        setSegmentDuration(cfg.storage.segment_duration);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setError(err.message);
      });

    // Storage stats are non-critical — failure silently hides the section.
    api
      .getStorageStats(controller.signal)
      .then(setStorageStats)
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        // Non-fatal: stats panel simply won't render.
      });

    return () => controller.abort();
  }, []);

  // Cleanup timers and in-flight requests on unmount
  useEffect(() => () => {
    if (successTimerRef.current) clearTimeout(successTimerRef.current);
    saveCtrlRef.current?.abort();
    pairingCtrlRef.current?.abort();
  }, []);

  // Derived dirty — true when any field differs from the loaded config
  const dirty = config != null && (
    logLevel !== (config.server.log_level || "info") ||
    hotRetention !== config.storage.hot_retention_days ||
    coldRetention !== config.storage.cold_retention_days ||
    segmentDuration !== config.storage.segment_duration
  );

  // QR payload for mobile pairing (Phase 12, CG11)
  const qrPayload = pairingCode
    ? JSON.stringify({ url: pairingUrl, code: pairingCode })
    : "";

  const handleGenerateQR = async () => {
    // Client-side URL validation (Issue #10) — reject obviously invalid URLs
    // before making the API call, so the mobile app doesn't get a bad QR payload.
    try {
      const parsed = new URL(pairingUrl.trim());
      if (!["http:", "https:"].includes(parsed.protocol)) {
        setPairingError("URL must use http:// or https://");
        return;
      }
    } catch {
      setPairingError("Please enter a valid URL (e.g. http://192.168.1.100:8099)");
      return;
    }

    setPairingLoading(true);
    setPairingError(null);
    setPairingCode(null);

    pairingCtrlRef.current?.abort();
    const ctrl = new AbortController();
    pairingCtrlRef.current = ctrl;

    try {
      const result = await api.generatePairingCode(ctrl.signal);
      setPairingCode(result.code);
      setPairingExpiry(result.expires_at);
      setPairingLoading(false);
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") return;
      setPairingError(err instanceof Error ? err.message : "Failed to generate pairing code");
      setPairingLoading(false);
    }
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setSubmitting(true);
    setSaveError(null);
    setSaveSuccess(false);

    saveCtrlRef.current?.abort();
    const ctrl = new AbortController();
    saveCtrlRef.current = ctrl;
    try {
      const updated = await api.updateConfig(
        {
          server: { log_level: logLevel },
          storage: {
            hot_retention_days: hotRetention,
            cold_retention_days: coldRetention,
            segment_duration: segmentDuration,
          },
        },
        ctrl.signal,
      );
      setConfig(updated);
      setSaveSuccess(true);
      // Auto-dismiss success banner after 3 seconds
      if (successTimerRef.current) clearTimeout(successTimerRef.current);
      successTimerRef.current = setTimeout(() => setSaveSuccess(false), 3_000);
      setSubmitting(false);
    } catch (err) {
      // On abort (component unmounted) skip all state updates — the finally block
      // would otherwise call setSubmitting(false) on the unmounted component.
      if (err instanceof DOMException && err.name === "AbortError") return;
      setSaveError(err instanceof Error ? err.message : "Failed to save settings");
      setSubmitting(false);
    }
  };

  if (!config && !error) {
    return (
      <div className="p-8 text-center py-16">
        <SettingsIcon className="w-12 h-12 text-faint mx-auto mb-4" />
        <p className="text-muted animate-pulse">Loading configuration...</p>
      </div>
    );
  }

  if (error && !config) {
    return (
      <div className="p-8">
        <h1 className="text-2xl font-semibold mb-6">Settings</h1>
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4">
          <p className="text-red-400 text-sm">Failed to load config: {error}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="p-8 max-w-2xl">
      <h1 className="text-2xl font-semibold mb-6">Settings</h1>

      {saveSuccess && (
        <div className="bg-green-900/20 border border-green-700 rounded-lg p-3 mb-6">
          <p className="text-green-400 text-sm">Settings saved.</p>
        </div>
      )}
      {saveError && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-3 mb-6">
          <p className="text-red-400 text-sm">{saveError}</p>
        </div>
      )}

      <form onSubmit={handleSave} className="space-y-6">
        {/* Server section */}
        <section className="bg-surface-raised border border-border rounded-lg p-5">
          <h2 className="text-sm font-medium text-muted mb-4">Server</h2>
          <div>
            <label htmlFor="log-level" className="block text-sm text-muted mb-1">
              Log Level
            </label>
            <select
              id="log-level"
              value={logLevel}
              onChange={(e) => { setLogLevel(e.target.value as LogLevel); setSaveSuccess(false); }}
              className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-sentinel-500"
            >
              <option value="debug">debug</option>
              <option value="info">info</option>
              <option value="warn">warn</option>
              <option value="error">error</option>
            </select>
          </div>
        </section>

        {/* Storage section */}
        <section className="bg-surface-raised border border-border rounded-lg p-5">
          <h2 className="text-sm font-medium text-muted mb-4">Storage</h2>
          <div className="space-y-4">
            <div>
              <label className="block text-sm text-muted mb-1">
                Hot Storage Path
                <span className="ml-2 text-xs text-faint">(read-only — requires restart to change)</span>
              </label>
              <p className="text-sm text-white/60 font-mono">{config?.storage.hot_path}</p>
            </div>
            {config?.storage.cold_path && (
              <div>
                <label className="block text-sm text-muted mb-1">
                  Cold Storage Path
                  <span className="ml-2 text-xs text-faint">(read-only — requires restart to change)</span>
                </label>
                <p className="text-sm text-white/60 font-mono">{config.storage.cold_path}</p>
              </div>
            )}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div>
                <label htmlFor="hot-retention" className="block text-sm text-muted mb-1">
                  Hot Retention (days)
                </label>
                <input
                  id="hot-retention"
                  type="number"
                  min={1}
                  value={hotRetention}
                  onChange={(e) => { setHotRetention(parseInt(e.target.value, 10) || 1); setSaveSuccess(false); }}
                  className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-sentinel-500"
                />
              </div>
              <div>
                <label htmlFor="cold-retention" className="block text-sm text-muted mb-1">
                  Cold Retention (days)
                </label>
                <input
                  id="cold-retention"
                  type="number"
                  min={1}
                  value={coldRetention}
                  onChange={(e) => { setColdRetention(parseInt(e.target.value, 10) || 1); setSaveSuccess(false); }}
                  className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-sentinel-500"
                />
              </div>
              <div>
                <label htmlFor="segment-duration" className="block text-sm text-muted mb-1">
                  Segment Duration (min)
                </label>
                <input
                  id="segment-duration"
                  type="number"
                  min={1}
                  max={60}
                  value={segmentDuration}
                  onChange={(e) => { setSegmentDuration(Math.min(60, parseInt(e.target.value, 10) || 1)); setSaveSuccess(false); }}
                  className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-sentinel-500"
                />
              </div>
            </div>
          </div>
        </section>

        {/* Storage usage (read-only, Phase 10, R13) — omitted if stats unavailable */}
        {storageStats && (
          <section className="bg-surface-raised border border-border rounded-lg p-5">
            <h2 className="text-sm font-medium text-muted mb-4">Storage Usage</h2>
            <div className="space-y-2 text-sm">
              <div className="flex justify-between">
                <span className="text-muted">
                  Hot storage
                  <span className="ml-2 text-xs text-faint font-mono">
                    {config?.storage.hot_path}
                  </span>
                </span>
                <span className="text-white/80 font-mono">
                  {formatBytes(storageStats.hot.used_bytes)}
                  <span className="text-faint ml-1">
                    ({storageStats.hot.segment_count.toLocaleString()} segments)
                  </span>
                </span>
              </div>
              {storageStats.cold && (
                <div className="flex justify-between">
                  <span className="text-muted">
                    Cold storage
                    <span className="ml-2 text-xs text-faint font-mono">
                      {storageStats.cold.path}
                    </span>
                  </span>
                  <span className="text-white/80 font-mono">
                    {formatBytes(storageStats.cold.used_bytes)}
                    <span className="text-faint ml-1">
                      ({storageStats.cold.segment_count.toLocaleString()} segments)
                    </span>
                  </span>
                </div>
              )}
            </div>
          </section>
        )}

        {/* Detection section (read-only) */}
        {config && (
          <section className="bg-surface-raised border border-border rounded-lg p-5">
            <h2 className="text-sm font-medium text-muted mb-4">Detection</h2>
            <div className="space-y-1 text-sm">
              <p className="text-muted">
                Backend:{" "}
                <span className="text-white/80 font-mono">{config.detection.backend || "—"}</span>
              </p>
              <p className="text-muted">
                Enabled:{" "}
                <span className="text-white/80">{config.detection.enabled ? "Yes" : "No"}</span>
              </p>
            </div>
          </section>
        )}

        {/* Remote Access — QR pairing for mobile app (Phase 12, CG11) */}
        <section className="bg-surface-raised border border-border rounded-lg p-5">
          <h2 className="text-sm font-medium text-muted mb-4">Remote Access</h2>
          <p className="text-xs text-faint mb-4">
            Generate a QR code to pair the Sentinel NVR mobile app.
            The code expires after 15 minutes and can only be used once.
          </p>

          <div className="mb-3">
            <label htmlFor="pairing-url" className="block text-sm text-muted mb-1">
              NVR URL (as reachable from your phone)
            </label>
            <input
              id="pairing-url"
              type="url"
              value={pairingUrl}
              onChange={(e) => { setPairingUrl(e.target.value); setPairingCode(null); }}
              className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-sentinel-500"
              placeholder="http://192.168.1.100:8099"
            />
          </div>

          {/* HTTPS warning (Issue #11) — QR encodes a session token; HTTP leaks it */}
          {pairingUrl.trim().toLowerCase().startsWith("http://") && (
            <p className="text-status-warn text-xs mb-2">
              Warning: The URL uses HTTP. The pairing code will be transmitted unencrypted.
              Consider using HTTPS for production deployments.
            </p>
          )}

          <button
            type="button"
            onClick={handleGenerateQR}
            disabled={pairingLoading || !pairingUrl.trim()}
            className="bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
          >
            {pairingLoading ? "Generating..." : pairingCode ? "Regenerate QR Code" : "Generate QR Code"}
          </button>

          {pairingError && (
            <p className="text-red-400 text-sm mt-2">{pairingError}</p>
          )}

          {pairingCode && (
            <div className="mt-4 flex flex-col items-center">
              <div className="bg-white p-4 rounded-lg">
                <QRCodeSVG value={qrPayload} size={200} level="M" />
              </div>
              <p className="text-xs text-faint mt-2">
                Expires: {pairingExpiry ? new Date(pairingExpiry).toLocaleTimeString() : "—"}
              </p>
            </div>
          )}
        </section>

        <div className="flex justify-end">
          <button
            type="submit"
            disabled={submitting || !dirty}
            className="bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-5 py-2 rounded-lg text-sm font-medium transition-colors"
          >
            {submitting ? "Saving..." : "Save Settings"}
          </button>
        </div>
      </form>
    </div>
  );
}
