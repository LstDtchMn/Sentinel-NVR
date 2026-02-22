/**
 * Dashboard — system health overview page.
 * Auto-refreshes every 10s via the /api/v1/health endpoint.
 * Uses AbortController so in-flight requests are cancelled on unmount.
 */
import { useEffect, useState } from "react";
import { api, HealthStatus } from "../api/client";
import { Activity, Database, Cpu, Film, HardDrive, Radio } from "lucide-react";

type StatColor = "green" | "blue" | "purple" | "cyan";

const STAT_COLOR_MAP: Record<StatColor, string> = {
  green: "text-green-400",
  blue: "text-blue-400",
  purple: "text-purple-400",
  cyan: "text-cyan-400",
};

export default function Dashboard() {
  const [health, setHealth] = useState<HealthStatus | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    // Create a new AbortController per fetch to avoid stale signal issues.
    // The previous in-flight request is aborted before starting the next,
    // preventing overlapping requests from racing on slow networks.
    let currentController: AbortController | null = null;
    let unmounted = false;

    const fetchHealth = () => {
      currentController?.abort(); // cancel any in-flight request
      currentController = new AbortController();
      api
        .getHealth(currentController.signal)
        .then((data) => {
          if (unmounted) return;
          setHealth(data);
          setError(null);
        })
        .catch((err) => {
          if (unmounted) return;
          if (err instanceof DOMException && err.name === "AbortError") return;
          setError(err.message);
        });
    };

    fetchHealth();
    const interval = setInterval(fetchHealth, 10_000);

    return () => {
      unmounted = true;
      currentController?.abort();
      clearInterval(interval);
    };
  }, []);

  return (
    <div className="p-8">
      <h1 className="text-2xl font-semibold mb-6">Dashboard</h1>

      {error && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4 mb-6">
          <p className="text-red-400 text-sm">Backend unavailable: {error}</p>
        </div>
      )}

      {health && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-4">
          <StatCard
            icon={Activity}
            label="Status"
            value={health.status}
            color="green"
          />
          <StatCard
            icon={Cpu}
            label="Uptime"
            value={health.uptime}
            color="blue"
          />
          <StatCard
            icon={HardDrive}
            label="Cameras"
            value={String(health.cameras_configured)}
            color="purple"
          />
          <StatCard
            icon={Film}
            label="Recordings"
            value={String(health.recordings_count)}
            color="cyan"
          />
          <StatCard
            icon={Database}
            label="Database"
            value={health.database}
            color="cyan"
          />
          <StatCard
            icon={Radio}
            label="go2rtc"
            value={health.go2rtc}
            color="green"
          />
        </div>
      )}

      {!health && !error && (
        <p className="text-muted animate-pulse">
          Connecting to backend...
        </p>
      )}
    </div>
  );
}

function StatCard({
  icon: Icon,
  label,
  value,
  color,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  color: StatColor;
}) {
  return (
    <div className="bg-surface-raised border border-border rounded-lg p-5">
      <div className="flex items-center gap-3 mb-3">
        <Icon className={`w-5 h-5 ${STAT_COLOR_MAP[color]}`} />
        <span className="text-sm text-muted">{label}</span>
      </div>
      <p className="text-xl font-semibold">{value}</p>
    </div>
  );
}
