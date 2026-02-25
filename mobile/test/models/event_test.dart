import 'package:flutter_test/flutter_test.dart';
import 'package:sentinel_nvr/models/event.dart';

void main() {
  group('EventRecord.fromJson', () {
    test('parses all required fields', () {
      final json = {
        'id': 42,
        'camera_id': 1,
        'type': 'detection',
        'label': 'person',
        'confidence': 0.95,
        'data': '{"objects":[]}',
        'thumbnail': '/api/v1/events/42/thumbnail',
        'has_clip': true,
        'start_time': '2026-01-15T10:30:00Z',
        'end_time': '2026-01-15T10:31:00Z',
        'created_at': '2026-01-15T10:30:00Z',
      };

      final ev = EventRecord.fromJson(json);

      expect(ev.id, 42);
      expect(ev.cameraId, 1);
      expect(ev.type, 'detection');
      expect(ev.label, 'person');
      expect(ev.confidence, 0.95);
      expect(ev.data, '{"objects":[]}');
      expect(ev.thumbnail, '/api/v1/events/42/thumbnail');
      expect(ev.hasClip, true);
      expect(ev.startTime, '2026-01-15T10:30:00Z');
      expect(ev.endTime, '2026-01-15T10:31:00Z');
      expect(ev.createdAt, '2026-01-15T10:30:00Z');
    });

    test('handles null/missing optional fields with defaults', () {
      final json = {
        'id': 1,
        'type': 'camera.online',
        'start_time': '2026-01-15T10:30:00Z',
        'created_at': '2026-01-15T10:30:00Z',
        // camera_id, label, confidence, data, thumbnail, has_clip, end_time omitted
      };

      final ev = EventRecord.fromJson(json);

      expect(ev.cameraId, isNull);
      expect(ev.label, '');
      expect(ev.confidence, 0.0);
      expect(ev.data, '');
      expect(ev.thumbnail, '');
      expect(ev.hasClip, false);
      expect(ev.endTime, isNull);
    });

    test('handles integer confidence (num type)', () {
      final json = {
        'id': 1,
        'type': 'detection',
        'label': 'car',
        'confidence': 1, // integer, not double
        'data': '{}',
        'thumbnail': '',
        'has_clip': false,
        'start_time': '2026-01-15T10:30:00Z',
        'created_at': '2026-01-15T10:30:00Z',
      };

      final ev = EventRecord.fromJson(json);
      expect(ev.confidence, 1.0);
    });

    test('handles null camera_id (orphaned event)', () {
      final json = {
        'id': 5,
        'camera_id': null,
        'type': 'detection',
        'start_time': '2026-01-15T10:30:00Z',
        'created_at': '2026-01-15T10:30:00Z',
      };

      final ev = EventRecord.fromJson(json);
      expect(ev.cameraId, isNull);
    });
  });

  group('HeatmapBucket.fromJson', () {
    test('parses bucket_start and detection_count', () {
      final json = {
        'bucket_start': '2026-01-15T10:30:00Z',
        'detection_count': 15,
      };

      final bucket = HeatmapBucket.fromJson(json);
      expect(bucket.bucketStart, '2026-01-15T10:30:00Z');
      expect(bucket.detectionCount, 15);
    });

    test('defaults detection_count to 0 when null', () {
      final json = {
        'bucket_start': '2026-01-15T10:00:00Z',
        'detection_count': null,
      };

      final bucket = HeatmapBucket.fromJson(json);
      expect(bucket.detectionCount, 0);
    });

    test('handles integer detection_count (num type)', () {
      final json = {
        'bucket_start': '2026-01-15T10:00:00Z',
        'detection_count': 42.0, // float from JSON deserializer
      };

      final bucket = HeatmapBucket.fromJson(json);
      expect(bucket.detectionCount, 42);
    });
  });
}
