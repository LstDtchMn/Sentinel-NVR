// 24h timeline bar with recording segment blocks and detection heatmap overlay (Phase 11, R6).
// Rendered via CustomPainter; supports tap + horizontal drag to seek.

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../models/event.dart';
import '../models/recording.dart';

class TimelineBar extends StatefulWidget {
  final List<TimelineSegment> segments;
  final List<HeatmapBucket> heatmapBuckets;
  final int currentSecond; // seconds since midnight, -1 = no playhead
  final void Function(int secondsSinceMidnight) onSeek;

  const TimelineBar({
    super.key,
    required this.segments,
    required this.heatmapBuckets,
    required this.currentSecond,
    required this.onSeek,
  });

  @override
  State<TimelineBar> createState() => _TimelineBarState();
}

class _TimelineBarState extends State<TimelineBar> {
  static const _barHeight = 56.0;
  static const _totalSeconds = 86400; // 24h in seconds

  void _onTapOrDrag(BuildContext context, Offset localPos) {
    final box = context.findRenderObject() as RenderBox?;
    if (box == null) return;
    final w = box.size.width;
    final frac = (localPos.dx / w).clamp(0.0, 1.0);
    widget.onSeek((frac * _totalSeconds).round());
  }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTapDown: (d) => _onTapOrDrag(context, d.localPosition),
      onHorizontalDragUpdate: (d) => _onTapOrDrag(context, d.localPosition),
      child: SizedBox(
        height: _barHeight,
        child: CustomPaint(
          painter: _TimelinePainter(
            segments: widget.segments,
            heatmapBuckets: widget.heatmapBuckets,
            currentSecond: widget.currentSecond,
          ),
          child: const SizedBox.expand(),
        ),
      ),
    );
  }
}

class _TimelinePainter extends CustomPainter {
  final List<TimelineSegment> segments;
  final List<HeatmapBucket> heatmapBuckets;
  final int currentSecond;

  static const _totalSeconds = 86400.0;
  static const _maxDensity = 20; // detection count clamped at this for max opacity

  _TimelinePainter({
    required this.segments,
    required this.heatmapBuckets,
    required this.currentSecond,
  });

  @override
  void paint(Canvas canvas, Size size) {
    final w = size.width;
    final h = size.height;

    // Background.
    canvas.drawRect(
      Rect.fromLTWH(0, 0, w, h),
      Paint()..color = const Color(0xFF161B22),
    );

    // Hour tick marks.
    final tickPaint = Paint()
      ..color = Colors.white12
      ..strokeWidth = 1;
    for (int hour = 0; hour <= 24; hour++) {
      final x = (hour * 3600 / _totalSeconds) * w;
      canvas.drawLine(Offset(x, h - 8), Offset(x, h), tickPaint);
    }

    // Recording segment blocks (white/grey).
    final segPaint = Paint()..color = const Color(0xFF58A6FF).withOpacity(0.6);
    for (final seg in segments) {
      final x0 = (seg.startSecond / _totalSeconds) * w;
      final x1 = (seg.endSecond / _totalSeconds) * w;
      canvas.drawRect(Rect.fromLTWH(x0, h * 0.55, x1 - x0, h * 0.35), segPaint);
    }

    // Heatmap overlay — blue gradient per 5-minute bucket (300s window).
    for (final bucket in heatmapBuckets) {
      // Convert to local time so bucket positions align with the timeline's
      // local-time coordinate system (mirrors TimelineSegment.startSecond).
      final t = DateTime.tryParse(bucket.bucketStart)?.toLocal();
      if (t == null) continue;
      final startSec = t.hour * 3600 + t.minute * 60 + t.second;
      final x0 = (startSec / _totalSeconds) * w;
      final x1 = ((startSec + 300) / _totalSeconds) * w; // 5-min window
      final opacity = (bucket.detectionCount / _maxDensity).clamp(0.0, 1.0);
      final heatPaint = Paint()
        ..color = const Color(0xFF58A6FF).withOpacity(opacity * 0.8);
      canvas.drawRect(Rect.fromLTWH(x0, h * 0.05, x1 - x0, h * 0.45), heatPaint);
    }

    // Playhead — red vertical line.
    if (currentSecond >= 0) {
      final px = (currentSecond / _totalSeconds) * w;
      canvas.drawLine(
        Offset(px, 0),
        Offset(px, h),
        Paint()
          ..color = const Color(0xFFF85149)
          ..strokeWidth = 2,
      );
      // Thumb triangle.
      final path = Path()
        ..moveTo(px - 5, 0)
        ..lineTo(px + 5, 0)
        ..lineTo(px, 8)
        ..close();
      canvas.drawPath(path, Paint()..color = const Color(0xFFF85149));
    }
  }

  @override
  bool shouldRepaint(_TimelinePainter old) {
    return old.currentSecond != currentSecond ||
        old.segments != segments ||
        old.heatmapBuckets != heatmapBuckets;
  }
}
