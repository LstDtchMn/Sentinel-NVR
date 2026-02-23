/** PlaybackControls — play/pause, skip, speed controls for recording playback (R6). */
import { Play, Pause, SkipBack, SkipForward } from "lucide-react";

const SPEEDS = [0.5, 1, 2, 4];

interface PlaybackControlsProps {
  playing: boolean;
  playbackRate: number;
  currentTimeDisplay: string; // "HH:MM:SS" wall-clock time
  segmentTimeDisplay: string; // "MM:SS / MM:SS" position within segment
  onPlayPause: () => void;
  onSkipPrev: () => void;
  onSkipNext: () => void;
  onSpeedChange: (rate: number) => void;
  hasPrev: boolean;
  hasNext: boolean;
}

export default function PlaybackControls({
  playing,
  playbackRate,
  currentTimeDisplay,
  segmentTimeDisplay,
  onPlayPause,
  onSkipPrev,
  onSkipNext,
  onSpeedChange,
  hasPrev,
  hasNext,
}: PlaybackControlsProps) {
  return (
    <div className="flex items-center justify-between">
      {/* Left: transport controls */}
      <div className="flex items-center gap-2">
        <button
          onClick={onSkipPrev}
          disabled={!hasPrev}
          className="p-1.5 rounded hover:bg-white/10 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
          title="Previous segment"
        >
          <SkipBack className="w-4 h-4" />
        </button>
        <button
          onClick={onPlayPause}
          className="p-2 rounded-full bg-white/10 hover:bg-white/20 transition-colors"
          title={playing ? "Pause" : "Play"}
        >
          {playing ? <Pause className="w-5 h-5" /> : <Play className="w-5 h-5 ml-0.5" />}
        </button>
        <button
          onClick={onSkipNext}
          disabled={!hasNext}
          className="p-1.5 rounded hover:bg-white/10 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
          title="Next segment"
        >
          <SkipForward className="w-4 h-4" />
        </button>
      </div>

      {/* Center: time display */}
      <div className="flex items-center gap-3">
        <span className="text-sm font-mono text-white">{currentTimeDisplay}</span>
        <span className="text-xs text-muted">{segmentTimeDisplay}</span>
      </div>

      {/* Right: speed selector */}
      <div className="flex items-center gap-1">
        {SPEEDS.map((s) => (
          <button
            key={s}
            onClick={() => onSpeedChange(s)}
            className={`px-2 py-0.5 rounded text-xs font-medium transition-colors ${
              playbackRate === s
                ? "bg-sentinel-500 text-white"
                : "text-muted hover:text-white"
            }`}
          >
            {s}x
          </button>
        ))}
      </div>
    </div>
  );
}
