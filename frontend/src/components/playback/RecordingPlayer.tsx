/**
 * RecordingPlayer — HTML5 video player for recording segment playback (R6).
 * Uses standard <video src> with Range header support from the play endpoint.
 * Reports currentTime via requestAnimationFrame for smooth playhead tracking.
 */
import { useRef, useEffect, useCallback, useState } from "react";
import { Film } from "lucide-react";
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
}

export default function RecordingPlayer({
  segment,
  segments,
  playbackRate,
  onTimeUpdate,
  onEnded,
  onSegmentChange,
  onPlaybackRateChange,
  className = "",
}: RecordingPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const rafRef = useRef<number | null>(null);
  // Ref mirrors playbackRate prop so the segment-load effect can read the
  // current speed without it being a dependency (which would reload the video).
  const playbackRateRef = useRef(playbackRate);
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
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !segment) return;

    video.src = api.recordingPlayURL(segment.id);
    // Set rate immediately (may be reset by browser on src change) and again
    // on canplay once the media pipeline has initialised.
    video.playbackRate = playbackRateRef.current;
    const onCanPlay = () => { video.playbackRate = playbackRateRef.current; };
    video.addEventListener("canplay", onCanPlay, { once: true });
    video.play().catch(() => {});
    return () => video.removeEventListener("canplay", onCanPlay);
  }, [segment?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // Keep the ref in sync so the segment-load effect always reads the latest speed.
  useEffect(() => {
    playbackRateRef.current = playbackRate;
    if (videoRef.current) videoRef.current.playbackRate = playbackRate;
  }, [playbackRate]);

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
        onTimeUpdate(v.currentTime);
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
  }, [segment?.id, onTimeUpdate]);

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
            <span className="text-muted text-sm">Select a camera and date to start playback</span>
          </div>
        )}
      </div>

      {/* Controls bar */}
      {segment && (
        <div className="bg-surface-raised border-t border-border px-4 py-2">
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
      )}
    </div>
  );
}
