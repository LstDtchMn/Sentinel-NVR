/**
 * Dashboard — system health overview page.
 * Auto-refreshes every 10s via the /api/v1/health endpoint.
 * Includes a per-camera health table below the stat cards.
 * Uses AbortController so in-flight requests are cancelled on unmount.
 */
import { useEffect, useState } from "react";
import { api, HealthStatus, CameraDetail, CameraState } from "../api/client";
import { Activity, Database, Cpu, Film, HardDrive, Radio, Check, Minus } from "lucide-react";

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

/** Format an ISO timestamp as a relative time string (e.g. "2h ago"). */
function formatRelativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  if (diff < 0) return "just now";
  const secs = Math.floor(diff / 1000);
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

const STATUS_BADGE_COLORS: Record<CameraState, { bg: string; text: string }> = {
  recording: { bg: "bg-green-900/30", text: "text-green-400" },
  streaming: { bg: "bg-green-900/30", text: "text-green-400" },
  connecting: { bg: "bg-yellow-900/30", text: "text-yellow-400" },
  error: { bg: "bg-red-900/30", text: "text-red-400" },
  idle: { bg: "bg-zinc-800/50", text: "text-faint" },
  stopped: { bg: "bg-zinc-800/50", text: "text-faint" },
};

export default function Dashboard() {
  const [health, setHealth] = useState<HealthStatus | null>(null);
  const [cameras, setCameras] = useState<CameraDetail[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    // Create a new AbortController per fetch to avoid stale signal issues.
    // The previous in-flight request is aborted before starting the next,
    // preventing overlapping requests from racing on slow networks.
    let currentController: AbortController | null = null;
    let unmounted = false;

    const fetchData = () => {
      currentController?.abort(); // cancel any in-flight request
      currentController = new AbortController();
      const signal = currentController.signal;

      api
        .getHealth(signal)
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

      api
        .getCameras(signal)
        .then((data) => {
          if (unmounted) return;
          setCameras(data);
        })
        .catch((err) => {
          if (unmounted) return;
          if (err instanceof DOMException && err.name === "AbortError") return;
          // Camera fetch failure is non-fatal — health card still shows
        });
    };

    fetchData();
    const interval = setInterval(fetchData, 10_000);

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
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-4">
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

      {/* Per-camera health table */}
      {cameras && cameras.length > 0 && (
        <div className="mt-8">
          <h2 className="text-lg font-semibold mb-4">Camera Status</h2>
          <div className="bg-surface-raised border border-border rounded-lg overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-muted">
                  <th className="px-4 py-3 font-medium">Camera Name</th>
                  <th className="px-4 py-3 font-medium">Status</th>
                  <th className="px-4 py-3 font-medium text-center">Recording</th>
                  <th className="px-4 py-3 font-medium text-center">Detecting</th>
                  <th className="px-4 py-3 font-medium">Last Error</th>
                  <th className="px-4 py-3 font-medium">Connected At</th>
                </tr>
              </thead>
              <tbody>
                {cameras.map((cam) => (
                  <CameraStatusRow key={cam.id} camera={cam} />
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}

function CameraStatusRow({ camera }: { camera: CameraDetail }) {
  const ps = camera.pipeline_status;
  const state: CameraState = ps?.state || "idle";
  const colors = STATUS_BADGE_COLORS[state] || STATUS_BADGE_COLORS.idle;

  return (
    <tr className="border-b border-border/50 last:border-b-0 hover:bg-white/[0.02]">
      <td className="px-4 py-3 font-medium">{camera.name}</td>
      <td className="px-4 py-3">
        <span className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${colors.bg} ${colors.text}`}>
          {state}
        </span>
      </td>
      <td className="px-4 py-3 text-center">
        {ps?.recording ? (
          <Check className="w-4 h-4 text-green-400 inline-block" />
        ) : (
          <Minus className="w-4 h-4 text-faint inline-block" />
        )}
      </td>
      <td className="px-4 py-3 text-center">
        {ps?.detecting ? (
          <Check className="w-4 h-4 text-green-400 inline-block" />
        ) : (
          <Minus className="w-4 h-4 text-faint inline-block" />
        )}
      </td>
      <td className="px-4 py-3 max-w-[200px]">
        {ps?.last_error ? (
          <span className="text-red-400 text-xs truncate block" title={ps.last_error}>
            {ps.last_error}
          </span>
        ) : (
          <span className="text-faint">&mdash;</span>
        )}
      </td>
      <td className="px-4 py-3 text-muted text-xs">
        {ps?.connected_at ? formatRelativeTime(ps.connected_at) : "\u2014"}
      </td>
    </tr>
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
