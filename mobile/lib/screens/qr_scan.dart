// QR code scanner screen for zero-config mobile pairing (Phase 12, CG11, R8).
// Scans a QR code from the web UI containing {"url":"...","code":"..."}.
// On successful scan, redeems the pairing code to establish a session.

import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:provider/provider.dart';

import '../services/api_client.dart';
import '../services/auth_service.dart';

class QrScanScreen extends StatefulWidget {
  const QrScanScreen({super.key});

  @override
  State<QrScanScreen> createState() => _QrScanScreenState();
}

class _QrScanScreenState extends State<QrScanScreen> {
  final MobileScannerController _scannerCtrl = MobileScannerController();
  bool _processing = false;
  String? _error;

  @override
  void dispose() {
    _scannerCtrl.dispose();
    super.dispose();
  }

  Future<void> _onDetect(BarcodeCapture capture) async {
    if (_processing) return; // Prevent multiple scans.
    final barcode = capture.barcodes.firstOrNull;
    if (barcode == null || barcode.rawValue == null) return;

    setState(() {
      _processing = true;
      _error = null;
    });

    try {
      // Parse QR payload.
      final data = jsonDecode(barcode.rawValue!) as Map<String, dynamic>;
      final url = data['url'] as String?;
      final code = data['code'] as String?;

      if (url == null || url.isEmpty || code == null || code.isEmpty) {
        throw const FormatException('Invalid QR code format');
      }

      // Validate URL format.
      final uri = Uri.tryParse(url);
      if (uri == null || !uri.hasScheme || !uri.scheme.startsWith('http')) {
        throw const FormatException('Invalid NVR URL in QR code');
      }

      final auth = context.read<AuthService>();
      final api = context.read<ApiClient>();

      await auth.pairViaQR(url, code, api);
      // Stop the scanner camera to save battery/CPU while GoRouter redirects.
      await _scannerCtrl.stop();
      // GoRouter redirect fires automatically on isAuthenticated changing → /live.
    } on FormatException catch (e) {
      if (mounted) {
        setState(() {
          _error = e.message;
          _processing = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = _friendlyError(e);
          _processing = false;
        });
      }
    }
  }

  String _friendlyError(Object e) {
    final msg = e.toString();
    if (msg.contains('401')) return 'Pairing code is invalid or expired.';
    if (msg.contains('429')) return 'Too many attempts. Please wait.';
    if (msg.contains('SocketException') || msg.contains('Connection refused')) {
      return 'Could not reach the NVR. Make sure you are on the same network.';
    }
    return 'Pairing failed. Please try again.';
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Scan QR Code')),
      body: Column(
        children: [
          Expanded(
            child: _processing
                ? const Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        CircularProgressIndicator(),
                        SizedBox(height: 16),
                        Text('Pairing...', style: TextStyle(color: Colors.grey)),
                      ],
                    ),
                  )
                : MobileScanner(
                    controller: _scannerCtrl,
                    onDetect: _onDetect,
                  ),
          ),
          if (_error != null)
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(16),
              color: const Color(0xFFF85149).withValues(alpha: 0.12),
              child: Text(
                _error!,
                style: const TextStyle(color: Color(0xFFF85149)),
                textAlign: TextAlign.center,
              ),
            ),
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(
              'Open Settings on the Sentinel NVR web UI and generate a QR code under "Remote Access".',
              style: TextStyle(color: Colors.grey[400], fontSize: 12),
              textAlign: TextAlign.center,
            ),
          ),
        ],
      ),
    );
  }
}
