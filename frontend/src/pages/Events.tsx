/**
 * Events — paginated list of detection and system events (Phase 5, R3).
 * Filter by camera, event type, and date. Load-more pagination (offset-based).
 * Phase 6 (CG8): SSE subscription prepends new events live without polling.
 */
import { useEffect, useState, useCallback, useRef } from "react";
import { Activity } from "lucide-react";
import { api, EventRecord, CameraDetail, EventListParams } from "../api/client";
import EventCard from "../components/events/EventCard";

const PAGE_SIZE = 50;

const EVENT_TYPES = [
  { value: "", label: "All types" },
  { value: "detection", label: "Detection" },
  { value: "face_match", label: "Face match" },
  { value: "audio_detection", label: "Audio detection" },
  { value: "camera.connected", label: "Camera connected" },
  { value: "camera.disconnected", label: "Camera disconnected" },
  { value: "recording.started", label: "Recording started" },
  { value: "recording.stopped", label: "Recording stopped" },
];

export default function Events() {
  const [events, setEvents] = useState<EventRecord[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [cameras, setCameras] = useState<CameraDetail[]>([]);
  const [filterCamera, setFilterCamera] = useState<number | "">("");
  const [filterType, setFilterType] = useState("");
  const [filterDate, setFilterDate] = useState("");

  // SSE connection state for the live indicator (Phase 6, CG8)
  const [liveConnected, setLiveConnected] = useState(false);

  // Track the current filter values so load-more uses the same params as the initial load.
  const offsetRef = useRef(0);
  const filtersRef = useRef<EventListParams>({});
  // Tracks the in-flight load-more request so it can be aborted on unmount or double-click.
  const loadMoreCtrlRef = useRef<AbortController | null>(null);

  // Load cameras for the filter dropdown.
  useEffect(() => {
    const ctrl = new AbortController();
    api
      .getCameras(ctrl.signal)
      .then(setCameras)
      .catch(() => {});
    return () => ctrl.abort();
  }, []);

  // SSE subscription — prepend matching events live without polling (Phase 6, CG8).
  // Started once on mount; EventSource auto-reconnects on transient errors.
  useEffect(() => {
    const source = api.subscribeEvents();
    source.onopen = () => setLiveConnected(true);
    source.onerror = () => {
      // Only clear the live indicator when the connection is permanently closed.
      // EventSource fires onerror on EVERY reconnect attempt (readyState = CONNECTING),
      // which would permanently kill the indicator even during brief network hiccups.
      if (source.readyState === EventSource.CLOSED) {
        setLiveConnected(false);
      }
    };
    source.onmessage = (e: MessageEvent) => {
      try {
        const event: EventRecord = JSON.parse(e.data as string);
        const f = filtersRef.current;
        // Drop events that don't match the active filter so the list stays consistent.
        if (f.camera_id !== undefined && event.camera_id !== f.camera_id) return;
        if (f.type && event.type !== f.type) return;
        if (f.date && event.start_time.slice(0, 10) !== f.date) return;
        // Prepend to list and bump total.
        setTotal((t) => t + 1);
        setEvents((prev) => [event, ...prev]);
      } catch {
        // Ignore unparseable SSE data (e.g. heartbeat comments forwarded by some proxies)
      }
    };
    return () => {
      source.close();
      setLiveConnected(false);
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps — filtersRef is a ref, not state

  // Abort any in-flight load-more request when the component unmounts.
  useEffect(() => () => loadMoreCtrlRef.current?.abort(), []);

  const loadEvents = useCallback(
    (params: EventListParams, append: boolean, signal: AbortSignal, onSuccess?: () => void) => {
      if (!append) setLoading(true);
      else setLoadingMore(true);

      api
        .getEvents(params, signal)
        .then(({ events: rows, total: t }) => {
          // Guard: if the signal was aborted (e.g. a newer filter-change request
          // already started), do not update state — the newer request owns loading.
          if (signal.aborted) return;
          if (append) {
            setEvents((prev) => [...prev, ...rows]);
          } else {
            setEvents(rows);
          }
          setTotal(t);
          setError(null);
          // Advance offset only after confirmed success — prevents skipping a page
          // when the request is aborted or fails before data is received.
          onSuccess?.();
          // Clear loading state only on success — avoid clearing when a subsequent
          // filter-change request has already set loading=true for its own fetch.
          setLoading(false);
          setLoadingMore(false);
        })
        .catch((err) => {
          if (err instanceof DOMException && err.name === "AbortError") return;
          // Do NOT setLoading(false) here for aborted requests — a new request may
          // already be in flight (filter change) and its setLoading(true) must persist.
          setError(err.message);
          setLoading(false);
          setLoadingMore(false);
        });
    },
    [],
  );

  // Reload from scratch whenever filters change.
  useEffect(() => {
    const ctrl = new AbortController();
    const params: EventListParams = {
      limit: PAGE_SIZE,
      offset: 0,
      ...(filterCamera !== "" && { camera_id: filterCamera }),
      ...(filterType && { type: filterType }),
      ...(filterDate && { date: filterDate }),
    };
    offsetRef.current = PAGE_SIZE;
    filtersRef.current = params;
    loadEvents(params, false, ctrl.signal);
    return () => ctrl.abort();
  }, [filterCamera, filterType, filterDate, loadEvents]);

  function handleLoadMore() {
    loadMoreCtrlRef.current?.abort(); // cancel any in-flight load-more before starting a new one
    const ctrl = new AbortController();
    loadMoreCtrlRef.current = ctrl;
    const params: EventListParams = {
      ...filtersRef.current,
      offset: offsetRef.current,
    };
    const nextOffset = offsetRef.current + PAGE_SIZE;
    loadEvents(params, true, ctrl.signal, () => {
      // Advance only on confirmed success so a failed/aborted page isn't skipped.
      offsetRef.current = nextOffset;
    });
  }

  function handleDelete(id: number) {
    setEvents((prev) => prev.filter((e) => e.id !== id));
    setTotal((t) => Math.max(0, t - 1));
  }

  const hasMore = events.length < total;

  return (
    <div className="p-8 flex flex-col gap-6 min-h-full">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Activity className="w-6 h-6 text-sentinel-500" />
        <h1 className="text-2xl font-semibold">Events</h1>
        {liveConnected && (
          <span className="flex items-center gap-1.5 text-xs text-green-400">
            <span className="w-1.5 h-1.5 rounded-full bg-green-400 animate-pulse" />
            Live
          </span>
        )}
        {total > 0 && (
          <span className="text-sm text-muted ml-auto">{total} total</span>
        )}
      </div>

      {/* Filter bar */}
      <div className="flex flex-wrap gap-3">
        {/* Camera */}
        <select
          value={filterCamera}
          onChange={(e) =>
            setFilterCamera(e.target.value === "" ? "" : Number(e.target.value))
          }
          className="bg-surface-raised border border-border rounded-lg px-3 py-2 text-sm
                     text-white focus:outline-none focus:ring-1 focus:ring-sentinel-500"
        >
          <option value="">All cameras</option>
          {cameras.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </select>

        {/* Event type */}
        <select
          value={filterType}
          onChange={(e) => setFilterType(e.target.value)}
          className="bg-surface-raised border border-border rounded-lg px-3 py-2 text-sm
                     text-white focus:outline-none focus:ring-1 focus:ring-sentinel-500"
        >
          {EVENT_TYPES.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>

        {/* Date */}
        <input
          type="date"
          value={filterDate}
          onChange={(e) => setFilterDate(e.target.value)}
          className="bg-surface-raised border border-border rounded-lg px-3 py-2 text-sm
                     text-white focus:outline-none focus:ring-1 focus:ring-sentinel-500
                     [color-scheme:dark]"
        />

        {(filterCamera !== "" || filterType || filterDate) && (
          <button
            onClick={() => {
              setFilterCamera("");
              setFilterType("");
              setFilterDate("");
            }}
            className="text-sm text-muted hover:text-white px-3 py-2 rounded-lg
                       hover:bg-surface-overlay transition-colors"
          >
            Clear filters
          </button>
        )}
      </div>

      {/* Content */}
      {loading ? (
        <div className="flex-1 flex items-center justify-center text-muted">
          Loading events…
        </div>
      ) : error ? (
        <div className="flex-1 flex items-center justify-center text-status-error">
          {error}
        </div>
      ) : events.length === 0 ? (
        <div className="flex-1 flex flex-col items-center justify-center gap-3 text-muted">
          <Activity className="w-12 h-12 opacity-20" />
          <p>No events found</p>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4">
            {events.map((event) => (
              <EventCard key={event.id} event={event} onDelete={handleDelete} />
            ))}
          </div>

          {hasMore && (
            <div className="flex justify-center pt-2">
              <button
                onClick={handleLoadMore}
                disabled={loadingMore}
                className="px-6 py-2 rounded-lg bg-surface-raised border border-border
                           text-sm text-muted hover:text-white hover:bg-surface-overlay
                           transition-colors disabled:opacity-50"
              >
                {loadingMore ? "Loading…" : `Load more (${total - events.length} remaining)`}
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
