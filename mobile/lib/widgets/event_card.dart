// Event list card — thumbnail, label, confidence, timestamp, type badge (Phase 11, CG11, R9).

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';
import 'package:provider/provider.dart';

import '../models/event.dart';
import '../services/api_client.dart';

class EventCard extends StatelessWidget {
  final EventRecord event;
  final VoidCallback? onTap;

  const EventCard({super.key, required this.event, this.onTap});

  Color _typeColor(String type) {
    if (type == 'face_match') return const Color(0xFFA855F7); // purple-400
    if (type == 'audio_detection') return const Color(0xFFFBBF24); // amber-400
    if (type.startsWith('detection')) return const Color(0xFF58A6FF);
    if (type.startsWith('camera')) return Colors.orange;
    if (type.startsWith('recording')) return Colors.green;
    return Colors.grey;
  }

  String _formatTime(String iso) {
    final dt = DateTime.tryParse(iso)?.toLocal();
    if (dt == null) return iso;
    return DateFormat('HH:mm:ss').format(dt);
  }

  @override
  Widget build(BuildContext context) {
    final api = context.read<ApiClient>();
    final hasThumbnail = event.thumbnail.isNotEmpty;

    return InkWell(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        decoration: BoxDecoration(
          border: Border(
            bottom: BorderSide(color: Colors.white.withOpacity(0.08)),
          ),
        ),
        child: Row(
          children: [
            // Thumbnail or placeholder.
            ClipRRect(
              borderRadius: BorderRadius.circular(4),
              child: SizedBox(
                width: 72,
                height: 48,
                child: hasThumbnail
                    ? Image.network(
                        api.eventThumbnailUrl(event.id),
                        fit: BoxFit.cover,
                        errorBuilder: (_, __, ___) => _placeholder,
                      )
                    : _placeholder,
              ),
            ),
            const SizedBox(width: 12),

            // Info.
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      // Event type badge.
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                        decoration: BoxDecoration(
                          color: _typeColor(event.type).withOpacity(0.2),
                          borderRadius: BorderRadius.circular(4),
                          border: Border.all(
                              color: _typeColor(event.type).withOpacity(0.5)),
                        ),
                        child: Text(
                          event.type,
                          style: TextStyle(
                              color: _typeColor(event.type), fontSize: 10),
                        ),
                      ),
                      if (event.label.isNotEmpty) ...[
                        const SizedBox(width: 6),
                        Text(
                          event.label,
                          style: const TextStyle(
                              fontWeight: FontWeight.w600, fontSize: 13),
                        ),
                        if (event.confidence > 0) ...[
                          const SizedBox(width: 4),
                          Text(
                            '${(event.confidence * 100).round()}%',
                            style: const TextStyle(color: Colors.grey, fontSize: 11),
                          ),
                        ],
                      ],
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(
                    _formatTime(event.startTime),
                    style: const TextStyle(color: Colors.grey, fontSize: 11),
                  ),
                ],
              ),
            ),

            const Icon(Icons.chevron_right, color: Colors.grey, size: 20),
          ],
        ),
      ),
    );
  }

  Widget get _placeholder => Container(
        color: const Color(0xFF161B22),
        child: const Center(
          child: Icon(Icons.image_not_supported, color: Colors.grey, size: 20),
        ),
      );
}
