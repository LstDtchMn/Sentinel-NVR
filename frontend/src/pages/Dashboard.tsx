/**
 * Dashboard — system health overview page.
 * Auto-refreshes every 10s via the /api/v1/health endpoint.
 * Uses AbortController so in-flight requests are cancelled on unmount.
 */
import { useEffect, useState } from "react";
import { api, HealthStatus } from "../api/client";
import { Activity, Database, Cpu, Film, HardDrive, Radio } from "lucide-react";

type StatColor = "green" | "blue" | "purple" | "cyan" | "yellow" | "red";

const STAT_COLOR_MAP: Record<StatColor, string> = {
  green: "text-status-ok",
  yellow: "text-status-warn",
  red: "text-status-error",
  blue: "text-status-info",
  purple: "text-status-accent",
  cyan: "text-status-highlight",
};

/** Convert Go duration string (e.g. "121h33m56s") to human-friendly "5d 1h 33m". */
function formatUptime(raw: string): string {
  const h = raw.match(/(\d+)h/);
  const m = raw.match(/(\d+)m/);
  const s = raw.match(/(\d+)s/);
  const hours = h ? parseInt(h[1], 10) : 0;
  const mins = m ? parseInt(m[1], 10) : 0;
  const secs = s ? parseInt(s[1], 10) : 0;
  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const remH = hours % 24;
    return remH > 0 ? `${days}d ${remH}h ${mins}m` : `${days}d ${mins}m`;
  }
  if (hours >= 1) return `${hours}h ${mins}m`;
  if (mins >= 1) return `${mins}m ${secs}s`;
  return `${secs}s`;
}

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
            color={health.status === "ok" ? "green" : "red"}
          />
          <StatCard
            icon={Cpu}
            label="Uptime"
            value={formatUptime(health.uptime)}
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
            color={health.database === "connected" ? "green" : "red"}
          />
          <StatCard
            icon={Radio}
            label="Streaming Engine"
            value={health.go2rtc}
            color={health.go2rtc === "connected" ? "green" : "red"}
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
