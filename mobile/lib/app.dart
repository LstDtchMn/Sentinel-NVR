// SentinelApp — MaterialApp.router with GoRouter, dark theme, and bottom navigation shell
// (Phase 11, CG11).  GoRouter's redirect guard enforces auth using AuthService as
// refreshListenable so every isAuthenticated change triggers a redirect evaluation.

import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:provider/provider.dart';

import 'screens/events.dart';
import 'screens/live_view.dart';
import 'screens/login.dart';
import 'screens/qr_scan.dart';
import 'screens/settings.dart';
import 'screens/setup.dart';
import 'screens/timeline.dart';
import 'services/auth_service.dart';

final GlobalKey<NavigatorState> rootNavigatorKey = GlobalKey<NavigatorState>();

GoRouter buildRouter(AuthService auth) {
  return GoRouter(
    navigatorKey: rootNavigatorKey,
    refreshListenable: auth, // re-evaluate redirect on every notifyListeners()
    initialLocation: '/live',
    redirect: (context, state) {
      final isLoggedIn = auth.isAuthenticated;
      final isLoading = auth.isLoading;
      final loc = state.matchedLocation;
      final isAuthRoute = loc == '/login' || loc == '/setup' || loc == '/qr-scan';

      // While initializing, stay put.
      if (isLoading) return null;

      if (auth.needsSetup && loc != '/setup') return '/setup';
      if (!isLoggedIn && !isAuthRoute) return '/login';
      if (isLoggedIn && isAuthRoute) return '/live';
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

/// Minimal event detail screen accessible via deep link /events/:id (R9).
class _EventDetailScreen extends StatefulWidget {
  final int eventId;

  const _EventDetailScreen({required this.eventId});

  @override
  State<_EventDetailScreen> createState() => _EventDetailScreenState();
}

class _EventDetailScreenState extends State<_EventDetailScreen> {
  @override
  Widget build(BuildContext context) {
    // The event detail view is accessed via notification deep link.
    // Full implementation beyond showing ID is deferred to a future iteration.
    return Scaffold(
      appBar: AppBar(title: Text('Event #${widget.eventId}')),
      body: Center(
        child: Text(
          'Event ${widget.eventId}',
          style: const TextStyle(color: Colors.grey),
        ),
      ),
    );
  }
}
