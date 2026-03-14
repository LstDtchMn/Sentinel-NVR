/**
 * EventCard — displays a single detection event in a card grid (Phase 5, R3).
 * Shows thumbnail, timestamp, event type, label, confidence, and a delete button.
 */
import { useState } from "react";
import { Link } from "react-router-dom";
import { Trash2, ImageOff, ShieldAlert, Camera, User, Volume2 } from "lucide-react";
import { api, EventRecord } from "../../api/client";

interface Props {
  event: EventRecord;
  onDelete: (id: number) => void;
}

function confidenceColor(c: number): string {
  if (c >= 0.8) return "bg-green-500/20 text-green-400";
  if (c >= 0.5) return "bg-yellow-500/20 text-yellow-400";
  return "bg-red-500/20 text-red-400";
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export default function EventCard({ event, onDelete }: Props) {
  const [imgError, setImgError] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const hasThumbnail = event.thumbnail !== "" && !imgError;

  async function handleDelete() {
    if (deleting) return;
    setDeleting(true);
    try {
      await api.deleteEvent(event.id);
      onDelete(event.id);
    } catch {
      // TODO(review): L3 — show error to user on delete failure
      setDeleting(false);
    }
  }

  return (
    <Link
      to={`/events/${event.id}`}
      className="group relative bg-surface-raised border border-border rounded-lg overflow-hidden flex flex-col
                 hover:border-sentinel-500/50 transition-colors"
    >
      {/* Thumbnail */}
      <div className="aspect-video bg-surface-overlay flex items-center justify-center relative">
        {hasThumbnail ? (
          <img
            src={api.eventThumbnailURL(event.id)}
            alt={`${event.label} detection`}
            className="w-full h-full object-cover"
            onError={() => setImgError(true)}
          />
        ) : (
          <ImageOff className="w-8 h-8 text-faint" />
        )}

        {/* Delete overlay on hover */}
        <button
          onClick={(e) => { e.preventDefault(); e.stopPropagation(); handleDelete(); }}
          disabled={deleting}
          className="absolute top-2 right-2 p-1.5 rounded-md bg-surface-base/80 text-faint
                     opacity-0 group-hover:opacity-100 transition-opacity
                     hover:text-red-400 hover:bg-surface-base disabled:opacity-30"
          title="Delete event"
        >
          <Trash2 className="w-4 h-4" />
        </button>
      </div>

      {/* Details */}
      <div className="p-3 flex flex-col gap-1.5 flex-1">
        {/* Type + Label row */}
        <div className="flex items-center gap-2 flex-wrap">
          {event.type === "detection" ? (
            <ShieldAlert className="w-4 h-4 text-sentinel-500 shrink-0" />
          ) : event.type === "face_match" ? (
            <User className="w-4 h-4 text-purple-400 shrink-0" />
          ) : event.type === "audio_detection" ? (
            <Volume2 className="w-4 h-4 text-amber-400 shrink-0" />
          ) : (
            <Camera className="w-4 h-4 text-muted shrink-0" />
          )}
          <span className="text-sm font-medium truncate flex-1">
            {event.label || event.type}
          </span>
          {(event.type === "detection" || event.type === "face_match" || event.type === "audio_detection") && event.confidence > 0 && (
            <span
              className={`text-xs px-1.5 py-0.5 rounded font-mono ${confidenceColor(event.confidence)}`}
            >
              {(event.confidence * 100).toFixed(0)}%
            </span>
          )}
        </div>

        {/* Timestamp */}
        <p className="text-xs text-faint">{formatTime(event.start_time)}</p>
      </div>
    </Link>
  );
}
