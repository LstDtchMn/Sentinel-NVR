// REST API client for Sentinel NVR backend (Phase 11, CG11, R8).
// Mirrors frontend/src/api/client.ts — every method maps 1:1 to a backend endpoint.
// Uses Dio with PersistCookieJar so the sentinel_access + sentinel_refresh httpOnly-equivalent
// cookies survive app restarts (matching the 7-day refresh-token TTL on the backend).

import 'dart:convert';
import 'dart:io';

import 'package:cookie_jar/cookie_jar.dart';
import 'package:dio/dio.dart';
import 'package:dio_cookie_manager/dio_cookie_manager.dart';
import 'package:flutter/foundation.dart';
import 'package:path_provider/path_provider.dart';

import '../models/camera.dart';
import '../models/event.dart';
import '../models/notification_prefs.dart';
import '../models/recording.dart';

class ApiClient extends ChangeNotifier {
  Dio? _dio;
  PersistCookieJar? _cookieJar;
  String _baseUrl = '';
  bool _configured = false;

  // Callbacks wired by AuthService to break the circular dependency.
  Future<void> Function()? onRefresh;
  Future<void> Function()? onLogout;

  bool get isConfigured => _configured;

  /// Initialise or reinitialise the Dio instance with a new host URL.
  /// Discards any existing cookie jar so stale cookies are not replayed
  /// to a different server.
  Future<void> configure(String hostUrl) async {
    final trimmed = hostUrl.trimRight().replaceAll(RegExp(r'/$'), '');
    _baseUrl = '$trimmed/api/v1';

    final dir = await getApplicationDocumentsDirectory();
    _cookieJar = PersistCookieJar(
      storage: FileStorage('${dir.path}/cookies/'),
    );

    _dio = Dio(BaseOptions(
      baseUrl: _baseUrl,
      connectTimeout: const Duration(seconds: 10),
      receiveTimeout: const Duration(seconds: 10),
      headers: {'Accept': 'application/json'},
    ));

    _dio!.interceptors.add(CookieManager(_cookieJar!));
    _dio!.interceptors.add(_buildRetryInterceptor());

    _configured = true;
    notifyListeners();
  }

  /// Build the 401-retry interceptor.  On a protected endpoint returning 401:
  ///  1. POST /auth/refresh — if successful, retry the original request once.
  ///  2. If refresh also returns 401, call onLogout() to clear state + navigate.
  InterceptorsWrapper _buildRetryInterceptor() {
    return InterceptorsWrapper(
      onError: (DioException error, ErrorInterceptorHandler handler) async {
        if (error.response?.statusCode != 401) {
          return handler.next(error);
        }
        // Avoid retry loops on two fronts:
        //  1. Don't retry auth endpoints themselves (refresh/login).
        //  2. Don't retry a request that was already retried once (_retried extra flag).
        final path = error.requestOptions.path;
        if (path.contains('/auth/refresh') || path.contains('/auth/login')) {
          return handler.next(error);
        }
        if (error.requestOptions.extra.containsKey('_retried')) {
          await onLogout?.call();
          return handler.next(error);
        }
        try {
          await onRefresh?.call();
          // Mark as retried so a second 401 on the retry triggers logout, not another loop.
          error.requestOptions.extra['_retried'] = true;
          final retryResp = await _dio!.fetch(error.requestOptions);
          return handler.resolve(retryResp);
        } catch (_) {
          await onLogout?.call();
          return handler.next(error);
        }
      },
    );
  }

  Dio get _client {
    assert(_dio != null, 'ApiClient.configure() must be called before use');
    return _dio!;
  }

  // ── URL builders ──────────────────────────────────────────────────────────

  /// WebSocket URL for WebRTC live streaming via the backend proxy (CG3).
  String streamWsUrl(String cameraName) {
    final ws = _baseUrl.replaceFirst('https://', 'wss://').replaceFirst('http://', 'ws://');
    return '$ws/streams/${Uri.encodeComponent(cameraName)}/ws';
  }

  String recordingPlayUrl(int id) => '$_baseUrl/recordings/$id/play';
  String eventThumbnailUrl(int id) => '$_baseUrl/events/$id/thumbnail';
  String cameraSnapshotUrl(String name) => '$_baseUrl/cameras/${Uri.encodeComponent(name)}/snapshot';

  /// Serialize the cookie jar's cookies for a given URL into a Cookie header string.
  /// Used to authenticate the WebSocket upgrade request.
  /// The cookie jar stores cookies keyed to http(s) URLs, so ws(s) schemes are
  /// converted before lookup — otherwise loadForRequest finds nothing.
  Future<String?> cookieHeader(String url) async {
    if (_cookieJar == null) return null;
    final httpUrl = url
        .replaceFirst('wss://', 'https://')
        .replaceFirst('ws://', 'http://');
    final uri = Uri.parse(httpUrl);
    final cookies = await _cookieJar!.loadForRequest(uri);
    if (cookies.isEmpty) return null;
    return cookies.map((c) => '${c.name}=${c.value}').join('; ');
  }

  /// Clear all stored cookies (called on logout).
  Future<void> clearCookies() async {
    await _cookieJar?.deleteAll();
  }

  // ── Auth ──────────────────────────────────────────────────────────────────

  Future<void> login(String username, String password) async {
    await _client.post('/auth/login', data: {
      'username': username,
      'password': password,
    });
  }

  Future<void> logout() async {
    await _client.post('/auth/logout');
  }

  Future<void> refreshSession() async {
    await _client.post('/auth/refresh');
  }

  Future<AuthUser> getMe() async {
    final resp = await _client.get('/auth/me');
    return AuthUser.fromJson(resp.data as Map<String, dynamic>);
  }

  Future<Map<String, dynamic>> checkSetup() async {
    final resp = await _client.get('/setup');
    return resp.data as Map<String, dynamic>;
  }

  Future<AuthUser> completeSetup(String username, String password) async {
    final resp = await _client.post('/setup', data: {
      'username': username,
      'password': password,
    });
    return AuthUser.fromJson((resp.data as Map<String, dynamic>)['user'] as Map<String, dynamic>);
  }

  // ── Health ────────────────────────────────────────────────────────────────

  Future<Map<String, dynamic>> getHealth() async {
    final resp = await _client.get('/health');
    return resp.data as Map<String, dynamic>;
  }

  // ── Cameras ───────────────────────────────────────────────────────────────

  Future<List<CameraDetail>> getCameras() async {
    final resp = await _client.get('/cameras');
    return (resp.data as List<dynamic>)
        .map((e) => CameraDetail.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<CameraDetail> getCamera(String name) async {
    final resp = await _client.get('/cameras/${Uri.encodeComponent(name)}');
    return CameraDetail.fromJson(resp.data as Map<String, dynamic>);
  }

  Future<CameraDetail> createCamera(Map<String, dynamic> input) async {
    final resp = await _client.post('/cameras', data: input);
    return CameraDetail.fromJson(resp.data as Map<String, dynamic>);
  }

  Future<CameraDetail> updateCamera(String name, Map<String, dynamic> input) async {
    final resp = await _client.put('/cameras/${Uri.encodeComponent(name)}', data: input);
    return CameraDetail.fromJson(resp.data as Map<String, dynamic>);
  }

  Future<void> deleteCamera(String name) async {
    await _client.delete('/cameras/${Uri.encodeComponent(name)}');
  }

  // ── Recordings ────────────────────────────────────────────────────────────

  Future<List<RecordingSegment>> getRecordings({
    String? camera,
    String? start,
    String? end,
    int limit = 100,
    int offset = 0,
  }) async {
    final resp = await _client.get('/recordings', queryParameters: {
      if (camera != null) 'camera': camera,
      if (start != null) 'start': start,
      if (end != null) 'end': end,
      'limit': limit,
      'offset': offset,
    });
    // Backend returns {"recordings": [...], "total": N}
    final map = resp.data as Map<String, dynamic>;
    final recordings = map['recordings'] as List<dynamic>? ?? const [];
    return recordings
        .map((e) => RecordingSegment.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<List<TimelineSegment>> getTimeline({
    required String camera,
    required String date,
  }) async {
    final resp = await _client.get('/recordings/timeline', queryParameters: {
      'camera': camera,
      'date': date,
    });
    final data = resp.data;
    if (data == null) return [];
    return (data as List<dynamic>)
        .map((e) => TimelineSegment.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<List<String>> getRecordingDays({
    required String camera,
    required String month,
  }) async {
    final resp = await _client.get('/recordings/days', queryParameters: {
      'camera': camera,
      'month': month,
    });
    return (resp.data as List<dynamic>).cast<String>();
  }

  Future<void> deleteRecording(int id) async {
    await _client.delete('/recordings/$id');
  }

  // ── Events ────────────────────────────────────────────────────────────────

  Future<Map<String, dynamic>> getEvents({
    int? cameraId,
    String? type,
    String? date,
    int limit = 50,
    int offset = 0,
  }) async {
    final resp = await _client.get('/events', queryParameters: {
      if (cameraId != null) 'camera_id': cameraId,
      if (type != null && type.isNotEmpty) 'type': type,
      if (date != null && date.isNotEmpty) 'date': date,
      'limit': limit,
      'offset': offset,
    });
    return resp.data as Map<String, dynamic>;
  }

  Future<EventRecord> getEvent(int id) async {
    final resp = await _client.get('/events/$id');
    return EventRecord.fromJson(resp.data as Map<String, dynamic>);
  }

  Future<void> deleteEvent(int id) async {
    await _client.delete('/events/$id');
  }

  Future<List<HeatmapBucket>> getEventHeatmap({
    required int cameraId,
    required String date,
  }) async {
    final resp = await _client.get('/events/heatmap', queryParameters: {
      'camera_id': cameraId,
      'date': date,
    });
    return (resp.data as List<dynamic>)
        .map((e) => HeatmapBucket.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// Server-Sent Events stream for live event updates (CG8).
  /// Yields parsed JSON objects, skipping heartbeat comment lines.
  /// Pass [cancelToken] to close the underlying HTTP connection when resubscribing
  /// or on widget disposal — ensures the server-side goroutine is released promptly.
  Stream<Map<String, dynamic>> subscribeEvents({CancelToken? cancelToken}) async* {
    final resp = await _client.get<ResponseBody>(
      '/events/stream',
      options: Options(responseType: ResponseType.stream),
      cancelToken: cancelToken,
    );
    var buffer = '';
    await for (final chunk in resp.data!.stream.transform(utf8.decoder)) {
      buffer += chunk;
      while (true) {
        final sepIdx = buffer.indexOf('\n\n');
        if (sepIdx == -1) break;
        final block = buffer.substring(0, sepIdx);
        buffer = buffer.substring(sepIdx + 2);
        for (final line in block.split('\n')) {
          if (line.startsWith(':')) continue; // heartbeat comment — skip
          if (line.startsWith('data: ')) {
            final jsonStr = line.substring(6).trim();
            if (jsonStr.isEmpty) continue;
            try {
              yield jsonDecode(jsonStr) as Map<String, dynamic>;
            } catch (_) {}
          }
        }
      }
    }
  }

  // ── Config ────────────────────────────────────────────────────────────────

  Future<Map<String, dynamic>> getConfig() async {
    final resp = await _client.get('/config');
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> updateConfig(Map<String, dynamic> input) async {
    final resp = await _client.put('/config', data: input);
    return resp.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> getStorageStats() async {
    final resp = await _client.get('/storage/stats');
    return resp.data as Map<String, dynamic>;
  }

  // ── Notifications ─────────────────────────────────────────────────────────

  Future<NotifToken> createNotifToken({
    required String provider,
    required String token,
    String? label,
  }) async {
    final resp = await _client.post('/notifications/tokens', data: {
      'provider': provider,
      'token': token,
      if (label != null) 'label': label,
    });
    return NotifToken.fromJson(resp.data as Map<String, dynamic>);
  }

  Future<List<NotifToken>> listNotifTokens() async {
    final resp = await _client.get('/notifications/tokens');
    return (resp.data as List<dynamic>)
        .map((e) => NotifToken.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<void> deleteNotifToken(int id) async {
    await _client.delete('/notifications/tokens/$id');
  }

  Future<List<NotifPref>> listNotifPrefs() async {
    final resp = await _client.get('/notifications/prefs');
    return (resp.data as List<dynamic>)
        .map((e) => NotifPref.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  Future<NotifPref> upsertNotifPref(Map<String, dynamic> input) async {
    final resp = await _client.put('/notifications/prefs', data: input);
    return NotifPref.fromJson(resp.data as Map<String, dynamic>);
  }

  Future<void> deleteNotifPref(int id) async {
    await _client.delete('/notifications/prefs/$id');
  }

  Future<List<NotifLogEntry>> listNotifLog({int limit = 50}) async {
    final resp = await _client.get('/notifications/log', queryParameters: {'limit': limit});
    return (resp.data as List<dynamic>)
        .map((e) => NotifLogEntry.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  // ── Remote Access / Pairing (Phase 12, CG11, R8) ──────────────────────────

  /// Redeems a pairing code scanned from the web UI QR code.
  /// On success, the backend sets httpOnly session cookies in the cookie jar.
  Future<void> redeemPairingCode(String code) async {
    await _client.post('/pairing/redeem', data: {'code': code});
  }

  /// Fetches ICE server configuration for WebRTC peer connections.
  /// Returns a list of maps with 'urls', optional 'username', 'credential'.
  Future<List<Map<String, dynamic>>> getIceServers() async {
    final resp = await _client.get('/relay/ice-servers');
    final data = resp.data as Map<String, dynamic>;
    return (data['ice_servers'] as List<dynamic>)
        .map((e) => e as Map<String, dynamic>)
        .toList();
  }
}
