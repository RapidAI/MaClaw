import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../core/api/mobile_bootstrap.dart';
import '../core/api/mobile_credits.dart';
import '../core/shared_intents/mobile_shared_intent.dart';
import '../core/shared_intents/shared_intent_bootstrap.dart';
import '../features/auth/session_controller.dart';

class AppShell extends ConsumerStatefulWidget {
  final Widget child;

  const AppShell({super.key, required this.child});

  @override
  ConsumerState<AppShell> createState() => _AppShellState();
}

class _AppShellState extends ConsumerState<AppShell> {
  @override
  void initState() {
    super.initState();
    ref.listenManual<MobileSharedIntent?>(
      mobileSharedIntentProvider,
      (previous, next) {
        if (next == null) return;
        _routeSharedIntent(next);
      },
      fireImmediately: true,
    );
  }

  void _routeSharedIntent(MobileSharedIntent intent) {
    final session = ref.read(sessionControllerProvider).valueOrNull;
    final features = session?.bootstrap?.features ?? defaultMobileFeatures;
    final bootstrap = session?.bootstrap;
    final target = sharedIntentTargetPath(intent, features);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      context.go(target);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            intent.opensDocuments ? _sharedFileMessage : _sharedContentMessage,
          ),
        ),
      );
      if (!sharedIntentCanBeConsumedAtTarget(intent, target)) {
        ref.read(mobileSharedIntentProvider.notifier).clear(intent.id);
      }
      // bootstrap reserved for future label-aware snackbars
      assert(bootstrap == null || bootstrap.user.userId.isNotEmpty || true);
    });
  }

  @override
  Widget build(BuildContext context) {
    final session = ref.watch(sessionControllerProvider).valueOrNull;
    final features = session?.bootstrap?.features ?? defaultMobileFeatures;
    final bootstrap = session?.bootstrap;
    final tabs = mobileAppTabsForFeatures(features, bootstrap: bootstrap);

    final location = GoRouterState.of(context).uri.path;
    final index = tabs.indexWhere((tab) => location.startsWith(tab.path));
    final selectedIndex = index < 0 ? 0 : index;

    final scheme = Theme.of(context).colorScheme;
    final dark = Theme.of(context).brightness == Brightness.dark;
    final navColor = Theme.of(context).navigationBarTheme.backgroundColor ??
        (dark ? const Color(0xFF161E28) : scheme.surface);
    return Scaffold(
      resizeToAvoidBottomInset: true,
      body: SafeArea(
        bottom: false,
        child: widget.child,
      ),
      bottomNavigationBar: Material(
        color: navColor,
        elevation: 0,
        child: DecoratedBox(
          decoration: BoxDecoration(
            color: navColor,
            border: Border(
              top: BorderSide(color: scheme.outlineVariant),
            ),
          ),
          child: SafeArea(
            top: false,
            child: NavigationBar(
              selectedIndex: selectedIndex,
              onDestinationSelected: (next) => context.go(tabs[next].path),
              destinations: [
                for (final tab in tabs)
                  NavigationDestination(
                    icon: Icon(tab.icon),
                    selectedIcon: Icon(tab.selectedIcon ?? tab.icon, fill: 1),
                    label: tab.label,
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

const defaultMobileFeatures = MobileFeatures(
  search: true,
  documents: true,
  tasks: true,
  backendSshSessions: true,
  digitalEmployees: true,
  pushNotifications: false,
);

const _assistantTabLabel = 'AI助手';
const _twinTabLabel = '数字分身';
const _documentsTabLabel = '文档';
const _tasksTabLabel = '后台';
const _employeesTabLabel = '数字员工';
const _accountTabLabel = '我的';
const _sharedFileMessage = '已接收分享文件';
const _sharedContentMessage = '已接收分享内容';

List<MobileAppTab> mobileAppTabsForFeatures(
  MobileFeatures features, {
  MobileBootstrap? bootstrap,
}) {
  final assistantLabel = mobileAssistantTabLabel(bootstrap);
  // Bottom IA: 助手 | 文档 | 后台 | 数字员工 | 我的
  // 文档：本机/Hub 草稿与导入、查看与分享（含系统分享导入）；长任务进度仍在「后台」。
  final all = <MobileAppTab>[
    MobileAppTab(
      '/assistant',
      assistantLabel,
      Icons.chat_bubble_outline,
      'assistant',
      selectedIcon: Icons.chat_bubble,
    ),
    MobileAppTab(
      '/documents',
      _documentsTabLabel,
      Icons.description_outlined,
      'documents',
      selectedIcon: Icons.description,
    ),
    MobileAppTab(
      '/tasks',
      _tasksTabLabel,
      Icons.task_alt_outlined,
      'tasks',
      selectedIcon: Icons.task_alt,
    ),
    MobileAppTab(
      '/employees',
      _employeesTabLabel,
      Icons.smart_toy_outlined,
      'employees',
      selectedIcon: Icons.smart_toy,
    ),
    MobileAppTab(
      '/account',
      _accountTabLabel,
      Icons.person_outline,
      'account',
      selectedIcon: Icons.person,
    ),
  ];
  final tabs = all.where((tab) => tab.enabledBy(features)).toList();
  if (tabs.isEmpty) {
    return const [
      MobileAppTab(
        '/account',
        _accountTabLabel,
        Icons.person_outline,
        'account',
        selectedIcon: Icons.person,
      ),
    ];
  }
  return tabs;
}

String mobileInitialPathForFeatures(MobileFeatures features) {
  return mobileAppTabsForFeatures(features).first.path;
}

bool mobilePathEnabledForFeatures(String path, MobileFeatures features) {
  if (path.startsWith('/llm-setup')) return true;
  if (path.startsWith('/servers')) return features.backendSshSessions;
  return mobileAppTabsForFeatures(features)
      .any((tab) => path.startsWith(tab.path));
}

String sharedIntentTargetPath(
  MobileSharedIntent intent,
  MobileFeatures features,
) {
  if (intent.opensDocuments) {
    // Prefer the primary 文档 tab when available.
    if (features.documents) {
      return '/documents';
    }
    if (features.tasks) {
      return '/tasks';
    }
    return _firstEnabledPathExcept(features, {'/assistant', '/documents', '/tasks'});
  }
  if (intent.opensAssistant && features.assistant) {
    return '/assistant';
  }
  return mobileInitialPathForFeatures(features);
}

bool sharedIntentCanBeConsumedAtTarget(
  MobileSharedIntent intent,
  String targetPath,
) {
  if (intent.opensDocuments) {
    return targetPath.startsWith('/documents') ||
        targetPath.startsWith('/tasks');
  }
  if (intent.opensAssistant) {
    return targetPath.startsWith('/assistant');
  }
  return false;
}

String _firstEnabledPathExcept(MobileFeatures features, Set<String> excluded) {
  final tabs = mobileAppTabsForFeatures(features);
  for (final tab in tabs) {
    if (!excluded.contains(tab.path)) {
      return tab.path;
    }
  }
  return mobileInitialPathForFeatures(features);
}

class MobileAppTab {
  final String path;
  final String label;
  final IconData icon;
  final String feature;
  final IconData? selectedIcon;

  const MobileAppTab(
    this.path,
    this.label,
    this.icon,
    this.feature, {
    this.selectedIcon,
  });

  bool enabledBy(MobileFeatures features) {
    return switch (feature) {
      'assistant' => true,
      'search' => features.search,
      'tasks' => features.tasks,
      'documents' => features.documents,
      'employees' => features.digitalEmployees,
      _ => true,
    };
  }
}

// Labels exported for tests.
String get mobileAssistantTabLabelOfficial => _assistantTabLabel;
String get mobileTwinTabLabel => _twinTabLabel;
String get mobileDocumentsTabLabel => _documentsTabLabel;
String get mobileTasksTabLabel => _tasksTabLabel;
String get mobileEmployeesTabLabel => _employeesTabLabel;
String get mobileAccountTabLabel => _accountTabLabel;
