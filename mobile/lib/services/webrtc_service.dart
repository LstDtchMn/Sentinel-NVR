// WebRTC live streaming service via the Sentinel backend WebSocket proxy (Phase 11, CG11).
//
// go2rtc uses a server-offers model over the existing WS relay at
// /api/v1/streams/{name}/ws.  The signaling protocol:
//
//   Client → WS: {"type":"webrtc"}                              announce mode
//   Server → WS: {"type":"webrtc/offer","value":{type,sdp}}     go2rtc offer
//   Client → WS: {"type":"webrtc/answer","value":{type,sdp}}    our answer
//   Both   ↔ WS: {"type":"webrtc/candidate","value":{...}}      ICE candidates
//
// The backend stream_proxy.go relays all messages bidirectionally, so this
// service needs no knowledge of go2rtc's internal address.

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_webrtc/flutter_webrtc.dart';

class WebRtcService {
  RTCPeerConnection? _pc;
  WebSocket? _ws;
  StreamSubscription? _wsSub; // stored so _cleanup() can cancel it
  final RTCVideoRenderer remoteRenderer = RTCVideoRenderer();
  bool _disposed = false;

  /// Called when the first remote video track arrives and [remoteRenderer]
  /// has a source object.  Widgets use this to transition from "connecting"
  /// spinner to the live video view.
  VoidCallback? onTrackReceived;

  /// Initialise the renderer.  Must be called before [connect].
  Future<void> initialize() async {
    await remoteRenderer.initialize();
  }

  /// Connect to the backend WS proxy at [wsUrl] and start WebRTC negotiation.
  /// Pass [cookieHeader] (from ApiClient.cookieHeader) to authenticate the
  /// WebSocket upgrade request.
  /// If [iceServers] is provided, use those instead of the default public STUN server.
  /// Fetch from GET /api/v1/relay/ice-servers for dynamic config (Phase 12, CG11).
  Future<void> connect(
    String wsUrl, {
    String? cookieHeader,
    List<Map<String, dynamic>>? iceServers,
  }) async {
    await _cleanup(disposeRenderer: false);
    _disposed = false;

    final headers = <String, dynamic>{};
    if (cookieHeader != null) headers[HttpHeaders.cookieHeader] = cookieHeader;

    _ws = await WebSocket.connect(wsUrl, headers: headers);

    // ICE configuration — use dynamic servers from /relay/ice-servers when available,
    // fall back to public STUN for local-network use (Phase 12, CG11).
    final iceConfig = iceServers ?? [
      {'urls': 'stun:stun.l.google.com:19302'},
    ];

    _pc = await createPeerConnection({
      'iceServers': iceConfig,
      'sdpSemantics': 'unified-plan',
    });

    _pc!.onTrack = (RTCTrackEvent event) {
      if (event.streams.isNotEmpty && !_disposed) {
        remoteRenderer.srcObject = event.streams[0];
        onTrackReceived?.call();
      }
    };

    _pc!.onIceCandidate = (RTCIceCandidate candidate) {
      if (_ws != null && candidate.candidate != null) {
        _wsSend({
          'type': 'webrtc/candidate',
          'value': {
            'candidate': candidate.candidate,
            'sdpMid': candidate.sdpMid,
            'sdpMLineIndex': candidate.sdpMLineIndex,
          },
        });
      }
    };

    // Listen for messages from go2rtc via the backend relay.
    // Store subscription so _cleanup() can cancel it on dispose.
    _wsSub = _ws!.listen(
      _onMessage,
      onError: (_) {},
      onDone: () {},
      cancelOnError: false,
    );

    // Announce WebRTC mode — go2rtc will respond with the SDP offer.
    _wsSend({'type': 'webrtc'});
  }

  void _onMessage(dynamic raw) async {
    if (_disposed || _pc == null) return;
    try {
      final msg = jsonDecode(raw as String) as Map<String, dynamic>;
      final type = msg['type'] as String?;

      if (type == 'webrtc/offer') {
        final value = msg['value'] as Map<String, dynamic>;
        await _pc!.setRemoteDescription(RTCSessionDescription(
          value['sdp'] as String,
          value['type'] as String,
        ));
        // Check _disposed after each await — dispose() may have been called
        // while we were suspended in an async call.
        if (_disposed || _pc == null) return;
        final answer = await _pc!.createAnswer();
        if (_disposed || _pc == null) return;
        await _pc!.setLocalDescription(answer);
        if (_disposed) return;
        _wsSend({
          'type': 'webrtc/answer',
          'value': {'type': answer.type, 'sdp': answer.sdp},
        });
      } else if (type == 'webrtc/candidate') {
        final value = msg['value'] as Map<String, dynamic>;
        if (_disposed || _pc == null) return;
        await _pc!.addCandidate(RTCIceCandidate(
          value['candidate'] as String?,
          value['sdpMid'] as String?,
          value['sdpMLineIndex'] as int?,
        ));
        if (_disposed || _pc == null) return;
      }
    } catch (_) {
      // Malformed message — ignore; connection continues.
    }
  }

  void _wsSend(Map<String, dynamic> msg) {
    _ws?.add(jsonEncode(msg));
  }

  /// Release all resources.  Safe to call multiple times.
  Future<void> dispose() async {
    _disposed = true;
    await _cleanup(disposeRenderer: true);
  }

  Future<void> _cleanup({required bool disposeRenderer}) async {
    await _wsSub?.cancel();
    _wsSub = null;
    await _ws?.close();
    _ws = null;
    await _pc?.close();
    _pc = null;
    remoteRenderer.srcObject = null;
    if (disposeRenderer) {
      await remoteRenderer.dispose();
    }
  }
}
