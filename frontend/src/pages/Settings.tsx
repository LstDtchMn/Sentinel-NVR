/**
 * Settings — editable system configuration page (Phase 9, R5).
 * Allows admins to update log level, retention settings, and segment duration.
 * Storage paths are read-only (require restart to change).
 * Shows live storage usage stats from GET /api/v1/storage/stats (Phase 10, R13).
 */
import { useEffect, useRef, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { api, SystemConfig, StorageStats, RetentionRule, CameraDetail } from "../api/client";
import { Settings as SettingsIcon } from "lucide-react";
import Toast from "../components/Toast";
import { useToast } from "../hooks/useToast";

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

  // MQTT state (v0.3)
  const [mqttEnabled, setMqttEnabled] = useState(false);
  const [mqttBroker, setMqttBroker] = useState("");
  const [mqttTopicPrefix, setMqttTopicPrefix] = useState("sentinel");
  const [mqttUsername, setMqttUsername] = useState("");
  const [mqttPassword, setMqttPassword] = useState("");
  const [mqttHADiscovery, setMqttHADiscovery] = useState(false);

  // SMTP email notification state
  const [smtpHost, setSmtpHost] = useState("");
  const [smtpPort, setSmtpPort] = useState(587);
  const [smtpUsername, setSmtpUsername] = useState("");
  const [smtpPassword, setSmtpPassword] = useState("");
  const [smtpFrom, setSmtpFrom] = useState("");
  const [smtpTls, setSmtpTls] = useState(true);

  // Submission state
  const [submitting, setSubmitting] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);

  // Retention rules state (R14)
  const [retentionRules, setRetentionRules] = useState<RetentionRule[]>([]);
  const [cameras, setCameras] = useState<CameraDetail[]>([]);
  const [newRuleCameraId, setNewRuleCameraId] = useState<string>("");   // "" = wildcard
  const [newRuleEventType, setNewRuleEventType] = useState<string>("");  // "" = wildcard
  const [newRuleDays, setNewRuleDays] = useState<number>(30);
  const [retentionError, setRetentionError] = useState<string | null>(null);
  const [retentionSubmitting, setRetentionSubmitting] = useState(false);

  // QR pairing state (Phase 12, CG11)
  const [pairingCode, setPairingCode] = useState<string | null>(null);
  const [pairingExpiry, setPairingExpiry] = useState<string | null>(null);
  const [pairingUrl, setPairingUrl] = useState(
    typeof window !== "undefined" ? window.location.origin : ""
  );
  const [pairingLoading, setPairingLoading] = useState(false);
  const [pairingError, setPairingError] = useState<string | null>(null);

  // Toast feedback
  const { toast, showToast, dismissToast } = useToast();

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
        // MQTT
        if (cfg.mqtt) {
          setMqttEnabled(cfg.mqtt.enabled);
          setMqttBroker(cfg.mqtt.broker || "");
          setMqttTopicPrefix(cfg.mqtt.topic_prefix || "sentinel");
          setMqttUsername(cfg.mqtt.username || "");
          setMqttPassword(cfg.mqtt.password || "");
          setMqttHADiscovery(cfg.mqtt.ha_discovery);
        }
        // SMTP
        if (cfg.notifications?.smtp) {
          setSmtpHost(cfg.notifications.smtp.host || "");
          setSmtpPort(cfg.notifications.smtp.port || 587);
          setSmtpUsername(cfg.notifications.smtp.username || "");
          setSmtpPassword(cfg.notifications.smtp.password || "");
          setSmtpFrom(cfg.notifications.smtp.from || "");
          setSmtpTls(cfg.notifications.smtp.tls ?? true);
        }
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

    // Retention rules (R14)
    api
      .listRetentionRules(controller.signal)
      .then(setRetentionRules)
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        // Non-fatal: retention section still renders without existing rules.
      });

    // Camera list for the retention rule camera picker
    api
      .getCameras(controller.signal)
      .then(setCameras)
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
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
    coldRetention !== Math.max(1, config.storage.cold_retention_days) ||
    segmentDuration !== config.storage.segment_duration ||
    mqttEnabled !== (config.mqtt?.enabled ?? false) ||
    mqttBroker !== (config.mqtt?.broker ?? "") ||
    mqttTopicPrefix !== (config.mqtt?.topic_prefix ?? "sentinel") ||
    mqttUsername !== (config.mqtt?.username ?? "") ||
    mqttPassword !== (config.mqtt?.password ?? "") ||
    mqttHADiscovery !== (config.mqtt?.ha_discovery ?? false) ||
    smtpHost !== (config.notifications?.smtp?.host ?? "") ||
    smtpPort !== (config.notifications?.smtp?.port ?? 587) ||
    smtpUsername !== (config.notifications?.smtp?.username ?? "") ||
    smtpPassword !== (config.notifications?.smtp?.password ?? "") ||
    smtpFrom !== (config.notifications?.smtp?.from ?? "") ||
    smtpTls !== (config.notifications?.smtp?.tls ?? true)
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

  // TODO(review): L2 — retention rule handlers lack AbortController
  const handleAddRetentionRule = async () => {
    if (retentionSubmitting) return;
    if (newRuleDays < 1) {
      setRetentionError("Days must be at least 1");
      return;
    }
    setRetentionSubmitting(true);
    setRetentionError(null);
    try {
      const rule = await api.createRetentionRule({
        camera_id: newRuleCameraId ? parseInt(newRuleCameraId, 10) : null,
        event_type: newRuleEventType || null,
        events_days: newRuleDays,
      });
      setRetentionRules((prev) => [...prev, rule]);
      setNewRuleCameraId("");
      setNewRuleEventType("");
      setNewRuleDays(30);
      showToast("Retention rule added", "success");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Failed to create rule";
      setRetentionError(msg);
      showToast(msg, "error");
    } finally {
      setRetentionSubmitting(false);
    }
  };

  const handleDeleteRetentionRule = async (id: number) => {
    if (!window.confirm("Remove this retention rule? This cannot be undone.")) return;
    try {
      await api.deleteRetentionRule(id);
      setRetentionRules((prev) => prev.filter((r) => r.id !== id));
      showToast("Retention rule removed", "success");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Failed to delete rule";
      setRetentionError(msg);
      showToast(msg, "error");
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
          mqtt: {
            enabled: mqttEnabled,
            broker: mqttBroker,
            topic_prefix: mqttTopicPrefix,
            username: mqttUsername,
            password: mqttPassword,
            ha_discovery: mqttHADiscovery,
          },
          notifications: {
            smtp: {
              host: smtpHost,
              port: smtpPort,
              username: smtpUsername,
              password: smtpPassword,
              from: smtpFrom,
              tls: smtpTls,
            },
          },
        },
        ctrl.signal,
      );
      setConfig(updated);
      setSaveSuccess(true);
      showToast("Settings saved", "success");
      // Auto-dismiss success banner after 3 seconds
      if (successTimerRef.current) clearTimeout(successTimerRef.current);
      successTimerRef.current = setTimeout(() => setSaveSuccess(false), 3_000);
      setSubmitting(false);
    } catch (err) {
      // On abort (component unmounted) skip all state updates — the finally block
      // would otherwise call setSubmitting(false) on the unmounted component.
      if (err instanceof DOMException && err.name === "AbortError") return;
      const msg = err instanceof Error ? err.message : "Failed to save settings";
      setSaveError(msg);
      showToast(msg, "error");
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
            <div className="space-y-4 text-sm">
              {/* Hot storage */}
              <div>
                <div className="flex justify-between mb-1">
                  <span className="text-muted">
                    Hot storage
                    <span className="ml-2 text-xs text-faint font-mono">
                      {config?.storage.hot_path}
                    </span>
                  </span>
                  <span className="text-white/80 font-mono">
                    {formatBytes(storageStats.hot.used_bytes)}
                    {storageStats.hot.total_bytes > 0 && (
                      <span className="text-faint ml-1">
                        / {formatBytes(storageStats.hot.total_bytes)}
                      </span>
                    )}
                    <span className="text-faint ml-1">
                      ({storageStats.hot.segment_count.toLocaleString()} segments)
                    </span>
                  </span>
                </div>
                {storageStats.hot.total_bytes > 0 && (() => {
                  const usedOnDisk = storageStats.hot.total_bytes - storageStats.hot.available_bytes;
                  const pct = Math.min(100, Math.round((usedOnDisk / storageStats.hot.total_bytes) * 100));
                  return (
                    <div className="w-full bg-surface-base rounded-full h-2 overflow-hidden" title={`${pct}% used (${formatBytes(usedOnDisk)} of ${formatBytes(storageStats.hot.total_bytes)})`}>
                      <div
                        className={`h-full rounded-full transition-all ${pct >= 90 ? 'bg-red-500' : pct >= 75 ? 'bg-yellow-500' : 'bg-sentinel-500'}`}
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                  );
                })()}
              </div>

              {/* Cold storage */}
              {storageStats.cold && (
                <div>
                  <div className="flex justify-between mb-1">
                    <span className="text-muted">
                      Cold storage
                      <span className="ml-2 text-xs text-faint font-mono">
                        {storageStats.cold.path}
                      </span>
                    </span>
                    <span className="text-white/80 font-mono">
                      {formatBytes(storageStats.cold.used_bytes)}
                      {storageStats.cold.total_bytes > 0 && (
                        <span className="text-faint ml-1">
                          / {formatBytes(storageStats.cold.total_bytes)}
                        </span>
                      )}
                      <span className="text-faint ml-1">
                        ({storageStats.cold.segment_count.toLocaleString()} segments)
                      </span>
                    </span>
                  </div>
                  {storageStats.cold.total_bytes > 0 && (() => {
                    const usedOnDisk = storageStats.cold!.total_bytes - storageStats.cold!.available_bytes;
                    const pct = Math.min(100, Math.round((usedOnDisk / storageStats.cold!.total_bytes) * 100));
                    return (
                      <div className="w-full bg-surface-base rounded-full h-2 overflow-hidden" title={`${pct}% used (${formatBytes(usedOnDisk)} of ${formatBytes(storageStats.cold!.total_bytes)})`}>
                        <div
                          className={`h-full rounded-full transition-all ${pct >= 90 ? 'bg-red-500' : pct >= 75 ? 'bg-yellow-500' : 'bg-sentinel-500'}`}
                          style={{ width: `${pct}%` }}
                        />
                      </div>
                    );
                  })()}
                </div>
              )}
            </div>
          </section>
        )}

        {/* Retention Rules (R14) — per-camera × per-event-type */}
        <section className="bg-surface-raised border border-border rounded-lg p-5">
          <h2 className="text-sm font-medium text-muted mb-1">Event Retention Rules</h2>
          <p className="text-xs text-faint mb-4">
            Override how long events are kept for a specific camera and/or event type.
            Leave camera or type blank to create a wildcard rule. Rules are applied most-specific first:
            (camera + type) &gt; (camera only) &gt; (type only) &gt; (global fallback = cold retention days).
          </p>

          {retentionError && (
            <p className="text-red-400 text-sm mb-3">{retentionError}</p>
          )}

          {/* Existing rules table */}
          {retentionRules.length > 0 && (
            <div className="mb-4 space-y-2">
              {retentionRules.map((rule) => {
                const camName = rule.camera_id != null
                  ? cameras.find((c) => c.id === rule.camera_id)?.name ?? `Camera ${rule.camera_id}`
                  : "All cameras";
                const typeName = rule.event_type ?? "All types";
                return (
                  <div
                    key={rule.id}
                    className="flex items-center justify-between text-sm bg-surface-base border border-border rounded-lg px-3 py-2"
                  >
                    <span className="text-white/80">
                      <span className="font-mono">{camName}</span>
                      <span className="text-faint mx-2">/</span>
                      <span className="font-mono">{typeName}</span>
                      <span className="text-faint ml-2">→</span>
                      <span className="ml-2 text-sentinel-400 font-medium">{rule.events_days}d</span>
                    </span>
                    <button
                      type="button"
                      onClick={() => handleDeleteRetentionRule(rule.id)}
                      className="text-red-400 hover:text-red-300 text-xs ml-4"
                    >
                      Remove
                    </button>
                  </div>
                );
              })}
            </div>
          )}

          {/* Add new rule */}
          <div className="grid grid-cols-1 sm:grid-cols-4 gap-2 items-end">
            <div>
              <label htmlFor="retention-camera" className="block text-xs text-muted mb-1">Camera</label>
              <select
                id="retention-camera"
                value={newRuleCameraId}
                onChange={(e) => setNewRuleCameraId(e.target.value)}
                className="w-full bg-surface-base border border-border rounded-lg px-2 py-1.5 text-sm text-white focus:outline-none focus:border-sentinel-500"
              >
                <option value="">All cameras</option>
                {cameras.map((cam) => (
                  <option key={cam.id} value={String(cam.id)}>{cam.name}</option>
                ))}
              </select>
            </div>
            <div>
              <label htmlFor="retention-event-type" className="block text-xs text-muted mb-1">Event type</label>
              <select
                id="retention-event-type"
                value={newRuleEventType}
                onChange={(e) => setNewRuleEventType(e.target.value)}
                className="w-full bg-surface-base border border-border rounded-lg px-2 py-1.5 text-sm text-white focus:outline-none focus:border-sentinel-500"
              >
                <option value="">All types</option>
                <option value="detection">detection</option>
                <option value="face_match">face_match</option>
                <option value="audio_detection">audio_detection</option>
                <option value="camera.online">camera.online</option>
                <option value="camera.offline">camera.offline</option>
                <option value="camera.connected">camera.connected</option>
                <option value="camera.disconnected">camera.disconnected</option>
                <option value="camera.error">camera.error</option>
              </select>
            </div>
            <div>
              <label htmlFor="retention-days" className="block text-xs text-muted mb-1">Keep (days)</label>
              <input
                id="retention-days"
                type="number"
                min={1}
                value={newRuleDays}
                onChange={(e) => setNewRuleDays(parseInt(e.target.value, 10) || 1)}
                className="w-full bg-surface-base border border-border rounded-lg px-2 py-1.5 text-sm text-white focus:outline-none focus:border-sentinel-500"
              />
            </div>
            <button
              type="button"
              onClick={handleAddRetentionRule}
              disabled={retentionSubmitting}
              className="bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-3 py-1.5 rounded-lg text-sm font-medium transition-colors"
            >
              {retentionSubmitting ? "Adding…" : "Add Rule"}
            </button>
          </div>
        </section>

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

        {/* MQTT section (v0.3) */}
        <section className="bg-surface-raised border border-border rounded-lg p-5">
          <h2 className="text-sm font-medium text-muted mb-1">MQTT</h2>
          <p className="text-xs text-faint mb-4">
            Publish detection and camera events to an MQTT broker for Home Assistant integration.
            Changes take effect after saving and restarting the server.
          </p>
          <div className="space-y-4">
            <label className="flex items-center gap-2 text-sm cursor-pointer select-none">
              <input
                type="checkbox"
                checked={mqttEnabled}
                onChange={(e) => { setMqttEnabled(e.target.checked); setSaveSuccess(false); }}
                className="sr-only peer"
              />
              <div
                className={`w-8 h-5 rounded-full relative transition-colors cursor-pointer ${
                  mqttEnabled ? "bg-sentinel-500" : "bg-border"
                }`}
              >
                <div
                  className={`absolute top-[3px] w-3.5 h-3.5 rounded-full bg-white transition-transform ${
                    mqttEnabled ? "translate-x-[14px]" : "translate-x-[3px]"
                  }`}
                />
              </div>
              <span className="text-muted">Enabled</span>
            </label>

            <div>
              <label htmlFor="mqtt-broker" className="block text-sm text-muted mb-1">
                Broker URL
              </label>
              <input
                id="mqtt-broker"
                type="text"
                value={mqttBroker}
                onChange={(e) => { setMqttBroker(e.target.value); setSaveSuccess(false); }}
                placeholder="tcp://localhost:1883"
                disabled={!mqttEnabled}
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500 disabled:opacity-50"
              />
            </div>

            <div>
              <label htmlFor="mqtt-topic-prefix" className="block text-sm text-muted mb-1">
                Topic Prefix
              </label>
              <input
                id="mqtt-topic-prefix"
                type="text"
                value={mqttTopicPrefix}
                onChange={(e) => { setMqttTopicPrefix(e.target.value); setSaveSuccess(false); }}
                placeholder="sentinel"
                disabled={!mqttEnabled}
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500 disabled:opacity-50"
              />
              <p className="mt-1 text-xs text-faint">
                Events publish to {mqttTopicPrefix || "sentinel"}/events/&#123;camera&#125;/&#123;label&#125;
              </p>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label htmlFor="mqtt-username" className="block text-sm text-muted mb-1">
                  Username
                </label>
                <input
                  id="mqtt-username"
                  type="text"
                  value={mqttUsername}
                  onChange={(e) => { setMqttUsername(e.target.value); setSaveSuccess(false); }}
                  placeholder="(optional)"
                  disabled={!mqttEnabled}
                  autoComplete="off"
                  className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500 disabled:opacity-50"
                />
              </div>
              <div>
                <label htmlFor="mqtt-password" className="block text-sm text-muted mb-1">
                  Password
                </label>
                <input
                  id="mqtt-password"
                  type="password"
                  value={mqttPassword}
                  onChange={(e) => { setMqttPassword(e.target.value); setSaveSuccess(false); }}
                  placeholder="(optional)"
                  disabled={!mqttEnabled}
                  autoComplete="off"
                  className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500 disabled:opacity-50"
                />
              </div>
            </div>

            <label className="flex items-center gap-2 text-sm cursor-pointer select-none">
              <input
                type="checkbox"
                checked={mqttHADiscovery}
                onChange={(e) => { setMqttHADiscovery(e.target.checked); setSaveSuccess(false); }}
                disabled={!mqttEnabled}
                className="sr-only peer"
              />
              <div
                className={`w-8 h-5 rounded-full relative transition-colors cursor-pointer ${
                  mqttHADiscovery && mqttEnabled ? "bg-sentinel-500" : "bg-border"
                } ${!mqttEnabled ? "opacity-50" : ""}`}
              >
                <div
                  className={`absolute top-[3px] w-3.5 h-3.5 rounded-full bg-white transition-transform ${
                    mqttHADiscovery ? "translate-x-[14px]" : "translate-x-[3px]"
                  }`}
                />
              </div>
              <span className={`text-muted ${!mqttEnabled ? "opacity-50" : ""}`}>
                Home Assistant Auto-Discovery
              </span>
            </label>
          </div>
        </section>

        {/* Email (SMTP) notification settings */}
        <section className="bg-surface-raised border border-border rounded-lg p-5">
          <h2 className="text-sm font-medium text-muted mb-1">Email (SMTP)</h2>
          <p className="text-xs text-faint mb-4">
            Configure SMTP server settings for email notifications.
            Register recipient email addresses on the Notification Settings page.
            Changes take effect after saving and restarting the server.
          </p>
          <div className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div className="sm:col-span-2">
                <label htmlFor="smtp-host" className="block text-sm text-muted mb-1">
                  SMTP Host
                </label>
                <input
                  id="smtp-host"
                  type="text"
                  value={smtpHost}
                  onChange={(e) => { setSmtpHost(e.target.value); setSaveSuccess(false); }}
                  placeholder="smtp.gmail.com"
                  className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
                />
              </div>
              <div>
                <label htmlFor="smtp-port" className="block text-sm text-muted mb-1">
                  Port
                </label>
                <input
                  id="smtp-port"
                  type="number"
                  min={1}
                  max={65535}
                  value={smtpPort}
                  onChange={(e) => { setSmtpPort(parseInt(e.target.value, 10) || 587); setSaveSuccess(false); }}
                  className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-sentinel-500"
                />
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label htmlFor="smtp-username" className="block text-sm text-muted mb-1">
                  Username
                </label>
                <input
                  id="smtp-username"
                  type="text"
                  value={smtpUsername}
                  onChange={(e) => { setSmtpUsername(e.target.value); setSaveSuccess(false); }}
                  placeholder="(optional)"
                  autoComplete="off"
                  className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
                />
              </div>
              <div>
                <label htmlFor="smtp-password" className="block text-sm text-muted mb-1">
                  Password
                </label>
                <input
                  id="smtp-password"
                  type="password"
                  value={smtpPassword}
                  onChange={(e) => { setSmtpPassword(e.target.value); setSaveSuccess(false); }}
                  placeholder="(optional)"
                  autoComplete="off"
                  className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
                />
              </div>
            </div>

            <div>
              <label htmlFor="smtp-from" className="block text-sm text-muted mb-1">
                From Address
              </label>
              <input
                id="smtp-from"
                type="email"
                value={smtpFrom}
                onChange={(e) => { setSmtpFrom(e.target.value); setSaveSuccess(false); }}
                placeholder="alerts@example.com"
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
              />
            </div>

            <label className="flex items-center gap-2 text-sm cursor-pointer select-none">
              <input
                type="checkbox"
                checked={smtpTls}
                onChange={(e) => { setSmtpTls(e.target.checked); setSaveSuccess(false); }}
                className="sr-only peer"
              />
              <div
                className={`w-8 h-5 rounded-full relative transition-colors cursor-pointer ${
                  smtpTls ? "bg-sentinel-500" : "bg-border"
                }`}
              >
                <div
                  className={`absolute top-[3px] w-3.5 h-3.5 rounded-full bg-white transition-transform ${
                    smtpTls ? "translate-x-[14px]" : "translate-x-[3px]"
                  }`}
                />
              </div>
              <span className="text-muted">Use TLS (STARTTLS)</span>
            </label>
          </div>
        </section>

        {/* Remote Access — QR pairing for mobile app (Phase 12, CG11) */}
        <section className="bg-surface-raised border border-border rounded-lg p-5">
          <h2 className="text-sm font-medium text-muted mb-4">Remote Access</h2>
          <p className="text-sm text-muted mb-4">
            Generate a one-time QR code for the Sentinel NVR mobile app. Your phone connects
            securely through a relay server — no port forwarding or VPN needed.
          </p>
          <p className="text-xs text-faint mb-4">
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

        <div className="sticky bottom-0 bg-surface-base/95 backdrop-blur-sm border-t border-border py-3 px-8 -mx-8">
          <div className="flex justify-end">
            <button
              type="submit"
              disabled={!dirty || submitting}
              className="bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-5 py-2 rounded-lg text-sm font-medium transition-colors"
            >
              {submitting ? "Saving..." : "Save Settings"}
            </button>
          </div>
        </div>
      </form>
      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}
    </div>
  );
}
