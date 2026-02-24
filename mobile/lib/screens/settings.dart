// Settings screen — host URL, notification preferences, account (Phase 11, CG11, R9).

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../models/camera.dart';
import '../models/notification_prefs.dart';
import '../services/api_client.dart';
import '../services/auth_service.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  final _urlCtrl = TextEditingController();
  bool _testingConnection = false;
  String? _connectionResult;

  List<NotifPref> _prefs = [];
  List<CameraDetail> _cameras = [];
  bool _biometricEnabled = false;
  bool _loadingPrefs = true;

  @override
  void initState() {
    super.initState();
    final auth = context.read<AuthService>();
    _urlCtrl.text = auth.hostUrl;
    _loadPrefs();
    _loadBiometric();
  }

  @override
  void dispose() {
    _urlCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadPrefs() async {
    try {
      final api = context.read<ApiClient>();
      final results = await Future.wait([
        api.listNotifPrefs(),
        api.getCameras(),
      ]);
      if (mounted) {
        setState(() {
          _prefs = results[0] as List<NotifPref>;
          _cameras = results[1] as List<CameraDetail>;
          _loadingPrefs = false;
        });
      }
    } catch (_) {
      if (mounted) setState(() => _loadingPrefs = false);
    }
  }

  Future<void> _loadBiometric() async {
    final auth = context.read<AuthService>();
    final enabled = await auth.isBiometricEnabled();
    if (mounted) setState(() => _biometricEnabled = enabled);
  }

  Future<void> _saveHostUrl() async {
    final url = _urlCtrl.text.trim();
    if (url.isEmpty) return;
    final auth = context.read<AuthService>();
    final api = context.read<ApiClient>();
    await auth.setHostUrl(url, api);
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Host URL saved')),
      );
    }
  }

  Future<void> _testConnection() async {
    setState(() {
      _testingConnection = true;
      _connectionResult = null;
    });
    try {
      final api = context.read<ApiClient>();
      final health = await api.getHealth();
      final status = health['status'] as String? ?? 'ok';
      final version = health['version'] as String? ?? '';
      if (mounted) {
        setState(() => _connectionResult = 'Connected — Sentinel $version ($status)');
      }
    } catch (e) {
      if (mounted) setState(() => _connectionResult = 'Failed: $e');
    } finally {
      if (mounted) setState(() => _testingConnection = false);
    }
  }

  Future<void> _toggleBiometric(bool value) async {
    final auth = context.read<AuthService>();
    await auth.setBiometricEnabled(value);
    setState(() => _biometricEnabled = value);
  }

  Future<void> _logout() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Sign out'),
        content: const Text('Are you sure you want to sign out?'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Sign out', style: TextStyle(color: Color(0xFFF85149))),
          ),
        ],
      ),
    );
    if (confirmed == true && mounted) {
      final auth = context.read<AuthService>();
      final api = context.read<ApiClient>();
      await auth.logout(api);
      // GoRouter redirect fires on isAuthenticated → false.
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          // ── Connection ────────────────────────────────────────────────────
          _SectionHeader(title: 'Connection'),
          const SizedBox(height: 8),
          TextFormField(
            controller: _urlCtrl,
            decoration: const InputDecoration(
              labelText: 'NVR Host URL',
              hintText: 'http://192.168.1.100:8099',
              prefixIcon: Icon(Icons.dns_outlined),
            ),
            keyboardType: TextInputType.url,
            autocorrect: false,
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              Expanded(
                child: OutlinedButton(
                  onPressed: _saveHostUrl,
                  child: const Text('Save URL'),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: OutlinedButton(
                  onPressed: _testingConnection ? null : _testConnection,
                  child: _testingConnection
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 2))
                      : const Text('Test Connection'),
                ),
              ),
            ],
          ),
          if (_connectionResult != null) ...[
            const SizedBox(height: 8),
            Text(
              _connectionResult!,
              style: TextStyle(
                color: _connectionResult!.startsWith('Connected')
                    ? Colors.green
                    : const Color(0xFFF85149),
                fontSize: 12,
              ),
            ),
          ],
          const SizedBox(height: 24),

          // ── Notifications ─────────────────────────────────────────────────
          _SectionHeader(title: 'Notifications'),
          const SizedBox(height: 8),
          if (_loadingPrefs)
            const Center(child: CircularProgressIndicator())
          else
            _NotifPrefsPanel(
              prefs: _prefs,
              cameras: _cameras,
              onToggle: _togglePref,
            ),
          const SizedBox(height: 24),

          // ── Account ───────────────────────────────────────────────────────
          _SectionHeader(title: 'Account'),
          const SizedBox(height: 8),
          SwitchListTile(
            value: _biometricEnabled,
            onChanged: _toggleBiometric,
            title: const Text('Biometric unlock'),
            subtitle: const Text('Use Face ID / fingerprint to unlock the app'),
          ),
          const SizedBox(height: 8),
          Consumer<AuthService>(
            builder: (context, auth, _) => ListTile(
              leading: const Icon(Icons.person_outline),
              title: Text(auth.currentUser?.username ?? ''),
              subtitle: Text(auth.currentUser?.role ?? ''),
            ),
          ),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            icon: const Icon(Icons.logout, color: Color(0xFFF85149)),
            label: const Text('Sign out', style: TextStyle(color: Color(0xFFF85149))),
            onPressed: _logout,
          ),
        ],
      ),
    );
  }

  Future<void> _togglePref(NotifPref pref, {bool? enabled, bool? critical}) async {
    try {
      final api = context.read<ApiClient>();
      final updated = await api.upsertNotifPref({
        'event_type': pref.eventType,
        if (pref.cameraId != null) 'camera_id': pref.cameraId,
        'enabled': enabled ?? pref.enabled,
        'critical': critical ?? pref.critical,
      });
      if (mounted) {
        setState(() {
          _prefs = _prefs.map((p) => p.id == updated.id ? updated : p).toList();
        });
      }
    } catch (_) {}
  }
}

class _SectionHeader extends StatelessWidget {
  final String title;
  const _SectionHeader({required this.title});

  @override
  Widget build(BuildContext context) {
    return Text(
      title,
      style: const TextStyle(
          color: Color(0xFF58A6FF), fontWeight: FontWeight.w600, fontSize: 13),
    );
  }
}

class _NotifPrefsPanel extends StatelessWidget {
  final List<NotifPref> prefs;
  final List<CameraDetail> cameras;
  final Future<void> Function(NotifPref, {bool? enabled, bool? critical}) onToggle;

  const _NotifPrefsPanel({
    required this.prefs,
    required this.cameras,
    required this.onToggle,
  });

  String _cameraName(int? id) {
    if (id == null) return 'All cameras';
    final cam = cameras.cast<CameraDetail?>().firstWhere(
          (c) => c?.id == id,
          orElse: () => null,
        );
    return cam?.name ?? 'Camera $id';
  }

  @override
  Widget build(BuildContext context) {
    if (prefs.isEmpty) {
      return const Text(
        'No notification preferences set.\nConfigure them from the web UI.',
        style: TextStyle(color: Colors.grey, fontSize: 12),
      );
    }
    return Column(
      children: prefs.map((p) {
        return ListTile(
          contentPadding: EdgeInsets.zero,
          title: Text('${p.eventType} — ${_cameraName(p.cameraId)}'),
          subtitle: p.critical ? const Text('Critical alert') : null,
          trailing: Switch(
            value: p.enabled,
            onChanged: (v) => onToggle(p, enabled: v),
          ),
        );
      }).toList(),
    );
  }
}
