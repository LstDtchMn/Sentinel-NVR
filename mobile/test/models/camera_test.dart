// Unit tests for CameraDetail, PipelineStatus, Zone, and ZonePoint model parsing.
// Run with: flutter test

import 'package:flutter_test/flutter_test.dart';
import 'package:sentinel_nvr/models/camera.dart';

void main() {
  // ─── CameraDetail.fromJson ──────────────────────────────────────────────────

  group('CameraDetail.fromJson', () {
    test('parses a minimal camera record (no optional fields)', () {
      final json = <String, dynamic>{
        'id': 1,
        'name': 'front-door',
        'enabled': true,
        'main_stream': 'rtsp://192.168.1.100:554/stream',
        'record': true,
        'detect': false,
        'created_at': '2024-01-01T00:00:00Z',
        'updated_at': '2024-01-01T00:00:00Z',
      };

      final cam = CameraDetail.fromJson(json);

      expect(cam.id, 1);
      expect(cam.name, 'front-door');
      expect(cam.enabled, true);
      expect(cam.mainStream, 'rtsp://192.168.1.100:554/stream');
      expect(cam.subStream, isNull);
      expect(cam.record, true);
      expect(cam.detect, false);
      expect(cam.onvifHost, isNull);
      expect(cam.onvifPort, isNull);
      expect(cam.onvifUser, isNull);
      expect(cam.pipelineStatus, isNull);
      expect(cam.zones, isEmpty);
    });

    test('parses optional sub_stream and ONVIF fields', () {
      final json = <String, dynamic>{
        'id': 2,
        'name': 'garage',
        'enabled': true,
        'main_stream': 'rtsp://host:554/main',
        'sub_stream': 'rtsp://host:554/sub',
        'record': true,
        'detect': true,
        'onvif_host': '192.168.1.101',
        'onvif_port': 80,
        'onvif_user': 'admin',
        'created_at': '2024-02-01T00:00:00Z',
        'updated_at': '2024-02-01T00:00:00Z',
      };

      final cam = CameraDetail.fromJson(json);

      expect(cam.subStream, 'rtsp://host:554/sub');
      expect(cam.onvifHost, '192.168.1.101');
      expect(cam.onvifPort, 80);
      expect(cam.onvifUser, 'admin');
    });

    test('parses pipeline_status when present', () {
      final json = <String, dynamic>{
        'id': 3,
        'name': 'side-gate',
        'enabled': true,
        'main_stream': 'rtsp://host/s',
        'record': false,
        'detect': true,
        'created_at': '2024-01-01T00:00:00Z',
        'updated_at': '2024-01-01T00:00:00Z',
        'pipeline_status': {
          'state': 'recording',
          'main_stream_active': true,
          'sub_stream_active': false,
          'recording': true,
          'detecting': false,
          'last_error': null,
          'connected_at': '2024-01-15T12:00:00Z',
        },
      };

      final cam = CameraDetail.fromJson(json);

      expect(cam.pipelineStatus, isNotNull);
      expect(cam.pipelineStatus!.state, 'recording');
      expect(cam.pipelineStatus!.mainStreamActive, true);
      expect(cam.pipelineStatus!.subStreamActive, false);
      expect(cam.pipelineStatus!.recording, true);
      expect(cam.pipelineStatus!.detecting, false);
      expect(cam.pipelineStatus!.lastError, isNull);
      expect(cam.pipelineStatus!.connectedAt, '2024-01-15T12:00:00Z');
    });

    test('parses zones array', () {
      final json = <String, dynamic>{
        'id': 4,
        'name': 'office',
        'enabled': true,
        'main_stream': 'rtsp://host/s',
        'record': false,
        'detect': true,
        'created_at': '2024-01-01T00:00:00Z',
        'updated_at': '2024-01-01T00:00:00Z',
        'zones': [
          {
            'id': 'zone-1',
            'name': 'desk area',
            'type': 'include',
            'points': [
              {'x': 0.1, 'y': 0.2},
              {'x': 0.5, 'y': 0.2},
              {'x': 0.5, 'y': 0.8},
              {'x': 0.1, 'y': 0.8},
            ],
          },
        ],
      };

      final cam = CameraDetail.fromJson(json);

      expect(cam.zones, hasLength(1));
      expect(cam.zones[0].id, 'zone-1');
      expect(cam.zones[0].name, 'desk area');
      expect(cam.zones[0].type, 'include');
      expect(cam.zones[0].points, hasLength(4));
      expect(cam.zones[0].points[0].x, closeTo(0.1, 0.0001));
      expect(cam.zones[0].points[0].y, closeTo(0.2, 0.0001));
    });

    test('treats missing zones key as empty list', () {
      final json = <String, dynamic>{
        'id': 5,
        'name': 'empty-zones',
        'enabled': false,
        'main_stream': 'rtsp://host/s',
        'record': false,
        'detect': false,
        'created_at': '2024-01-01T00:00:00Z',
        'updated_at': '2024-01-01T00:00:00Z',
        // zones key intentionally omitted
      };

      final cam = CameraDetail.fromJson(json);
      expect(cam.zones, isEmpty);
    });
  });

  // ─── PipelineStatus.fromJson ────────────────────────────────────────────────

  group('PipelineStatus.fromJson', () {
    test('defaults boolean fields to false when absent', () {
      final json = <String, dynamic>{'state': 'idle'};

      final status = PipelineStatus.fromJson(json);

      expect(status.state, 'idle');
      expect(status.mainStreamActive, false);
      expect(status.subStreamActive, false);
      expect(status.recording, false);
      expect(status.detecting, false);
      expect(status.lastError, isNull);
      expect(status.connectedAt, isNull);
    });

    test('parses an error state with last_error message', () {
      final json = <String, dynamic>{
        'state': 'error',
        'main_stream_active': false,
        'sub_stream_active': false,
        'recording': false,
        'detecting': false,
        'last_error': 'ffmpeg exited with code 1',
      };

      final status = PipelineStatus.fromJson(json);

      expect(status.state, 'error');
      expect(status.lastError, 'ffmpeg exited with code 1');
    });
  });

  // ─── Zone.fromJson ──────────────────────────────────────────────────────────

  group('Zone.fromJson', () {
    test('parses an exclusion zone', () {
      final json = <String, dynamic>{
        'id': 'z-exc',
        'name': 'road',
        'type': 'exclude',
        'points': [
          {'x': 0.0, 'y': 0.0},
          {'x': 1.0, 'y': 0.0},
          {'x': 1.0, 'y': 0.3},
          {'x': 0.0, 'y': 0.3},
        ],
      };

      final zone = Zone.fromJson(json);

      expect(zone.id, 'z-exc');
      expect(zone.name, 'road');
      expect(zone.type, 'exclude');
      expect(zone.points, hasLength(4));
    });
  });

  // ─── ZonePoint.fromJson ─────────────────────────────────────────────────────

  group('ZonePoint.fromJson', () {
    test('parses integer coordinates as doubles', () {
      // Backend may return integer JSON values (1 instead of 1.0)
      final json = <String, dynamic>{'x': 1, 'y': 0};
      final pt = ZonePoint.fromJson(json);

      expect(pt.x, 1.0);
      expect(pt.y, 0.0);
    });

    test('preserves fractional coordinates', () {
      final json = <String, dynamic>{'x': 0.375, 'y': 0.625};
      final pt = ZonePoint.fromJson(json);

      expect(pt.x, closeTo(0.375, 0.0001));
      expect(pt.y, closeTo(0.625, 0.0001));
    });
  });
}
