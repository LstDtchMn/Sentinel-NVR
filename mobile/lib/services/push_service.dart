// Push notification service — FCM (Android) + APNs via FCM bridge (iOS) (Phase 11, R9).
//
// Prerequisites (not automated here):
//   • Android: google-services.json in android/app/
//   • iOS: GoogleService-Info.plist in ios/Runner/
//   • AndroidManifest: INTERNET permission + FCM <service> declaration
//   • Info.plist: UIBackgroundModes [fetch, remote-notification]
//   • iOS entitlement: com.apple.developer.usernotifications.critical-alerts (R9)
//
// The Android notification channel ID "sentinel_alerts" must match the value
// sent by the backend's FCMSender.Send() in internal/notification/fcm.go.

import 'dart:async';
import 'dart:io';

import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import 'api_client.dart';

// Top-level background message handler — must be a top-level function (FCM requirement).
@pragma('vm:entry-point')
Future<void> _firebaseMessagingBackgroundHandler(RemoteMessage message) async {
  // Background messages are handled by the OS notification tray.
  // No processing needed here for Phase 11.
}

class PushService {
  static const _channelId = 'sentinel_alerts';
  static const _channelName = 'Sentinel Alerts';

  final FlutterLocalNotificationsPlugin _flnPlugin =
      FlutterLocalNotificationsPlugin();

  StreamSubscription<String>? _tokenRefreshSub;
  StreamSubscription<RemoteMessage>? _onMessageSub;
  StreamSubscription<RemoteMessage>? _onMessageOpenedSub;

  /// GoRouter navigate callback — wired by main.dart after router is created.
  void Function(String route)? onNavigate;

  Future<void> initialize(ApiClient api) async {
    // Register the background handler before any Firebase calls.
    FirebaseMessaging.onBackgroundMessage(_firebaseMessagingBackgroundHandler);

    // Request permission.  criticalAlert = true enables R9 DND-bypass on iOS
    // (requires Apple entitlement com.apple.developer.usernotifications.critical-alerts).
    await FirebaseMessaging.instance.requestPermission(
      alert: true,
      badge: true,
      sound: true,
      criticalAlert: true,
    );

    // Android: create the notification channel that backend fcm.go targets.
    if (Platform.isAndroid) {
      const channel = AndroidNotificationChannel(
        _channelId,
        _channelName,
        description: 'Camera detection and system alerts from Sentinel NVR',
        importance: Importance.high,
        enableVibration: true,
      );
      await _flnPlugin
          .resolvePlatformSpecificImplementation<
              AndroidFlutterLocalNotificationsPlugin>()
          ?.createNotificationChannel(channel);
    }

    // Initialise flutter_local_notifications for foreground message display.
    const androidInit = AndroidInitializationSettings('@mipmap/ic_launcher');
    const iosInit = DarwinInitializationSettings();
    await _flnPlugin.initialize(
      const InitializationSettings(android: androidInit, iOS: iosInit),
      onDidReceiveNotificationResponse: (details) {
        final payload = details.payload;
        if (payload != null && payload.isNotEmpty) {
          onNavigate?.call(payload);
        }
      },
    );

    // Get FCM token and register with the backend.
    final token = await FirebaseMessaging.instance.getToken();
    if (token != null) await _registerToken(token, api);

    // Re-register on token refresh (e.g. after app reinstall).
    _tokenRefreshSub = FirebaseMessaging.instance.onTokenRefresh.listen((t) => _registerToken(t, api));

    // Show local notification for foreground messages.
    _onMessageSub = FirebaseMessaging.onMessage.listen((message) {
      _showLocalNotification(message);
    });

    // Handle notification tap when app is in background.
    _onMessageOpenedSub = FirebaseMessaging.onMessageOpenedApp.listen((message) {
      _handleDeepLink(message);
    });

    // Handle notification tap that launched the app from terminated state.
    final initial = await FirebaseMessaging.instance.getInitialMessage();
    if (initial != null) _handleDeepLink(initial);
  }

  Future<void> _registerToken(String token, ApiClient api) async {
    try {
      await api.createNotifToken(
        provider: 'fcm',
        token: token,
        label: Platform.isIOS ? 'iOS' : 'Android',
      );
    } catch (_) {
      // Non-critical — registration will be retried on next app start.
    }
  }

  void _showLocalNotification(RemoteMessage message) {
    final notification = message.notification;
    if (notification == null) return;

    final deepLink = message.data['deep_link'] as String?;

    // TODO(review): L13 — notification.hashCode is unstable; use stable ID from message data
    _flnPlugin.show(
      notification.hashCode,
      notification.title,
      notification.body,
      NotificationDetails(
        android: AndroidNotificationDetails(
          _channelId,
          _channelName,
          icon: '@mipmap/ic_launcher',
          importance: Importance.high,
          priority: Priority.high,
        ),
        iOS: const DarwinNotificationDetails(
          sound: 'default',
        ),
      ),
      payload: deepLink,
    );
  }

  void dispose() {
    _tokenRefreshSub?.cancel();
    _onMessageSub?.cancel();
    _onMessageOpenedSub?.cancel();
  }

  void _handleDeepLink(RemoteMessage message) {
    final deepLink = message.data['deep_link'] as String?;
    if (deepLink != null && deepLink.isNotEmpty) {
      onNavigate?.call(deepLink);
    }
  }
}
