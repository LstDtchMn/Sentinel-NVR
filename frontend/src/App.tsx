/**
 * App — root layout component (CG11).
 * Phase 7: wraps all routes in AuthProvider; ProtectedRoute gates the main shell.
 * Unauthenticated users are redirected to /login; after login they return to their
 * originally requested URL via location.state.from.
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
import Login from "./pages/Login";

/**
 * ProtectedRoute renders the main app shell only when the user is authenticated.
 * While the session check is in flight (loading=true) it shows nothing to avoid
 * a flash of redirect. Once loading resolves:
 * - authenticated → render children
 * - unauthenticated → redirect to /login, preserving the requested path in state
 */
function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();
  const location = useLocation();

  if (loading) return null; // brief blank while checking /auth/me
  if (!user) {
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
        {/* Public: login page (no auth required) */}
        <Route path="/login" element={<Login />} />
        {/* All other routes: protected by ProtectedRoute inside AppShell */}
        <Route path="/*" element={<AppShell />} />
      </Routes>
    </AuthProvider>
  );
}

export default App;
