/**
 * Settings — read-only system configuration viewer.
 * Displays the current sentinel.yml configuration sections.
 * TODO: Phase 9 — Make settings editable via the Web UI.
 */
import { useEffect, useState } from "react";
import { api, SystemConfig } from "../api/client";
import { Settings as SettingsIcon } from "lucide-react";

export default function Settings() {
  const [config, setConfig] = useState<SystemConfig | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();

    api
      .getConfig(controller.signal)
      .then(setConfig)
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setError(err.message);
      });

    return () => controller.abort();
  }, []);

  return (
    <div className="p-8">
      <h1 className="text-2xl font-semibold mb-2">Settings</h1>
      <p className="text-muted mb-6">
        System configuration (read-only — editing via UI is planned for Phase 9)
      </p>

      {error && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4 mb-6">
          <p className="text-red-400 text-sm">Failed to load config: {error}</p>
        </div>
      )}

      {!config && !error && (
        <div className="text-center py-16">
          <SettingsIcon className="w-12 h-12 text-faint mx-auto mb-4" />
          <p className="text-muted animate-pulse">Loading configuration...</p>
        </div>
      )}

      {config && (
        <div className="space-y-4">
          <ConfigSection title="Server" data={config.server} />
          <ConfigSection title="Storage" data={config.storage} />
          <ConfigSection title="Detection" data={config.detection} />
          <ConfigSection
            title="Cameras"
            data={
              config.cameras.length > 0
                ? config.cameras
                : "No cameras configured"
            }
          />
        </div>
      )}
    </div>
  );
}

function ConfigSection({ title, data }: { title: string; data: unknown }) {
  return (
    <div className="bg-surface-raised border border-border rounded-lg p-5">
      <h3 className="text-sm font-medium text-muted mb-3">{title}</h3>
      <pre className="text-sm text-white/80 overflow-auto max-h-96">
        {typeof data === "string" ? data : JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}
