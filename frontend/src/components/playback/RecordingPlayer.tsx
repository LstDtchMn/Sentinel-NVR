/**
 * RecordingPlayer — HTML5 video player for recording segment playback (R6).
 * Uses standard <video src> with Range header support from the play endpoint.
 * Reports currentTime via requestAnimationFrame for smooth playhead tracking.
 *
 * Imperative handle (via forwardRef):
 *   seekTo(seconds)      — seek within the current segment immediately
 *   setInitialSeek(s)    — defer a seek applied when the next segment loads
 * Callers (Playback.tsx) call setInitialSeek before changing the active segment
 * so that timeline click-to-seek works across segment boundaries (R6).
 */
import { useRef, useEffect, useCallback, useState, forwardRef, useImperativeHandle } from "react";
import { Film, Download } from "lucide-react";
import { api, type TimelineSegment } from "../../api/client";
import { formatWallClock } from "../../utils/time";
import PlaybackControls from "./PlaybackControls";

interface RecordingPlayerProps {
  segment: TimelineSegment | null;
  segments: TimelineSegment[];     // full segment list for prev/next navigation
  playbackRate: number;
  onTimeUpdate: (videoCurrentTime: number) => void;
  onEnded: () => void;
  onSegmentChange: (segment: TimelineSegment) => void;
  onPlaybackRateChange: (rate: number) => void;
  className?: string;
  /** Override the empty-state message shown when no segment is loaded. */
  emptyMessage?: string;
}

/** Imperative API exposed via ref so Playback.tsx can seek on timeline click (R6). */
export interface RecordingPlayerHandle {
  /** Seek within the currently-loaded segment immediately. */
  seekTo(seconds: number): void;
  /** Store an offset applied when the next segment finishes loading (cross-segment seek). */
  setInitialSeek(seconds: number): void;
}

const RecordingPlayer = forwardRef<RecordingPlayerHandle, RecordingPlayerProps>(
  function RecordingPlayer(
    {
      segment,
      segments,
      playbackRate,
      onTimeUpdate,
      onEnded,
      onSegmentChange,
      onPlaybackRateChange,
      className = "",
      emptyMessage,
    }: RecordingPlayerProps,
    ref,
  ) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const rafRef = useRef<number | null>(null);
  // Ref mirrors playbackRate prop so the segment-load effect can read the
  // current speed without it being a dependency (which would reload the video).
  const playbackRateRef = useRef(playbackRate);
  // Pending seek offset applied on next canplay event (cross-segment seek, R6).
  const pendingSeekRef = useRef<number | null>(null);
  // Ref mirrors onTimeUpdate so the rAF loop can call it without the callback
  // identity being a dep that restarts the loop on every parent re-render.
  const onTimeUpdateRef = useRef(onTimeUpdate);
  const [playing, setPlaying] = useState(false);
  const [videoTime, setVideoTime] = useState(0);
  const [videoDuration, setVideoDuration] = useState(0);

  // Current segment index for prev/next
  const currentIndex = segment ? segments.findIndex((s) => s.id === segment.id) : -1;
  const hasPrev = currentIndex > 0;
  const hasNext = currentIndex >= 0 && currentIndex < segments.length - 1;

  // Load segment into video element.
  // playbackRate is intentionally NOT in the dep array — the dedicated effect below
  // handles rate changes without reloading the video. The canplay event ensures the
  // rate sticks even if the browser resets it during media loading.
  // pendingSeekRef (set by setInitialSeek) is consumed here for cross-segment seeks (R6).
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !segment) return;

    // Consume pending seek before src changes so it applies to the incoming segment.
    const seekOffset = pendingSeekRef.current;
    pendingSeekRef.current = null;

    video.src = api.recordingPlayURL(segment.id);
    // Set rate immediately (may be reset by browser on src change) and again
    // on canplay once the media pipeline has initialised.
    video.playbackRate = playbackRateRef.current;
    const onCanPlay = () => {
      video.playbackRate = playbackRateRef.current;
      if (seekOffset !== null) {
        video.currentTime = Math.max(0, Math.min(seekOffset, video.duration || 0));
        // Ensure playback starts after seeking (e.g. URL deep-link from EventDetail)
        video.play().catch(() => {});
      }
    };
    video.addEventListener("canplay", onCanPlay, { once: true });
    // canplay may already have fired if the browser returned a cached response
    // before addEventListener could register.  Apply the seek immediately in that case.
    if (video.readyState >= 3) {
      onCanPlay();
      video.removeEventListener("canplay", onCanPlay);
    }
    video.play().catch(() => {});
    return () => video.removeEventListener("canplay", onCanPlay);
  }, [segment?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // Keep rate ref in sync so the segment-load effect always reads the latest speed.
  useEffect(() => {
    playbackRateRef.current = playbackRate;
    if (videoRef.current) videoRef.current.playbackRate = playbackRate;
  }, [playbackRate]);

  // Keep onTimeUpdate ref in sync without it being a rAF-loop dep.
  useEffect(() => { onTimeUpdateRef.current = onTimeUpdate; }, [onTimeUpdate]);

  // rAF loop for smooth playhead tracking — only runs while playing.
  // Starts on "play" event, stops on "pause"/"ended" to avoid burning
  // ~60 callbacks/sec when the video is idle.
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !segment) return;

    const startLoop = () => {
      const tick = () => {
        // Re-read videoRef each tick in case the element is swapped
        const v = videoRef.current;
        if (!v || v.paused || v.ended) {
          rafRef.current = null;
          return;
        }
        setVideoTime(v.currentTime);
        onTimeUpdateRef.current(v.currentTime);
        rafRef.current = requestAnimationFrame(tick);
      };
      if (rafRef.current === null) {
        rafRef.current = requestAnimationFrame(tick);
      }
    };

    const stopLoop = () => {
      if (rafRef.current !== null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };

    // Start immediately if already playing (e.g., autoplay on segment change)
    if (!video.paused && !video.ended) startLoop();

    video.addEventListener("play", startLoop);
    video.addEventListener("pause", stopLoop);
    video.addEventListener("ended", stopLoop);

    return () => {
      stopLoop();
      video.removeEventListener("play", startLoop);
      video.removeEventListener("pause", stopLoop);
      video.removeEventListener("ended", stopLoop);
    };
  }, [segment?.id]); // onTimeUpdate is accessed via ref — no restart on identity change

  // Track play/pause state
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const onPlay = () => setPlaying(true);
    const onPause = () => setPlaying(false);
    const onDurationChange = () => setVideoDuration(video.duration || 0);
    const onTimeUpdateEvt = () => setVideoTime(video.currentTime);

    video.addEventListener("play", onPlay);
    video.addEventListener("pause", onPause);
    video.addEventListener("durationchange", onDurationChange);
    video.addEventListener("timeupdate", onTimeUpdateEvt);

    return () => {
      video.removeEventListener("play", onPlay);
      video.removeEventListener("pause", onPause);
      video.removeEventListener("durationchange", onDurationChange);
      video.removeEventListener("timeupdate", onTimeUpdateEvt);
    };
  }, []);

  const handlePlayPause = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused) {
      video.play().catch(() => {});
    } else {
      video.pause();
    }
  }, []);

  const handleSkipPrev = useCallback(() => {
    if (hasPrev) onSegmentChange(segments[currentIndex - 1]);
  }, [hasPrev, segments, currentIndex, onSegmentChange]);

  const handleSkipNext = useCallback(() => {
    if (hasNext) onSegmentChange(segments[currentIndex + 1]);
  }, [hasNext, segments, currentIndex, onSegmentChange]);

  // Seek within current segment
  const seekTo = useCallback((seconds: number) => {
    const video = videoRef.current;
    if (!video) return;
    video.currentTime = Math.max(0, Math.min(seconds, video.duration || 0));
  }, []);

  // Expose seekTo and setInitialSeek to parent via ref (R6 timeline click-to-seek).
  useImperativeHandle(ref, () => ({
    seekTo,
    setInitialSeek(seconds: number) {
      pendingSeekRef.current = seconds;
    },
  }), [seekTo]);

  // Format time displays
  const wallClock = segment ? formatWallClock(segment.start_time, videoTime) : "--:--:--";
  const formatMmSs = (s: number) => {
    const m = Math.floor(s / 60);
    const sec = Math.floor(s % 60);
    return `${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  };
  const segmentTimeDisplay = segment
    ? `${formatMmSs(videoTime)} / ${formatMmSs(videoDuration)}`
    : "-- / --";

  return (
    <div className={`relative bg-surface-base rounded-lg overflow-hidden flex flex-col ${className}`}>
      {/* Video element */}
      <div className="flex-1 min-h-0 relative">
        <video
          ref={videoRef}
          className={`w-full h-full object-contain ${segment ? "block" : "hidden"}`}
          playsInline
          onEnded={onEnded}
        />

        {/* Wall-clock time overlay */}
        {segment && (
          <div className="absolute top-3 left-3 bg-black/70 px-2 py-1 rounded text-xs text-white font-mono">
            {wallClock}
          </div>
        )}

        {/* Empty state */}
        {!segment && (
          <div className="absolute inset-0 flex flex-col items-center justify-center">
            <Film className="w-12 h-12 text-faint mb-3" />
            <span className="text-muted text-sm">{emptyMessage ?? "Select a camera and date to start playback"}</span>
          </div>
        )}
      </div>

      {/* Controls bar */}
      {segment && (
        <div className="bg-surface-raised border-t border-border px-4 py-2 flex items-center gap-2">
          <div className="flex-1 min-w-0">
            <PlaybackControls
              playing={playing}
              playbackRate={playbackRate}
              currentTimeDisplay={wallClock}
              segmentTimeDisplay={segmentTimeDisplay}
              onPlayPause={handlePlayPause}
              onSkipPrev={handleSkipPrev}
              onSkipNext={handleSkipNext}
              onSpeedChange={onPlaybackRateChange}
              hasPrev={hasPrev}
              hasNext={hasNext}
            />
          </div>
          <a
            href={api.downloadRecordingURL(segment.id)}
            download
            className="bg-surface-overlay border border-border hover:bg-surface-raised px-3 py-1.5 rounded-lg text-sm flex items-center gap-2 shrink-0 transition-colors"
            title="Download segment"
          >
            <Download className="w-4 h-4" />
            Download
          </a>
        </div>
      )}
    </div>
  );
  }, // end forwardRef inner function
); // end forwardRef

export default RecordingPlayer;
