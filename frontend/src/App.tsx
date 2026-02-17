import { useEffect, useState } from "react";

interface HealthStatus {
  status: string;
  version: string;
  uptime: string;
  go_version: string;
  os: string;
  arch: string;
  cameras_configured: number;
}

function App() {
  const [health, setHealth] = useState<HealthStatus | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch("/api/v1/health")
      .then((res) => res.json())
      .then(setHealth)
      .catch((err) => setError(err.message));
  }, []);

  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="text-center space-y-6">
        <h1 className="text-4xl font-bold tracking-tight">Sentinel NVR</h1>
        <p className="text-sentinel-100/60">Open-source network video recorder</p>

        {error && (
          <div className="bg-red-900/30 border border-red-700 rounded-lg p-4 max-w-md mx-auto">
            <p className="text-red-400 text-sm">Backend unavailable: {error}</p>
          </div>
        )}

        {health && (
          <div className="bg-sentinel-900/50 border border-sentinel-700/30 rounded-lg p-6 max-w-md mx-auto text-left space-y-2">
            <div className="flex justify-between">
              <span className="text-sentinel-100/60">Status</span>
              <span className="text-green-400 font-medium">{health.status}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-sentinel-100/60">Version</span>
              <span>{health.version}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-sentinel-100/60">Uptime</span>
              <span>{health.uptime}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-sentinel-100/60">Cameras</span>
              <span>{health.cameras_configured}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-sentinel-100/60">Platform</span>
              <span>{health.os}/{health.arch}</span>
            </div>
          </div>
        )}

        {!health && !error && (
          <p className="text-sentinel-100/40 animate-pulse">Connecting to backend...</p>
        )}
      </div>
    </div>
  );
}

export default App;
