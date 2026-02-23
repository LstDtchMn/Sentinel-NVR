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
  /** Authenticates with username/password. Throws on bad credentials. */
  login: (username: string, password: string) => Promise<void>;
  /** Revokes session and clears state. */
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [loading, setLoading] = useState(true);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  // Check session on mount. If the cookie is present and valid, /auth/me returns 200.
  useEffect(() => {
    const ctrl = new AbortController();
    api
      .getMe(ctrl.signal)
      .then(setUser)
      .catch(() => setUser(null))
      .finally(() => setLoading(false));
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
      setUser(null);
    }
  }, []);

  return (
    <AuthContext.Provider value={{ user, loading, login, logout }}>
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
