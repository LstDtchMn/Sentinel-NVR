// Authentication state manager (Phase 11, CG6, CG11).
// Persists the NVR host URL in SharedPreferences.
// Calls GET /setup + GET /auth/me at startup to restore session from cookie jar.
// Biometric unlock via local_auth — only activates if session cookie is still valid.

import 'package:flutter/foundation.dart';
import 'package:local_auth/local_auth.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/notification_prefs.dart';
import 'api_client.dart';

const _keyHostUrl = 'sentinel_host_url';
const _keyBiometricEnabled = 'biometric_enabled';

class AuthService extends ChangeNotifier {
  AuthUser? _user;
  bool _loading = true;
  bool _needsSetup = false;
  String _hostUrl = '';

  bool get isAuthenticated => _user != null;
  bool get isLoading => _loading;
  bool get needsSetup => _needsSetup;
  AuthUser? get currentUser => _user;
  String get hostUrl => _hostUrl;

  final _localAuth = LocalAuthentication();

  /// Called once before [runApp].  Loads the stored host URL, configures the API
  /// client, checks first-run setup state, then restores the session from the
  /// persistent cookie jar.
  Future<void> initialize(ApiClient api) async {
    final prefs = await SharedPreferences.getInstance();
    _hostUrl = prefs.getString(_keyHostUrl) ?? '';

    // Wire callbacks on ApiClient to break the circular dependency.
    api.onRefresh = () => refreshSession(api);
    api.onLogout = () => forceLogout(api);

    if (_hostUrl.isNotEmpty) {
      await api.configure(_hostUrl);
      await _checkSetupAndSession(api);
    }

    _loading = false;
    notifyListeners();
  }

  Future<void> _checkSetupAndSession(ApiClient api) async {
    try {
      final setup = await api.checkSetup();
      _needsSetup = setup['needs_setup'] as bool? ?? false;
    } catch (_) {
      // If the server is unreachable, stay unauthenticated and let the user retry.
      return;
    }

    if (_needsSetup) return; // No existing users — redirect to setup screen.

    try {
      _user = await api.getMe();
    } on Exception {
      _user = null; // 401 or network error — user must log in.
    }
  }

  /// Persist [url] and reconfigure the API client.  Clears any existing session
  /// so stale cookies are not replayed to a different server.
  Future<void> setHostUrl(String url, ApiClient api) async {
    _hostUrl = url;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyHostUrl, url);
    await api.clearCookies();
    await api.configure(url);
    notifyListeners();
  }

  /// POST /auth/login → cookie jar populated → GET /auth/me → set [_user].
  Future<void> login(String username, String password, ApiClient api) async {
    await api.login(username, password);
    _user = await api.getMe();
    _needsSetup = false;
    notifyListeners();
  }

  /// POST /auth/logout → clear cookies → [_user] = null.
  Future<void> logout(ApiClient api) async {
    try {
      await api.logout();
    } catch (_) {
      // Best-effort — even if the server call fails, clear local state.
    }
    await api.clearCookies();
    _user = null;
    notifyListeners();
  }

  /// POST /auth/refresh.  Called by the ApiClient 401 interceptor.
  Future<void> refreshSession(ApiClient api) async {
    await api.refreshSession();
  }

  /// Called by the 401 interceptor when the refresh token has also expired.
  /// Public so that main.dart can wire it as the ApiClient.onLogout callback.
  Future<void> forceLogout(ApiClient api) async {
    await api.clearCookies();
    _user = null;
    notifyListeners();
  }

  /// Biometric + PIN unlock.  Only succeeds if the existing session cookie is
  /// still valid (i.e. we can GET /auth/me successfully after biometric auth).
  Future<bool> biometricUnlock(ApiClient api) async {
    // canCheckBiometrics is false on devices with no enrolled biometrics (e.g.
    // PIN only). isDeviceSupported() returns true for any local auth method,
    // so check both to allow PIN-only devices through.
    final canCheck = (await _localAuth.canCheckBiometrics) ||
        (await _localAuth.isDeviceSupported());
    if (!canCheck) return false;

    final authenticated = await _localAuth.authenticate(
      localizedReason: 'Unlock Sentinel NVR',
      options: const AuthenticationOptions(
        biometricOnly: false, // allow PIN fallback
        stickyAuth: true,
      ),
    );
    if (!authenticated) return false;

    // Confirm session is still valid.
    try {
      _user = await api.getMe();
      notifyListeners();
      return true;
    } catch (_) {
      return false;
    }
  }

  // ── QR pairing (Phase 12, CG11, R8) ─────────────────────────────────────

  /// Pair with the NVR by scanning a QR code from the web UI.
  /// Sets the host URL, redeems the pairing code (which sets session cookies),
  /// and loads the user profile.
  Future<void> pairViaQR(String url, String code, ApiClient api) async {
    await setHostUrl(url, api);
    await api.redeemPairingCode(code);
    _user = await api.getMe();
    _needsSetup = false;
    notifyListeners();
  }

  // ── First-run setup ───────────────────────────────────────────────────────

  /// POST /setup — creates the first admin account.
  Future<void> completeSetup(String username, String password, ApiClient api) async {
    final user = await api.completeSetup(username, password);
    _user = user;
    _needsSetup = false;
    notifyListeners();
  }

  // ── Biometric preference ──────────────────────────────────────────────────

  Future<bool> isBiometricEnabled() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(_keyBiometricEnabled) ?? false;
  }

  Future<void> setBiometricEnabled(bool value) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_keyBiometricEnabled, value);
    notifyListeners();
  }
}
