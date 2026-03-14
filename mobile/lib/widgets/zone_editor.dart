// Zone editor widget — draw inclusion/exclusion polygons on a camera snapshot (Phase 11, R5).
// Renders on top of Image.network; normalizes tap coordinates to [0,1].

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../models/camera.dart';
import '../services/api_client.dart';

class ZoneEditor extends StatefulWidget {
  final CameraDetail camera;

  /// Called with the updated zones list when the user taps Save.
  final void Function(List<Zone>) onSave;

  const ZoneEditor({super.key, required this.camera, required this.onSave});

  @override
  State<ZoneEditor> createState() => _ZoneEditorState();
}

class _ZoneEditorState extends State<ZoneEditor> {
  late List<Zone> _zones;
  final List<ZonePoint> _currentPoints = [];
  String _activeType = 'include';
  bool _saving = false;
  String? _error;
  int _nameCounter = 1;

  // Capture the image render box size for coordinate normalization.
  final _imageKey = GlobalKey();

  @override
  void initState() {
    super.initState();
    _zones = List.from(widget.camera.zones);
    _nameCounter = _zones.length + 1;
  }

  Size? _imageSize() {
    final box = _imageKey.currentContext?.findRenderObject() as RenderBox?;
    return box?.size;
  }

  void _onTap(TapDownDetails details) {
    final size = _imageSize();
    if (size == null) return;
    final x = (details.localPosition.dx / size.width).clamp(0.0, 1.0);
    final y = (details.localPosition.dy / size.height).clamp(0.0, 1.0);
    setState(() => _currentPoints.add(ZonePoint(x: x, y: y)));
  }

  void _closePolygon() {
    if (_currentPoints.length < 3) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Need at least 3 points to create a zone')),
      );
      return;
    }
    final zone = Zone(
      id: DateTime.now().millisecondsSinceEpoch.toString(),
      name: 'Zone $_nameCounter',
      type: _activeType,
      points: List.from(_currentPoints),
    );
    setState(() {
      _zones.add(zone);
      _currentPoints.clear();
      _nameCounter++;
    });
  }

  void _deleteZone(String id) {
    setState(() => _zones.removeWhere((z) => z.id == id));
  }

  void _clearCurrent() {
    setState(() => _currentPoints.clear());
  }

  Future<void> _save() async {
    setState(() => _saving = true);
    try {
      final api = context.read<ApiClient>();
      final body = widget.camera.toJson();
      body['zones'] = _zones.map((z) => z.toJson()).toList();
      await api.updateCamera(widget.camera.name, body);
      widget.onSave(_zones);
    } catch (e) {
      if (mounted) setState(() => _error = 'Save failed: $e');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final api = context.read<ApiClient>();

    return Scaffold(
      appBar: AppBar(
        title: Text('Zones — ${widget.camera.name}'),
        actions: [
          if (_saving)
            const Padding(
              padding: EdgeInsets.all(12),
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          else
            TextButton(
              onPressed: _save,
              child: const Text('Save'),
            ),
        ],
      ),
      body: Column(
        children: [
          if (_error != null)
            Container(
              padding: const EdgeInsets.all(8),
              color: const Color(0xFFF85149).withOpacity(0.15),
              child: Text(_error!, style: const TextStyle(color: Color(0xFFF85149))),
            ),

          // Zone type selector.
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            child: SegmentedButton<String>(
              segments: const [
                ButtonSegment(value: 'include', label: Text('Include')),
                ButtonSegment(value: 'exclude', label: Text('Exclude')),
              ],
              selected: {_activeType},
              onSelectionChanged: (s) => setState(() => _activeType = s.first),
            ),
          ),

          // Camera snapshot with tap-to-add-vertex overlay.
          Expanded(
            child: Stack(
              children: [
                // Background snapshot.
                GestureDetector(
                  onTapDown: _onTap,
                  child: SizedBox.expand(
                    key: _imageKey,
                    child: Image.network(
                      api.cameraSnapshotUrl(widget.camera.name),
                      fit: BoxFit.contain,
                      errorBuilder: (_, __, ___) => const Center(
                        child: Text('Snapshot unavailable', style: TextStyle(color: Colors.grey)),
                      ),
                    ),
                  ),
                ),

                // Zones + current-points overlay.
                CustomPaint(
                  painter: _ZonePainter(
                    zones: _zones,
                    currentPoints: _currentPoints,
                    activeType: _activeType,
                  ),
                  child: const SizedBox.expand(),
                ),
              ],
            ),
          ),

          // Zone controls.
          Container(
            color: const Color(0xFF161B22),
            padding: const EdgeInsets.all(12),
            child: Column(
              children: [
                Row(
                  children: [
                    Expanded(
                      child: OutlinedButton(
                        onPressed: _currentPoints.isEmpty ? null : _closePolygon,
                        child: Text('Close zone (${_currentPoints.length} pts)'),
                      ),
                    ),
                    const SizedBox(width: 8),
                    OutlinedButton(
                      onPressed: _currentPoints.isEmpty ? null : _clearCurrent,
                      child: const Text('Clear'),
                    ),
                  ],
                ),
                if (_zones.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  SizedBox(
                    height: 36,
                    child: ListView.separated(
                      scrollDirection: Axis.horizontal,
                      itemCount: _zones.length,
                      separatorBuilder: (_, __) => const SizedBox(width: 8),
                      itemBuilder: (_, i) {
                        final z = _zones[i];
                        return Chip(
                          label: Text(z.name, style: const TextStyle(fontSize: 11)),
                          deleteIcon: const Icon(Icons.close, size: 14),
                          onDeleted: () => _deleteZone(z.id),
                          backgroundColor: z.type == 'exclude'
                              ? const Color(0xFFF85149).withOpacity(0.2)
                              : const Color(0xFF58A6FF).withOpacity(0.2),
                        );
                      },
                    ),
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ZonePainter extends CustomPainter {
  final List<Zone> zones;
  final List<ZonePoint> currentPoints;
  final String activeType;

  _ZonePainter({
    required this.zones,
    required this.currentPoints,
    required this.activeType,
  });

  @override
  void paint(Canvas canvas, Size size) {
    // Draw saved zones.
    for (final zone in zones) {
      if (zone.points.length < 2) continue;
      final isExclude = zone.type == 'exclude';
      final color = isExclude ? const Color(0xFFF85149) : const Color(0xFF58A6FF);
      final path = Path();
      final first = zone.points.first;
      path.moveTo(first.x * size.width, first.y * size.height);
      for (int i = 1; i < zone.points.length; i++) {
        path.lineTo(zone.points[i].x * size.width, zone.points[i].y * size.height);
      }
      path.close();
      canvas.drawPath(path, Paint()..color = color.withOpacity(0.2));
      canvas.drawPath(
          path,
          Paint()
            ..color = color
            ..style = PaintingStyle.stroke
            ..strokeWidth = 2);
    }

    // Draw in-progress polygon.
    if (currentPoints.isEmpty) return;
    final color = activeType == 'exclude'
        ? const Color(0xFFF85149)
        : const Color(0xFF58A6FF);

    // Vertex dots.
    final dotPaint = Paint()..color = color;
    for (final pt in currentPoints) {
      canvas.drawCircle(Offset(pt.x * size.width, pt.y * size.height), 5, dotPaint);
    }

    // Lines connecting vertices.
    if (currentPoints.length >= 2) {
      final linePaint = Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.5;
      final path = Path();
      final first = currentPoints.first;
      path.moveTo(first.x * size.width, first.y * size.height);
      for (int i = 1; i < currentPoints.length; i++) {
        path.lineTo(currentPoints[i].x * size.width, currentPoints[i].y * size.height);
      }
      canvas.drawPath(path, linePaint);
    }
  }

  @override
  bool shouldRepaint(_ZonePainter old) =>
      old.zones != zones ||
      old.currentPoints != currentPoints ||
      old.activeType != activeType;
}
