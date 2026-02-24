// Live view screen — camera grid with Focus Mode (Phase 11, CG11, R7).
// Polls GET /cameras every 30s.  Tap a tile → Focus Mode (WebRTC, full-screen).

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../models/camera.dart';
import '../services/api_client.dart';
import '../services/webrtc_service.dart';
import '../widgets/camera_tile.dart';
import '../widgets/video_player.dart';

class LiveViewScreen extends StatefulWidget {
  const LiveViewScreen({super.key});

  @override
  State<LiveViewScreen> createState() => _LiveViewScreenState();
}

class _LiveViewScreenState extends State<LiveViewScreen> {
  List<CameraDetail>? _cameras;
  String? _error;
  CameraDetail? _focused;
  WebRtcService? _webRtcSvc;
  Timer? _pollTimer;

  @override
  void initState() {
    super.initState();
    _fetchCameras();
    _pollTimer = Timer.periodic(const Duration(seconds: 30), (_) => _fetchCameras());
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    _webRtcSvc?.dispose();
    super.dispose();
  }

  Future<void> _fetchCameras() async {
    try {
      final api = context.read<ApiClient>();
      final cameras = await api.getCameras();
      if (mounted) {
        setState(() {
          _cameras = cameras.where((c) => c.enabled).toList();
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = 'Could not load cameras: $e');
    }
  }

  Future<void> _enterFocusMode(CameraDetail cam) async {
    // Dispose any active WebRTC session before starting a new one.
    // Without this, tapping a different camera tile while already in Focus Mode
    // would leak the old peer connection and WS subscription.
    final oldSvc = _webRtcSvc;
    if (oldSvc != null) {
      setState(() {
        _focused = null;
        _webRtcSvc = null;
      });
      await oldSvc.dispose();
    }

    final api = context.read<ApiClient>();
    final svc = WebRtcService();
    await svc.initialize();

    final wsUrl = api.streamWsUrl(cam.name);
    final cookieHeader = await api.cookieHeader(wsUrl);

    // Fetch dynamic ICE servers for WebRTC (Phase 12, CG11).
    List<Map<String, dynamic>>? iceServers;
    try {
      iceServers = await api.getIceServers();
    } catch (_) {
      // Fall back to hardcoded STUN if the endpoint fails.
    }

    // Guard: widget may have been disposed while awaiting ICE servers (Issue #5).
    if (!mounted) {
      await svc.dispose();
      return;
    }

    try {
      await svc.connect(wsUrl, cookieHeader: cookieHeader, iceServers: iceServers);
    } catch (_) {
      await svc.dispose();
      return;
    }

    if (mounted) {
      setState(() {
        _focused = cam;
        _webRtcSvc = svc;
      });
    } else {
      // Widget was disposed while connect() was in-flight — release native resources.
      await svc.dispose();
    }
  }

  Future<void> _exitFocusMode() async {
    await _webRtcSvc?.dispose();
    if (mounted) setState(() {
      _focused = null;
      _webRtcSvc = null;
    });
  }

  int _crossAxisCount(int count) {
    if (count <= 1) return 1;
    if (count <= 4) return 2;
    if (count <= 9) return 3;
    return 4;
  }

  @override
  Widget build(BuildContext context) {
    // Focus Mode: full-screen WebRTC player.
    if (_focused != null && _webRtcSvc != null) {
      return _FocusView(
        camera: _focused!,
        service: _webRtcSvc!,
        onClose: _exitFocusMode,
      );
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('Live View'),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: _fetchCameras,
            tooltip: 'Refresh',
          ),
        ],
      ),
      body: Builder(builder: (context) {
        if (_cameras == null && _error == null) {
          return const Center(child: CircularProgressIndicator());
        }
        if (_error != null) {
          return _ErrorState(message: _error!, onRetry: _fetchCameras);
        }
        if (_cameras!.isEmpty) {
          return const Center(
            child: Text('No cameras configured',
                style: TextStyle(color: Colors.grey)),
          );
        }
        return GridView.builder(
          padding: const EdgeInsets.all(4),
          gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: _crossAxisCount(_cameras!.length),
            childAspectRatio: 16 / 9,
            crossAxisSpacing: 4,
            mainAxisSpacing: 4,
          ),
          itemCount: _cameras!.length,
          itemBuilder: (context, i) {
            final cam = _cameras![i];
            return GestureDetector(
              onTap: () => _enterFocusMode(cam),
              child: CameraTile(camera: cam),
            );
          },
        );
      }),
    );
  }
}

/// Full-screen live stream player with camera name overlay and close button.
class _FocusView extends StatelessWidget {
  final CameraDetail camera;
  final WebRtcService service;
  final VoidCallback onClose;

  const _FocusView({
    required this.camera,
    required this.service,
    required this.onClose,
  });

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      body: SafeArea(
        child: Stack(
          children: [
            // Full-screen WebRTC video.
            WebRtcVideoPlayer(service: service),

            // Camera name + close button overlay.
            Positioned(
              top: 12,
              left: 12,
              right: 12,
              child: Row(
                children: [
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                    decoration: BoxDecoration(
                      color: Colors.black54,
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Text(
                      camera.name,
                      style: const TextStyle(
                          color: Colors.white, fontWeight: FontWeight.w600),
                    ),
                  ),
                  const Spacer(),
                  GestureDetector(
                    onTap: onClose,
                    child: Container(
                      padding: const EdgeInsets.all(8),
                      decoration: const BoxDecoration(
                        color: Colors.black54,
                        shape: BoxShape.circle,
                      ),
                      child: const Icon(Icons.close, color: Colors.white, size: 20),
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _ErrorState({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(message,
              style: const TextStyle(color: Color(0xFFF85149)),
              textAlign: TextAlign.center),
          const SizedBox(height: 16),
          OutlinedButton(onPressed: onRetry, child: const Text('Retry')),
        ],
      ),
    );
  }
}
