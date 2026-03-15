/**
 * EventDetail — full-page view of a single detection event (Phase 5, R3).
 * Displays full-size thumbnail, event metadata, and a link to playback if a clip exists.
 */
import { useEffect, useState } from "react";
import { useParams, Link, useNavigate } from "react-router-dom";
import {
  ArrowLeft,
  ShieldAlert,
  Camera,
  User,
  Volume2,
  Film,
  ImageOff,
} from "lucide-react";
import { api, type EventRecord, type CameraDetail } from "../api/client";
import { eventTypeBadge, confidenceColor } from "../utils/events";
import { formatEventTimeLong } from "../utils/time";

function EventTypeIcon({ type }: { type: string }) {
  if (type === "detection") return <ShieldAlert className="w-5 h-5 text-sentinel-500" />;
  if (type === "face_match") return <User className="w-5 h-5 text-purple-400" />;
  if (type === "audio_detection") return <Volume2 className="w-5 h-5 text-amber-400" />;
  return <Camera className="w-5 h-5 text-muted" />;
}

function eventTypeLabel(type: string): string {
  switch (type) {
    case "detection": return "Detection";
    case "face_match": return "Face match";
    case "audio_detection": return "Audio detection";
    case "camera.connected": return "Camera connected";
    case "camera.disconnected": return "Camera disconnected";
    case "recording.started": return "Recording started";
    case "recording.stopped": return "Recording stopped";
    default: return type;
  }
}

export default function EventDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [event, setEvent] = useState<EventRecord | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [imgError, setImgError] = useState(false);
  const [cameras, setCameras] = useState<CameraDetail[]>([]);

  // Fetch cameras on mount to resolve camera_id → camera name for "Jump to Recording"
  useEffect(() => {
    const ctrl = new AbortController();
    api
      .getCameras(ctrl.signal)
      .then((data) => {
        if (ctrl.signal.aborted) return;
        setCameras(data);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        // Non-critical — camera lookup just won't work
      });
    return () => ctrl.abort();
  }, []);

  useEffect(() => {
    if (!id) return;
    const ctrl = new AbortController();
    setLoading(true);
    setError(null);
    api
      .getEvent(Number(id), ctrl.signal)
      .then((ev) => {
        if (ctrl.signal.aborted) return;
        setEvent(ev);
        setLoading(false);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setError(err.message);
        setLoading(false);
      });
    return () => ctrl.abort();
  }, [id]);

  if (loading) {
    return (
      <div className="p-8 flex items-center justify-center min-h-full text-muted">
        Loading event...
      </div>
    );
  }

  if (error || !event) {
    return (
      <div className="p-8 flex flex-col items-center justify-center min-h-full gap-4">
        <p className="text-status-error">{error || "Event not found"}</p>
        <button
          onClick={() => navigate("/events")}
          className="flex items-center gap-2 text-sm text-muted hover:text-white transition-colors"
        >
          <ArrowLeft className="w-4 h-4" />
          Back to events
        </button>
      </div>
    );
  }

  const hasThumbnail = event.thumbnail !== "" && !imgError;
  const showConfidence =
    (event.type === "detection" || event.type === "face_match" || event.type === "audio_detection") &&
    event.confidence > 0;

  return (
    <div className="p-8 flex flex-col gap-6 max-w-4xl mx-auto">
      {/* Back button */}
      <Link
        to="/events"
        className="flex items-center gap-2 text-sm text-muted hover:text-white transition-colors w-fit"
      >
        <ArrowLeft className="w-4 h-4" />
        Back to events
      </Link>

      {/* Thumbnail */}
      <div className="bg-surface-raised border border-border rounded-lg overflow-hidden">
        {hasThumbnail ? (
          <img
            src={api.eventThumbnailURL(event.id)}
            alt={`${event.label} detection`}
            className="w-full max-h-[60vh] object-contain bg-black"
            onError={() => setImgError(true)}
          />
        ) : (
          <div className="h-48 flex items-center justify-center bg-surface-overlay">
            <ImageOff className="w-16 h-16 text-faint" />
          </div>
        )}
      </div>

      {/* Event details */}
      <div className="bg-surface-raised border border-border rounded-lg p-6 flex flex-col gap-4">
        {/* Type + Label */}
        <div className="flex items-center gap-3">
          <EventTypeIcon type={event.type} />
          <h1 className="text-xl font-semibold">
            {event.label || eventTypeLabel(event.type)}
          </h1>
          {showConfidence && (
            <span
              className={`text-sm px-2 py-0.5 rounded font-mono ${confidenceColor(event.confidence)}`}
            >
              {(event.confidence * 100).toFixed(1)}%
            </span>
          )}
        </div>

        {/* Metadata rows */}
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          <dt className="text-muted">Event type</dt>
          <dd>
            {(() => {
              const badge = eventTypeBadge(event.type);
              return (
                <span className={`text-xs font-medium px-1.5 py-0.5 rounded ${badge.bg} ${badge.text}`}>
                  {eventTypeLabel(event.type)}
                </span>
              );
            })()}
          </dd>

          {event.label && event.label !== event.type && (
            <>
              <dt className="text-muted">Label</dt>
              <dd>{event.label}</dd>
            </>
          )}

          <dt className="text-muted">Timestamp</dt>
          <dd>{formatEventTimeLong(event.start_time)}</dd>

          {event.camera_id !== null && (() => {
            const cameraName = cameras.find((c) => c.id === event.camera_id)?.name;
            return (
              <>
                <dt className="text-muted">Camera</dt>
                <dd>{cameraName || `Camera #${event.camera_id}`}</dd>
              </>
            );
          })()}

          <dt className="text-muted">Has clip</dt>
          <dd>
            {event.has_clip ? (
              <span className="text-green-400">Yes</span>
            ) : (
              <span className="text-faint">No</span>
            )}
          </dd>
        </dl>

        {/* Jump to Recording link */}
        {event.has_clip && (() => {
          const cam = event.camera_id !== null
            ? cameras.find((c) => c.id === event.camera_id)
            : undefined;
          const cameraName = cam?.name;
          const startDate = new Date(event.start_time);
          const dateStr = `${startDate.getFullYear()}-${String(startDate.getMonth() + 1).padStart(2, "0")}-${String(startDate.getDate()).padStart(2, "0")}`;
          const secondsSinceMidnight = startDate.getHours() * 3600 + startDate.getMinutes() * 60 + startDate.getSeconds();
          const params = new URLSearchParams();
          if (cameraName) params.set("camera", cameraName);
          params.set("date", dateStr);
          params.set("time", String(secondsSinceMidnight));
          return (
            <Link
              to={`/playback?${params.toString()}`}
              className="flex items-center gap-2 text-sm text-sentinel-400 hover:text-sentinel-300
                         transition-colors w-fit mt-2"
            >
              <Film className="w-4 h-4" />
              Jump to Recording
            </Link>
          );
        })()}
      </div>
    </div>
  );
}
