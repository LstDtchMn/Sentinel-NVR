/**
 * NotificationSettings — manage push/webhook token registrations and alert preferences.
 * Phase 8, R9: FCM, APNs, and webhook delivery with optional iOS Critical Alerts.
 */
import { useEffect, useState, useCallback, useRef } from "react";
import { api, NotifToken, NotifPref, NotifLogEntry } from "../api/client";
import { Bell, Plus, Trash2, CheckCircle, XCircle, Clock, Zap } from "lucide-react";
import Toast from "../components/Toast";
import { useToast } from "../hooks/useToast";

// Known event types that can trigger notifications.
const EVENT_TYPES = [
  { value: "*", label: "All events" },
  { value: "detection", label: "Detection" },
  { value: "camera.offline", label: "Camera offline" },
  { value: "camera.connected", label: "Camera connected" },
];

const PROVIDERS = ["fcm", "apns", "webhook"] as const;

export default function NotificationSettings() {
  const [tokens, setTokens] = useState<NotifToken[]>([]);
  const [prefs, setPrefs] = useState<NotifPref[]>([]);
  const [log, setLog] = useState<NotifLogEntry[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  // Toast feedback
  const { toast, showToast, dismissToast } = useToast();

  // Track which token is currently being tested (for loading/disabled state).
  const [testingTokenId, setTestingTokenId] = useState<number | null>(null);

  // New token form state
  const [newProvider, setNewProvider] = useState<"fcm" | "apns" | "webhook">("webhook");
  const [newToken, setNewToken] = useState("");
  const [newLabel, setNewLabel] = useState("");
  const [savingToken, setSavingToken] = useState(false);

  // Ref for fire-and-forget loadAll calls (e.g. after mutating operations) so
  // the in-flight request can be aborted on unmount to prevent stale state updates.
  const manualCtrlRef = useRef<AbortController | null>(null);

  // New pref form state
  const [newEventType, setNewEventType] = useState("*");
  const [newEnabled, setNewEnabled] = useState(true);
  const [newCritical, setNewCritical] = useState(false);
  const [savingPref, setSavingPref] = useState(false);

  const loadAll = useCallback((signal?: AbortSignal) => {
    setLoading(true);
    Promise.all([
      api.listNotifTokens(signal),
      api.listNotifPrefs(signal),
      api.listNotifLog(50, signal),
    ])
      .then(([t, p, l]) => {
        setTokens(t);
        setPrefs(p);
        setLog(l);
        setError(null);
        setLoading(false);
      })
      .catch((err) => {
        // .finally() would fire even on abort, clearing loading for an in-flight
        // request that a concurrent re-mount may already be tracking.
        if (err instanceof DOMException && err.name === "AbortError") return;
        setError(err.message);
        setLoading(false);
      });
  }, []);

  useEffect(() => {
    const ctrl = new AbortController();
    loadAll(ctrl.signal);
    return () => {
      ctrl.abort();
      // Abort any in-flight manual loadAll (e.g. post-create reload) on unmount.
      manualCtrlRef.current?.abort();
    };
  }, [loadAll]);

  async function handleAddToken(e: React.FormEvent) {
    e.preventDefault();
    if (!newToken.trim()) return;
    setSavingToken(true);
    try {
      // Optimistic append: API returns the created token, so we can append it
      // directly instead of reloading all three lists (tokens, prefs, log).
      const created = await api.createNotifToken({
        provider: newProvider,
        token: newToken.trim(),
        label: newLabel.trim(),
      });
      setTokens((prev) => [...prev, created]);
      setNewToken("");
      setNewLabel("");
      showToast("Channel registered", "success");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to register token");
    } finally {
      setSavingToken(false);
    }
  }

  async function handleDeleteToken(id: number) {
    if (!window.confirm("Remove this notification channel? This cannot be undone.")) return;
    try {
      await api.deleteNotifToken(id);
      setTokens((prev) => prev.filter((t) => t.id !== id));
      showToast("Channel removed", "success");
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to remove token";
      setError(msg);
      showToast(msg, "error");
    }
  }

  async function handleTestToken(id: number) {
    setTestingTokenId(id);
    try {
      await api.testNotification(id);
      showToast("Test notification sent", "success");
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to send test notification";
      showToast(msg, "error");
    } finally {
      setTestingTokenId(null);
    }
  }

  async function handleAddPref(e: React.FormEvent) {
    e.preventDefault();
    setSavingPref(true);
    try {
      const result = await api.upsertNotifPref({
        event_type: newEventType,
        camera_id: null,
        enabled: newEnabled,
        critical: newCritical,
      });
      setPrefs((prev) => {
        const idx = prev.findIndex((p) => p.id === result.id);
        if (idx >= 0) {
          const next = [...prev];
          next[idx] = result;
          return next;
        }
        return [...prev, result];
      });
      showToast("Rule saved", "success");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to save preference");
    } finally {
      setSavingPref(false);
    }
  }

  async function handleDeletePref(id: number) {
    if (!window.confirm("Remove this alert rule? This cannot be undone.")) return;
    try {
      await api.deleteNotifPref(id);
      setPrefs((prev) => prev.filter((p) => p.id !== id));
      showToast("Rule removed", "success");
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to remove preference";
      setError(msg);
      showToast(msg, "error");
    }
  }

  return (
    <div className="p-8 max-w-4xl">
      <div className="flex items-center gap-3 mb-2">
        <Bell className="w-6 h-6 text-sentinel-500" />
        <h1 className="text-2xl font-semibold">Notification Settings</h1>
      </div>
      <p className="text-muted mb-8">
        Register device tokens and configure which events trigger push or webhook alerts.
      </p>

      {error && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4 mb-6">
          <p className="text-red-400 text-sm">{error}</p>
        </div>
      )}

      {loading ? (
        <div className="text-center py-16">
          <Bell className="w-12 h-12 text-faint mx-auto mb-4" />
          <p className="text-muted animate-pulse">Loading notification settings…</p>
        </div>
      ) : (
        <div className="space-y-8">
          {/* ── Device Tokens ─────────────────────────────────────────── */}
          <section>
            <h2 className="text-lg font-medium mb-4">Notification Channels</h2>

            {/* Registered token list */}
            {tokens.length > 0 ? (
              <div className="space-y-2 mb-4">
                {tokens.map((tok) => (
                  <div
                    key={tok.id}
                    className="flex items-center justify-between bg-surface-raised border border-border rounded-lg px-4 py-3"
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-xs font-medium px-1.5 py-0.5 rounded bg-sentinel-500/20 text-sentinel-400 uppercase tracking-wide">
                          {tok.provider}
                        </span>
                        {tok.label && (
                          <span className="text-sm font-medium truncate">{tok.label}</span>
                        )}
                      </div>
                      <p className="text-xs text-faint mt-0.5 truncate font-mono">{tok.token}</p>
                    </div>
                    <div className="flex items-center gap-1 ml-3 flex-shrink-0">
                      <button
                        onClick={() => handleTestToken(tok.id)}
                        disabled={testingTokenId === tok.id}
                        className="p-1.5 text-muted hover:text-sentinel-400 rounded transition-colors border border-border disabled:opacity-50"
                        title="Send test notification"
                        aria-label={`Test notification channel ${tok.label || tok.provider}`}
                      >
                        <Zap className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => handleDeleteToken(tok.id)}
                        className="p-1.5 text-muted hover:text-status-error rounded transition-colors flex-shrink-0"
                        title="Remove token"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-muted text-sm mb-4">No tokens registered yet.</p>
            )}

            {/* Add token form */}
            <form
              onSubmit={handleAddToken}
              className="bg-surface-raised border border-border rounded-lg p-4 space-y-3"
            >
              <h3 className="text-sm font-medium text-muted">Add notification channel</h3>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <select
                  value={newProvider}
                  onChange={(e) => setNewProvider(e.target.value as typeof newProvider)}
                  aria-label="Channel type"
                  className="col-span-1 bg-surface-base border border-border rounded px-3 py-2 text-sm"
                >
                  {PROVIDERS.map((p) => (
                    <option key={p} value={p}>{p.toUpperCase()}</option>
                  ))}
                </select>
                <input
                  type="text"
                  placeholder={newProvider === "webhook" ? "https://your-webhook-url/alert" : newProvider === "fcm" ? "FCM device token" : "APNs device token"}
                  value={newToken}
                  onChange={(e) => setNewToken(e.target.value)}
                  required
                  aria-label="Token value"
                  className="col-span-2 bg-surface-base border border-border rounded px-3 py-2 text-sm placeholder:text-faint"
                />
              </div>
              <input
                type="text"
                placeholder="Label (optional) — e.g. My iPhone"
                value={newLabel}
                onChange={(e) => setNewLabel(e.target.value)}
                aria-label="Token label"
                className="w-full bg-surface-base border border-border rounded px-3 py-2 text-sm placeholder:text-faint"
              />
              <button
                type="submit"
                disabled={savingToken || !newToken.trim()}
                className="flex items-center gap-2 px-4 py-2 bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50
                           rounded text-sm font-medium transition-colors"
              >
                <Plus className="w-4 h-4" />
                {savingToken ? "Adding…" : "Add Channel"}
              </button>
            </form>
          </section>

          {/* ── Notification Preferences ───────────────────────────────── */}
          <section>
            <h2 className="text-lg font-medium mb-4">Alert Rules</h2>

            {prefs.length > 0 ? (
              <div className="space-y-2 mb-4">
                {prefs.map((pref) => (
                  <div
                    key={pref.id}
                    className="flex items-center justify-between bg-surface-raised border border-border rounded-lg px-4 py-3"
                  >
                    <div className="flex items-center gap-3">
                      <span className="text-sm font-medium">
                        {EVENT_TYPES.find((e) => e.value === pref.event_type)?.label ?? pref.event_type}
                      </span>
                      {pref.camera_id !== null && (
                        <span className="text-xs text-muted">camera #{pref.camera_id}</span>
                      )}
                      <span
                        className={`text-xs px-1.5 py-0.5 rounded ${
                          pref.enabled
                            ? "bg-status-ok/20 text-status-ok"
                            : "bg-surface-overlay text-faint"
                        }`}
                      >
                        {pref.enabled ? "enabled" : "muted"}
                      </span>
                      {pref.critical && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-status-error/20 text-status-error">
                          critical
                        </span>
                      )}
                    </div>
                    <button
                      onClick={() => handleDeletePref(pref.id)}
                      className="p-1.5 text-muted hover:text-status-error rounded transition-colors"
                      title="Remove preference"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-muted text-sm mb-4">No preferences configured — no notifications will be sent.</p>
            )}

            {/* Add pref form */}
            <form
              onSubmit={handleAddPref}
              className="bg-surface-raised border border-border rounded-lg p-4 space-y-3"
            >
              <h3 className="text-sm font-medium text-muted">Add or update a rule</h3>
              <select
                value={newEventType}
                onChange={(e) => setNewEventType(e.target.value)}
                aria-label="Event type"
                className="w-full bg-surface-base border border-border rounded px-3 py-2 text-sm"
              >
                {EVENT_TYPES.map((et) => (
                  <option key={et.value} value={et.value}>{et.label}</option>
                ))}
              </select>
              <div className="flex items-center gap-6">
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <input
                    type="checkbox"
                    checked={newEnabled}
                    onChange={(e) => setNewEnabled(e.target.checked)}
                    className="w-4 h-4 accent-sentinel-500"
                  />
                  Enabled
                </label>
                <label className="flex items-center gap-2 text-sm cursor-pointer" title="Bypasses iOS Do Not Disturb">
                  <input
                    type="checkbox"
                    checked={newCritical}
                    onChange={(e) => setNewCritical(e.target.checked)}
                    className="w-4 h-4 accent-status-error"
                  />
                  Critical alert (bypass iOS Do Not Disturb)
                </label>
              </div>
              <button
                type="submit"
                disabled={savingPref}
                className="flex items-center gap-2 px-4 py-2 bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50
                           rounded text-sm font-medium transition-colors"
              >
                <Plus className="w-4 h-4" />
                {savingPref ? "Saving…" : "Save Rule"}
              </button>
            </form>
          </section>

          {/* ── Delivery Log ──────────────────────────────────────────── */}
          <section>
            <h2 className="text-lg font-medium mb-4">Delivery Log</h2>
            {log.length === 0 ? (
              <p className="text-muted text-sm">No delivery attempts recorded yet.</p>
            ) : (
              <div className="space-y-2">
                {log.map((entry) => (
                  <div
                    key={entry.id}
                    className="flex items-start gap-3 bg-surface-raised border border-border rounded-lg px-4 py-3"
                  >
                    <div className="mt-0.5 flex-shrink-0">
                      {entry.status === "sent" ? (
                        <CheckCircle className="w-4 h-4 text-status-ok" />
                      ) : entry.status === "failed" ? (
                        <XCircle className="w-4 h-4 text-status-error" />
                      ) : (
                        <Clock className="w-4 h-4 text-status-warn" />
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="text-sm font-medium">{entry.title}</span>
                        <span className="text-xs text-faint">{entry.provider}</span>
                        <span className="text-xs text-faint">
                          {new Date(entry.scheduled_at).toLocaleString()}
                        </span>
                      </div>
                      <p className="text-xs text-muted mt-0.5">{entry.body}</p>
                      {entry.last_error && (
                        <p className="text-xs text-status-error mt-0.5 font-mono">{entry.last_error}</p>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </section>
        </div>
      )}
      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}
    </div>
  );
}
