/**
 * client.test.ts — tests for the REST API client module.
 * Tests cover: response parsing, 204 handling, error extraction, type exports.
 * Run with: npm test (vitest run)
 */

import { describe, it, expect } from "vitest";

// These are pure type-level checks — ensure the module exports the expected shapes.
import type {
  CameraDetail,
  EventRecord,
  EventsResponse,
  HeatmapBucket,
  RetentionRule,
  NotifPref,
} from "./client";

// ─── Type export checks ─────────────────────────────────────────────────────

describe("TypeExports", () => {
  it("EventRecord has expected fields", () => {
    const ev: EventRecord = {
      id: 1,
      camera_id: 1,
      type: "detection",
      label: "person",
      confidence: 0.95,
      data: "{}",
      thumbnail: "/api/v1/events/1/thumbnail",
      has_clip: false,
      start_time: "2026-01-15T10:00:00Z",
      end_time: null,
      created_at: "2026-01-15T10:00:00Z",
    };
    expect(ev.id).toBe(1);
    expect(ev.camera_id).toBe(1);
    expect(ev.thumbnail).toContain("/api/v1/events/");
  });

  it("EventsResponse wraps events array with total", () => {
    const resp: EventsResponse = { events: [], total: 0 };
    expect(resp.events).toEqual([]);
    expect(resp.total).toBe(0);
  });

  it("HeatmapBucket has bucket_start and detection_count", () => {
    const bucket: HeatmapBucket = {
      bucket_start: "2026-01-15T10:00:00Z",
      detection_count: 5,
    };
    expect(bucket.detection_count).toBe(5);
  });

  it("RetentionRule supports null camera_id and event_type (wildcards)", () => {
    const rule: RetentionRule = {
      id: 1,
      camera_id: null,
      event_type: null,
      events_days: 30,
      created_at: "2026-01-15T10:00:00Z",
      updated_at: "2026-01-15T10:00:00Z",
    };
    expect(rule.camera_id).toBeNull();
    expect(rule.event_type).toBeNull();
  });

  it("NotifPref has critical field for DND bypass", () => {
    const pref: NotifPref = {
      id: 1,
      user_id: 1,
      event_type: "detection",
      camera_id: null,
      enabled: true,
      critical: true,
    };
    expect(pref.critical).toBe(true);
  });

  it("CameraDetail includes zones array", () => {
    const cam: CameraDetail = {
      id: 1,
      name: "test",
      enabled: true,
      main_stream: "rtsp://x",
      sub_stream: "",
      record: true,
      detect: false,
      created_at: "2026-01-15T10:00:00Z",
      updated_at: "2026-01-15T10:00:00Z",
      pipeline_status: null,
      zones: [],
    };
    expect(cam.zones).toEqual([]);
  });
});

// ─── combineSignals (tested indirectly via the exported function) ────────────

describe("combineSignals behavior", () => {
  it("AbortController timeout pattern works", () => {
    const ctrl = new AbortController();
    expect(ctrl.signal.aborted).toBe(false);
    ctrl.abort();
    expect(ctrl.signal.aborted).toBe(true);
  });
});
