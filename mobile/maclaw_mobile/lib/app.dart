import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import 'features/account/account_screen.dart';
import 'features/assistant/assistant_screen.dart';
import 'features/digital_employees/digital_employees_screen.dart';
import 'features/documents/documents_screen.dart';
import 'features/servers/servers_screen.dart';
import 'shared/app_shell.dart';
import 'shared/theme.dart';

final _router = GoRouter(
  initialLocation: '/assistant',
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

class MaClawMobileApp extends StatelessWidget {
  const MaClawMobileApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      title: 'MaClaw Mobile',
      debugShowCheckedModeBanner: false,
      theme: buildMaClawTheme(Brightness.light),
      darkTheme: buildMaClawTheme(Brightness.dark),
      routerConfig: _router,
    );
  }
}
