// Events screen — paginated AI detection + system event list with live SSE updates
// (Phase 11, CG8, CG11, R9).

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';
import 'package:provider/provider.dart';

import '../models/camera.dart';
import '../models/event.dart';
import '../services/api_client.dart';
import '../widgets/event_card.dart';

class EventsScreen extends StatefulWidget {
  const EventsScreen({super.key});

  @override
  State<EventsScreen> createState() => _EventsScreenState();
}

class _EventsScreenState extends State<EventsScreen> {
  List<EventRecord> _events = [];
  int _total = 0;
  bool _loading = true;
  String? _error;

  // Filters
  List<CameraDetail> _cameras = [];
  int? _filterCameraId;
  String _filterType = '';
  String _filterDate = '';

  static const _pageSize = 50;
  int _offset = 0;
  bool _hasMore = true;

  final _scrollCtrl = ScrollController();
  StreamSubscription<Map<String, dynamic>>? _sseSub;
  CancelToken? _sseCancelToken; // Dio cancel token to close the HTTP connection on resubscribe
  Timer? _sseReconnectTimer;

  @override
  void initState() {
    super.initState();
    _loadCameras();
    _loadEvents(reset: true);
    _subscribeSSE();
    _scrollCtrl.addListener(_onScroll);
  }

  @override
  void dispose() {
    _sseSub?.cancel();
    _sseCancelToken?.cancel('disposed');
    _sseReconnectTimer?.cancel();
    _scrollCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadCameras() async {
    try {
      final cameras = await context.read<ApiClient>().getCameras();
      if (mounted) setState(() => _cameras = cameras);
    } catch (_) {}
  }

  Future<void> _loadEvents({bool reset = false}) async {
    if (reset) {
      _offset = 0;
      _hasMore = true;
    }
    if (!_hasMore) return;

    setState(() {
      _loading = true;
      if (reset) _error = null;
    });

    try {
      final api = context.read<ApiClient>();
      final result = await api.getEvents(
        cameraId: _filterCameraId,
        type: _filterType,
        date: _filterDate,
        limit: _pageSize,
        offset: _offset,
      );

      final list = (result['events'] as List<dynamic>)
          .map((e) => EventRecord.fromJson(e as Map<String, dynamic>))
          .toList();
      final total = result['total'] as int? ?? list.length;

      if (mounted) {
        setState(() {
          if (reset) {
            _events = list;
          } else {
            _events = [..._events, ...list];
          }
          _total = total;
          _offset += list.length;
          _hasMore = _events.length < _total;
          _loading = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = 'Failed to load events: $e';
          _loading = false;
        });
      }
    }
  }

  void _subscribeSSE() {
    _sseSub?.cancel();
    // Cancel the Dio CancelToken so the previous HTTP connection is closed on the
    // server side and its goroutine is released promptly — not just the Dart subscription.
    _sseCancelToken?.cancel('resubscribing');
    _sseCancelToken = CancelToken();
    final api = context.read<ApiClient>();
    _sseSub = api.subscribeEvents(cancelToken: _sseCancelToken).listen(
      (data) {
        final event = EventRecord.fromJson(data);
        if (!_matchesFilters(event)) return;
        if (mounted) {
          setState(() {
            _events = [event, ..._events];
            _total++;
          });
        }
      },
      onError: (_) {
        // Reconnect after 5s — mirrors EventSource auto-reconnect behaviour.
        _sseReconnectTimer?.cancel();
        _sseReconnectTimer = Timer(const Duration(seconds: 5), () {
          if (mounted) _subscribeSSE();
        });
      },
      cancelOnError: false,
    );
  }

  bool _matchesFilters(EventRecord event) {
    if (_filterCameraId != null && event.cameraId != _filterCameraId) return false;
    if (_filterType.isNotEmpty && event.type != _filterType) return false;
    if (_filterDate.isNotEmpty) {
      // Use toLocal() before extracting the date string to avoid off-by-one
      // date mismatches when the server returns UTC timestamps and the device
      // timezone differs (mirrors DaysWithRecordings convention).
      final dt = DateTime.tryParse(event.startTime)?.toLocal();
      if (dt == null) return false;
      final dateStr = DateFormat('yyyy-MM-dd').format(dt);
      if (dateStr != _filterDate) return false;
    }
    return true;
  }

  void _onScroll() {
    if (_scrollCtrl.position.pixels >=
        _scrollCtrl.position.maxScrollExtent - 200) {
      if (!_loading && _hasMore) _loadEvents();
    }
  }

  void _applyFilter({int? cameraId, String? type, String? date}) {
    setState(() {
      if (cameraId != null) _filterCameraId = cameraId == -1 ? null : cameraId;
      if (type != null) _filterType = type;
      if (date != null) _filterDate = date;
    });
    _loadEvents(reset: true);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('Events${_total > 0 ? ' ($_total)' : ''}'),
        actions: [
          IconButton(
            icon: const Icon(Icons.filter_list),
            onPressed: _showFilterSheet,
            tooltip: 'Filter',
          ),
        ],
      ),
      body: Column(
        children: [
          // Active filter chips.
          if (_filterCameraId != null || _filterType.isNotEmpty || _filterDate.isNotEmpty)
            _FilterBar(
              cameraId: _filterCameraId,
              cameras: _cameras,
              type: _filterType,
              date: _filterDate,
              onClear: () => _applyFilter(cameraId: -1, type: '', date: ''),
            ),

          Expanded(
            child: Builder(builder: (context) {
              if (_loading && _events.isEmpty) {
                return const Center(child: CircularProgressIndicator());
              }
              if (_error != null && _events.isEmpty) {
                return Center(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(_error!,
                          style: const TextStyle(color: Color(0xFFF85149))),
                      const SizedBox(height: 12),
                      OutlinedButton(
                        onPressed: () => _loadEvents(reset: true),
                        child: const Text('Retry'),
                      ),
                    ],
                  ),
                );
              }
              if (_events.isEmpty) {
                return const Center(
                  child: Text('No events found', style: TextStyle(color: Colors.grey)),
                );
              }
              return ListView.builder(
                controller: _scrollCtrl,
                itemCount: _events.length + (_hasMore ? 1 : 0),
                itemBuilder: (context, i) {
                  if (i == _events.length) {
                    return const Padding(
                      padding: EdgeInsets.all(16),
                      child: Center(child: CircularProgressIndicator()),
                    );
                  }
                  final event = _events[i];
                  return EventCard(
                    event: event,
                    onTap: () => context.go('/events/${event.id}'),
                  );
                },
              );
            }),
          ),
        ],
      ),
    );
  }

  void _showFilterSheet() {
    showModalBottomSheet<void>(
      context: context,
      builder: (context) => _FilterSheet(
        cameras: _cameras,
        currentCameraId: _filterCameraId,
        currentType: _filterType,
        currentDate: _filterDate,
        onApply: ({required int? cameraId, required String type, required String date}) {
          Navigator.pop(context);
          _applyFilter(cameraId: cameraId ?? -1, type: type, date: date);
        },
      ),
    );
  }
}

class _FilterBar extends StatelessWidget {
  final int? cameraId;
  final List<CameraDetail> cameras;
  final String type;
  final String date;
  final VoidCallback onClear;

  const _FilterBar({
    required this.cameraId,
    required this.cameras,
    required this.type,
    required this.date,
    required this.onClear,
  });

  @override
  Widget build(BuildContext context) {
    final cameraName = cameraId != null
        ? cameras.cast<CameraDetail?>().firstWhere(
              (c) => c?.id == cameraId,
              orElse: () => null,
            )?.name ??
            'Camera #$cameraId'
        : null;

    return Container(
      height: 44,
      padding: const EdgeInsets.symmetric(horizontal: 12),
      child: Row(
        children: [
          const Text('Filters:', style: TextStyle(color: Colors.grey, fontSize: 12)),
          const SizedBox(width: 8),
          if (cameraName != null) _Chip(label: cameraName),
          if (type.isNotEmpty) _Chip(label: type),
          if (date.isNotEmpty) _Chip(label: date),
          const Spacer(),
          TextButton(
            onPressed: onClear,
            child: const Text('Clear', style: TextStyle(fontSize: 12)),
          ),
        ],
      ),
    );
  }
}

class _Chip extends StatelessWidget {
  final String label;
  const _Chip({required this.label});

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(right: 6),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: const Color(0xFF58A6FF).withOpacity(0.2),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: const Color(0xFF58A6FF).withOpacity(0.5)),
      ),
      child: Text(label, style: const TextStyle(fontSize: 11, color: Color(0xFF58A6FF))),
    );
  }
}

class _FilterSheet extends StatefulWidget {
  final List<CameraDetail> cameras;
  final int? currentCameraId;
  final String currentType;
  final String currentDate;
  final void Function({required int? cameraId, required String type, required String date}) onApply;

  const _FilterSheet({
    required this.cameras,
    required this.currentCameraId,
    required this.currentType,
    required this.currentDate,
    required this.onApply,
  });

  @override
  State<_FilterSheet> createState() => _FilterSheetState();
}

class _FilterSheetState extends State<_FilterSheet> {
  late int? _cameraId;
  late String _type;
  late String _date;

  static const _eventTypeLabels = <String, String>{
    '': 'All types',
    'detection': 'Detection',
    'face_match': 'Face match',
    'audio_detection': 'Audio detection',
    'camera.connected': 'Camera connected',
    'camera.disconnected': 'Camera disconnected',
    'recording.started': 'Recording started',
    'recording.stopped': 'Recording stopped',
  };

  @override
  void initState() {
    super.initState();
    _cameraId = widget.currentCameraId;
    _type = widget.currentType;
    _date = widget.currentDate;
  }

  Future<void> _pickDate() async {
    final picked = await showDatePicker(
      context: context,
      initialDate: _date.isNotEmpty ? DateTime.tryParse(_date) ?? DateTime.now() : DateTime.now(),
      firstDate: DateTime(2020),
      lastDate: DateTime.now(),
    );
    if (picked != null) {
      setState(() => _date = DateFormat('yyyy-MM-dd').format(picked));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('Filter Events',
              style: Theme.of(context).textTheme.titleMedium?.copyWith(fontWeight: FontWeight.bold)),
          const SizedBox(height: 16),

          // Camera filter.
          DropdownButtonFormField<int?>(
            value: _cameraId,
            decoration: const InputDecoration(labelText: 'Camera'),
            items: [
              const DropdownMenuItem(value: null, child: Text('All cameras')),
              ...widget.cameras.map((c) => DropdownMenuItem(value: c.id, child: Text(c.name))),
            ],
            onChanged: (v) => setState(() => _cameraId = v),
          ),
          const SizedBox(height: 12),

          // Event type filter.
          DropdownButtonFormField<String>(
            value: _type,
            decoration: const InputDecoration(labelText: 'Event type'),
            items: _eventTypeLabels.entries
                .map((e) => DropdownMenuItem(
                    value: e.key, child: Text(e.value)))
                .toList(),
            onChanged: (v) => setState(() => _type = v ?? ''),
          ),
          const SizedBox(height: 12),

          // Date filter.
          ListTile(
            contentPadding: EdgeInsets.zero,
            title: Text(_date.isEmpty ? 'Any date' : _date),
            subtitle: const Text('Date filter'),
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (_date.isNotEmpty)
                  IconButton(
                    icon: const Icon(Icons.clear),
                    onPressed: () => setState(() => _date = ''),
                  ),
                IconButton(
                  icon: const Icon(Icons.calendar_today),
                  onPressed: _pickDate,
                ),
              ],
            ),
          ),
          const SizedBox(height: 16),

          FilledButton(
            onPressed: () => widget.onApply(cameraId: _cameraId, type: _type, date: _date),
            child: const Text('Apply Filters'),
          ),
        ],
      ),
    );
  }
}
