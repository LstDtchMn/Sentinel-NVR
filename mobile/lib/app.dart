// SentinelApp — MaterialApp.router with GoRouter, dark theme, and bottom navigation shell
// (Phase 11, CG11).  GoRouter's redirect guard enforces auth using AuthService as
// refreshListenable so every isAuthenticated change triggers a redirect evaluation.

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:provider/provider.dart';

import 'models/event.dart';
import 'screens/events.dart';
import 'screens/live_view.dart';
import 'screens/login.dart';
import 'screens/qr_scan.dart';
import 'screens/settings.dart';
import 'screens/setup.dart';
import 'screens/timeline.dart';
import 'services/api_client.dart';
import 'services/auth_service.dart';

final GlobalKey<NavigatorState> rootNavigatorKey = GlobalKey<NavigatorState>();

GoRouter buildRouter(AuthService auth) {
  return GoRouter(
    navigatorKey: rootNavigatorKey,
    refreshListenable: auth, // re-evaluate redirect on every notifyListeners()
    initialLocation: '/live',
    // Redirect guard — state machine with four ordered rules:
    //   1. isLoading → null (stay put; auth state is still initialising)
    //   2. needsSetup → /setup (first-run: no users exist yet)
    //   3. !isLoggedIn && non-auth route → /login (unauthenticated → login)
    //   4. isLoggedIn && auth route → /live (already logged in → main app)
    // Falls through to null (no redirect) when none of the above apply.
    redirect: (context, state) {
      final isLoggedIn = auth.isAuthenticated;
      final isLoading = auth.isLoading;
      final loc = state.matchedLocation;
      final isAuthRoute = loc == '/login' || loc == '/setup' || loc == '/qr-scan';

      if (isLoading) return null;                              // 1. still initialising
      if (auth.needsSetup && loc != '/setup') return '/setup'; // 2. first-run setup
      if (!isLoggedIn && !isAuthRoute) return '/login';        // 3. unauthenticated
      if (isLoggedIn && isAuthRoute) return '/live';           // 4. already logged in
      return null;
    },
    routes: [
      GoRoute(path: '/login', builder: (_, __) => const LoginScreen()),
      GoRoute(path: '/setup', builder: (_, __) => const SetupScreen()),
      GoRoute(path: '/qr-scan', builder: (_, __) => const QrScanScreen()),

      // Shell route provides the bottom navigation bar for the main app.
      StatefulShellRoute.indexedStack(
        builder: (context, state, shell) => _ScaffoldWithNav(shell: shell),
        branches: [
          StatefulShellBranch(routes: [
            GoRoute(path: '/live', builder: (_, __) => const LiveViewScreen()),
          ]),
          StatefulShellBranch(routes: [
            GoRoute(
              path: '/events',
              builder: (_, __) => const EventsScreen(),
              routes: [
                GoRoute(
                  path: ':id',
                  builder: (context, state) => _EventDetailScreen(
                    eventId: int.parse(state.pathParameters['id']!),
                  ),
                ),
              ],
            ),
          ]),
          StatefulShellBranch(routes: [
            GoRoute(
              path: '/settings',
              builder: (_, __) => const SettingsScreen(),
              routes: [
                GoRoute(
                  path: 'timeline',
                  builder: (_, __) => const TimelineScreen(),
                ),
              ],
            ),
          ]),
        ],
      ),
    ],
  );
}

class SentinelApp extends StatelessWidget {
  final GoRouter router;

  const SentinelApp({super.key, required this.router});

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      title: 'Sentinel NVR',
      debugShowCheckedModeBanner: false,
      routerConfig: router,
      theme: _buildTheme(),
    );
  }

  ThemeData _buildTheme() {
    return ThemeData(
      useMaterial3: true,
      brightness: Brightness.dark,
      colorScheme: const ColorScheme.dark(
        primary: Color(0xFF58A6FF),   // accent blue (CG11 design language)
        secondary: Color(0xFF58A6FF),
        error: Color(0xFFF85149),     // critical alert red
        surface: Color(0xFF161B22),
        onSurface: Colors.white,
      ),
      scaffoldBackgroundColor: const Color(0xFF0D1117),
      appBarTheme: const AppBarTheme(
        backgroundColor: Color(0xFF161B22),
        foregroundColor: Colors.white,
        elevation: 0,
        centerTitle: false,
      ),
      cardTheme: const CardTheme(
        color: Color(0xFF161B22),
        elevation: 0,
      ),
      dividerColor: Colors.white12,
      inputDecorationTheme: InputDecorationTheme(
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: const BorderSide(color: Colors.white24),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: const BorderSide(color: Colors.white24),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: const BorderSide(color: Color(0xFF58A6FF)),
        ),
        filled: true,
        fillColor: const Color(0xFF0D1117),
        labelStyle: const TextStyle(color: Colors.grey),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: const Color(0xFF58A6FF),
          foregroundColor: Colors.white,
          minimumSize: const Size.fromHeight(48),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
        ),
      ),
    );
  }
}

/// Shell scaffold with bottom navigation bar: Live | Events | Settings.
class _ScaffoldWithNav extends StatelessWidget {
  final StatefulNavigationShell shell;

  const _ScaffoldWithNav({required this.shell});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: shell,
      bottomNavigationBar: BottomNavigationBar(
        currentIndex: shell.currentIndex,
        onTap: (i) => shell.goBranch(i, initialLocation: i == shell.currentIndex),
        backgroundColor: const Color(0xFF161B22),
        selectedItemColor: const Color(0xFF58A6FF),
        unselectedItemColor: Colors.grey,
        type: BottomNavigationBarType.fixed,
        items: const [
          BottomNavigationBarItem(
            icon: Icon(Icons.videocam_outlined),
            activeIcon: Icon(Icons.videocam),
            label: 'Live',
          ),
          BottomNavigationBarItem(
            icon: Icon(Icons.notifications_outlined),
            activeIcon: Icon(Icons.notifications),
            label: 'Events',
          ),
          BottomNavigationBarItem(
            icon: Icon(Icons.settings_outlined),
            activeIcon: Icon(Icons.settings),
            label: 'Settings',
          ),
        ],
      ),
    );
  }
}

/// Event detail screen accessible via deep link /events/:id (R9).
/// Fetches the event from the API and displays thumbnail, metadata, and clip.
class _EventDetailScreen extends StatefulWidget {
  final int eventId;

  const _EventDetailScreen({required this.eventId});

  @override
  State<_EventDetailScreen> createState() => _EventDetailScreenState();
}

class _EventDetailScreenState extends State<_EventDetailScreen> {
  EventRecord? _event;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadEvent();
  }

  Future<void> _loadEvent() async {
    try {
      final api = context.read<ApiClient>();
      final ev = await api.getEvent(widget.eventId);
      if (!mounted) return;
      setState(() { _event = ev; _loading = false; });
    } catch (e) {
      if (!mounted) return;
      setState(() { _error = e.toString(); _loading = false; });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('Event #${widget.eventId}')),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error != null
              ? Center(child: Text(_error!, style: const TextStyle(color: Colors.red)))
              : _buildDetail(),
    );
  }

  Widget _buildDetail() {
    final ev = _event!;
    final api = context.read<ApiClient>();
    final localTime = DateTime.tryParse(ev.startTime)?.toLocal();
    final timeStr = localTime != null
        ? '${localTime.year}-${localTime.month.toString().padLeft(2, '0')}-${localTime.day.toString().padLeft(2, '0')} '
          '${localTime.hour.toString().padLeft(2, '0')}:${localTime.minute.toString().padLeft(2, '0')}:${localTime.second.toString().padLeft(2, '0')}'
        : ev.startTime;

    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Thumbnail
          if (ev.thumbnail.isNotEmpty)
            ClipRRect(
              borderRadius: BorderRadius.circular(8),
              child: Image.network(
                api.eventThumbnailUrl(ev.id),
                width: double.infinity,
                fit: BoxFit.contain,
                errorBuilder: (_, __, ___) => Container(
                  height: 200,
                  color: Colors.grey[800],
                  child: const Center(child: Icon(Icons.broken_image, color: Colors.grey)),
                ),
              ),
            ),
          const SizedBox(height: 16),
          // Type + label
          Text(
            ev.label.isNotEmpty ? ev.label : ev.type,
            style: Theme.of(context).textTheme.headlineSmall,
          ),
          const SizedBox(height: 8),
          // Confidence
          if (ev.confidence > 0)
            Text('Confidence: ${(ev.confidence * 100).toStringAsFixed(0)}%',
                style: TextStyle(color: Colors.grey[400])),
          const SizedBox(height: 4),
          // Time
          Text(timeStr, style: TextStyle(color: Colors.grey[400])),
          const SizedBox(height: 4),
          // Clip indicator
          if (ev.hasClip)
            Chip(
              label: const Text('Has clip'),
              avatar: const Icon(Icons.videocam, size: 16),
              backgroundColor: Colors.blue[800],
            ),
        ],
      ),
    );
  }
}
