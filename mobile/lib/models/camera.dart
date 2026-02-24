// Models mirroring frontend/src/api/client.ts CameraDetail + related shapes (Phase 11, CG11).

class CameraDetail {
  final int id;
  final String name;
  final bool enabled;
  final String mainStream;
  final String? subStream;
  final bool record;
  final bool detect;
  final String? onvifHost;
  final int? onvifPort;
  final String? onvifUser;
  final String createdAt;
  final String updatedAt;
  final PipelineStatus? pipelineStatus;
  final List<Zone> zones;

  const CameraDetail({
    required this.id,
    required this.name,
    required this.enabled,
    required this.mainStream,
    this.subStream,
    required this.record,
    required this.detect,
    this.onvifHost,
    this.onvifPort,
    this.onvifUser,
    required this.createdAt,
    required this.updatedAt,
    this.pipelineStatus,
    required this.zones,
  });

  factory CameraDetail.fromJson(Map<String, dynamic> json) {
    return CameraDetail(
      id: json['id'] as int,
      name: json['name'] as String,
      enabled: json['enabled'] as bool,
      mainStream: json['main_stream'] as String,
      subStream: json['sub_stream'] as String?,
      record: json['record'] as bool,
      detect: json['detect'] as bool,
      onvifHost: json['onvif_host'] as String?,
      onvifPort: json['onvif_port'] as int?,
      onvifUser: json['onvif_user'] as String?,
      createdAt: json['created_at'] as String,
      updatedAt: json['updated_at'] as String,
      pipelineStatus: json['pipeline_status'] != null
          ? PipelineStatus.fromJson(json['pipeline_status'] as Map<String, dynamic>)
          : null,
      zones: (json['zones'] as List<dynamic>? ?? [])
          .map((z) => Zone.fromJson(z as Map<String, dynamic>))
          .toList(),
    );
  }

  Map<String, dynamic> toJson() => {
        'name': name,
        'enabled': enabled,
        'main_stream': mainStream,
        if (subStream != null) 'sub_stream': subStream,
        'record': record,
        'detect': detect,
        if (onvifHost != null) 'onvif_host': onvifHost,
        if (onvifPort != null) 'onvif_port': onvifPort,
        if (onvifUser != null) 'onvif_user': onvifUser,
      };
}

class PipelineStatus {
  /// "idle" | "connecting" | "streaming" | "recording" | "error" | "stopped"
  final String state;
  final bool mainStreamActive;
  final bool subStreamActive;
  final bool recording;
  final bool detecting;
  final String? lastError;
  final String? connectedAt; // RFC3339, null when not yet connected

  const PipelineStatus({
    required this.state,
    required this.mainStreamActive,
    required this.subStreamActive,
    required this.recording,
    required this.detecting,
    this.lastError,
    this.connectedAt,
  });

  factory PipelineStatus.fromJson(Map<String, dynamic> json) {
    return PipelineStatus(
      state: json['state'] as String,
      mainStreamActive: json['main_stream_active'] as bool? ?? false,
      subStreamActive: json['sub_stream_active'] as bool? ?? false,
      recording: json['recording'] as bool? ?? false,
      detecting: json['detecting'] as bool? ?? false,
      lastError: json['last_error'] as String?,
      connectedAt: json['connected_at'] as String?,
    );
  }
}

/// Exclusion/inclusion zone drawn on the camera feed (Phase 9, R5).
class Zone {
  final String id;
  final String name;

  /// "include" | "exclude"
  final String type;

  /// Polygon vertices, normalized to [0.0, 1.0] relative to frame dimensions.
  final List<ZonePoint> points;

  const Zone({
    required this.id,
    required this.name,
    required this.type,
    required this.points,
  });

  factory Zone.fromJson(Map<String, dynamic> json) {
    return Zone(
      id: json['id'] as String,
      name: json['name'] as String,
      type: json['type'] as String,
      points: (json['points'] as List<dynamic>)
          .map((p) => ZonePoint.fromJson(p as Map<String, dynamic>))
          .toList(),
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'type': type,
        'points': points.map((p) => p.toJson()).toList(),
      };
}

class ZonePoint {
  final double x;
  final double y;

  const ZonePoint({required this.x, required this.y});

  factory ZonePoint.fromJson(Map<String, dynamic> json) {
    return ZonePoint(
      x: (json['x'] as num).toDouble(),
      y: (json['y'] as num).toDouble(),
    );
  }

  Map<String, dynamic> toJson() => {'x': x, 'y': y};
}
