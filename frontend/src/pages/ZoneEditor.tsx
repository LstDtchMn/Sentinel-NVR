/**
 * ZoneEditor — visual zone configuration for a camera (Phase 9, R5).
 *
 * Loads a live snapshot from the camera, displays it as a background, and
 * overlays an HTML5 canvas for polygon drawing. Zones are saved via PUT /cameras/:name.
 *
 * Drawing mode:
 *   - Click "Draw Zone" to enter drawing mode (crosshair cursor).
 *   - Click canvas to add polygon vertices (normalised [0,1] coords).
 *   - Click within 12px of the first vertex (when >=3 points placed) to close the polygon.
 *   - Escape or right-click cancels the in-progress polygon.
 *   - Double-click also closes the polygon when >=3 points placed.
 *   - After closing, a form appears to name the zone and choose include/exclude type.
 *
 * Snapshot auto-refreshes every 10 seconds to stay current.
 */
import { useEffect, useRef, useState, useCallback } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import { api, CameraDetail, Zone, ZonePoint, ZoneType } from "../api/client";
import { ArrowLeft, Trash2, Plus } from "lucide-react";

// Pixel distance threshold for closing a polygon by clicking near the first vertex.
const CLOSE_THRESHOLD_PX = 12;

type DrawState = "idle" | "drawing";

interface PendingZone {
  points: ZonePoint[];
}

function generateID(): string {
  // crypto.randomUUID() is available in all modern browsers and produces a
  // cryptographically-random UUID, unlike Math.random() which is predictable.
  return crypto.randomUUID();
}

// Zone colours — include zones are blue, exclude zones are red.
const ZONE_COLORS: Record<ZoneType, { fill: string; stroke: string }> = {
  include: { fill: "rgba(76, 110, 245, 0.25)", stroke: "rgba(76, 110, 245, 0.85)" },
  exclude: { fill: "rgba(248, 81, 73, 0.25)", stroke: "rgba(248, 81, 73, 0.85)" },
};
// Pending polygon (not yet confirmed) — grey
const PENDING_STROKE = "rgba(255,255,255,0.7)";
const PENDING_FILL = "rgba(255,255,255,0.07)";
const VERTEX_COLOR = "rgba(255,255,255,0.9)";
const FIRST_VERTEX_SNAP_COLOR = "rgba(255,255,0,0.9)";

export default function ZoneEditor() {
  const { name } = useParams<{ name: string }>();
  const navigate = useNavigate();
  const cameraName = name ?? "";

  // Snapshot state — tick increments every 10s to force image src refresh.
  // The <img> element is NOT keyed by tick — only src changes. This avoids
  // unmounting the DOM element (which would flash the canvas blank).
  const [tick, setTick] = useState(0);
  const imgRef = useRef<HTMLImageElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Full camera detail loaded on mount — used in handleSave to avoid a TOCTOU re-fetch.
  const loadedCameraRef = useRef<CameraDetail | null>(null);

  // Camera zones (persisted)
  const [zones, setZones] = useState<Zone[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);

  // Drawing state — pendingPointsRef mirrors state so event handlers always
  // read the latest value (avoids stale closures in click/dblclick sequences).
  const [drawState, setDrawState] = useState<DrawState>("idle");
  const drawStateRef = useRef<DrawState>("idle");
  const [pendingPoints, setPendingPoints] = useState<ZonePoint[]>([]);
  const pendingPointsRef = useRef<ZonePoint[]>([]);
  // Timestamps (ms) when each pending vertex was added. Used in handleCanvasDblClick
  // to strip phantom vertices from the two click events that precede every dblclick.
  const clickTimestampsRef = useRef<number[]>([]);
  const [pendingZone, setPendingZone] = useState<PendingZone | null>(null);
  const [mousePos, setMousePos] = useState<{ x: number; y: number } | null>(null);

  // Keep refs in sync with state
  useEffect(() => { pendingPointsRef.current = pendingPoints; }, [pendingPoints]);
  useEffect(() => { drawStateRef.current = drawState; }, [drawState]);

  // Pending zone form inputs
  const [pendingName, setPendingName] = useState("");
  const [pendingType, setPendingType] = useState<ZoneType>("include");

  // Save state
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);

  // AbortController for save — stored in ref so unmount cleanup can cancel it.
  const saveCtrlRef = useRef<AbortController | null>(null);

  // Cleanup on unmount: abort any in-flight save request.
  useEffect(() => () => { saveCtrlRef.current?.abort(); }, []);

  // Load current camera detail on mount.
  // The full detail is stored in loadedCameraRef so handleSave can preserve all
  // non-zone fields without a second fetch (avoids TOCTOU races on concurrent edits).
  useEffect(() => {
    const ctrl = new AbortController();
    api
      .getCamera(cameraName, ctrl.signal)
      .then((cam) => {
        loadedCameraRef.current = cam;
        setZones(cam.zones ?? []);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setLoadError(err.message);
      });
    return () => ctrl.abort();
  }, [cameraName]);

  // Auto-refresh snapshot every 10 seconds — paused while the user is actively drawing
  // to prevent canvas bitmap clears and visual flicker during polygon vertex placement.
  useEffect(() => {
    if (drawState === "drawing") return; // no timer while mid-draw
    const id = setInterval(() => setTick((t) => t + 1), 10_000);
    return () => clearInterval(id);
  }, [drawState]);

  // Sync canvas dimensions to the rendered image size.
  // Guards against zero dimensions to avoid clearing the canvas bitmap and
  // corrupting mid-draw vertex coordinates (toNorm divides by canvas size).
  const syncCanvasSize = useCallback(() => {
    const img = imgRef.current;
    const canvas = canvasRef.current;
    if (!img || !canvas) return;
    const w = img.clientWidth;
    const h = img.clientHeight;
    if (w === 0 || h === 0) return; // image not laid out yet — skip
    if (canvas.width !== w) canvas.width = w;
    if (canvas.height !== h) canvas.height = h;
  }, []);

  // ResizeObserver keeps canvas in sync when the window or layout changes.
  // Without this, drawn zones and new clicks would be misaligned after resize.
  useEffect(() => {
    const img = imgRef.current;
    if (!img) return;
    const ro = new ResizeObserver(() => syncCanvasSize());
    ro.observe(img);
    return () => ro.disconnect();
  }, [syncCanvasSize]);

  // Convert [0,1] normalised coords back to canvas pixels
  const toPx = useCallback(
    (pt: ZonePoint): { x: number; y: number } => {
      const canvas = canvasRef.current;
      if (!canvas) return { x: 0, y: 0 };
      return { x: pt.x * canvas.width, y: pt.y * canvas.height };
    },
    [],
  );

  // Distance between two pixel positions
  const dist = (ax: number, ay: number, bx: number, by: number) =>
    Math.sqrt((ax - bx) ** 2 + (ay - by) ** 2);

  // ── Canvas rendering ──────────────────────────────────────────────────────
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    ctx.clearRect(0, 0, canvas.width, canvas.height);

    // Draw confirmed zones
    for (const zone of zones) {
      if (zone.points.length < 2) continue;
      const colors = ZONE_COLORS[zone.type] ?? ZONE_COLORS.include;
      ctx.beginPath();
      const first = toPx(zone.points[0]);
      ctx.moveTo(first.x, first.y);
      for (let i = 1; i < zone.points.length; i++) {
        const p = toPx(zone.points[i]);
        ctx.lineTo(p.x, p.y);
      }
      ctx.closePath();
      ctx.fillStyle = colors.fill;
      ctx.fill();
      ctx.strokeStyle = colors.stroke;
      ctx.lineWidth = 2;
      ctx.stroke();

      // Vertex dots
      for (const pt of zone.points) {
        const p = toPx(pt);
        ctx.beginPath();
        ctx.arc(p.x, p.y, 4, 0, Math.PI * 2);
        ctx.fillStyle = colors.stroke;
        ctx.fill();
      }
    }

    // Draw in-progress polygon
    if (drawState === "drawing" && pendingPoints.length > 0) {
      // Determine if cursor is near the first vertex for snap indicator
      const firstPx = toPx(pendingPoints[0]);
      const nearFirst =
        pendingPoints.length >= 3 &&
        mousePos !== null &&
        dist(mousePos.x, mousePos.y, firstPx.x, firstPx.y) <= CLOSE_THRESHOLD_PX;

      ctx.beginPath();
      ctx.moveTo(firstPx.x, firstPx.y);
      for (let i = 1; i < pendingPoints.length; i++) {
        const p = toPx(pendingPoints[i]);
        ctx.lineTo(p.x, p.y);
      }
      // Draw a preview line to current mouse position
      if (mousePos && !nearFirst) {
        ctx.lineTo(mousePos.x, mousePos.y);
      } else if (mousePos && nearFirst) {
        ctx.closePath();
      }
      ctx.strokeStyle = PENDING_STROKE;
      ctx.lineWidth = 1.5;
      ctx.setLineDash([4, 3]);
      ctx.stroke();
      ctx.setLineDash([]);

      // Fill preview when >=3 points
      if (pendingPoints.length >= 3) {
        ctx.beginPath();
        const fp = toPx(pendingPoints[0]);
        ctx.moveTo(fp.x, fp.y);
        for (let i = 1; i < pendingPoints.length; i++) {
          const p = toPx(pendingPoints[i]);
          ctx.lineTo(p.x, p.y);
        }
        ctx.closePath();
        ctx.fillStyle = PENDING_FILL;
        ctx.fill();
      }

      // Vertex dots
      for (let i = 0; i < pendingPoints.length; i++) {
        const p = toPx(pendingPoints[i]);
        const isFirst = i === 0;
        ctx.beginPath();
        ctx.arc(p.x, p.y, isFirst ? 6 : 4, 0, Math.PI * 2);
        ctx.fillStyle =
          isFirst && nearFirst ? FIRST_VERTEX_SNAP_COLOR : VERTEX_COLOR;
        ctx.fill();
      }
    }
  }, [zones, pendingPoints, drawState, mousePos, toPx]);

  // ── Canvas event handlers ──────────────────────────────────────────────────

  const getCanvasPos = (e: React.MouseEvent<HTMLCanvasElement>) => {
    const rect = canvasRef.current!.getBoundingClientRect();
    return { x: e.clientX - rect.left, y: e.clientY - rect.top };
  };

  const handleCanvasMouseMove = (e: React.MouseEvent<HTMLCanvasElement>) => {
    if (drawState !== "drawing") return;
    setMousePos(getCanvasPos(e));
  };

  const handleCanvasMouseLeave = () => {
    setMousePos(null);
  };

  const closePendingPolygon = useCallback((pts: ZonePoint[]) => {
    setPendingZone({ points: pts });
    setPendingPoints([]);
    pendingPointsRef.current = [];
    clickTimestampsRef.current = [];
    setDrawState("idle");
    drawStateRef.current = "idle";
    setMousePos(null);
    setPendingName("");
    setPendingType("include");
  }, []);

  // Single-click: add vertex or close polygon (if near first vertex).
  // Reads from pendingPointsRef to get the latest points including any prior
  // clicks in the same event batch (avoids the double-click stale-closure bug
  // where click→click→dblclick would read pre-batch state).
  const handleCanvasClick = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    if (drawStateRef.current !== "drawing") return;
    const rect = canvasRef.current!.getBoundingClientRect();
    const pos = { x: e.clientX - rect.left, y: e.clientY - rect.top };
    const pts = pendingPointsRef.current;

    if (pts.length >= 3) {
      const canvas = canvasRef.current;
      if (canvas && canvas.width > 0 && canvas.height > 0) {
        const firstX = pts[0].x * canvas.width;
        const firstY = pts[0].y * canvas.height;
        if (dist(pos.x, pos.y, firstX, firstY) <= CLOSE_THRESHOLD_PX) {
          closePendingPolygon(pts);
          return;
        }
      }
    }

    const canvas = canvasRef.current;
    if (!canvas || canvas.width === 0 || canvas.height === 0) return;
    const newPt: ZonePoint = { x: pos.x / canvas.width, y: pos.y / canvas.height };
    const next = [...pts, newPt];
    pendingPointsRef.current = next;
    clickTimestampsRef.current = [...clickTimestampsRef.current, Date.now()];
    setPendingPoints(next);
  }, [closePendingPolygon]);

  // Double-click: close polygon.
  // A browser double-click fires click→click→dblclick. Both preceding click events
  // pass through handleCanvasClick and add phantom vertices at the dblclick position.
  // We strip them by removing any vertices whose recorded timestamp is within 400ms
  // of now — those are the two clicks that form this double-click.
  const handleCanvasDblClick = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    if (drawStateRef.current !== "drawing") return;
    const now = Date.now();
    let pts = pendingPointsRef.current;
    const ts = clickTimestampsRef.current;
    while (pts.length > 0 && ts.length > 0 && now - ts[ts.length - 1] < 400) {
      pts = pts.slice(0, -1);
      ts.pop(); // mutate in-place — ts is the same array as clickTimestampsRef.current
    }
    pendingPointsRef.current = pts;
    if (pts.length < 3) return;
    closePendingPolygon(pts);
  }, [closePendingPolygon]);

  const handleCanvasRightClick = (e: React.MouseEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    if (drawStateRef.current !== "drawing") return;
    setPendingPoints([]);
    pendingPointsRef.current = [];
    clickTimestampsRef.current = [];
    setDrawState("idle");
    drawStateRef.current = "idle";
    setMousePos(null);
  };

  // Escape key cancels in-progress drawing
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape" && drawStateRef.current === "drawing") {
        setPendingPoints([]);
        pendingPointsRef.current = [];
        clickTimestampsRef.current = [];
        setDrawState("idle");
        drawStateRef.current = "idle";
        setMousePos(null);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  // ── Zone management ────────────────────────────────────────────────────────

  const handleAddZone = () => {
    if (!pendingZone || !pendingName.trim()) return;
    const newZone: Zone = {
      id: generateID(),
      name: pendingName.trim(),
      type: pendingType,
      points: pendingZone.points,
    };
    setZones((prev) => [...prev, newZone]);
    setPendingZone(null);
    setDirty(true);
  };

  const handleDeleteZone = (id: string) => {
    setZones((prev) => prev.filter((z) => z.id !== id));
    setDirty(true);
  };

  const handleSave = async () => {
    if (saving) return;
    // Use the camera detail loaded on mount to preserve non-zone fields without
    // a second fetch — avoids TOCTOU races where a concurrent edit could be overwritten.
    const cam = loadedCameraRef.current;
    if (!cam) {
      setSaveError("Camera not loaded — please refresh the page");
      return;
    }
    setSaving(true);
    setSaveError(null);
    saveCtrlRef.current?.abort();
    const ctrl = new AbortController();
    saveCtrlRef.current = ctrl;
    let navigated = false;
    try {
      await api.updateCamera(
        cameraName,
        {
          name: cam.name,
          main_stream: cam.main_stream,
          sub_stream: cam.sub_stream || undefined,
          enabled: cam.enabled,
          record: cam.record,
          detect: cam.detect,
          zones,
        },
        ctrl.signal,
      );
      setDirty(false);
      navigated = true;
      navigate("/cameras");
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") return;
      setSaveError(err instanceof Error ? err.message : "Failed to save zones");
    } finally {
      // Don't call setSaving on the unmounted component after a successful navigate.
      if (!navigated) setSaving(false);
    }
  };

  const handleBack = () => {
    if (dirty && !window.confirm("You have unsaved changes. Leave without saving?")) return;
    navigate("/cameras");
  };

  const snapshotURL = `${api.cameraSnapshotURL(cameraName)}?t=${tick}`;

  return (
    <div className="p-8">
      {/* Header */}
      <div className="flex items-center gap-4 mb-6">
        <button
          onClick={handleBack}
          className="flex items-center gap-2 text-muted hover:text-white text-sm transition-colors"
        >
          <ArrowLeft className="w-4 h-4" />
          Back to Cameras
        </button>
        <h1 className="text-2xl font-semibold">Zone Editor — {cameraName}</h1>
      </div>

      {loadError && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4 mb-6">
          <p className="text-red-400 text-sm">Failed to load camera: {loadError}</p>
        </div>
      )}
      {saveError && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4 mb-6">
          <p className="text-red-400 text-sm">{saveError}</p>
        </div>
      )}

      <div className="flex gap-6">
        {/* Canvas area */}
        <div className="flex-1 min-w-0">
          <div
            ref={containerRef}
            className="relative bg-black rounded-lg overflow-hidden select-none"
            style={{ lineHeight: 0 }}
          >
            {/* No key={tick} — only src changes. Keeps the DOM element stable so the
                canvas overlay is never disrupted by a React remount. */}
            <img
              ref={imgRef}
              src={snapshotURL}
              onLoad={syncCanvasSize}
              alt={`${cameraName} snapshot`}
              className="w-full h-auto block"
              draggable={false}
            />
            <canvas
              ref={canvasRef}
              className="absolute inset-0"
              style={{ cursor: drawState === "drawing" ? "crosshair" : "default" }}
              onClick={handleCanvasClick}
              onDoubleClick={handleCanvasDblClick}
              onMouseMove={handleCanvasMouseMove}
              onMouseLeave={handleCanvasMouseLeave}
              onContextMenu={handleCanvasRightClick}
            />
          </div>
          <p className="text-xs text-faint mt-2">
            {drawState === "drawing"
              ? "Click to add vertices. Click near the first vertex (or double-click) to close. Escape or right-click to cancel."
              : "Click \"Draw Zone\" to start drawing a polygon zone."}
          </p>
          <p className="text-xs text-faint">Snapshot refreshes every 10 seconds.</p>
        </div>

        {/* Sidebar */}
        <div className="w-72 flex-shrink-0 flex flex-col gap-4">
          {/* Zone list */}
          <div className="bg-surface-raised border border-border rounded-lg p-4">
            <h3 className="text-sm font-medium text-muted mb-3">
              Zones {zones.length > 0 ? `(${zones.length})` : ""}
            </h3>
            {zones.length === 0 && (
              <p className="text-xs text-faint">No zones configured. Draw a zone to get started.</p>
            )}
            <ul className="space-y-2">
              {zones.map((zone) => (
                <li key={zone.id} className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <span
                      className="w-2.5 h-2.5 rounded-full flex-shrink-0"
                      style={{
                        backgroundColor: zone.type === "include"
                          ? "rgba(76,110,245,0.9)"
                          : "rgba(248,81,73,0.9)",
                      }}
                    />
                    <span className="text-sm truncate">{zone.name}</span>
                    <span className="text-xs text-faint flex-shrink-0">{zone.type}</span>
                  </div>
                  <button
                    onClick={() => handleDeleteZone(zone.id)}
                    className="text-faint hover:text-red-400 transition-colors p-0.5 flex-shrink-0"
                    aria-label={`Delete zone ${zone.name}`}
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </li>
              ))}
            </ul>
          </div>

          {/* Pending zone form — shown after polygon is closed */}
          {pendingZone && (
            <div className="bg-surface-raised border border-sentinel-500/40 rounded-lg p-4">
              <h3 className="text-sm font-medium mb-3">New Zone</h3>
              <div className="space-y-3">
                <div>
                  <label htmlFor="zone-name" className="block text-xs text-muted mb-1">Name</label>
                  <input
                    id="zone-name"
                    type="text"
                    value={pendingName}
                    onChange={(e) => setPendingName(e.target.value)}
                    placeholder="Driveway"
                    autoFocus
                    className="w-full bg-surface-base border border-border rounded-lg px-3 py-1.5 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
                  />
                </div>
                <div>
                  <label htmlFor="zone-type" className="block text-xs text-muted mb-1">Type</label>
                  <select
                    id="zone-type"
                    value={pendingType}
                    onChange={(e) => setPendingType(e.target.value as ZoneType)}
                    className="w-full bg-surface-base border border-border rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-sentinel-500"
                  >
                    <option value="include">Include — detect only inside zone</option>
                    <option value="exclude">Exclude — ignore detections inside zone</option>
                  </select>
                </div>
                <div className="flex gap-2 pt-1">
                  <button
                    onClick={handleAddZone}
                    disabled={!pendingName.trim()}
                    className="flex-1 flex items-center justify-center gap-1.5 bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-3 py-1.5 rounded-lg text-sm font-medium transition-colors"
                  >
                    <Plus className="w-3.5 h-3.5" />
                    Add Zone
                  </button>
                  <button
                    onClick={() => setPendingZone(null)}
                    className="px-3 py-1.5 text-sm text-muted hover:text-white transition-colors"
                  >
                    Cancel
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* Draw Zone button */}
          {!pendingZone && (
            <button
              onClick={() => {
                setPendingPoints([]);
                pendingPointsRef.current = [];
                setDrawState("drawing");
              }}
              disabled={drawState === "drawing"}
              className="w-full flex items-center justify-center gap-2 border border-border hover:border-sentinel-500 disabled:opacity-50 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
            >
              <Plus className="w-4 h-4" />
              Draw Zone
            </button>
          )}

          {/* Save Zones button */}
          <button
            onClick={handleSave}
            disabled={saving || !dirty}
            className="w-full bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
          >
            {saving ? "Saving..." : "Save Zones"}
          </button>

          <Link
            to="/cameras"
            onClick={(e) => {
              if (dirty) {
                if (!window.confirm("You have unsaved changes. Leave without saving?")) {
                  e.preventDefault();
                }
              }
            }}
            className="text-center text-sm text-muted hover:text-white transition-colors"
          >
            Cancel
          </Link>
        </div>
      </div>
    </div>
  );
}
