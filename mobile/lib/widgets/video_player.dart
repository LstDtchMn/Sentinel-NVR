// WebRTC video player widget — wraps WebRtcService with loading/error states (Phase 11, CG11).
// Manages connection lifecycle: connect on build/active=true, dispose on unmount.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';

import '../services/webrtc_service.dart';

enum _PlayerState { idle, connecting, playing, error }

class WebRtcVideoPlayer extends StatefulWidget {
  final WebRtcService service;

  const WebRtcVideoPlayer({super.key, required this.service});

  @override
  State<WebRtcVideoPlayer> createState() => _WebRtcVideoPlayerState();
}

class _WebRtcVideoPlayerState extends State<WebRtcVideoPlayer> {
  _PlayerState _state = _PlayerState.connecting;
  Timer? _errorTimer;

  @override
  void initState() {
    super.initState();
    // The service is already initialized and connected by the parent screen
    // (LiveViewScreen._enterFocusMode).  Register onFirstFrameRendered to
    // transition to playing once the first video frame arrives.
    _listenToRenderer();
  }

  @override
  void dispose() {
    // Cancel the error timeout and clear the callback to prevent setState after unmount.
    _errorTimer?.cancel();
    widget.service.remoteRenderer.onFirstFrameRendered = null;
    super.dispose();
  }

  void _listenToRenderer() {
    // Transition to playing when the first decoded frame is rendered.
    widget.service.remoteRenderer.onFirstFrameRendered = () {
      if (mounted) setState(() => _state = _PlayerState.playing);
    };
    // Show error state if no frame arrives within 15 seconds (local-network
    // WebRTC negotiation should complete in under 5s; 15s handles slow paths).
    // Use a cancellable Timer so dispose() can cancel it and avoid setState after unmount.
    _errorTimer = Timer(const Duration(seconds: 15), () {
      if (mounted && _state == _PlayerState.connecting) {
        setState(() => _state = _PlayerState.error);
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      color: Colors.black,
      child: Stack(
        fit: StackFit.expand,
        children: [
          // WebRTC video view.
          RTCVideoView(
            widget.service.remoteRenderer,
            objectFit: RTCVideoViewObjectFit.RTCVideoViewObjectFitContain,
          ),

          // Loading overlay.
          if (_state == _PlayerState.connecting)
            const Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  CircularProgressIndicator(color: Color(0xFF58A6FF)),
                  SizedBox(height: 12),
                  Text('Connecting...', style: TextStyle(color: Colors.white70)),
                ],
              ),
            ),

          // Error overlay.
          if (_state == _PlayerState.error)
            const Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(Icons.videocam_off, color: Color(0xFFF85149), size: 48),
                  SizedBox(height: 8),
                  Text('Stream unavailable',
                      style: TextStyle(color: Color(0xFFF85149))),
                ],
              ),
            ),
        ],
      ),
    );
  }
}
