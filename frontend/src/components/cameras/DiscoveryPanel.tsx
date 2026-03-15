import { useState, useEffect, useRef } from "react";
import {
  Camera,
  Loader2,
  Monitor,
  Plus,
  Radio,
  Search,
  Wifi,
  X,
} from "lucide-react";
import { api, DiscoveredCamera, ProbeResult } from "../../api/client";
import { sanitizeName } from "../../utils/strings";
import { CameraPrefill } from "./CameraForm";

export function DiscoveryPanel({
  onAdd,
  onClose,
}: {
  onAdd: (prefill: CameraPrefill) => void;
  onClose: () => void;
}) {
  const [scanning, setScanning] = useState(false);
  const [scanned, setScanned] = useState(false);
  const [discovered, setDiscovered] = useState<DiscoveredCamera[]>([]);
  const [scanWarning, setScanWarning] = useState<string | null>(null);
  const [scanError, setScanError] = useState<string | null>(null);

  // Manual probe state
  const [probeHost, setProbeHost] = useState("");
  const [probePort, setProbePort] = useState("80");
  const [probeUser, setProbeUser] = useState("");
  const [probePass, setProbePass] = useState("");
  const [probing, setProbing] = useState(false);
  const [probeResult, setProbeResult] = useState<ProbeResult | null>(null);
  const [probeError, setProbeError] = useState<string | null>(null);

  // Per-discovered-camera probe state (for getting stream URIs before adding)
  const [probingCamera, setProbingCamera] = useState<string | null>(null); // xaddr being probed
  const [cameraProbeResult, setCameraProbeResult] = useState<Record<string, ProbeResult>>({});
  const [cameraProbeUser, setCameraProbeUser] = useState<Record<string, string>>({});
  const [cameraProbePass, setCameraProbePass] = useState<Record<string, string>>({});
  const [cameraProbeError, setCameraProbeError] = useState<Record<string, string>>({});

  const scanCtrlRef = useRef<AbortController | null>(null);
  const probeCtrlRef = useRef<AbortController | null>(null);
  const cameraProbCtrlRef = useRef<AbortController | null>(null);

  // Abort all in-flight requests on unmount
  useEffect(() => () => {
    scanCtrlRef.current?.abort();
    probeCtrlRef.current?.abort();
    cameraProbCtrlRef.current?.abort();
  }, []);

  const handleScan = () => {
    scanCtrlRef.current?.abort();
    const ctrl = new AbortController();
    scanCtrlRef.current = ctrl;
    setScanning(true);
    setScanError(null);
    setScanWarning(null);
    setDiscovered([]);
    setScanned(false);

    api
      .discoverCameras(ctrl.signal)
      .then((res) => {
        if (ctrl.signal.aborted) return;
        setDiscovered(res.cameras || []);
        setScanWarning(res.warning || null);
        setScanned(true);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        if (ctrl.signal.aborted) return;
        const msg = err instanceof Error ? err.message : "Network scan failed";
        if (msg.includes("403")) {
          setScanError("Admin access required to scan the network.");
        } else {
          setScanError(msg);
        }
      })
      .finally(() => {
        if (ctrl.signal.aborted) return;
        setScanning(false);
      });
  };

  const handleProbe = (e: React.FormEvent) => {
    e.preventDefault();
    if (!probeHost.trim()) return;

    probeCtrlRef.current?.abort();
    const ctrl = new AbortController();
    probeCtrlRef.current = ctrl;
    setProbing(true);
    setProbeError(null);
    setProbeResult(null);

    api
      .probeCamera(
        {
          host: probeHost.trim(),
          port: parseInt(probePort, 10) || 80,
          username: probeUser,
          password: probePass,
        },
        ctrl.signal,
      )
      .then((res) => {
        if (ctrl.signal.aborted) return;
        setProbeResult(res);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        if (ctrl.signal.aborted) return;
        setProbeError(err instanceof Error ? err.message : "Probe failed");
      })
      .finally(() => {
        if (ctrl.signal.aborted) return;
        setProbing(false);
      });
  };

  /** Probe a discovered camera to get its stream URIs before adding. */
  const handleCameraProbe = (cam: DiscoveredCamera) => {
    const key = cam.endpoint_ref || cam.xaddr;
    cameraProbCtrlRef.current?.abort();
    const ctrl = new AbortController();
    cameraProbCtrlRef.current = ctrl;
    setProbingCamera(key);
    setCameraProbeError((prev) => ({ ...prev, [key]: "" }));

    const user = cameraProbeUser[key] || "";
    const pass = cameraProbePass[key] || "";

    api
      .probeCamera(
        { host: cam.ip, port: cam.port, username: user, password: pass },
        ctrl.signal,
      )
      .then((res) => {
        if (ctrl.signal.aborted) return;
        setCameraProbeResult((prev) => ({ ...prev, [key]: res }));
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        if (ctrl.signal.aborted) return;
        setCameraProbeError((prev) => ({
          ...prev,
          [key]: err instanceof Error ? err.message : "Probe failed",
        }));
      })
      .finally(() => {
        if (ctrl.signal.aborted) return;
        setProbingCamera(null);
      });
  };

  /** Build prefill from a ProbeResult and pass it to the Add Camera form. */
  const addFromProbe = (result: ProbeResult, host: string, port: number, user: string, pass: string) => {
    const mainUri = result.streams?.[0]?.stream_uri || "";
    const subUri = result.streams?.[1]?.stream_uri || "";
    const name = sanitizeName(result.device.model || result.device.manufacturer || "Camera");
    onAdd({
      name,
      main_stream: mainUri,
      sub_stream: subUri || undefined,
      onvif_host: host,
      onvif_port: port,
      onvif_user: user || undefined,
      onvif_pass: pass || undefined,
    });
  };

  /** Quick-add from discovered camera without probe (uses xaddr-derived guess). */
  const addFromDiscovery = (cam: DiscoveredCamera) => {
    const key = cam.endpoint_ref || cam.xaddr;
    const probed = cameraProbeResult[key];
    if (probed) {
      const user = cameraProbeUser[key] || "";
      const pass = cameraProbePass[key] || "";
      addFromProbe(probed, cam.ip, cam.port, user, pass);
      return;
    }
    // Fallback: no probe result, guess RTSP from IP
    const name = sanitizeName(cam.hardware || cam.name || "Camera");
    onAdd({
      name,
      main_stream: `rtsp://${cam.ip}:554/stream1`,
      onvif_host: cam.ip,
      onvif_port: cam.port,
    });
  };

  return (
    <div className="bg-surface-raised border border-border rounded-lg p-6 mb-6">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-medium flex items-center gap-2">
          <Radio className="w-5 h-5 text-sentinel-400" />
          ONVIF Camera Discovery
        </h2>
        <button type="button" onClick={onClose} className="text-muted hover:text-white p-1" aria-label="Close discovery panel">
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* Scan button */}
      {!scanning && !scanned && (
        <div className="text-center py-6">
          <p className="text-muted text-sm mb-4">
            Scan your local network for ONVIF-compatible IP cameras.
          </p>
          <button
            type="button"
            onClick={handleScan}
            className="flex items-center gap-2 mx-auto bg-sentinel-500 hover:bg-sentinel-600 text-white px-5 py-2.5 rounded-lg text-sm font-medium transition-colors"
          >
            <Search className="w-4 h-4" />
            Start Scan
          </button>
        </div>
      )}

      {/* Scanning spinner */}
      {scanning && (
        <div className="flex items-center justify-center gap-3 py-8">
          <Loader2 className="w-5 h-5 text-sentinel-400 animate-spin" />
          <span className="text-muted text-sm">Scanning for cameras...</span>
        </div>
      )}

      {/* Scan error */}
      {scanError && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-3 mb-4">
          <p className="text-red-400 text-sm">{scanError}</p>
          <button
            type="button"
            onClick={handleScan}
            className="text-red-400 hover:text-red-300 text-xs mt-2 underline"
          >
            Retry
          </button>
        </div>
      )}

      {/* Scan warning */}
      {scanWarning && (
        <div className="bg-yellow-900/20 border border-yellow-800 rounded-lg p-3 mb-4">
          <p className="text-yellow-400 text-sm">{scanWarning}</p>
        </div>
      )}

      {/* Discovery results */}
      {scanned && !scanning && (
        <div className="mb-4">
          {discovered.length > 0 ? (
            <>
              <p className="text-sm text-muted mb-3">
                Found {discovered.length} camera{discovered.length !== 1 ? "s" : ""} on the network:
              </p>
              <div className="space-y-2 max-h-64 overflow-y-auto">
                {discovered.map((cam) => {
                  const key = cam.endpoint_ref || cam.xaddr;
                  const probed = cameraProbeResult[key];
                  const probeErr = cameraProbeError[key];
                  const isProbing = probingCamera === key;
                  return (
                    <DiscoveredCameraRow
                      key={key}
                      camera={cam}
                      probeResult={probed}
                      probeError={probeErr}
                      isProbing={isProbing}
                      probeUser={cameraProbeUser[key] || ""}
                      probePass={cameraProbePass[key] || ""}
                      onProbeUserChange={(v) => setCameraProbeUser((prev) => ({ ...prev, [key]: v }))}
                      onProbePassChange={(v) => setCameraProbePass((prev) => ({ ...prev, [key]: v }))}
                      onProbe={() => handleCameraProbe(cam)}
                      onAdd={() => addFromDiscovery(cam)}
                    />
                  );
                })}
              </div>
            </>
          ) : (
            <div className="bg-surface-base border border-border rounded-lg p-4 text-center">
              <Wifi className="w-8 h-8 text-faint mx-auto mb-2" />
              <p className="text-muted text-sm mb-1">No cameras found via network scan.</p>
              <p className="text-faint text-xs">
                This can happen in Docker bridge mode or if cameras don't support WS-Discovery.
              </p>
            </div>
          )}

          <button
            type="button"
            onClick={handleScan}
            className="text-sentinel-400 hover:text-sentinel-300 text-xs mt-3 underline"
          >
            Scan again
          </button>
        </div>
      )}

      {/* Manual probe section — always visible after scan or as fallback */}
      <div className="border-t border-border pt-4 mt-4">
        <h3 className="text-sm font-medium text-muted mb-3 flex items-center gap-2">
          <Monitor className="w-4 h-4" />
          Manual Probe
        </h3>
        <p className="text-faint text-xs mb-3">
          Enter the IP address of a camera to probe it directly for stream information.
        </p>
        <form onSubmit={handleProbe} className="space-y-3">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <div>
              <label htmlFor="probe-host" className="block text-xs text-faint mb-1">IP Address *</label>
              <input
                id="probe-host"
                type="text"
                value={probeHost}
                onChange={(e) => setProbeHost(e.target.value)}
                placeholder="192.168.1.100"
                required
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
              />
            </div>
            <div>
              <label htmlFor="probe-port" className="block text-xs text-faint mb-1">Port</label>
              <input
                id="probe-port"
                type="number"
                value={probePort}
                onChange={(e) => setProbePort(e.target.value)}
                placeholder="80"
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
              />
            </div>
            <div>
              <label htmlFor="probe-user" className="block text-xs text-faint mb-1">Username</label>
              <input
                id="probe-user"
                type="text"
                value={probeUser}
                onChange={(e) => setProbeUser(e.target.value)}
                placeholder="admin"
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
              />
            </div>
            <div>
              <label htmlFor="probe-pass" className="block text-xs text-faint mb-1">Password</label>
              <input
                id="probe-pass"
                type="password"
                value={probePass}
                onChange={(e) => setProbePass(e.target.value)}
                placeholder="password"
                className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
              />
            </div>
          </div>
          <div className="flex items-center gap-3">
            <button
              type="submit"
              disabled={probing || !probeHost.trim()}
              className="flex items-center gap-2 bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
            >
              {probing ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Search className="w-4 h-4" />
              )}
              {probing ? "Probing..." : "Probe Camera"}
            </button>
          </div>
        </form>

        {/* Probe error */}
        {probeError && (
          <div className="bg-red-900/20 border border-red-800 rounded-lg p-3 mt-3">
            <p className="text-red-400 text-sm">{probeError}</p>
          </div>
        )}

        {/* Probe result */}
        {probeResult && (
          <div className="bg-surface-base border border-border rounded-lg p-4 mt-3">
            <div className="flex items-center justify-between mb-3">
              <div>
                <p className="text-sm font-medium text-white">
                  {probeResult.device.manufacturer} {probeResult.device.model}
                </p>
                <p className="text-xs text-faint">
                  FW: {probeResult.device.firmware_version || "N/A"}
                  {probeResult.device.serial_number ? ` | S/N: ${probeResult.device.serial_number}` : ""}
                </p>
              </div>
              <button
                type="button"
                onClick={() =>
                  addFromProbe(probeResult, probeHost.trim(), parseInt(probePort, 10) || 80, probeUser, probePass)
                }
                className="flex items-center gap-1.5 bg-green-600 hover:bg-green-700 text-white px-3 py-1.5 rounded-lg text-sm font-medium transition-colors"
              >
                <Plus className="w-3.5 h-3.5" />
                Add
              </button>
            </div>
            {probeResult.streams && probeResult.streams.length > 0 && (
              <div className="space-y-1.5">
                <p className="text-xs text-muted font-medium">Stream Profiles:</p>
                {probeResult.streams.map((s, i) => (
                  <div
                    key={s.token || i}
                    className="flex items-center gap-3 text-xs bg-surface-raised rounded px-3 py-2"
                  >
                    <span className="text-sentinel-400 font-medium min-w-[60px]">
                      {s.name || `Profile ${i + 1}`}
                    </span>
                    <span className="text-muted">{s.resolution}</span>
                    <span className="text-faint">{s.encoding}</span>
                    <span className="text-faint truncate flex-1 font-mono" title={s.stream_uri}>
                      {s.stream_uri}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/** A single discovered camera row with inline credential inputs and probe/add buttons. */
function DiscoveredCameraRow({
  camera,
  probeResult,
  probeError,
  isProbing,
  probeUser,
  probePass,
  onProbeUserChange,
  onProbePassChange,
  onProbe,
  onAdd,
}: {
  camera: DiscoveredCamera;
  probeResult?: ProbeResult;
  probeError?: string;
  isProbing: boolean;
  probeUser: string;
  probePass: string;
  onProbeUserChange: (v: string) => void;
  onProbePassChange: (v: string) => void;
  onProbe: () => void;
  onAdd: () => void;
}) {
  const [showCreds, setShowCreds] = useState(false);

  return (
    <div className="bg-surface-base border border-border rounded-lg p-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3 min-w-0">
          <Camera className="w-5 h-5 text-sentinel-400 flex-shrink-0" />
          <div className="min-w-0">
            <p className="text-sm font-medium text-white truncate">
              {camera.name || camera.hardware || "Unknown Camera"}
            </p>
            <p className="text-xs text-faint">
              {camera.ip}:{camera.port}
              {camera.hardware && camera.name ? ` | ${camera.hardware}` : ""}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          {!probeResult && (
            <button
              type="button"
              onClick={() => setShowCreds(!showCreds)}
              className="text-xs text-faint hover:text-muted transition-colors px-2 py-1"
              title="Enter credentials to probe for stream URLs"
            >
              {showCreds ? "Hide" : "Credentials"}
            </button>
          )}
          {!probeResult && (
            <button
              type="button"
              onClick={onProbe}
              disabled={isProbing}
              className="flex items-center gap-1.5 border border-border hover:border-sentinel-500 text-muted hover:text-white px-3 py-1.5 rounded-lg text-xs font-medium transition-colors disabled:opacity-50"
            >
              {isProbing ? (
                <Loader2 className="w-3 h-3 animate-spin" />
              ) : (
                <Search className="w-3 h-3" />
              )}
              Probe
            </button>
          )}
          <button
            type="button"
            onClick={onAdd}
            className="flex items-center gap-1.5 bg-green-600 hover:bg-green-700 text-white px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
          >
            <Plus className="w-3 h-3" />
            Add
          </button>
        </div>
      </div>

      {/* Inline credentials for probing */}
      {showCreds && !probeResult && (
        <div className="grid grid-cols-2 gap-2 mt-2 pt-2 border-t border-border">
          <input
            type="text"
            value={probeUser}
            onChange={(e) => onProbeUserChange(e.target.value)}
            placeholder="Username"
            className="bg-surface-raised border border-border rounded px-2 py-1.5 text-xs text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
          />
          <input
            type="password"
            value={probePass}
            onChange={(e) => onProbePassChange(e.target.value)}
            placeholder="Password"
            className="bg-surface-raised border border-border rounded px-2 py-1.5 text-xs text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
          />
        </div>
      )}

      {/* Probe error */}
      {probeError && (
        <p className="text-red-400 text-xs mt-2">{probeError}</p>
      )}

      {/* Probe result (inline) */}
      {probeResult && (
        <div className="mt-2 pt-2 border-t border-border">
          <p className="text-xs text-muted">
            {probeResult.device.manufacturer} {probeResult.device.model}
            {probeResult.streams?.length ? ` | ${probeResult.streams.length} stream profile${probeResult.streams.length !== 1 ? "s" : ""}` : ""}
          </p>
          {probeResult.streams?.map((s, i) => (
            <p key={s.token || i} className="text-xs text-faint font-mono truncate mt-0.5" title={s.stream_uri}>
              {s.name || `Profile ${i + 1}`}: {s.resolution} {s.encoding} — {s.stream_uri}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}
