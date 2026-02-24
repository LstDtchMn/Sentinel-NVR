// Camera grid tile — snapshot image with status dot and camera name overlay (Phase 11, CG11).
// Refreshes the snapshot every 10s via a periodic timer.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../models/camera.dart';
import '../services/api_client.dart';

class CameraTile extends StatefulWidget {
  final CameraDetail camera;

  const CameraTile({super.key, required this.camera});

  @override
  State<CameraTile> createState() => _CameraTileState();
}

class _CameraTileState extends State<CameraTile> {
  Timer? _refreshTimer;
  int _cacheBust = 0;

  @override
  void initState() {
    super.initState();
    _refreshTimer = Timer.periodic(const Duration(seconds: 10), (_) {
      if (mounted) setState(() => _cacheBust++);
    });
  }

  @override
  void dispose() {
    _refreshTimer?.cancel();
    super.dispose();
  }

  Color _statusColor(String? state) {
    switch (state) {
      case 'streaming':
      case 'recording':
        return Colors.green;
      case 'connecting':
        return Colors.orange;
      case 'error':
        return const Color(0xFFF85149);
      default:
        return Colors.grey;
    }
  }

  @override
  Widget build(BuildContext context) {
    final api = context.read<ApiClient>();
    final snapshotUrl =
        '${api.cameraSnapshotUrl(widget.camera.name)}?_cb=$_cacheBust';
    final status = widget.camera.pipelineStatus;

    return ClipRRect(
      borderRadius: BorderRadius.circular(4),
      child: Stack(
        fit: StackFit.expand,
        children: [
          // Snapshot image.
          Image.network(
            snapshotUrl,
            fit: BoxFit.cover,
            headers: const {'Accept': 'image/jpeg'},
            errorBuilder: (_, __, ___) => Container(
              color: const Color(0xFF0D1117),
              child: const Center(
                child: Icon(Icons.videocam_off, color: Colors.grey, size: 32),
              ),
            ),
            loadingBuilder: (_, child, progress) {
              if (progress == null) return child;
              return Container(
                color: const Color(0xFF0D1117),
                child: const Center(child: CircularProgressIndicator(strokeWidth: 2)),
              );
            },
          ),

          // Bottom gradient for readability.
          Positioned(
            bottom: 0,
            left: 0,
            right: 0,
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
              decoration: const BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.bottomCenter,
                  colors: [Colors.transparent, Colors.black87],
                ),
              ),
              child: Row(
                children: [
                  // Status dot.
                  Container(
                    width: 8,
                    height: 8,
                    decoration: BoxDecoration(
                      color: _statusColor(status?.state),
                      shape: BoxShape.circle,
                    ),
                  ),
                  const SizedBox(width: 6),
                  // Camera name.
                  Expanded(
                    child: Text(
                      widget.camera.name,
                      style: const TextStyle(
                          color: Colors.white,
                          fontSize: 11,
                          fontWeight: FontWeight.w600),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  // Recording badge.
                  if (status?.recording == true)
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
                      decoration: BoxDecoration(
                        color: const Color(0xFFF85149),
                        borderRadius: BorderRadius.circular(3),
                      ),
                      child: const Text('REC',
                          style: TextStyle(color: Colors.white, fontSize: 9)),
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}
