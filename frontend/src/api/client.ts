/**
 * client.ts — typed REST API client for the Sentinel NVR backend (CG7).
 * Singleton instance exported as `api`.
 *
 * - All requests have a 10s timeout to avoid hanging on backend failure.
 * - AbortSignal can be passed per-call for React effect cleanup.
 * - GET requests omit Content-Type (no body to describe).
 */

/** Default request timeout in milliseconds. */
const REQUEST_TIMEOUT_MS = 10_000;

const API_BASE = "/api/v1";

/** Camera pipeline states matching backend camera.State values. */
export type CameraState =
  | "idle"
  | "connecting"
  | "streaming"
  | "recording"
  | "error"
  | "stopped"
  | "disconnected";

export interface HealthStatus {
  status: string;
  version: string;
  uptime: string;
  go_version: string;
  os: string;
  arch: string;
  cameras_configured: number;
  database: string;
  go2rtc: string;
}

export interface PipelineStatus {
  state: CameraState;
  main_stream_active: boolean;
  sub_stream_active: boolean;
  recording: boolean;
  detecting: boolean;
  last_error?: string;
  connected_at?: string | null;
}

/** Full camera detail returned by the API (DB record + pipeline status). */
export interface CameraDetail {
  id: number;
  name: string;
  enabled: boolean;
  main_stream: string;
  sub_stream: string;
  record: boolean;
  detect: boolean;
  onvif_host?: string;
  onvif_port?: number;
  onvif_user?: string;
  // onvif_pass is excluded from API responses (json:"-" on backend)
  created_at: string;
  updated_at: string;
  pipeline_status: PipelineStatus;
}

/** Request body for creating/updating a camera. */
export interface CameraInput {
  name: string;
  enabled?: boolean;
  main_stream: string;
  sub_stream?: string;
  record?: boolean;
  detect?: boolean;
}

export interface SystemConfig {
  server: { host: string; port: number; log_level: string };
  storage: {
    hot_path: string;             // fast storage (SSD) — recent recordings
    cold_path: string;            // archival storage (HDD/NAS)
    hot_retention_days: number;
    cold_retention_days: number;
    segment_duration: number;
    segment_format: string;
  };
  detection: { enabled: boolean; backend: string };
  cameras: Array<{
    name: string;
    enabled: boolean;
    record: boolean;
    detect: boolean;
  }>;
}

/**
 * Combines two AbortSignals so aborting either one aborts the result.
 * Uses the native AbortSignal.any() when available (Chrome 116+, Firefox 124+,
 * Safari 17.4+), with a manual fallback for older browsers.
 */
function combineSignals(a: AbortSignal, b: AbortSignal): AbortSignal {
  if (typeof AbortSignal.any === "function") {
    return AbortSignal.any([a, b]);
  }
  // Fallback: wire both signals to a combined controller.
  // Each listener removes the other on fire to prevent leaks.
  const combined = new AbortController();
  if (a.aborted || b.aborted) {
    combined.abort();
  } else {
    const onAbortA = () => {
      combined.abort();
      b.removeEventListener("abort", onAbortB);
    };
    const onAbortB = () => {
      combined.abort();
      a.removeEventListener("abort", onAbortA);
    };
    a.addEventListener("abort", onAbortA, { once: true });
    b.addEventListener("abort", onAbortB, { once: true });
  }
  return combined.signal;
}

class ApiClient {
  /**
   * Core request method with timeout and abort support.
   * @param path - API path relative to /api/v1
   * @param options - fetch RequestInit, extended with optional signal for abort
   */
  private async request<T>(
    path: string,
    options?: RequestInit,
  ): Promise<T> {
    // Combine caller-provided signal (e.g. AbortController from useEffect cleanup)
    // with an automatic timeout signal so requests don't hang indefinitely.
    const timeoutController = new AbortController();
    const timeoutId = setTimeout(
      () => timeoutController.abort(),
      REQUEST_TIMEOUT_MS,
    );

    // If the caller passed a signal (for component unmount), abort on either.
    const signal = options?.signal
      ? combineSignals(options.signal, timeoutController.signal)
      : timeoutController.signal;

    try {
      const response = await fetch(`${API_BASE}${path}`, {
        ...options,
        signal,
      });

      if (!response.ok) {
        // Try to extract a JSON error body; fall back to status text.
        let detail = response.statusText;
        try {
          const body = await response.json();
          if (body.error) detail = body.error;
        } catch {
          // response wasn't JSON — use statusText
        }
        throw new Error(`API error ${response.status}: ${detail}`);
      }

      return (await response.json()) as T;
    } finally {
      clearTimeout(timeoutId);
    }
  }

  getHealth(signal?: AbortSignal): Promise<HealthStatus> {
    return this.request<HealthStatus>("/health", { signal });
  }

  getCameras(signal?: AbortSignal): Promise<CameraDetail[]> {
    return this.request<CameraDetail[]>("/cameras", { signal });
  }

  getCamera(name: string, signal?: AbortSignal): Promise<CameraDetail> {
    return this.request<CameraDetail>(`/cameras/${encodeURIComponent(name)}`, { signal });
  }

  createCamera(input: CameraInput, signal?: AbortSignal): Promise<CameraDetail> {
    return this.request<CameraDetail>("/cameras", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
      signal,
    });
  }

  updateCamera(name: string, input: CameraInput, signal?: AbortSignal): Promise<CameraDetail> {
    return this.request<CameraDetail>(`/cameras/${encodeURIComponent(name)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
      signal,
    });
  }

  async deleteCamera(name: string, signal?: AbortSignal): Promise<void> {
    await this.request(`/cameras/${encodeURIComponent(name)}`, {
      method: "DELETE",
      signal,
    });
  }

  getConfig(signal?: AbortSignal): Promise<SystemConfig> {
    return this.request<SystemConfig>("/config", { signal });
  }
}

export const api = new ApiClient();
