/**
 * App — root layout component (CG11).
 * Renders the sidebar navigation shell with routed page content.
 */
import { Routes, Route, Navigate } from "react-router-dom";
import Sidebar from "./components/Sidebar";
import Dashboard from "./pages/Dashboard";
import Cameras from "./pages/Cameras";
import Settings from "./pages/Settings";

function App() {
  return (
    <div className="flex h-screen bg-surface-base text-white">
      <Sidebar />
      <main className="flex-1 overflow-auto">
        <Routes>
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<Dashboard />} />
          <Route path="/cameras" element={<Cameras />} />
          <Route path="/settings" element={<Settings />} />
          {/* Catch-all: redirect unknown paths to dashboard */}
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Routes>
      </main>
    </div>
  );
}

export default App;
