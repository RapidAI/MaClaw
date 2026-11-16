import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'core/app_update.dart';
import 'core/platform.dart' show supportsSideloadUpdate;
import 'core/native_voice.dart';
import 'core/voice_engine.dart';
import 'core/providers.dart';
import 'core/theme.dart';
import 'features/account/account_screen.dart';
import 'features/account/update_dialog.dart';
import 'features/auth/login_screen.dart';
import 'features/bots/bot_chat_controller.dart';
import 'features/bots/bot_chat_screen.dart';
import 'features/bots/bot_demo_screen.dart';
import 'features/bots/bot_desktop_screen.dart';
import 'features/bots/bot_llm_screen.dart';
import 'features/bots/bot_models.dart';
import 'features/bots/bot_settings_screen.dart';
import 'features/bots/bots_controller.dart';
import 'features/bots/bots_screen.dart';
import 'features/remote/remote_models.dart';
import 'features/remote/remote_screen.dart';
import 'features/remote/remote_task_screen.dart';
import 'features/rooms/rooms_screen.dart';
import 'features/status/status_screen.dart';

class MaClawBotApp extends ConsumerWidget {
  const MaClawBotApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(_routerProvider);
    return MaterialApp.router(
      title: 'MaClawBot',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      routerConfig: router,
    );
  }
}

final _routerProvider = Provider<GoRouter>((ref) {
  // Recreating the GoRouter on every session change would drop the whole
  // navigation stack (deep pages bounce back to '/'). Keep one router and
  // re-run the redirect via refreshListenable instead.
  final refresher = ValueNotifier<int>(0);
  ref.onDispose(refresher.dispose);
  ref.listen(sessionProvider, (_, _) => refresher.value++);
  return GoRouter(
    initialLocation: '/',
    refreshListenable: refresher,
    redirect: (context, state) {
      final session = ref.read(sessionProvider);
      if (!session.loaded) {
        return state.matchedLocation == '/splash' ? null : '/splash';
      }
      if (state.matchedLocation == '/splash') {
        return session.active != null ? '/' : '/login';
      }
      final signedIn = session.active != null;
      final onLogin = state.matchedLocation.startsWith('/login');
      final mode = state.uri.queryParameters['mode'];
      // Expired credentials force re-verification — but leave escape
      // hatches: the account screen (switch to a healthy account) and
      // mode=add (enroll a new one).
      if (session.expired &&
          mode != 'relogin' &&
          mode != 'add' &&
          state.matchedLocation != '/account') {
        return '/login?mode=relogin';
      }
      if (!signedIn && !onLogin) return '/login';
      if (signedIn && onLogin && mode != 'add' && mode != 'relogin') {
        return '/';
      }
      return null;
    },
    routes: [
      GoRoute(
        path: '/splash',
        builder: (_, _) =>
            const Scaffold(body: Center(child: CircularProgressIndicator())),
      ),
      GoRoute(
        path: '/login',
        builder: (context, state) => LoginScreen(
          addingAccount: state.uri.queryParameters['mode'] == 'add',
          relogin: state.uri.queryParameters['mode'] == 'relogin',
        ),
      ),
      ShellRoute(
        builder: (context, state, child) => _HomeShell(child: child),
        routes: [
          GoRoute(path: '/', builder: (_, _) => const BotsScreen()),
          GoRoute(path: '/remote', builder: (_, _) => const RemoteScreen()),
          GoRoute(path: '/status', builder: (_, _) => const StatusScreen()),
          GoRoute(path: '/account', builder: (_, _) => const AccountScreen()),
        ],
      ),
      GoRoute(
        path: '/bot/:id',
        builder: (context, state) =>
            _BotRoute(botId: state.pathParameters['id']!, tab: _BotTab.chat),
      ),
      GoRoute(
        path: '/bot/:id/desktop',
        builder: (context, state) =>
            _BotRoute(botId: state.pathParameters['id']!, tab: _BotTab.desktop),
      ),
      GoRoute(
        path: '/bot/:id/status',
        builder: (context, state) =>
            _BotRoute(botId: state.pathParameters['id']!, tab: _BotTab.status),
      ),
      GoRoute(
        path: '/bot/:id/settings',
        builder: (context, state) => _BotRoute(
          botId: state.pathParameters['id']!,
          tab: _BotTab.settings,
        ),
      ),
      GoRoute(path: '/llm-settings', builder: (_, _) => const BotLLMScreen()),
      GoRoute(path: '/rooms', builder: (_, _) => const RoomsScreen()),
      GoRoute(
        path: '/rooms/:rid',
        builder: (context, state) {
          // extra carries the room object on a tap-through; a restored
          // route (process death, deep link) arrives with none.
          final room = state.extra;
          if (room is BotRoom) return RoomChatScreen(room: room);
          return Scaffold(
            appBar: AppBar(),
            body: Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Text('找不到这个房间'),
                  const SizedBox(height: 12),
                  FilledButton.tonal(
                    onPressed: () => context.go('/rooms'),
                    child: const Text('返回房间列表'),
                  ),
                ],
              ),
            ),
          );
        },
      ),
      GoRoute(
        path: '/remote/task',
        builder: (context, state) {
          final extra = state.extra;
          if (extra is ({RcGrant grant, String taskPath, String taskName})) {
            return RemoteTaskScreen(
              grant: extra.grant,
              taskPath: extra.taskPath,
              taskName: extra.taskName,
            );
          }
          return Scaffold(
            appBar: AppBar(),
            body: Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Text('找不到这个远程任务'),
                  const SizedBox(height: 12),
                  FilledButton.tonal(
                    onPressed: () => context.go('/remote'),
                    child: const Text('返回远程控制'),
                  ),
                ],
              ),
            ),
          );
        },
      ),
      GoRoute(
        path: '/bot/:id/style',
        builder: (context, state) =>
            _BotRoute(botId: state.pathParameters['id']!, tab: _BotTab.style),
      ),
      GoRoute(
        path: '/bot/:id/demo',
        builder: (context, state) =>
            _BotRoute(botId: state.pathParameters['id']!, tab: _BotTab.demo),
      ),
    ],
  );
});

enum _BotTab { chat, desktop, status, settings, style, demo }

/// Resolves the route's bot against the loaded list, then mounts the right
/// screen inside a ProviderScope that binds the chat controller to it.
class _BotRoute extends ConsumerWidget {
  final String botId;
  final _BotTab tab;

  const _BotRoute({required this.botId, required this.tab});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final bots = ref.watch(botsProvider).bots;
    final loading = ref.watch(botsProvider).loading;
    Bot? bot;
    for (final item in bots) {
      if (item.id == botId) {
        bot = item;
        break;
      }
    }
    if (bot == null) {
      if (loading) {
        return const Scaffold(body: Center(child: CircularProgressIndicator()));
      }
      return Scaffold(
        appBar: AppBar(),
        body: Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text('找不到这个 Bot'),
              const SizedBox(height: 12),
              FilledButton.tonal(
                onPressed: () => context.go('/'),
                child: const Text('返回列表'),
              ),
            ],
          ),
        ),
      );
    }
    return ProviderScope(
      overrides: [botChatBotIdProvider.overrideWithValue(botId)],
      child: switch (tab) {
        _BotTab.chat => BotChatScreen(bot: bot),
        _BotTab.desktop => BotDesktopScreen(bot: bot),
        _BotTab.status => BotStatusScreen(bot: bot),
        _BotTab.settings => BotSettingsScreen(bot: bot),
        _BotTab.style => BotStyleScreen(bot: bot),
        _BotTab.demo => BotDemoScreen(bot: bot),
      },
    );
  }
}

/// Bottom navigation shell: Bot 列表 / 工作状态 / 我的.
class _HomeShell extends ConsumerStatefulWidget {
  final Widget child;

  const _HomeShell({required this.child});

  @override
  ConsumerState<_HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends ConsumerState<_HomeShell> {
  static const _tabs = ['/', '/remote', '/status', '/account'];
  Timer? _updateTimer;

  int _indexOf(String location) {
    if (location.startsWith('/remote')) return 1;
    if (location.startsWith('/status')) return 2;
    if (location.startsWith('/account')) return 3;
    return 0;
  }

  @override
  void initState() {
    super.initState();
    // Voice models provision themselves in the background — the files live
    // in external media storage, so upgrades/reinstalls skip the download.
    if (NativeVoice.supported) {
      unawaited(
        Future<void>.delayed(const Duration(seconds: 2), () {
          // Logout within the window disposes the shell — ref would throw.
          if (!mounted) return Future<void>.value();
          final hub = ref.read(sessionProvider).active?.hubUrl;
          return ref.read(voiceEngineProvider).autoEnsure(hubBase: hub);
        }),
      );
    }
    // One silent update check per launch where sideloading is allowed
    // (Android APK; iOS uses the App Store, HarmonyOS NEXT AppGallery).
    // Network failures stay invisible; only a real update surfaces the sheet.
    if (!supportsSideloadUpdate) return;
    _updateTimer = Timer(const Duration(seconds: 4), _autoCheck);
  }

  Future<void> _autoCheck() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final last = prefs.getInt('mobilebot.update.lastCheck') ?? 0;
      if (DateTime.now().millisecondsSinceEpoch - last <
          const Duration(hours: 6).inMilliseconds) {
        return;
      }
      final info = await PackageInfo.fromPlatform();
      final result = await AppUpdater().check(info.version);
      await prefs.setInt(
        'mobilebot.update.lastCheck',
        DateTime.now().millisecondsSinceEpoch,
      );
      if (!mounted || !result.hasUpdate) return;
      await showUpdateSheet(context, prechecked: result.update);
    } on Object {
      // A failed background check is silent — the 我的 tab offers retry.
    }
  }

  @override
  void dispose() {
    _updateTimer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final location = GoRouterState.of(context).matchedLocation;
    final index = _indexOf(location);
    return Scaffold(
      body: widget.child,
      bottomNavigationBar: NavigationBar(
        selectedIndex: index,
        onDestinationSelected: (i) {
          if (i != index) context.go(_tabs[i]);
        },
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.smart_toy_outlined),
            selectedIcon: Icon(Icons.smart_toy),
            label: 'Bot',
          ),
          NavigationDestination(
            icon: Icon(Icons.connected_tv_outlined),
            selectedIcon: Icon(Icons.connected_tv),
            label: '远控',
          ),
          NavigationDestination(
            icon: Icon(Icons.fact_check_outlined),
            selectedIcon: Icon(Icons.fact_check),
            label: '工作状态',
          ),
          NavigationDestination(
            icon: Icon(Icons.person_outline),
            selectedIcon: Icon(Icons.person),
            label: '我的',
          ),
        ],
      ),
    );
  }
}
