/**
 * AuthContext — provides the current user session and auth actions (Phase 7, CG6).
 *
 * Strategy:
 * - On mount, call GET /auth/me to check if a valid httpOnly cookie session exists.
 * - If 401: user is unauthenticated (no cookie or expired).
 * - login() calls POST /auth/login, sets cookies, then re-checks /auth/me.
 * - logout() calls POST /auth/logout to revoke the refresh token and clear cookies.
 * - ProtectedRoute renders children only when authenticated; redirects to /login otherwise.
 */
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  useCallback,
  type ReactNode,
} from "react";
import { api, type AuthUser } from "../api/client";

interface AuthContextValue {
  /** Null = unauthenticated or loading. */
  user: AuthUser | null;
  /** True while the initial /auth/me check is in progress. */
  loading: boolean;
  /** True if the backend has OIDC/SSO enabled (Phase 7, CG6). */
  oidcEnabled: boolean;
  /**
   * True when no admin account exists yet (first-run setup required).
   * Null while the setup check is in-flight. ProtectedRoute reads this
   * to redirect to /setup instead of /login, eliminating the double
   * checkSetup call that previously occurred in ProtectedRoute itself.
   */
  needsSetup: boolean | null;
  /** Authenticates with username/password. Throws on bad credentials. */
  login: (username: string, password: string) => Promise<void>;
  /** Revokes session and clears state. */
  logout: () => Promise<void>;
  /**
   * Called by the Setup page after a successful POST /setup. Injects the
   * newly-created admin user into the auth context so ProtectedRoute considers
   * the session active without an extra GET /auth/me round-trip.
   */
  onSetupComplete: (user: AuthUser) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [loading, setLoading] = useState(true);
  const [oidcEnabled, setOidcEnabled] = useState(false);
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  // Check session and setup state on mount. Both /setup/status and /auth/me are
  // fetched concurrently; loading is cleared only after BOTH settle so that
  // ProtectedRoute never sees loading=false with needsSetup still null (which
  // could briefly redirect to the wrong route on first run).
  useEffect(() => {
    const ctrl = new AbortController();

    const checkSetupP = api
      .checkSetup(ctrl.signal)
      .then(({ needs_setup, oidc_enabled }) => {
        if (mountedRef.current) {
          setNeedsSetup(needs_setup);
          setOidcEnabled(oidc_enabled);
        }
      })
      .catch(() => {
        // Non-critical; OIDC badge won't show. Default needsSetup=false so
        // ProtectedRoute falls through to /login rather than /setup on error.
        if (mountedRef.current) setNeedsSetup(false);
      });

    const getMeP = api
      .getMe(ctrl.signal)
      .then((u) => { if (mountedRef.current) setUser(u); })
      .catch(() => {
        if (ctrl.signal.aborted) return; // StrictMode cleanup abort — second mount handles it
        if (mountedRef.current) setUser(null);
      });

    // Clear loading only after both promises settle — prevents the brief window
    // where loading=false but needsSetup is still null (wrong route flash).
    Promise.allSettled([checkSetupP, getMeP]).then(() => {
      if (ctrl.signal.aborted) return;
      if (mountedRef.current) setLoading(false);
    });

    return () => ctrl.abort();
  }, []);

  const login = useCallback(async (username: string, password: string) => {
    const ctrl = new AbortController();
    // Pass the same signal to both calls so in-flight requests can be cancelled
    // if the component unmounts between the two awaits.
    await api.login(username, password, ctrl.signal);
    const me = await api.getMe(ctrl.signal);
    if (mountedRef.current) setUser(me);
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      if (mountedRef.current) setUser(null);
    }
  }, []);

  // Called by the Setup page after POST /setup succeeds — injects the new user
  // directly so ProtectedRoute immediately considers the session active.
  const onSetupComplete = useCallback((newUser: AuthUser) => {
    if (mountedRef.current) {
      setUser(newUser);
      setNeedsSetup(false);
    }
  }, []);

  return (
    <AuthContext.Provider value={{ user, loading, oidcEnabled, needsSetup, login, logout, onSetupComplete }}>
      {children}
    </AuthContext.Provider>
  );
}

/** Returns the auth context. Must be used inside <AuthProvider>. */
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside AuthProvider");
  return ctx;
}
