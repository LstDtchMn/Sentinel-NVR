// Timeline / Playback screen — 24h recording scrubber with heatmap overlay (Phase 11, R6, CG11).
// Mirrors the web Playback page: camera selector + date picker + TimelineBar + VideoPlayer.

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';
import 'package:provider/provider.dart';
import 'package:video_player/video_player.dart';

import '../models/camera.dart';
import '../models/event.dart';
import '../models/recording.dart';
import '../services/api_client.dart';
import '../widgets/timeline_bar.dart';

class TimelineScreen extends StatefulWidget {
  const TimelineScreen({super.key});

  @override
  State<TimelineScreen> createState() => _TimelineScreenState();
}

class _TimelineScreenState extends State<TimelineScreen> {
  List<CameraDetail> _cameras = [];
  CameraDetail? _selected;
  String _date = DateFormat('yyyy-MM-dd').format(DateTime.now());

  List<TimelineSegment> _segments = [];
  List<HeatmapBucket> _heatmap = [];
  TimelineSegment? _activeSegment;
  int _currentSecond = 0; // seconds since midnight, drives playhead

  VideoPlayerController? _videoCtrl;
  VoidCallback? _videoListener; // stored so we can removeListener before dispose
  bool _videoLoading = false;
  String? _error;

  // Generation counter: incremented at the start of each _playSegment call.
  // Each callback closure captures its own generation value and bails out if
  // _generation has advanced, preventing use-after-dispose when rapid timeline
  // taps trigger overlapping async calls.
  int _generation = 0;

  double _playbackRate = 1.0;
  static const _rates = [0.5, 1.0, 2.0, 4.0, 8.0];

  @override
  void initState() {
    super.initState();
    _loadCameras();
  }

  @override
  @override
  void dispose() {
    if (_videoCtrl != null && _videoListener != null) {
      _videoCtrl!.removeListener(_videoListener!);
    }
    _videoCtrl?.dispose();
    super.dispose();
  }

  Future<void> _loadCameras() async {
    try {
      final cameras = await context.read<ApiClient>().getCameras();
      if (mounted) {
        setState(() {
          _cameras = cameras.where((c) => c.enabled).toList();
          if (_cameras.isNotEmpty && _selected == null) {
            _selected = _cameras.first;
            _loadTimeline();
          }
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = 'Could not load cameras: $e');
    }
  }

  Future<void> _loadTimeline() async {
    if (_selected == null) return;
    try {
      final api = context.read<ApiClient>();
      final segments = await api.getTimeline(camera: _selected!.name, date: _date);
      final heatmap = await api.getEventHeatmap(cameraId: _selected!.id, date: _date);
      if (mounted) {
        if (_videoCtrl != null && _videoListener != null) {
          _videoCtrl!.removeListener(_videoListener!);
          _videoListener = null;
        }
        _videoCtrl?.dispose();
        setState(() {
          _videoCtrl = null;
          _segments = segments;
          _heatmap = heatmap;
          _activeSegment = null;
          _currentSecond = 0;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = 'Could not load timeline: $e');
    }
  }

  void _onTimelineSeek(int secondsSinceMidnight) {
    final seg = _segments.cast<TimelineSegment?>().firstWhere(
          (s) => s != null && secondsSinceMidnight >= s.startSecond && secondsSinceMidnight <= s.endSecond,
          orElse: () => null,
        );
    if (seg == null) return;

    final offset = secondsSinceMidnight - seg.startSecond;
    if (seg.id == _activeSegment?.id) {
      // Same segment — seek directly.
      _videoCtrl?.seekTo(Duration(seconds: offset));
      setState(() => _currentSecond = secondsSinceMidnight);
    } else {
      _playSegment(seg, initialOffset: offset);
    }
  }

  Future<void> _playSegment(TimelineSegment seg, {int initialOffset = 0}) async {
    if (_videoLoading) return; // prevent concurrent loads from rapid timeline taps
    final gen = ++_generation; // capture generation for stale-callback detection
    setState(() => _videoLoading = true);

    // Remove the old listener before disposing to prevent double-advance from
    // the old controller's listener firing after the new one is registered.
    if (_videoCtrl != null && _videoListener != null) {
      _videoCtrl!.removeListener(_videoListener!);
      _videoListener = null;
    }
    _videoCtrl?.dispose();
    _videoCtrl = null;

    final url = context.read<ApiClient>().recordingPlayUrl(seg.id);
    final ctrl = VideoPlayerController.networkUrl(Uri.parse(url));

    try {
      await ctrl.initialize();
      // Check generation: another _playSegment call may have started while we awaited.
      if (gen != _generation || !mounted) {
        ctrl.dispose();
        return;
      }
      ctrl.setPlaybackSpeed(_playbackRate);
      if (initialOffset > 0) {
        await ctrl.seekTo(Duration(seconds: initialOffset));
      }
      _videoListener = () {
        if (gen != _generation) return; // stale callback — a newer segment took over
        if (ctrl.value.isCompleted) {
          if (mounted) _onSegmentEnd();
        }
        if (!ctrl.value.isCompleted && ctrl.value.isPlaying) {
          final pos = ctrl.value.position;
          if (mounted) {
            setState(() {
              _currentSecond = seg.startSecond + pos.inSeconds;
            });
          }
        }
      };
      // Set _activeSegment before addListener so _onSegmentEnd sees the correct
      // segment if the listener fires immediately on play() (e.g. very short clips).
      _activeSegment = seg;
      ctrl.addListener(_videoListener!);
      await ctrl.play();

      if (gen != _generation || !mounted) {
        // Widget disposed or another segment started while awaiting play().
        ctrl.removeListener(_videoListener!);
        _videoListener = null;
        ctrl.dispose();
        return;
      }
      setState(() {
        _videoCtrl = ctrl;
        _videoLoading = false;
        _currentSecond = seg.startSecond + initialOffset;
      });
    } catch (e) {
      ctrl.dispose();
      _videoListener = null;
      if (gen == _generation && mounted) setState(() => _videoLoading = false);
    }
  }

  void _onSegmentEnd() {
    if (!mounted || _activeSegment == null) return;
    final idx = _segments.indexWhere((s) => s.id == _activeSegment!.id);
    if (idx >= 0 && idx < _segments.length - 1) {
      _playSegment(_segments[idx + 1]);
    }
  }

  Future<void> _pickDate() async {
    final picked = await showDatePicker(
      context: context,
      initialDate: DateTime.tryParse(_date) ?? DateTime.now(),
      firstDate: DateTime(2020),
      lastDate: DateTime.now(),
    );
    if (picked != null && mounted) {
      setState(() => _date = DateFormat('yyyy-MM-dd').format(picked));
      _loadTimeline();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Playback'),
        actions: [
          // Date picker button.
          TextButton.icon(
            icon: const Icon(Icons.calendar_today, size: 18),
            label: Text(_date),
            onPressed: _pickDate,
          ),
        ],
      ),
      body: Column(
        children: [
          // Camera selector.
          if (_cameras.isNotEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
              child: DropdownButton<CameraDetail>(
                value: _selected,
                isExpanded: true,
                underline: const SizedBox.shrink(),
                items: _cameras
                    .map((c) => DropdownMenuItem(value: c, child: Text(c.name)))
                    .toList(),
                onChanged: (cam) {
                  setState(() => _selected = cam);
                  _loadTimeline();
                },
              ),
            ),

          // Error banner.
          if (_error != null)
            Container(
              margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
              padding: const EdgeInsets.all(10),
              decoration: BoxDecoration(
                color: const Color(0xFFF85149).withOpacity(0.12),
                borderRadius: BorderRadius.circular(8),
              ),
              child: Text(_error!, style: const TextStyle(color: Color(0xFFF85149))),
            ),

          // Video player.
          Expanded(
            child: Container(
              color: Colors.black,
              child: Builder(builder: (context) {
                if (_videoLoading) {
                  return const Center(child: CircularProgressIndicator());
                }
                if (_videoCtrl != null && _videoCtrl!.value.isInitialized) {
                  return Column(
                    children: [
                      Expanded(
                        child: AspectRatio(
                          aspectRatio: _videoCtrl!.value.aspectRatio,
                          child: VideoPlayer(_videoCtrl!),
                        ),
                      ),
                      _PlaybackControls(
                        controller: _videoCtrl!,
                        playbackRate: _playbackRate,
                        rates: _rates,
                        onRateChange: (r) {
                          setState(() => _playbackRate = r);
                          _videoCtrl!.setPlaybackSpeed(r);
                        },
                      ),
                    ],
                  );
                }
                return Center(
                  child: Text(
                    _segments.isEmpty ? 'No recordings on this date' : 'Tap the timeline to play',
                    style: const TextStyle(color: Colors.grey),
                  ),
                );
              }),
            ),
          ),

          // Timeline bar.
          TimelineBar(
            segments: _segments,
            heatmapBuckets: _heatmap,
            currentSecond: _currentSecond,
            onSeek: _onTimelineSeek,
          ),
        ],
      ),
    );
  }
}

class _PlaybackControls extends StatelessWidget {
  final VideoPlayerController controller;
  final double playbackRate;
  final List<double> rates;
  final void Function(double) onRateChange;

  const _PlaybackControls({
    required this.controller,
    required this.playbackRate,
    required this.rates,
    required this.onRateChange,
  });

  String _formatDuration(Duration d) {
    final m = d.inMinutes.remainder(60).toString().padLeft(2, '0');
    final s = d.inSeconds.remainder(60).toString().padLeft(2, '0');
    return '$m:$s';
  }

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<VideoPlayerValue>(
      valueListenable: controller,
      builder: (context, value, _) {
        final pos = value.position;
        final dur = value.duration;
        return Container(
          color: const Color(0xFF161B22),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
          child: Row(
            children: [
              IconButton(
                icon: Icon(value.isPlaying ? Icons.pause : Icons.play_arrow),
                onPressed: () =>
                    value.isPlaying ? controller.pause() : controller.play(),
              ),
              Text(
                '${_formatDuration(pos)} / ${_formatDuration(dur)}',
                style: const TextStyle(fontSize: 12, color: Colors.grey),
              ),
              const Spacer(),
              // Speed selector.
              DropdownButton<double>(
                value: playbackRate,
                underline: const SizedBox.shrink(),
                isDense: true,
                items: rates
                    .map((r) => DropdownMenuItem(
                        value: r, child: Text('${r}x', style: const TextStyle(fontSize: 12))))
                    .toList(),
                onChanged: (r) { if (r != null) onRateChange(r); },
              ),
            ],
          ),
        );
      },
    );
  }
}
