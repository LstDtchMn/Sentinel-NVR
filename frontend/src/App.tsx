/**
 * App — root layout component (CG11).
 * Phase 7: wraps all routes in AuthProvider; ProtectedRoute gates the main shell.
 * Unauthenticated users are redirected to /login; after login they return to their
 * originally requested URL via location.state.from.
 * First-run: AuthContext checks /setup/status on mount and exposes needsSetup so
 * ProtectedRoute can redirect to /setup without a second checkSetup call.
 */
import { Routes, Route, Navigate, useLocation } from "react-router-dom";
import { AuthProvider, useAuth } from "./context/AuthContext";
import Sidebar from "./components/Sidebar";
import LiveView from "./pages/LiveView";
import Playback from "./pages/Playback";
import Events from "./pages/Events";
import Dashboard from "./pages/Dashboard";
import Cameras from "./pages/Cameras";
import Settings from "./pages/Settings";
import NotificationSettings from "./pages/NotificationSettings";
import Faces from "./pages/Faces";
import Models from "./pages/Models";
import Import from "./pages/Import";
import ZoneEditor from "./pages/ZoneEditor";
import Login from "./pages/Login";
import Setup from "./pages/Setup";

/**
 * ProtectedRoute renders the main app shell only when the user is authenticated.
 * While the session check is in flight (loading=true) it shows nothing to avoid
 * a flash of redirect. Once loading resolves:
 * - authenticated → render children
 * - unauthenticated + needs_setup → redirect to /setup
 * - unauthenticated → redirect to /login, preserving the requested path in state
 *
 * needsSetup is sourced from AuthContext (which already called checkSetup on mount)
 * to avoid a second round-trip to /api/v1/setup/status.
 */
function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { user, loading, needsSetup } = useAuth();
  const location = useLocation();

  if (loading) return null; // brief blank while checking /auth/me
  if (!user) {
    // Wait for setup check before redirecting (needsSetup is null while in-flight)
    if (needsSetup === null) return null;
    if (needsSetup) return <Navigate to="/setup" replace />;
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }
  return <>{children}</>;
}

function AppShell() {
  return (
    <ProtectedRoute>
      <div className="flex h-screen bg-surface-base text-white">
        <Sidebar />
        <main className="flex-1 overflow-auto">
          <Routes>
            <Route path="/" element={<Navigate to="/live" replace />} />
            <Route path="/live" element={<LiveView />} />
            <Route path="/playback" element={<Playback />} />
            <Route path="/events" element={<Events />} />
            <Route path="/dashboard" element={<Dashboard />} />
            <Route path="/cameras" element={<Cameras />} />
            <Route path="/cameras/:name/zones" element={<ZoneEditor />} />
            <Route path="/faces" element={<Faces />} />
            <Route path="/models" element={<Models />} />
            <Route path="/import" element={<Import />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="/notifications" element={<NotificationSettings />} />
            {/* Catch-all: redirect unknown paths to live view */}
            <Route path="*" element={<Navigate to="/live" replace />} />
          </Routes>
        </main>
      </div>
    </ProtectedRoute>
  );
}

function App() {
  return (
    <AuthProvider>
      <Routes>
        {/* Public: login and first-run setup (no auth required) */}
        <Route path="/login" element={<Login />} />
        <Route path="/setup" element={<Setup />} />
        {/* All other routes: protected by ProtectedRoute inside AppShell */}
        <Route path="/*" element={<AppShell />} />
      </Routes>
    </AuthProvider>
  );
}

export default App;
