import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'core/api/mobile_bootstrap.dart';
import 'core/api/mobile_realtime_bridge.dart';
import 'core/settings/app_preferences.dart';
import 'features/account/account_screen.dart';
import 'features/assistant/assistant_screen.dart';
import 'features/auth/llm_setup_screen.dart';
import 'features/auth/session_controller.dart';
import 'features/auth/startup_splash_screen.dart';
import 'features/digital_employees/digital_employees_screen.dart';
import 'features/documents/documents_screen.dart';
import 'features/servers/servers_screen.dart';
import 'shared/app_shell.dart';
import 'shared/theme.dart';

GoRouter mobileRouterForFeatures(MobileFeatures features) => GoRouter(
      initialLocation: mobileInitialPathForFeatures(features),
      redirect: (context, state) {
        final path = state.uri.path;
        if (mobilePathEnabledForFeatures(path, features)) return null;
        return mobileInitialPathForFeatures(features);
      },
      routes: [
        ShellRoute(
          builder: (context, state, child) => AppShell(child: child),
          routes: [
            GoRoute(
              path: '/assistant',
              pageBuilder: (context, state) => const NoTransitionPage(
                child: AssistantScreen(),
              ),
            ),
            GoRoute(
              path: '/documents',
              pageBuilder: (context, state) => const NoTransitionPage(
                child: DocumentsScreen(),
              ),
            ),
            GoRoute(
              path: '/servers',
              pageBuilder: (context, state) => const NoTransitionPage(
                child: ServersScreen(),
              ),
            ),
            GoRoute(
              path: '/employees',
              pageBuilder: (context, state) => const NoTransitionPage(
                child: DigitalEmployeesScreen(),
              ),
            ),
            GoRoute(
              path: '/account',
              pageBuilder: (context, state) => const NoTransitionPage(
                child: AccountScreen(),
              ),
            ),
          ],
        ),
      ],
    );

bool mobileLlmConfigured(MobileBootstrap? bootstrap) {
  if (bootstrap == null) return false;
  final status = bootstrap.llmAccess.status.toLowerCase().trim();
  if (status == 'missing' ||
      status == 'disabled' ||
      status == 'unavailable' ||
      status == 'not_configured') {
    return false;
  }
  return bootstrap.llmAccess.official || bootstrap.llmAccess.desktopQrDelegated;
}

class MaClawMobileApp extends ConsumerWidget {
  const MaClawMobileApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionControllerProvider);
    ref.watch(mobileRealtimeBridgeProvider);
    final preferences =
        ref.watch(appPreferencesProvider).valueOrNull ?? const AppPreferences();
    return MaterialApp.router(
      title: 'MaClaw Mobile',
      debugShowCheckedModeBanner: false,
      theme: buildMaClawTheme(Brightness.light),
      darkTheme: buildMaClawTheme(Brightness.dark),
      themeMode: preferences.themeMode,
      routerConfig: session.maybeWhen(
        data: (state) =>
            state.authenticated && mobileLlmConfigured(state.bootstrap)
                ? mobileRouterForFeatures(state.bootstrap!.features)
                : _setupRouter,
        orElse: () => _loadingRouter,
      ),
    );
  }
}

final _setupRouter = GoRouter(
  routes: [
    GoRoute(
      path: '/',
      builder: (context, state) => const LlmSetupScreen(),
    ),
  ],
);

final _loadingRouter = GoRouter(
  routes: [
    GoRoute(
      path: '/',
      builder: (context, state) => const StartupSplashScreen(),
    ),
  ],
);
