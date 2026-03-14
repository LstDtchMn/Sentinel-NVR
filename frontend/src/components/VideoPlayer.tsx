/**
 * VideoPlayer — MSE live stream player that connects to go2rtc via the backend
 * WebSocket proxy (CG3, R7). Uses the browser's native MediaSource API to render
 * fragmented MP4 frames received over WebSocket.
 *
 * Protocol (go2rtc MSE over WebSocket):
 * 1. Client sends: {"type":"mse","value":"<comma-separated codec list>"}
 * 2. Server replies: {"type":"mse","value":"<negotiated MIME type>"}
 * 3. Server streams: binary fMP4 frames (ftyp+moov init, then moof+mdat segments)
 */
import { useRef, useEffect, useCallback, useState } from "react";
import { AlertCircle, Loader2, VideoOff } from "lucide-react";

/** Codecs to probe for MSE support — covers H.264, H.265, AAC, Opus, FLAC. */
const CODEC_CANDIDATES = [
  "avc1.640029", // H.264 High 4.1
  "avc1.64002A", // H.264 High 4.2
  "avc1.640033", // H.264 High 5.1
  "hvc1.1.6.L153.B0", // H.265 Main
  "mp4a.40.2", // AAC-LC
  "mp4a.40.5", // AAC-HE
  "flac",
  "opus",
];

type PlayerState = "idle" | "connecting" | "playing" | "error" | "reconnecting";

interface VideoPlayerProps {
  cameraName: string;
  active?: boolean; // false = disconnect WS to save bandwidth (Focus Mode)
  className?: string;
  onError?: (err: string) => void;
}

export default function VideoPlayer({
  cameraName,
  active = true,
  className = "",
  onError,
}: VideoPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const msRef = useRef<MediaSource | null>(null);
  const sbRef = useRef<SourceBuffer | null>(null);
  // Saved updateend handler so cleanup() can call removeEventListener with the
  // exact same function reference — preventing listener accumulation across reconnects.
  const updateEndHandlerRef = useRef<(() => void) | null>(null);
  const bufferQueue = useRef<ArrayBuffer[]>([]);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectDelay = useRef(3000);
  const objectURL = useRef<string | null>(null);
  const unmountedRef = useRef(false);
  const liveEdgeTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  // Stabilise onError: store in a ref so connect() never has it as a dep.
  // This prevents reconnect loops when a parent renders a new arrow function
  // on each render (e.g. onError={() => setError(...)}).
  const onErrorRef = useRef(onError);
  useEffect(() => {
    onErrorRef.current = onError;
  }, [onError]);

  const [state, setState] = useState<PlayerState>("idle");
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  /** Build the WebSocket URL for this camera's MSE stream. */
  const getWSURL = useCallback(() => {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    return `${proto}//${location.host}/api/v1/streams/${encodeURIComponent(cameraName)}/ws`;
  }, [cameraName]);

  /** Detect supported codecs via MediaSource.isTypeSupported(). */
  // TODO(review): L4 — guard MediaSource availability for iOS Safari/WebKit
  const getSupportedCodecs = useCallback(() => {
    return CODEC_CANDIDATES.filter((codec) =>
      MediaSource.isTypeSupported(`video/mp4; codecs="${codec}"`)
    );
  }, []);

  /** Flush queued buffers into the SourceBuffer when it's ready. */
  const flushQueue = useCallback(() => {
    const sb = sbRef.current;
    if (!sb || sb.updating || bufferQueue.current.length === 0) return;

    const chunk = bufferQueue.current.shift();
    if (chunk) {
      try {
        sb.appendBuffer(chunk);
      } catch (e) {
        // QuotaExceededError — buffer full, try removing old data then retry.
        // Guard: only remove if the buffered range is >10s, otherwise remove()
        // would produce an invalid range (end < start) and throw a RangeError.
        if (e instanceof DOMException && e.name === "QuotaExceededError") {
          try {
            const buffered = sb.buffered;
            if (buffered.length > 0) {
              const start = buffered.start(0);
              const end = buffered.end(0);
              if (end - start > 10) {
                sb.remove(start, end - 10);
              }
            }
          } catch {
            // Ignore — will retry on next updateend
          }
          // Re-queue the chunk for retry
          bufferQueue.current.unshift(chunk);
        }
      }
    }
  }, []);

  /** Clean up all resources. */
  const cleanup = useCallback(() => {
    if (reconnectTimer.current) {
      clearTimeout(reconnectTimer.current);
      reconnectTimer.current = null;
    }
    if (liveEdgeTimer.current) {
      clearInterval(liveEdgeTimer.current);
      liveEdgeTimer.current = null;
    }

    if (wsRef.current) {
      wsRef.current.onopen = null;
      wsRef.current.onclose = null;
      wsRef.current.onerror = null;
      wsRef.current.onmessage = null;
      if (
        wsRef.current.readyState === WebSocket.OPEN ||
        wsRef.current.readyState === WebSocket.CONNECTING
      ) {
        wsRef.current.close(1000, "cleanup");
      }
      wsRef.current = null;
    }

    // Remove the updateend listener before clearing sbRef so we use the exact
    // function reference that was registered — prevents listener accumulation
    // across reconnects (each reconnect creates a new SourceBuffer).
    if (sbRef.current && updateEndHandlerRef.current) {
      sbRef.current.removeEventListener("updateend", updateEndHandlerRef.current);
      updateEndHandlerRef.current = null;
    }
    sbRef.current = null;
    bufferQueue.current = [];

    if (msRef.current) {
      if (msRef.current.readyState === "open") {
        try {
          msRef.current.endOfStream();
        } catch {
          // Ignore — already ended or detached
        }
      }
      msRef.current = null;
    }

    // Clear video.src BEFORE revoking the object URL — revoking while the
    // video element still references the blob URL can cause browser console
    // errors and stalled loads on Firefox and older Safari.
    if (videoRef.current) {
      videoRef.current.src = "";
      videoRef.current.load();
    }

    if (objectURL.current) {
      URL.revokeObjectURL(objectURL.current);
      objectURL.current = null;
    }
  }, []);

  /** Connect to the go2rtc MSE WebSocket and start playback.
   *
   * Note: reconnectDelay is NOT reset here — the caller is responsible.
   * This lets exponential backoff accumulate across reconnect attempts while
   * a fresh connect (initial mount, active toggle, Retry button) resets it. */
  const connect = useCallback(() => {
    if (unmountedRef.current) return;

    cleanup();
    setState("connecting");
    setErrorMsg(null);

    const ws = new WebSocket(getWSURL());
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    const ms = new MediaSource();
    msRef.current = ms;
    objectURL.current = URL.createObjectURL(ms);

    if (videoRef.current) {
      videoRef.current.src = objectURL.current;
    }

    // Coordinate codec negotiation between MediaSource.sourceopen and WebSocket.onopen.
    // Either can fire first depending on browser and network timing — in most browsers
    // sourceopen fires after the blob URL is assigned to video.src, which happens before
    // the WS handshake completes. Whichever event fires second actually sends the message.
    let sourceReady = false;
    let wsReady = false;

    const sendCodecNegotiation = () => {
      if (!sourceReady || !wsReady) return; // wait until both are ready
      // Guard against stale callbacks from a previous connect() cycle (e.g. StrictMode
      // double-invoke where the first WS was cleaned up but MS fires sourceopen late).
      if (unmountedRef.current || ws !== wsRef.current) return;
      if (!msRef.current || msRef.current.readyState !== "open") return;

      const codecs = getSupportedCodecs();
      if (codecs.length === 0) {
        setErrorMsg("No supported codecs");
        setState("error");
        onErrorRef.current?.("No supported codecs for MSE playback");
        return;
      }
      ws.send(JSON.stringify({ type: "mse", value: codecs.join(",") }));
    };

    ms.addEventListener("sourceopen", () => {
      if (unmountedRef.current || ws !== wsRef.current) return;
      sourceReady = true;
      sendCodecNegotiation();
    });

    ws.onopen = () => {
      if (unmountedRef.current || ws !== wsRef.current) return;
      wsReady = true;
      sendCodecNegotiation();
    };

    ws.onmessage = (event: MessageEvent) => {
      // Guard against messages from a stale WS after reconnect (e.g. StrictMode).
      if (unmountedRef.current || ws !== wsRef.current) return;

      if (typeof event.data === "string") {
        // JSON message from go2rtc
        try {
          const msg = JSON.parse(event.data);

          if (msg.type === "mse") {
            // Server negotiated MIME type — create SourceBuffer
            const mimeType = msg.value as string;
            if (!msRef.current || msRef.current.readyState !== "open") return;

            try {
              const sb = msRef.current.addSourceBuffer(mimeType);
              sb.mode = "segments";
              sbRef.current = sb;

              // Save the listener reference so cleanup() can remove it precisely.
              // Without removal, every reconnect adds a new listener to its own
              // SourceBuffer; stale updateend events from old SBs would call
              // flushQueue() against the current session's sbRef.
              const onUpdateEnd = () => {
                flushQueue();

                // Trim buffer if it grows beyond 30s to prevent memory bloat.
                // Wrapped in try/catch: sb.buffered may throw InvalidStateError
                // if the SourceBuffer is detached after cleanup() runs while a
                // prior appendBuffer is still in flight.
                try {
                  if (sb.buffered.length > 0) {
                    const end = sb.buffered.end(sb.buffered.length - 1);
                    const start = sb.buffered.start(0);
                    if (end - start > 30 && !sb.updating) {
                      try {
                        sb.remove(start, end - 15);
                      } catch {
                        // Ignore — concurrent update
                      }
                    }
                  }
                } catch {
                  // SourceBuffer detached — no-op
                }
              };
              updateEndHandlerRef.current = onUpdateEnd;
              sb.addEventListener("updateend", onUpdateEnd);

              setState("playing");

              // Periodically sync to live edge to minimise latency.
              // Use sbRef.current (not closed-over sb) so that if cleanup()
              // detaches the SourceBuffer, the interval reads null and returns
              // rather than calling sb.buffered on a detached buffer (InvalidStateError).
              // Seek when >3s behind; land 1.5s behind live edge (not 0.5s) so
              // normal network jitter doesn't immediately cause a stall.
              liveEdgeTimer.current = setInterval(() => {
                const video = videoRef.current;
                const currentSB = sbRef.current;
                if (!video || !currentSB) return;
                try {
                  if (!currentSB.buffered.length) return;
                  const liveEdge = currentSB.buffered.end(currentSB.buffered.length - 1);
                  if (liveEdge - video.currentTime > 3) {
                    video.currentTime = liveEdge - 1.5;
                  }
                } catch {
                  // SourceBuffer detached — interval will be cleared on next cleanup
                }
              }, 5000);
            } catch (e) {
              const errStr = `Unsupported codec: ${mimeType}`;
              setErrorMsg(errStr);
              setState("error");
              onErrorRef.current?.(errStr);
            }
          } else if (msg.type === "error") {
            setErrorMsg(msg.value || "Stream error");
            setState("error");
            onErrorRef.current?.(msg.value || "Stream error");
          }
        } catch {
          // Ignore malformed JSON
        }
      } else if (event.data instanceof ArrayBuffer) {
        // Binary fMP4 frame — queue for SourceBuffer
        bufferQueue.current.push(event.data);
        flushQueue();
      }
    };

    ws.onerror = () => {
      // onerror fires before onclose — actual handling happens in onclose
    };

    ws.onclose = (event: CloseEvent) => {
      // Guard against stale onclose callbacks from a previous connect() cycle.
      // In React StrictMode the effect fires twice (mount → cleanup → remount);
      // the first WS is closed with code 1000 by cleanup(), but the close event
      // fires asynchronously. By the time it arrives, wsRef.current points to
      // the new WS — the stale guard prevents a spurious reconnect.
      if (unmountedRef.current || ws !== wsRef.current) return;

      // Normal closure (e.g. Focus Mode toggle, cleanup) — don't reconnect
      if (event.code === 1000) {
        setState("idle");
        return;
      }

      // Abnormal closure — attempt reconnect with exponential backoff.
      // Advance the delay BEFORE scheduling; connect() no longer resets it,
      // so the delay accumulates correctly across successive reconnects.
      setState("reconnecting");
      setErrorMsg("Connection lost, reconnecting...");

      const delay = reconnectDelay.current;
      reconnectDelay.current = Math.min(delay * 1.5, 15000);

      reconnectTimer.current = setTimeout(() => {
        if (unmountedRef.current) return;
        connect();
      }, delay);
    };
  }, [getWSURL, getSupportedCodecs, cleanup, flushQueue]);
  // onError intentionally omitted — accessed via onErrorRef to avoid reconnect loops.

  // Connect/disconnect based on `active` prop.
  // Reset backoff unconditionally — connect is a new callback identity whenever
  // cameraName changes, so the effect re-runs on camera change too. Without the
  // unconditional reset, a camera switch after a reconnect episode would inherit
  // the elevated backoff delay from the previous camera's failure history.
  useEffect(() => {
    unmountedRef.current = false;
    reconnectDelay.current = 3000; // reset regardless of active/inactive transition

    if (active) {
      connect();
    } else {
      cleanup();
      setState("idle");
    }

    return () => {
      unmountedRef.current = true;
      cleanup();
    };
  }, [active, connect, cleanup]);

  return (
    <div
      className={`relative bg-surface-base overflow-hidden ${className}`}
    >
      {/* Video element — always rendered, hidden when not playing */}
      <video
        ref={videoRef}
        autoPlay
        muted
        playsInline
        className={`w-full h-full object-contain ${state === "playing" ? "block" : "hidden"}`}
      />

      {/* Loading state */}
      {state === "connecting" && (
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <Loader2 className="w-8 h-8 text-muted animate-spin mb-2" />
          <span className="text-sm text-muted">Connecting...</span>
        </div>
      )}

      {/* Reconnecting state */}
      {state === "reconnecting" && (
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <Loader2 className="w-8 h-8 text-status-warn animate-spin mb-2" />
          <span className="text-sm text-status-warn">Reconnecting...</span>
        </div>
      )}

      {/* Error state */}
      {state === "error" && (
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <AlertCircle className="w-8 h-8 text-status-error mb-2" />
          <span className="text-sm text-status-error">{errorMsg || "Stream error"}</span>
          <button
            onClick={() => {
              reconnectDelay.current = 3000;
              connect();
            }}
            className="mt-3 text-xs text-sentinel-400 hover:text-sentinel-300 font-medium"
          >
            Retry
          </button>
        </div>
      )}

      {/* Idle state (disconnected, e.g. Focus Mode inactive) */}
      {state === "idle" && !active && (
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <VideoOff className="w-8 h-8 text-faint mb-2" />
          <span className="text-sm text-faint">Paused</span>
        </div>
      )}
    </div>
  );
}
