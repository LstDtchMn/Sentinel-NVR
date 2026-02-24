// Auth + notification models mirroring frontend/src/api/client.ts shapes (Phase 11, CG11).

class AuthUser {
  final int id;
  final String username;
  final String role;

  const AuthUser({
    required this.id,
    required this.username,
    required this.role,
  });

  factory AuthUser.fromJson(Map<String, dynamic> json) {
    return AuthUser(
      id: json['id'] as int,
      username: json['username'] as String,
      role: json['role'] as String,
    );
  }
}

/// Device token for FCM / APNs / webhook push delivery (Phase 8, R9).
class NotifToken {
  final int id;
  final int userId;
  final String token;

  /// "fcm" | "apns" | "webhook"
  final String provider;
  final String? label;
  final String createdAt;
  final String updatedAt;

  const NotifToken({
    required this.id,
    required this.userId,
    required this.token,
    required this.provider,
    this.label,
    required this.createdAt,
    required this.updatedAt,
  });

  factory NotifToken.fromJson(Map<String, dynamic> json) {
    return NotifToken(
      id: json['id'] as int,
      userId: json['user_id'] as int,
      token: json['token'] as String,
      provider: json['provider'] as String,
      label: json['label'] as String?,
      createdAt: json['created_at'] as String,
      updatedAt: json['updated_at'] as String,
    );
  }
}

/// Per-event-type + per-camera notification preference (Phase 8, R9).
class NotifPref {
  final int id;
  final int userId;
  final String eventType;
  final int? cameraId; // null = all cameras
  final bool enabled;
  final bool critical; // bypass DND on iOS
  final String createdAt;
  final String updatedAt;

  const NotifPref({
    required this.id,
    required this.userId,
    required this.eventType,
    this.cameraId,
    required this.enabled,
    required this.critical,
    required this.createdAt,
    required this.updatedAt,
  });

  factory NotifPref.fromJson(Map<String, dynamic> json) {
    return NotifPref(
      id: json['id'] as int,
      userId: json['user_id'] as int,
      eventType: json['event_type'] as String,
      cameraId: json['camera_id'] as int?,
      enabled: json['enabled'] as bool,
      critical: json['critical'] as bool,
      createdAt: json['created_at'] as String,
      updatedAt: json['updated_at'] as String,
    );
  }

  Map<String, dynamic> toUpsertJson() => {
        'event_type': eventType,
        if (cameraId != null) 'camera_id': cameraId,
        'enabled': enabled,
        'critical': critical,
      };
}

/// Delivery log entry for the notification history view.
class NotifLogEntry {
  final int id;
  final int eventId;
  final int tokenId;
  final String provider;
  final String title;
  final String body;
  final String? deepLink;
  final String status;
  final int attempts;
  final String? lastError;
  final String scheduledAt;
  final String? sentAt;

  const NotifLogEntry({
    required this.id,
    required this.eventId,
    required this.tokenId,
    required this.provider,
    required this.title,
    required this.body,
    this.deepLink,
    required this.status,
    required this.attempts,
    this.lastError,
    required this.scheduledAt,
    this.sentAt,
  });

  factory NotifLogEntry.fromJson(Map<String, dynamic> json) {
    return NotifLogEntry(
      id: json['id'] as int,
      eventId: json['event_id'] as int,
      tokenId: json['token_id'] as int,
      provider: json['provider'] as String,
      title: json['title'] as String,
      body: json['body'] as String,
      deepLink: json['deep_link'] as String?,
      status: json['status'] as String,
      attempts: json['attempts'] as int,
      lastError: json['last_error'] as String?,
      scheduledAt: json['scheduled_at'] as String,
      sentAt: json['sent_at'] as String?,
    );
  }
}
