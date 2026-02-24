// Event models mirroring frontend/src/api/client.ts EventRecord + heatmap shapes (Phase 11, CG11).

class EventRecord {
  final int id;
  final int? cameraId;
  final String type;
  final String label;
  final double confidence;

  /// Raw JSON string — parse with DetectedObject.listFromJson() for bounding boxes.
  final String data;

  /// Empty string when no snapshot was captured.
  final String thumbnail;
  final bool hasClip;
  final String startTime; // RFC3339
  final String? endTime;
  final String createdAt;

  const EventRecord({
    required this.id,
    this.cameraId,
    required this.type,
    required this.label,
    required this.confidence,
    required this.data,
    required this.thumbnail,
    required this.hasClip,
    required this.startTime,
    this.endTime,
    required this.createdAt,
  });

  factory EventRecord.fromJson(Map<String, dynamic> json) {
    return EventRecord(
      id: json['id'] as int,
      cameraId: json['camera_id'] as int?,
      type: json['type'] as String,
      label: json['label'] as String? ?? '',
      confidence: (json['confidence'] as num?)?.toDouble() ?? 0.0,
      data: json['data'] as String? ?? '',
      thumbnail: json['thumbnail'] as String? ?? '',
      hasClip: json['has_clip'] as bool? ?? false,
      startTime: json['start_time'] as String,
      endTime: json['end_time'] as String?,
      createdAt: json['created_at'] as String,
    );
  }
}

class HeatmapBucket {
  final String bucketStart; // RFC3339 — start of 5-minute window
  final int detectionCount;

  const HeatmapBucket({
    required this.bucketStart,
    required this.detectionCount,
  });

  factory HeatmapBucket.fromJson(Map<String, dynamic> json) {
    return HeatmapBucket(
      bucketStart: json['bucket_start'] as String,
      detectionCount: (json['detection_count'] as num?)?.toInt() ?? 0,
    );
  }
}
