/** TimelineBar — 24h horizontal scrubber showing recording coverage and detection heatmap (R6). */
import { useCallback, useMemo, useRef, useEffect } from "react";
import type { TimelineSegment, HeatmapBucket } from "../../api/client";
import { isoToSecondsSinceMidnight, formatHour, formatDate } from "../../utils/time";

export type ZoomLevel = "24h" | "6h" | "2h" | "1h";

const ZOOM_DURATIONS: Record<ZoomLevel, number> = {
  "24h": 86400,
  "6h": 21600,
  "2h": 7200,
  "1h": 3600,
};

const SECONDS_PER_DAY = 86400;

interface TimelineBarProps {
  segments: TimelineSegment[];
  heatmapBuckets?: HeatmapBucket[]; // Phase 6: detection density overlay (R6)
  activeSegmentId: number | null;
  currentTime: number | null; // seconds since midnight (playhead position)
  zoomLevel: ZoomLevel;
  zoomCenter: number;         // seconds since midnight for center of zoom window
  onSeek: (secondsSinceMidnight: number) => void;
  onZoomChange: (level: ZoomLevel) => void;
  onZoomCenterChange: (center: number) => void;
  date: string;               // YYYY-MM-DD for display
}

export default function TimelineBar({
  segments,
  heatmapBuckets,
  activeSegmentId,
  currentTime,
  zoomLevel,
  zoomCenter,
  onSeek,
  onZoomChange,
  onZoomCenterChange,
  date,
}: TimelineBarProps) {
  const barRef = useRef<HTMLDivElement>(null);

  // Memoize visible window bounds — only recompute when zoom settings change,
  // not on every 60fps playhead update triggered by currentTime.
  const { viewStart, viewEnd, viewDuration, halfDuration } = useMemo(() => {
    const zoomDuration = ZOOM_DURATIONS[zoomLevel];
    const half = zoomDuration / 2;
    let start = Math.max(0, zoomCenter - half);
    let end = start + zoomDuration;
    if (end > SECONDS_PER_DAY) {
      end = SECONDS_PER_DAY;
      start = Math.max(0, end - zoomDuration);
    }
    return { viewStart: start, viewEnd: end, viewDuration: end - start, halfDuration: half };
  }, [zoomCenter, zoomLevel]);

  // Memoize position helper — stable as long as the view window doesn't change.
  const toPercent = useCallback(
    (seconds: number) => ((seconds - viewStart) / viewDuration) * 100,
    [viewStart, viewDuration],
  );

  // Click-to-seek handler
  const handleBarClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      const rect = e.currentTarget.getBoundingClientRect();
      const fraction = (e.clientX - rect.left) / rect.width;
      const clickedSeconds = viewStart + fraction * viewDuration;
      onSeek(Math.max(0, Math.min(SECONDS_PER_DAY, clickedSeconds)));
    },
    [viewStart, viewDuration, onSeek],
  );

  // Keep a ref with the latest wheel-handler inputs so the listener can be
  // registered once ([] deps) instead of on every pan step. Without this,
  // zoomCenter in deps causes remove+add on every scroll tick, risking missed
  // events between the remove and the re-add during rapid panning.
  const wheelStateRef = useRef({ zoomCenter, zoomLevel, viewDuration, halfDuration, onZoomCenterChange });
  wheelStateRef.current = { zoomCenter, zoomLevel, viewDuration, halfDuration, onZoomCenterChange };

  // Wheel to pan when zoomed — uses a native non-passive listener so
  // preventDefault() actually works (React attaches wheel listeners as
  // passive by default, making preventDefault a no-op).
  useEffect(() => {
    const el = barRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      const { zoomLevel: zl, zoomCenter: zc, viewDuration: vd, halfDuration: hd, onZoomCenterChange: onChange } =
        wheelStateRef.current;
      if (zl === "24h") return;
      e.preventDefault();
      const panStep = vd * 0.1;
      onChange(Math.max(hd, Math.min(SECONDS_PER_DAY - hd, zc + (e.deltaY > 0 ? panStep : -panStep))));
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []); // register once — stale closure prevented by wheelStateRef

  // Pre-compute the max detection count once per heatmap data update, not on every render.
  const heatmapMaxCount = useMemo(
    () =>
      heatmapBuckets && heatmapBuckets.length > 0
        ? Math.max(...heatmapBuckets.map((b) => b.detection_count))
        : 0,
    [heatmapBuckets],
  );

  // Memoize tick marks — only recomputes when the view window changes, not on every
  // 60fps playhead tick. Without this the array is rebuilt ~60 times/sec while playing.
  const ticks = useMemo(() => {
    const tickInterval = zoomLevel === "1h" ? 900 : zoomLevel === "2h" ? 1800 : 3600;
    const firstTick = Math.ceil(viewStart / tickInterval) * tickInterval;
    const result: number[] = [];
    for (let t = firstTick; t <= viewEnd; t += tickInterval) {
      result.push(t);
    }
    return result;
  }, [viewStart, viewEnd, zoomLevel]);

  // Memoize heatmap JSX — stable while heatmap data and view window are unchanged.
  const heatmapNodes = useMemo(() => {
    if (!heatmapBuckets || heatmapBuckets.length === 0) return null;
    return heatmapBuckets.map((bucket) => {
      const bucketStartSec = isoToSecondsSinceMidnight(bucket.bucket_start);
      const bucketEndSec = bucketStartSec + 300; // 5-minute bucket
      const left = toPercent(bucketStartSec);
      const right = toPercent(bucketEndSec);
      if (right < 0 || left > 100) return null;
      const clampedLeft = Math.max(0, left);
      const clampedWidth = Math.max(0, Math.min(100, right) - clampedLeft);
      const intensity = heatmapMaxCount > 0 ? bucket.detection_count / heatmapMaxCount : 0;
      const alpha = 0.15 + intensity * 0.65;
      return (
        <div
          key={bucket.bucket_start}
          className="absolute top-0 pointer-events-none"
          style={{
            left: `${clampedLeft}%`,
            width: `${clampedWidth}%`,
            height: "40%",
            backgroundColor: `rgba(248, 81, 73, ${alpha})`,
          }}
        />
      );
    });
  }, [heatmapBuckets, heatmapMaxCount, toPercent]);

  // Memoize recording segment JSX — only recomputes when segments, active selection,
  // or the view window changes (not on 60fps playhead ticks).
  const segmentNodes = useMemo(
    () =>
      segments.map((seg) => {
        // Skip in-progress segments that have no end_time yet — they have no
        // fixed end boundary and would render with segEnd=NaN causing a crash.
        if (!seg.end_time) return null;
        const segStart = isoToSecondsSinceMidnight(seg.start_time);
        const segEnd = isoToSecondsSinceMidnight(seg.end_time);
        const left = toPercent(segStart);
        const right = toPercent(segEnd);
        if (right < 0 || left > 100) return null;
        const clampedLeft = Math.max(0, left);
        const clampedRight = Math.min(100, right);
        const clampedWidth = Math.max(0, clampedRight - clampedLeft);
        const isActive = seg.id === activeSegmentId;
        return (
          <div
            key={seg.id}
            className={`absolute top-0 bottom-0 rounded-sm transition-colors ${
              isActive ? "bg-sentinel-400" : "bg-sentinel-500/50 hover:bg-sentinel-500/70"
            }`}
            style={{ left: `${clampedLeft}%`, width: `${clampedWidth}%` }}
          />
        );
      }),
    [segments, activeSegmentId, toPercent],
  );

  // Memoize tick mark JSX — stable while view window is unchanged.
  const tickNodes = useMemo(
    () =>
      ticks.map((t) => {
        const pct = toPercent(t);
        if (pct < 0 || pct > 100) return null;
        const isHour = t % 3600 === 0;
        return (
          <div
            key={t}
            className="absolute top-0 bottom-0 pointer-events-none"
            style={{ left: `${pct}%` }}
          >
            <div className={`w-px h-full ${isHour ? "bg-border" : "bg-border/50"}`} />
            {isHour && (
              <span className="absolute -bottom-4 -translate-x-1/2 text-[10px] text-faint whitespace-nowrap">
                {formatHour(t)}
              </span>
            )}
          </div>
        );
      }),
    [ticks, toPercent],
  );

  return (
    <div className="flex flex-col gap-1">
      {/* Header row: zoom controls + date */}
      <div className="flex items-center justify-between px-1">
        <div className="flex items-center gap-1">
          {(["24h", "6h", "2h", "1h"] as ZoomLevel[]).map((level) => (
            <button
              key={level}
              onClick={() => onZoomChange(level)}
              className={`px-2 py-0.5 rounded text-xs font-medium transition-colors ${
                zoomLevel === level
                  ? "bg-sentinel-500 text-white"
                  : "text-muted hover:text-white hover:bg-surface-overlay"
              }`}
            >
              {level}
            </button>
          ))}
        </div>
        <span className="text-xs text-muted">{date ? formatDate(date) : ""}</span>
      </div>

      {/* Timeline bar */}
      <div
        ref={barRef}
        className="relative h-10 bg-surface-base rounded border border-border cursor-pointer select-none overflow-hidden"
        onClick={handleBarClick}
      >
        {/* Detection heatmap overlay — semi-transparent red bands at top of bar (Phase 6, R6).
            Rendered below recording segments so segment colors remain visible.
            JSX is memoized in heatmapNodes above; only recomputes on data/view changes. */}
        {heatmapNodes}

        {/* Recording segments — memoized in segmentNodes above */}
        {segmentNodes}

        {/* Tick marks — memoized in tickNodes above */}
        {tickNodes}

        {/* "Now" indicator — dashed yellow line at current wall clock time (today only) */}
        {date === new Date().toISOString().slice(0, 10) && (() => {
          const now = new Date();
          const nowSec = now.getHours() * 3600 + now.getMinutes() * 60 + now.getSeconds();
          if (nowSec >= viewStart && nowSec <= viewEnd) {
            return (
              <div
                className="absolute top-0 bottom-0 w-0.5 bg-yellow-400/60 pointer-events-none z-[5] border-l border-dashed border-yellow-400/40"
                style={{ left: `${toPercent(nowSec)}%` }}
                title={`Now: ${now.toLocaleTimeString()}`}
              />
            );
          }
          return null;
        })()}

        {/* Playhead */}
        {currentTime !== null && currentTime >= viewStart && currentTime <= viewEnd && (
          <div
            className="absolute top-0 bottom-0 w-0.5 bg-status-error pointer-events-none z-10"
            style={{ left: `${toPercent(currentTime)}%` }}
          >
            <div className="absolute -top-1 -translate-x-1/2 w-2 h-2 bg-status-error rounded-full" />
          </div>
        )}
      </div>

      {/* Bottom margin for tick labels */}
      <div className="h-3" />
    </div>
  );
}
