// Recording models mirroring frontend/src/api/client.ts shapes (Phase 11, CG11).

class RecordingSegment {
  final int id;
  final int cameraId;
  final String cameraName;
  final String path;
  final String startTime; // RFC3339
  final String? endTime;
  final int durationS;
  final int sizeBytes;
  final String createdAt;

  const RecordingSegment({
    required this.id,
    required this.cameraId,
    required this.cameraName,
    required this.path,
    required this.startTime,
    this.endTime,
    required this.durationS,
    required this.sizeBytes,
    required this.createdAt,
  });

  factory RecordingSegment.fromJson(Map<String, dynamic> json) {
    return RecordingSegment(
      id: json['id'] as int,
      cameraId: json['camera_id'] as int,
      cameraName: json['camera_name'] as String,
      path: json['path'] as String? ?? '',
      startTime: json['start_time'] as String,
      endTime: json['end_time'] as String?,
      // Go serialises duration_s as float64 (e.g. 600.0); Dart decodes it as
      // double so we must use num? → toInt() rather than a direct int? cast.
      durationS: (json['duration_s'] as num?)?.toInt() ?? 0,
      sizeBytes: (json['size_bytes'] as num?)?.toInt() ?? 0,
      createdAt: json['created_at'] as String,
    );
  }
}

/// Lightweight segment used for 24h timeline rendering (path omitted by API).
class TimelineSegment {
  final int id;
  final String startTime; // RFC3339
  final String endTime;
  final int durationS;

  const TimelineSegment({
    required this.id,
    required this.startTime,
    required this.endTime,
    required this.durationS,
  });

  factory TimelineSegment.fromJson(Map<String, dynamic> json) {
    return TimelineSegment(
      id: json['id'] as int,
      startTime: json['start_time'] as String,
      endTime: json['end_time'] as String,
      durationS: (json['duration_s'] as num?)?.toInt() ?? 0,
    );
  }

  /// Seconds since midnight (local time) for [startTime].
  /// RFC3339 strings are parsed to UTC by Dart then converted to local time
  /// so the timeline position reflects the device's local clock.
  int get startSecond {
    final t = DateTime.tryParse(startTime)?.toLocal();
    if (t == null) return 0;
    return t.hour * 3600 + t.minute * 60 + t.second;
  }

  /// Seconds since midnight (local time) for [endTime].
  int get endSecond {
    final t = DateTime.tryParse(endTime)?.toLocal();
    if (t == null) return 0;
    return t.hour * 3600 + t.minute * 60 + t.second;
  }
}
