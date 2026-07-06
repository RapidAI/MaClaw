import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'core/api/mobile_bootstrap.dart';
import 'core/api/mobile_credits.dart';
import 'core/api/mobile_realtime_bridge.dart';
import 'core/notifications/mobile_notification_service.dart';
import 'core/security/mobile_redaction.dart';
import 'core/settings/app_preferences.dart';
import 'features/account/account_screen.dart';
import 'features/assistant/assistant_screen.dart';
import 'features/auth/login_screen.dart';
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
  if (!_mobileLlmStatusAvailable(status)) {
    return false;
  }
  if (bootstrap.llmAccess.official) {
    return isTrustedPhoneCreditsAccount(bootstrap.llmAccess.creditsAccount);
  }
  if (bootstrap.llmAccess.desktopQrDelegated) {
    return bootstrap.llmAccess.authorizationId.trim().isNotEmpty;
  }
  return false;
}

bool _mobileLlmStatusAvailable(String status) {
  return switch (status) {
    'available' || 'authorized' || 'active' || 'ready' || 'configured' => true,
    _ => false,
  };
}

String? mobileNotificationTargetPath(
  String payload,
  MobileFeatures features,
) {
  final path = mobileNotificationPayloadBasePath(payload);
  if (path == null) return null;
  if (mobilePathEnabledForFeatures(path, features)) return path;
  return mobileInitialPathForFeatures(features);
}

String mobileNotificationRecoveryMessage(String payload, String? targetPath) {
  if (targetPath == null) return '无法识别任务提醒';
  final value = payload.trim();
  final detail = switch (targetPath) {
    '/documents' => '已打开任务提醒：请在文档页查看导入、导出或草稿状态',
    '/employees' => '已打开任务提醒：请在数字员工页查看远程任务状态',
    '/servers' => '已打开任务提醒：请在远程页查看 SSH 连接或服务器资料',
    _ => '已打开任务提醒',
  };
  if (targetPath == '/documents' &&
      value.startsWith(mobileDocumentExportNotificationPrefix)) {
    return '已打开任务提醒：请在文档页查看导出任务状态';
  }
  if (targetPath == '/documents' &&
      value.startsWith(mobileDocumentUploadNotificationPrefix)) {
    return '已打开任务提醒：请在文档页查看导入任务状态';
  }
  if (targetPath == '/documents' &&
      value.startsWith(mobileDocumentDraftNotificationPrefix)) {
    return '已打开任务提醒：请在文档页查看草稿';
  }
  if (targetPath == '/employees' &&
      value.startsWith(mobileDigitalEmployeeTaskNotificationPrefix)) {
    return '已打开任务提醒：请在数字员工页查看远程任务状态';
  }
  if (targetPath == '/servers' &&
      value.startsWith(mobileServerProfileNotificationPrefix)) {
    return '已打开任务提醒：请在远程页查看 SSH 连接或服务器资料';
  }
  return detail;
}

class MaClawMobileApp extends ConsumerWidget {
  const MaClawMobileApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionControllerProvider);
    ref.watch(mobileRealtimeBridgeProvider);
    final preferences =
        ref.watch(appPreferencesProvider).valueOrNull ?? const AppPreferences();
    final routerConfig = session.maybeWhen(
      data: (state) {
        if (!state.authenticated) return _loginRouter;
        return mobileRouterForFeatures(state.bootstrap!.features);
      },
      orElse: () => _loadingRouter,
    );
    final canRouteNotifications = session.valueOrNull?.authenticated == true;
    return MaterialApp.router(
      title: 'MaClaw Mobile',
      debugShowCheckedModeBanner: false,
      theme: buildMaClawTheme(Brightness.light),
      darkTheme: buildMaClawTheme(Brightness.dark),
      themeMode: preferences.themeMode,
      builder: (context, child) => _NotificationOpenBridge(
        router: routerConfig,
        canRoute: canRouteNotifications,
        child: child ?? const SizedBox.shrink(),
      ),
      routerConfig: routerConfig,
    );
  }
}

class _NotificationOpenBridge extends ConsumerWidget {
  final GoRouter router;
  final bool canRoute;
  final Widget child;

  const _NotificationOpenBridge({
    required this.router,
    required this.canRoute,
    required this.child,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (!canRoute) return child;
    final payload =
        ref.read(mobileNotificationServiceProvider).consumeLastOpenedPayload();
    if (payload != null && payload.isNotEmpty) {
      final features = ref
              .read(sessionControllerProvider)
              .valueOrNull
              ?.bootstrap
              ?.features ??
          defaultMobileFeatures;
      final targetPath = mobileNotificationTargetPath(payload, features);
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!context.mounted) return;
        if (canRoute && targetPath != null) {
          router.go(targetPath);
        }
        final message = mobileNotificationRecoveryMessage(payload, targetPath);
        ScaffoldMessenger.maybeOf(context)?.showSnackBar(
          SnackBar(
            content: Text('$message：${redactMobileSensitiveText(payload)}'),
          ),
        );
      });
    }
    return child;
  }
}

final _loginRouter = GoRouter(
  routes: [
    GoRoute(
      path: '/',
      builder: (context, state) => const LoginScreen(),
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
