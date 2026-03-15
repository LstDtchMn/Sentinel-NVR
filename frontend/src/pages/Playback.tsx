/**
 * Playback — recording playback page with timeline scrubber (R6).
 * Allows selecting a camera and date, viewing recording coverage on a 24h timeline,
 * and playing back segments with seek, speed control, and auto-advance.
 */
import { useState, useEffect, useCallback, useRef } from "react";
import { useSearchParams } from "react-router-dom";
import { ChevronLeft, ChevronRight, Download, Scissors, X, Loader2 } from "lucide-react";
import { api, type CameraDetail, type TimelineSegment, type HeatmapBucket } from "../api/client";
import { todayDateString, currentMonthString, isoToSecondsSinceMidnight } from "../utils/time";
import CameraSelector from "../components/playback/CameraSelector";
import DatePicker from "../components/playback/DatePicker";
import TimelineBar, { type ZoomLevel } from "../components/playback/TimelineBar";
import RecordingPlayer, { type RecordingPlayerHandle } from "../components/playback/RecordingPlayer";

export default function Playback() {
  // Camera state
  const [cameras, setCameras] = useState<CameraDetail[]>([]);
  const [selectedCamera, setSelectedCamera] = useState<string | null>(
    () => localStorage.getItem('playback-last-camera') || null,
  );

  // Date state
  const [selectedDate, setSelectedDate] = useState(todayDateString());
  const [displayMonth, setDisplayMonth] = useState(currentMonthString());
  const [availableDays, setAvailableDays] = useState<Set<string>>(new Set());

  // Timeline data
  const [segments, setSegments] = useState<TimelineSegment[]>([]);

  // Playback state
  const [activeSegment, setActiveSegment] = useState<TimelineSegment | null>(null);
  const [playbackRate, setPlaybackRate] = useState(1);
  const [currentTime, setCurrentTime] = useState<number | null>(null);

  // Heatmap data — detection density buckets for overlay on timeline (Phase 6, R6)
  const [heatmapBuckets, setHeatmapBuckets] = useState<HeatmapBucket[]>([]);

  // Timeline zoom
  const [zoomLevel, setZoomLevel] = useState<ZoomLevel>("24h");
  const [zoomCenter, setZoomCenter] = useState(43200); // noon

  // UI state
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  // Ref for stable onTimeUpdate callback
  const activeSegmentRef = useRef(activeSegment);
  activeSegmentRef.current = activeSegment;

  // Ref to the player for imperative seek on timeline click (R6).
  const playerRef = useRef<RecordingPlayerHandle | null>(null);

  // URL query params for deep-link from EventDetail "Jump to Recording"
  const [searchParams] = useSearchParams();
  // Pending seek from URL params — consumed once after segments load
  const pendingUrlSeekRef = useRef<number | null>(null);

  // Export clip state (v0.3)
  const [exportMode, setExportMode] = useState(false);
  const [exportStart, setExportStart] = useState<number | null>(null); // seconds since midnight
  const [exportEnd, setExportEnd] = useState<number | null>(null);     // seconds since midnight
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState<string | null>(null);
  const exportCtrlRef = useRef<AbortController | null>(null);

  // Apply URL query params on mount (camera, date, time)
  useEffect(() => {
    const cameraParam = searchParams.get("camera");
    const dateParam = searchParams.get("date");
    const timeParam = searchParams.get("time");

    if (cameraParam) {
      setSelectedCamera(cameraParam);
    }
    if (dateParam) {
      setSelectedDate(dateParam);
      // Sync calendar display month
      const parts = dateParam.split("-");
      if (parts.length >= 2) {
        setDisplayMonth(`${parts[0]}-${parts[1]}`);
      }
    }
    if (timeParam) {
      pendingUrlSeekRef.current = Number(timeParam);
    }
    // Only run once on mount — searchParams identity changes on every render
    // but we only want the initial URL values.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Fetch cameras on mount
  useEffect(() => {
    const controller = new AbortController();
    api
      .getCameras(controller.signal)
      .then((data) => setCameras(data))
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setError(err.message);
      });
    return () => controller.abort();
  }, []);

  // Fetch available days when camera or display month changes
  useEffect(() => {
    if (!selectedCamera) {
      setAvailableDays(new Set());
      return;
    }
    const controller = new AbortController();
    api
      .getRecordingDays({ camera: selectedCamera, month: displayMonth }, controller.signal)
      .then((days) => setAvailableDays(new Set(days)))
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        console.error("Failed to fetch recording days:", err);
      });
    return () => controller.abort();
  }, [selectedCamera, displayMonth]);

  // Fetch timeline when camera or selected date changes
  useEffect(() => {
    if (!selectedCamera) {
      setSegments([]);
      setActiveSegment(null);
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    api
      .getTimeline({ camera: selectedCamera, date: selectedDate }, controller.signal)
      .then((data) => {
        setSegments(data);
        setActiveSegment(null);
        setCurrentTime(null);
        setLoading(false);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") {
          // Always reset loading on abort — the next render's effect will
          // set it back to true if a new fetch starts.
          setLoading(false);
          return;
        }
        setError(err.message);
        setLoading(false);
      });
    return () => controller.abort();
  }, [selectedCamera, selectedDate]);

  // Derive the camera ID for the selected camera name. Using the numeric ID as the effect
  // dependency prevents spurious heatmap re-fetches when the cameras array updates due to
  // background status polling — camera IDs are stable once assigned.
  const selectedCameraId =
    selectedCamera !== null
      ? (cameras.find((c) => c.name === selectedCamera)?.id ?? null)
      : null;

  // Fetch detection heatmap for the timeline overlay when camera or date changes (Phase 6, R6).
  // Heatmap failure is non-critical — log silently rather than showing an error banner.
  useEffect(() => {
    if (selectedCameraId === null) {
      setHeatmapBuckets([]);
      return;
    }
    const controller = new AbortController();
    api
      .getEventHeatmap({ camera_id: selectedCameraId, date: selectedDate }, controller.signal)
      .then(setHeatmapBuckets)
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        console.warn("Failed to fetch event heatmap:", err);
        setHeatmapBuckets([]);
      });
    return () => controller.abort();
  }, [selectedCameraId, selectedDate]);

  // Camera selection handler — persist to localStorage so Playback remembers the last camera
  const handleCameraSelect = useCallback((name: string) => {
    setSelectedCamera(name);
    localStorage.setItem('playback-last-camera', name);
    setActiveSegment(null);
    setCurrentTime(null);
  }, []);

  // Date selection handler
  const handleDateSelect = useCallback((date: string) => {
    setSelectedDate(date);
    setActiveSegment(null);
    setCurrentTime(null);
  }, []);

  // Prev/next day navigation helpers
  const shiftDate = useCallback(
    (days: number) => {
      const d = new Date(selectedDate + "T00:00:00");
      d.setDate(d.getDate() + days);
      const shifted = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
      handleDateSelect(shifted);
      // Sync calendar display month when navigating across month boundary
      setDisplayMonth(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`);
    },
    [selectedDate, handleDateSelect],
  );

  const isToday = selectedDate === todayDateString();

  // Timeline seek handler — find segment at the clicked time and seek the player (R6).
  // For same-segment seeks, calls seekTo() directly on the loaded video.
  // For cross-segment seeks, calls setInitialSeek() before changing activeSegment so
  // the RecordingPlayer applies the offset after the new segment's canplay fires.
  const handleTimelineSeek = useCallback(
    (secondsSinceMidnight: number) => {
      const seg = segments.find((s) => {
        const start = isoToSecondsSinceMidnight(s.start_time);
        const end = isoToSecondsSinceMidnight(s.end_time);
        return secondsSinceMidnight >= start && secondsSinceMidnight <= end;
      });

      if (seg) {
        const segStart = isoToSecondsSinceMidnight(seg.start_time);
        const offsetWithinSeg = secondsSinceMidnight - segStart;
        if (seg.id === activeSegmentRef.current?.id) {
          // Same segment: seek the already-loaded video immediately.
          playerRef.current?.seekTo(offsetWithinSeg);
        } else {
          // Different segment: store offset before React loads the new segment.
          playerRef.current?.setInitialSeek(offsetWithinSeg);
          setActiveSegment(seg);
        }
        setCurrentTime(secondsSinceMidnight);
      } else {
        // Snap to nearest segment (start of that segment)
        let closest: TimelineSegment | null = null;
        let closestDist = Infinity;
        for (const s of segments) {
          const start = isoToSecondsSinceMidnight(s.start_time);
          const end = isoToSecondsSinceMidnight(s.end_time);
          const dist = Math.min(
            Math.abs(secondsSinceMidnight - start),
            Math.abs(secondsSinceMidnight - end),
          );
          if (dist < closestDist) {
            closestDist = dist;
            closest = s;
          }
        }
        if (closest) {
          setActiveSegment(closest);
          setCurrentTime(isoToSecondsSinceMidnight(closest.start_time));
        }
      }
    },
    [segments],
  );

  // When segments load with a pending URL seek, trigger the seek
  useEffect(() => {
    if (segments.length > 0 && pendingUrlSeekRef.current !== null) {
      const seekTime = pendingUrlSeekRef.current;
      pendingUrlSeekRef.current = null;
      handleTimelineSeek(seekTime);
    }
  }, [segments, handleTimelineSeek]);

  // Video time update — convert to seconds since midnight for timeline playhead
  const handleTimeUpdate = useCallback((videoCurrentTime: number) => {
    const seg = activeSegmentRef.current;
    if (!seg) return;
    const segStartSeconds = isoToSecondsSinceMidnight(seg.start_time);
    setCurrentTime(segStartSeconds + videoCurrentTime);
  }, []);

  // Auto-advance to next segment
  const handleEnded = useCallback(() => {
    if (!activeSegment) return;
    const idx = segments.findIndex((s) => s.id === activeSegment.id);
    if (idx >= 0 && idx < segments.length - 1) {
      setActiveSegment(segments[idx + 1]);
    }
  }, [activeSegment, segments]);

  // Segment change from player controls (skip prev/next)
  const handleSegmentChange = useCallback((seg: TimelineSegment) => {
    setActiveSegment(seg);
    setCurrentTime(isoToSecondsSinceMidnight(seg.start_time));
  }, []);

  // Zoom change — center on playhead if available
  const handleZoomChange = useCallback(
    (level: ZoomLevel) => {
      setZoomLevel(level);
      if (currentTime !== null) {
        setZoomCenter(currentTime);
      }
    },
    [currentTime],
  );

  // Export clip handlers (v0.3)
  const handleEnterExportMode = useCallback(() => {
    setExportMode(true);
    setExportError(null);
    // Default range: if playhead exists, center a 30s clip around it
    if (currentTime !== null) {
      setExportStart(Math.max(0, currentTime - 15));
      setExportEnd(Math.min(86400, currentTime + 15));
    } else if (segments.length > 0) {
      const segStart = isoToSecondsSinceMidnight(segments[0].start_time);
      setExportStart(segStart);
      setExportEnd(Math.min(86400, segStart + 30));
    }
  }, [currentTime, segments]);

  const handleCancelExport = useCallback(() => {
    setExportMode(false);
    setExportStart(null);
    setExportEnd(null);
    setExportError(null);
    exportCtrlRef.current?.abort();
  }, []);

  const exportDuration = exportStart !== null && exportEnd !== null
    ? Math.max(0, exportEnd - exportStart)
    : 0;

  const handleExportDownload = useCallback(async () => {
    if (!selectedCamera || exportStart === null || exportEnd === null) return;
    if (exportDuration > 300) {
      setExportError("Maximum export duration is 5 minutes");
      return;
    }
    if (exportDuration < 1) {
      setExportError("Select at least 1 second");
      return;
    }

    setExporting(true);
    setExportError(null);
    exportCtrlRef.current?.abort();
    const ctrl = new AbortController();
    exportCtrlRef.current = ctrl;

    try {
      // Build RFC3339 timestamps from selectedDate + seconds-since-midnight
      const dateBase = selectedDate + "T";
      const startH = Math.floor(exportStart / 3600);
      const startM = Math.floor((exportStart % 3600) / 60);
      const startS = Math.floor(exportStart % 60);
      const endH = Math.floor(exportEnd / 3600);
      const endM = Math.floor((exportEnd % 3600) / 60);
      const endS = Math.floor(exportEnd % 60);
      const pad = (n: number) => String(n).padStart(2, "0");
      const startRFC = dateBase + `${pad(startH)}:${pad(startM)}:${pad(startS)}Z`;
      const endRFC = dateBase + `${pad(endH)}:${pad(endM)}:${pad(endS)}Z`;

      const result = await api.exportClip(
        { camera_name: selectedCamera, start: startRFC, end: endRFC },
        ctrl.signal,
      );

      // Trigger download via hidden anchor
      const a = document.createElement("a");
      a.href = api.exportDownloadURL(result.export_id);
      a.download = `${selectedCamera}_${selectedDate}_clip.mp4`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);

      setExportMode(false);
      setExportStart(null);
      setExportEnd(null);
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") return;
      setExportError(err instanceof Error ? err.message : "Export failed");
    } finally {
      setExporting(false);
    }
  }, [selectedCamera, selectedDate, exportStart, exportEnd, exportDuration]);

  // Cleanup export controller on unmount
  useEffect(() => () => exportCtrlRef.current?.abort(), []);

  // Keyboard shortcuts for playback controls.
  // Skip processing when an input element is focused to avoid interfering with typing.
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const tag = document.activeElement?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;

      switch (e.key) {
        case " ":
          e.preventDefault();
          playerRef.current?.togglePlayPause();
          break;
        case "ArrowLeft":
          e.preventDefault();
          playerRef.current?.seekRelative(-10);
          break;
        case "ArrowRight":
          e.preventDefault();
          playerRef.current?.seekRelative(10);
          break;
        case "[":
          if (activeSegment) {
            const idx = segments.findIndex((s) => s.id === activeSegment.id);
            if (idx > 0) handleSegmentChange(segments[idx - 1]);
          }
          break;
        case "]":
          if (activeSegment) {
            const idx = segments.findIndex((s) => s.id === activeSegment.id);
            if (idx >= 0 && idx < segments.length - 1) handleSegmentChange(segments[idx + 1]);
          }
          break;
        case "1":
          setPlaybackRate(1);
          break;
        case "2":
          setPlaybackRate(2);
          break;
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [activeSegment, segments, handleSegmentChange]);

  return (
    <div className="h-full flex flex-col">
      {/* Header */}
      <div className="flex items-center gap-4 px-6 py-3 border-b border-border flex-shrink-0">
        <h1 className="text-lg font-semibold">Playback</h1>
        <CameraSelector
          cameras={cameras}
          selected={selectedCamera}
          onSelect={handleCameraSelect}
        />
        <button
          type="button"
          onClick={() => shiftDate(-1)}
          className="p-1.5 rounded-lg bg-surface-base border border-border text-muted
                     hover:text-white hover:border-sentinel-500/50 transition-colors
                     focus:outline-none focus:ring-1 focus:ring-sentinel-500"
          aria-label="Previous day"
          title="Previous day"
        >
          <ChevronLeft className="w-4 h-4" />
        </button>
        <DatePicker
          selectedDate={selectedDate}
          availableDays={availableDays}
          displayMonth={displayMonth}
          onDateSelect={handleDateSelect}
          onMonthChange={setDisplayMonth}
        />
        <button
          type="button"
          onClick={() => shiftDate(1)}
          disabled={isToday}
          className="p-1.5 rounded-lg bg-surface-base border border-border text-muted
                     hover:text-white hover:border-sentinel-500/50 transition-colors
                     focus:outline-none focus:ring-1 focus:ring-sentinel-500
                     disabled:opacity-30 disabled:cursor-not-allowed disabled:hover:text-muted disabled:hover:border-border"
          aria-label="Next day"
          title="Next day"
        >
          <ChevronRight className="w-4 h-4" />
        </button>

        {/* Export Clip button (v0.3) */}
        <div className="ml-auto">
          {!exportMode ? (
            <button
              type="button"
              onClick={handleEnterExportMode}
              disabled={!selectedCamera || segments.length === 0}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium
                         bg-surface-base border border-border text-muted
                         hover:text-white hover:border-sentinel-500/50 transition-colors
                         disabled:opacity-30 disabled:cursor-not-allowed"
              title="Export a clip from this recording"
            >
              <Scissors className="w-4 h-4" />
              Export Clip
            </button>
          ) : (
            <button
              type="button"
              onClick={handleCancelExport}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium
                         bg-surface-base border border-red-800/50 text-red-400
                         hover:text-red-300 hover:border-red-700 transition-colors"
            >
              <X className="w-4 h-4" />
              Cancel Export
            </button>
          )}
        </div>
      </div>

      {/* Error banner */}
      {error && (
        <div className="mx-6 mt-3 px-4 py-2 bg-status-error/10 border border-status-error/30 rounded-lg text-sm text-status-error">
          {error}
        </div>
      )}

      {/* Loading state */}
      {loading && (
        <div className="flex items-center justify-center py-8">
          <span className="text-sm text-muted">Loading timeline...</span>
        </div>
      )}

      {/* Main content */}
      <div className="flex-1 flex flex-col min-h-0 p-3 gap-3">
        {/* Video player */}
        <RecordingPlayer
          ref={playerRef}
          segment={activeSegment}
          segments={segments}
          playbackRate={playbackRate}
          onTimeUpdate={handleTimeUpdate}
          onEnded={handleEnded}
          onSegmentChange={handleSegmentChange}
          onPlaybackRateChange={setPlaybackRate}
          className="flex-1 min-h-0"
          emptyMessage={
            !selectedCamera
              ? undefined
              : loading
                ? "Loading recordings..."
                : segments.length === 0
                  ? "No recordings for this date"
                  : "Click on the timeline to start playback"
          }
        />

        {/* Export clip controls (v0.3) — range sliders + download button */}
        {exportMode && (
          <div className="flex items-center gap-4 px-2 py-2 bg-surface-raised border border-sentinel-500/30 rounded-lg flex-shrink-0">
            <div className="flex items-center gap-2 flex-1 min-w-0">
              <label className="text-xs text-muted whitespace-nowrap">Start</label>
              <input
                type="range"
                min={0}
                max={86400}
                step={1}
                value={exportStart ?? 0}
                onChange={(e) => {
                  const v = Number(e.target.value);
                  setExportStart(v);
                  if (exportEnd !== null && v >= exportEnd) setExportEnd(Math.min(86400, v + 1));
                }}
                className="flex-1 accent-sentinel-500"
              />
              <span className="text-xs text-white/80 font-mono w-14 text-right">
                {exportStart !== null
                  ? `${String(Math.floor(exportStart / 3600)).padStart(2, "0")}:${String(Math.floor((exportStart % 3600) / 60)).padStart(2, "0")}:${String(Math.floor(exportStart % 60)).padStart(2, "0")}`
                  : "--:--:--"}
              </span>
            </div>
            <div className="flex items-center gap-2 flex-1 min-w-0">
              <label className="text-xs text-muted whitespace-nowrap">End</label>
              <input
                type="range"
                min={0}
                max={86400}
                step={1}
                value={exportEnd ?? 0}
                onChange={(e) => {
                  const v = Number(e.target.value);
                  setExportEnd(v);
                  if (exportStart !== null && v <= exportStart) setExportStart(Math.max(0, v - 1));
                }}
                className="flex-1 accent-sentinel-500"
              />
              <span className="text-xs text-white/80 font-mono w-14 text-right">
                {exportEnd !== null
                  ? `${String(Math.floor(exportEnd / 3600)).padStart(2, "0")}:${String(Math.floor((exportEnd % 3600) / 60)).padStart(2, "0")}:${String(Math.floor(exportEnd % 60)).padStart(2, "0")}`
                  : "--:--:--"}
              </span>
            </div>
            <div className="flex items-center gap-3 flex-shrink-0">
              <span className={`text-xs font-medium ${exportDuration > 300 ? "text-red-400" : "text-sentinel-400"}`}>
                {exportDuration >= 60
                  ? `${Math.floor(exportDuration / 60)}m ${exportDuration % 60}s`
                  : `${exportDuration}s`}
                {exportDuration > 300 && " (max 5 min)"}
              </span>
              <button
                type="button"
                onClick={handleExportDownload}
                disabled={exporting || exportDuration < 1 || exportDuration > 300}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium
                           bg-sentinel-500 hover:bg-sentinel-600 text-white transition-colors
                           disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {exporting ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : (
                  <Download className="w-4 h-4" />
                )}
                {exporting ? "Exporting..." : "Download"}
              </button>
            </div>
          </div>
        )}
        {exportError && (
          <div className="px-3 py-1.5 bg-status-error/10 border border-status-error/30 rounded text-xs text-status-error">
            {exportError}
          </div>
        )}

        {/* Timeline bar */}
        <TimelineBar
          segments={segments}
          heatmapBuckets={heatmapBuckets}
          activeSegmentId={activeSegment?.id ?? null}
          currentTime={currentTime}
          zoomLevel={zoomLevel}
          zoomCenter={zoomCenter}
          onSeek={handleTimelineSeek}
          onZoomChange={handleZoomChange}
          onZoomCenterChange={setZoomCenter}
          date={selectedDate}
        />
      </div>
    </div>
  );
}
