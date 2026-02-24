/**
 * Setup — first-run wizard shown when no users exist in the database.
 * Accessible at /setup. Redirects to /live after account creation.
 * POST /api/v1/setup creates the admin user and auto-logs in via cookies.
 */
import { useState, useRef, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import { useAuth } from "../context/AuthContext";

export default function Setup() {
  const { onSetupComplete, oidcEnabled } = useAuth();
  const navigate = useNavigate();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);

    if (password !== confirm) {
      setError("Passwords do not match.");
      return;
    }
    if (password.length < 8) {
      setError("Password must be at least 8 characters.");
      return;
    }

    // Cancel any previous in-flight submit before starting a new one — prevents
    // a double-submit race where two concurrent POST /setup requests could both
    // attempt to create the admin user on a slow network.
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setSubmitting(true);

    try {
      const result = await api.completeSetup(username, password, ctrl.signal);
      onSetupComplete(result.user);
      navigate("/live", { replace: true });
    } catch (err: unknown) {
      if ((err as Error).name === "AbortError") return;
      setError(
        err instanceof Error ? err.message : "Setup failed. Please try again."
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen bg-surface-base flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        {/* Branding */}
        <div className="text-center mb-8">
          <div className="inline-flex items-center justify-center w-14 h-14 rounded-2xl bg-primary/10 border border-primary/20 mb-4">
            <svg
              className="w-7 h-7 text-primary"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M15.75 10.5l4.72-4.72a.75.75 0 011.28.53v11.38a.75.75 0 01-1.28.53l-4.72-4.72M4.5 18.75h9a2.25 2.25 0 002.25-2.25v-9a2.25 2.25 0 00-2.25-2.25h-9A2.25 2.25 0 002.25 7.5v9a2.25 2.25 0 002.25 2.25z"
              />
            </svg>
          </div>
          <h1 className="text-2xl font-semibold text-white">Sentinel NVR</h1>
          <p className="mt-1 text-sm text-text-muted">Create your admin account</p>
        </div>

        {/* Form card */}
        <div className="bg-surface-card border border-surface-border rounded-xl p-6 shadow-lg">
          <form onSubmit={handleSubmit} className="space-y-4" noValidate>
            <div>
              <label
                htmlFor="setup-username"
                className="block text-sm font-medium text-text-secondary mb-1.5"
              >
                Username
              </label>
              <input
                id="setup-username"
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
                autoFocus
                autoComplete="username"
                disabled={submitting}
                className="w-full px-3 py-2 rounded-lg bg-surface-raised border border-surface-border text-white placeholder-text-muted text-sm focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent disabled:opacity-50"
                placeholder="admin"
              />
            </div>

            <div>
              <label
                htmlFor="setup-password"
                className="block text-sm font-medium text-text-secondary mb-1.5"
              >
                Password
              </label>
              <input
                id="setup-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                autoComplete="new-password"
                disabled={submitting}
                className="w-full px-3 py-2 rounded-lg bg-surface-raised border border-surface-border text-white placeholder-text-muted text-sm focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent disabled:opacity-50"
                placeholder="At least 8 characters"
              />
            </div>

            <div>
              <label
                htmlFor="setup-confirm"
                className="block text-sm font-medium text-text-secondary mb-1.5"
              >
                Confirm password
              </label>
              <input
                id="setup-confirm"
                type="password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                required
                autoComplete="new-password"
                disabled={submitting}
                className="w-full px-3 py-2 rounded-lg bg-surface-raised border border-surface-border text-white placeholder-text-muted text-sm focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent disabled:opacity-50"
                placeholder="Repeat password"
              />
            </div>

            {error && (
              <p className="text-sm text-status-error" role="alert">
                {error}
              </p>
            )}

            <button
              type="submit"
              disabled={submitting || !username || !password || !confirm}
              className="w-full py-2 px-4 rounded-lg bg-primary text-white text-sm font-medium hover:bg-primary/90 focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-surface-card disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {submitting ? "Creating account…" : "Create account"}
            </button>
          </form>
        </div>

        <p className="mt-4 text-center text-xs text-text-muted">
          This account will have full administrator access.
        </p>

        {oidcEnabled && (
          <p className="mt-2 text-center text-sm text-text-secondary">
            Or{" "}
            <a
              href="/api/v1/auth/oidc/login"
              className="text-accent-primary hover:underline"
            >
              sign in with SSO
            </a>{" "}
            if an account already exists.
          </p>
        )}
      </div>
    </div>
  );
}
