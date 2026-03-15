/**
 * Login — username/password login form (Phase 7, CG6).
 * On success, navigates to the originally requested page (or /live as default).
 */
import { useState, type FormEvent } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { Shield } from "lucide-react";
import { useAuth } from "../context/AuthContext";

export default function Login() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // Navigate to the page the user originally requested, or /live as default.
  // Only accept same-origin paths (starting with "/") to prevent open redirects.
  const raw = (location.state as { from?: string } | null)?.from ?? "";
  const from = raw.startsWith("/") ? raw : "/live";

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(username, password);
      navigate(from, { replace: true });
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Login failed";
      // Map generic API error messages to user-friendly text.
      if (msg.includes("401") || msg.toLowerCase().includes("invalid credentials")) {
        setError("Invalid username or password.");
      } else {
        setError(msg);
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen bg-surface-base flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        {/* Logo */}
        <div className="flex items-center justify-center gap-3 mb-8">
          <Shield className="w-8 h-8 text-sentinel-500" />
          <span className="text-2xl font-semibold tracking-tight">Sentinel NVR</span>
        </div>

        <form
          onSubmit={handleSubmit}
          className="bg-surface-raised border border-border rounded-xl p-6 flex flex-col gap-4"
        >
          <h1 className="text-lg font-medium text-center">Sign in</h1>

          {error && (
            <p className="text-sm text-status-error bg-status-error/10 border border-status-error/30
                          rounded-lg px-3 py-2 text-center">
              {error}
            </p>
          )}

          <div className="flex flex-col gap-1.5">
            <label htmlFor="username" className="text-xs text-muted font-medium uppercase tracking-wide">
              Username
            </label>
            <input
              id="username"
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
              autoComplete="username"
              className="bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white
                         focus:outline-none focus:ring-1 focus:ring-sentinel-500 placeholder:text-faint"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="password" className="text-xs text-muted font-medium uppercase tracking-wide">
              Password
            </label>
            <input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              autoComplete="current-password"
              className="bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white
                         focus:outline-none focus:ring-1 focus:ring-sentinel-500"
            />
          </div>

          <button
            type="submit"
            disabled={submitting}
            className="mt-2 w-full py-2 rounded-lg bg-sentinel-500 text-white text-sm font-medium
                       hover:bg-sentinel-400 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {submitting ? "Signing in…" : "Sign in"}
          </button>
          <p className="text-xs text-faint text-center mt-3">
            Forgot password? Reset via CLI:{" "}
            <code className="text-muted font-mono">sentinel -reset-password &lt;user&gt;</code>
          </p>
        </form>
      </div>
    </div>
  );
}
