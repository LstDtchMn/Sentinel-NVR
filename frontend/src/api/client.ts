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
  | "stopped";

/** Authenticated user info returned by GET /auth/me (Phase 7, CG6). */
export interface AuthUser {
  id: number;
  username: string;
  role: string;
}

export interface HealthStatus {
  status: string;
  version: string;
  uptime: string;
  go_version: string;
  os: string;
  arch: string;
  cameras_configured: number;
  recordings_count: number;
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
  connected_at?: string;
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
  pipeline_status: PipelineStatus | null;
  zones: Zone[]; // Phase 9: detection zone polygons; backend always returns array (default [])
}

/** Request body for creating/updating a camera. */
export interface CameraInput {
  name: string;
  enabled?: boolean;
  main_stream: string;
  sub_stream?: string;
  record?: boolean;
  detect?: boolean;
  onvif_host?: string;
  onvif_port?: number;
  onvif_user?: string;
  onvif_pass?: string;
  zones?: Zone[]; // Phase 9: when omitted, server preserves existing zones
}

/** A single recording segment returned by the API. */
export interface RecordingSegment {
  id: number;
  camera_id: number;
  camera_name: string;
  path: string;
  start_time: string;
  end_time: string | null;
  duration_s: number;
  size_bytes: number;
  created_at: string;
}

/** Query parameters for listing recordings. */
export interface RecordingListParams {
  camera?: string;
  start?: string; // RFC3339
  end?: string;   // RFC3339
  limit?: number;
  offset?: number;
}

/** A recording segment projected for timeline rendering — no path field (R6). */
export interface TimelineSegment {
  id: number;
  start_time: string; // RFC3339
  end_time: string;   // RFC3339
  duration_s: number;
}

/** A normalized bounding box from a detection event (coordinates in [0.0, 1.0]). */
export interface BBox {
  x_min: number;
  y_min: number;
  x_max: number;
  y_max: number;
}

// --- Zone types (Phase 9, R5) ---

/** Include: detections must be inside the zone. Exclude: detections inside are suppressed. */
export type ZoneType = "include" | "exclude";

/** A single polygon vertex, normalised to [0.0, 1.0] relative to frame dimensions. */
export interface ZonePoint {
  x: number;
  y: number;
}

/** A named polygon region on a camera's field of view for detection zone filtering. */
export interface Zone {
  id: string;
  name: string;
  type: ZoneType;
  points: ZonePoint[];
}

/** A single object detected in a frame. */
export interface DetectedObject {
  label: string;
  confidence: number;
  bbox: BBox;
}

/** A single row from the events table (Phase 5, R3). */
export interface EventRecord {
  id: number;
  camera_id: number | null;
  type: string;
  label: string;
  confidence: number;
  /** Raw JSON string — parse to DetectedObject[] for detection events. */
  data: string;
  thumbnail: string; // empty when no snapshot was saved
  has_clip: boolean;
  start_time: string; // RFC3339
  end_time: string | null;
  created_at: string;
}

export interface EventsResponse {
  events: EventRecord[];
  total: number;
}

export interface EventListParams {
  camera_id?: number;
  type?: string;
  date?: string; // YYYY-MM-DD
  limit?: number;
  offset?: number;
}

/** A 5-minute detection density bucket for timeline heatmap overlay (Phase 6, R6). */
export interface HeatmapBucket {
  bucket_start: string; // RFC3339 — start of the 5-minute window
  detection_count: number;
}

// --- Notification types (Phase 8, R9) ---

/** A registered device token for push delivery (FCM, APNs, or webhook). */
export interface NotifToken {
  id: number;
  user_id: number;
  token: string;
  provider: "fcm" | "apns" | "webhook";
  label: string;
  created_at: string;
  updated_at: string;
}

/** A per-user notification preference controlling when alerts fire. */
export interface NotifPref {
  id: number;
  user_id: number;
  event_type: string; // "detection" | "camera.offline" | "*" (any)
  camera_id: number | null; // null = all cameras
  enabled: boolean;
  critical: boolean; // true = iOS Critical Alert (bypasses Do Not Disturb)
}

/** A single notification delivery attempt recorded in notification_log. */
export interface NotifLogEntry {
  id: number;
  event_id: number | null;
  token_id: number;
  provider: string;
  title: string;
  body: string;
  deep_link: string;
  status: "pending" | "sent" | "failed";
  attempts: number;
  last_error: string;
  scheduled_at: string;
  sent_at: string | null;
}

export interface SystemConfig {
  server: { host: string; port: number; log_level: string };
  storage: {
    hot_path: string;             // fast storage (SSD) — recent recordings
    cold_path?: string;           // archival storage (HDD/NAS) — absent when not configured
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

// --- Face Recognition types (Phase 13, R11) ---

/** An enrolled face identity for recognition matching. */
export interface FaceRecord {
  id: number;
  name: string;
  thumbnail: string;
  created_at: string;
  updated_at: string;
}

// --- Migration / Import types (Phase 14, R15) ---

/** A camera parsed from an external NVR config file during import preview. */
export interface ImportedCamera {
  name: string;
  enabled: boolean;
  main_stream: string;
  sub_stream: string;
  record: boolean;
  detect: boolean;
  onvif_host?: string;
  onvif_port?: number;
  onvif_user?: string;
  // onvif_pass excluded — backend uses json:"-" to prevent credential leakage in previews
}

/** Result of a dry-run import preview (POST /import/preview). */
export interface ImportResult {
  format: string;
  cameras: ImportedCamera[];
  warnings: string[];
  errors: string[];
}

/** Result of an actual import execution (POST /import). */
export interface ImportExecuteResult {
  imported: number;
  skipped: number;
  errors: string[];
  warnings: string[];
}

/** Pairing code returned by POST /pairing/qr (Phase 12, CG11). */
export interface PairingCode {
  code: string;
  expires_at: string; // RFC3339
}

/** Per-tier storage usage returned by GET /api/v1/storage/stats (Phase 10, R13). */
export interface StorageTierStats {
  path: string;
  used_bytes: number;
  segment_count: number;
}

/** Aggregate storage stats for hot and (optionally) cold tiers (Phase 10, R13). */
export interface StorageStats {
  hot: StorageTierStats;
  cold: StorageTierStats | null;
}

/**
 * Per-camera × per-event-type retention rule (R14).
 * camera_id=null and event_type=null act as wildcards.
 */
export interface RetentionRule {
  id: number;
  camera_id: number | null;
  event_type: string | null;
  events_days: number;
  created_at: string;
  updated_at: string;
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
        // credentials: "include" sends httpOnly cookies cross-origin in development
        // (Vite dev server proxies /api/v1 to localhost:8099, but the origin header
        // from localhost:5173 still requires the server to allow credentials).
        credentials: "include",
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
        const err = new Error(`API error ${response.status}: ${detail}`) as Error & { status?: number };
        err.status = response.status;
        throw err;
      }

      // 204 No Content has an empty body — return undefined rather than attempting
      // JSON.parse on an empty response, which would throw a SyntaxError.
      if (response.status === 204) {
        return undefined as unknown as T;
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

  getCameraStatus(name: string, signal?: AbortSignal): Promise<PipelineStatus> {
    return this.request<PipelineStatus>(`/cameras/${encodeURIComponent(name)}/status`, { signal });
  }

  /** Returns a URL that fetches a single JPEG snapshot from the camera's go2rtc stream.
   *  Used as the background image for the zone editor (Phase 9). The server grabs a
   *  live frame — append a cache-busting parameter to force a fresh fetch each call. */
  cameraSnapshotURL(name: string): string {
    return `${API_BASE}/cameras/${encodeURIComponent(name)}/snapshot`;
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

  /** Returns aggregate storage usage per tier (Phase 10, R13). */
  getStorageStats(signal?: AbortSignal): Promise<StorageStats> {
    return this.request<StorageStats>("/storage/stats", { signal });
  }

  /** Updates non-sensitive config fields and persists to disk (admin only). Phase 9. */
  updateConfig(
    input: {
      server?: { log_level?: string };
      storage?: { hot_retention_days?: number; cold_retention_days?: number; segment_duration?: number };
    },
    signal?: AbortSignal,
  ): Promise<SystemConfig> {
    return this.request<SystemConfig>("/config", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
      signal,
    });
  }

  async getRecordings(
    params?: RecordingListParams,
    signal?: AbortSignal,
  ): Promise<{ recordings: RecordingSegment[]; total: number }> {
    const query = new URLSearchParams();
    if (params?.camera) query.set("camera", params.camera);
    if (params?.start) query.set("start", params.start);
    if (params?.end) query.set("end", params.end);
    if (params?.limit !== undefined) query.set("limit", String(params.limit));
    if (params?.offset !== undefined) query.set("offset", String(params.offset));
    const qs = query.toString();
    // Backend returns {"recordings":[...],"total":N} — unwrap into typed response.
    return this.request<{ recordings: RecordingSegment[]; total: number }>(
      `/recordings${qs ? `?${qs}` : ""}`,
      { signal },
    );
  }

  getRecording(id: number, signal?: AbortSignal): Promise<RecordingSegment> {
    return this.request<RecordingSegment>(`/recordings/${id}`, { signal });
  }

  async deleteRecording(id: number, signal?: AbortSignal): Promise<void> {
    await this.request(`/recordings/${id}`, { method: "DELETE", signal });
  }

  /** Returns segments for a camera on a given day, for timeline rendering (R6). */
  getTimeline(params: { camera: string; date: string }, signal?: AbortSignal): Promise<TimelineSegment[]> {
    const query = new URLSearchParams();
    query.set("camera", params.camera);
    query.set("date", params.date);
    return this.request<TimelineSegment[]>(`/recordings/timeline?${query}`, { signal });
  }

  /** Returns dates with recordings for a camera in a given month (R6). */
  getRecordingDays(params: { camera: string; month: string }, signal?: AbortSignal): Promise<string[]> {
    const query = new URLSearchParams();
    query.set("camera", params.camera);
    query.set("month", params.month);
    return this.request<string[]>(`/recordings/days?${query}`, { signal });
  }

  /** Returns the URL for streaming/downloading a recording segment. */
  recordingPlayURL(id: number): string {
    return `${API_BASE}/recordings/${id}/play`;
  }

  /** Returns detection events with optional filtering and pagination (Phase 5, R3). */
  getEvents(params?: EventListParams, signal?: AbortSignal): Promise<EventsResponse> {
    const query = new URLSearchParams();
    if (params?.camera_id !== undefined) query.set("camera_id", String(params.camera_id));
    if (params?.type) query.set("type", params.type);
    if (params?.date) query.set("date", params.date);
    if (params?.limit !== undefined) query.set("limit", String(params.limit));
    if (params?.offset !== undefined) query.set("offset", String(params.offset));
    const qs = query.toString();
    return this.request<EventsResponse>(`/events${qs ? `?${qs}` : ""}`, { signal });
  }

  /** Returns the URL for serving an event thumbnail JPEG. */
  eventThumbnailURL(id: number): string {
    return `${API_BASE}/events/${id}/thumbnail`;
  }

  getEvent(id: number, signal?: AbortSignal): Promise<EventRecord> {
    return this.request<EventRecord>(`/events/${id}`, { signal });
  }

  async deleteEvent(id: number, signal?: AbortSignal): Promise<void> {
    await this.request(`/events/${id}`, { method: "DELETE", signal });
  }

  /** Returns detection density buckets for the timeline heatmap overlay (Phase 6, R6). */
  getEventHeatmap(params: { camera_id: number; date: string }, signal?: AbortSignal): Promise<HeatmapBucket[]> {
    const query = new URLSearchParams();
    query.set("camera_id", String(params.camera_id));
    query.set("date", params.date);
    return this.request<HeatmapBucket[]>(`/events/heatmap?${query}`, { signal });
  }

  // --- Auth (Phase 7, CG6) ---

  /** Logs in and sets httpOnly cookies. Throws on invalid credentials (401). */
  login(username: string, password: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>("/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
      signal,
    });
  }

  /** Rotates the refresh token and reissues cookies. Throws 401 on expired session. */
  refreshSession(signal?: AbortSignal): Promise<void> {
    return this.request<void>("/auth/refresh", { method: "POST", signal });
  }

  /** Revokes the refresh token and clears auth cookies. */
  logout(signal?: AbortSignal): Promise<void> {
    return this.request<void>("/auth/logout", { method: "POST", signal });
  }

  /** Returns the currently authenticated user, or throws 401 if not logged in. */
  getMe(signal?: AbortSignal): Promise<AuthUser> {
    return this.request<AuthUser>("/auth/me", { signal });
  }

  /** Checks if first-run setup is needed (no users exist yet). Public endpoint (Phase 7, CG6). */
  checkSetup(signal?: AbortSignal): Promise<{ needs_setup: boolean; oidc_enabled: boolean }> {
    return this.request<{ needs_setup: boolean; oidc_enabled: boolean }>("/setup", { signal });
  }

  /**
   * Creates the first admin account during first-run setup and sets auth cookies.
   * Returns the created user. Throws 409 if setup was already completed (Phase 7, CG6).
   */
  completeSetup(username: string, password: string, signal?: AbortSignal): Promise<{ user: AuthUser }> {
    return this.request<{ user: AuthUser }>("/setup", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
      signal,
    });
  }

  /**
   * Opens a Server-Sent Events connection to receive live event notifications (Phase 6, CG8).
   * The caller must close the returned EventSource on component unmount:
   *   const source = api.subscribeEvents();
   *   return () => source.close();
   */
  subscribeEvents(): EventSource {
    return new EventSource(`${API_BASE}/events/stream`, { withCredentials: true });
  }

  /** Returns the WebSocket URL for a camera's live MSE stream (CG3). */
  streamWSURL(cameraName: string): string {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    return `${proto}//${location.host}${API_BASE}/streams/${encodeURIComponent(cameraName)}/ws`;
  }

  // --- Notifications (Phase 8, R9) ---

  /** Registers (or updates the label of) a device token for push delivery. */
  createNotifToken(
    input: { provider: "fcm" | "apns" | "webhook"; token: string; label?: string },
    signal?: AbortSignal,
  ): Promise<NotifToken> {
    return this.request<NotifToken>("/notifications/tokens", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
      signal,
    });
  }

  /** Returns all device tokens registered for the current user. */
  listNotifTokens(signal?: AbortSignal): Promise<NotifToken[]> {
    return this.request<NotifToken[]>("/notifications/tokens", { signal });
  }

  /** Removes a registered device token. */
  async deleteNotifToken(id: number, signal?: AbortSignal): Promise<void> {
    await this.request(`/notifications/tokens/${id}`, { method: "DELETE", signal });
  }

  /** Returns the current user's notification preferences. */
  listNotifPrefs(signal?: AbortSignal): Promise<NotifPref[]> {
    return this.request<NotifPref[]>("/notifications/prefs", { signal });
  }

  /** Creates or updates a notification preference. */
  upsertNotifPref(
    input: { event_type: string; camera_id?: number | null; enabled: boolean; critical: boolean },
    signal?: AbortSignal,
  ): Promise<NotifPref> {
    return this.request<NotifPref>("/notifications/prefs", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
      signal,
    });
  }

  /** Removes a notification preference by ID. */
  async deleteNotifPref(id: number, signal?: AbortSignal): Promise<void> {
    await this.request(`/notifications/prefs/${id}`, { method: "DELETE", signal });
  }

  /** Returns recent notification delivery log entries for the current user. */
  listNotifLog(limit?: number, signal?: AbortSignal): Promise<NotifLogEntry[]> {
    const qs = limit ? `?limit=${limit}` : "";
    return this.request<NotifLogEntry[]>(`/notifications/log${qs}`, { signal });
  }

  // --- Remote Access / Pairing (Phase 12, CG11) ---

  /** Generates a new pairing code for QR-based mobile app pairing (admin only). */
  generatePairingCode(signal?: AbortSignal): Promise<PairingCode> {
    return this.request<PairingCode>("/pairing/qr", {
      method: "POST",
      signal,
    });
  }

  // --- Face Recognition (Phase 13, R11) ---

  /** Returns all enrolled faces (without embeddings). */
  listFaces(signal?: AbortSignal): Promise<FaceRecord[]> {
    return this.request<{ faces: FaceRecord[] }>("/faces", { signal }).then((r) => r.faces);
  }

  /** Returns a single enrolled face by ID. */
  getFace(id: number, signal?: AbortSignal): Promise<FaceRecord> {
    return this.request<FaceRecord>(`/faces/${id}`, { signal });
  }

  /** Enrolls a new face with a name and pre-computed embedding (admin only). */
  createFace(
    input: { name: string; embedding: number[] },
    signal?: AbortSignal,
  ): Promise<FaceRecord> {
    return this.request<FaceRecord>("/faces", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
      signal,
    });
  }

  /** Renames an enrolled face (admin only). Returns the updated record. */
  updateFace(id: number, name: string, signal?: AbortSignal): Promise<FaceRecord> {
    return this.request<FaceRecord>(`/faces/${id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
      signal,
    });
  }

  /** Deletes an enrolled face (admin only). */
  async deleteFace(id: number, signal?: AbortSignal): Promise<void> {
    await this.request(`/faces/${id}`, { method: "DELETE", signal });
  }

  // --- Migration / Import (Phase 14, R15) ---

  /**
   * Dry-run import: parses an uploaded config file and returns a preview of cameras
   * that would be imported (admin only).
   */
  async importPreview(
    format: "blue_iris" | "frigate",
    file: File,
    signal?: AbortSignal,
  ): Promise<ImportResult> {
    const formData = new FormData();
    formData.append("format", format);
    formData.append("file", file);
    // Use raw fetch because FormData sets its own Content-Type with boundary.
    const timeoutCtrl = new AbortController();
    const timeoutId = setTimeout(() => timeoutCtrl.abort(), 30_000);
    const sig = signal
      ? combineSignals(signal, timeoutCtrl.signal)
      : timeoutCtrl.signal;
    try {
      const resp = await fetch(`${API_BASE}/import/preview`, {
        method: "POST",
        body: formData,
        credentials: "include",
        signal: sig,
      });
      clearTimeout(timeoutId);
      if (!resp.ok) {
        let detail = resp.statusText;
        try { const b = await resp.json(); if (b.error) detail = b.error; } catch {}
        throw new Error(`API error ${resp.status}: ${detail}`);
      }
      return (await resp.json()) as ImportResult;
    } finally {
      clearTimeout(timeoutId);
    }
  }

  // --- Retention Rules (R14) ---

  /** Lists all retention rules (GET /retention/rules). */
  async listRetentionRules(signal?: AbortSignal): Promise<RetentionRule[]> {
    return this.request<RetentionRule[]>("/retention/rules", { signal });
  }

  /**
   * Creates a retention rule (POST /retention/rules).
   * Omit camera_id for a global rule; omit event_type to cover all event types.
   */
  async createRetentionRule(
    body: { camera_id?: number | null; event_type?: string | null; events_days: number },
    signal?: AbortSignal,
  ): Promise<RetentionRule> {
    return this.request<RetentionRule>("/retention/rules", {
      method: "POST",
      body: JSON.stringify(body),
      signal,
    });
  }

  /** Updates events_days for an existing rule (PUT /retention/rules/:id). */
  async updateRetentionRule(
    id: number,
    events_days: number,
    signal?: AbortSignal,
  ): Promise<RetentionRule> {
    return this.request<RetentionRule>(`/retention/rules/${id}`, {
      method: "PUT",
      body: JSON.stringify({ events_days }),
      signal,
    });
  }

  /** Deletes a retention rule (DELETE /retention/rules/:id). */
  async deleteRetentionRule(id: number, signal?: AbortSignal): Promise<void> {
    await this.request(`/retention/rules/${id}`, { method: "DELETE", signal });
  }

  /** Executes the import — creates cameras from the uploaded file (admin only). */
  async importExecute(
    format: "blue_iris" | "frigate",
    file: File,
    signal?: AbortSignal,
  ): Promise<ImportExecuteResult> {
    const formData = new FormData();
    formData.append("format", format);
    formData.append("file", file);
    const timeoutCtrl = new AbortController();
    const timeoutId = setTimeout(() => timeoutCtrl.abort(), 60_000);
    const sig = signal
      ? combineSignals(signal, timeoutCtrl.signal)
      : timeoutCtrl.signal;
    try {
      const resp = await fetch(`${API_BASE}/import`, {
        method: "POST",
        body: formData,
        credentials: "include",
        signal: sig,
      });
      clearTimeout(timeoutId);
      if (!resp.ok) {
        let detail = resp.statusText;
        try { const b = await resp.json(); if (b.error) detail = b.error; } catch {}
        throw new Error(`API error ${resp.status}: ${detail}`);
      }
      return (await resp.json()) as ImportExecuteResult;
    } finally {
      clearTimeout(timeoutId);
    }
  }
}

export const api = new ApiClient();
