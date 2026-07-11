import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../core/api/mobile_bootstrap.dart';
import '../core/api/mobile_credits.dart';
import '../core/settings/app_preferences_model.dart';
import '../core/shared_intents/mobile_shared_intent.dart';
import '../core/shared_intents/shared_intent_bootstrap.dart';
import '../features/auth/session_controller.dart';
import '../l10n/app_strings.dart';

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
    final strings = ref.read(appStringsProvider);
    final target = sharedIntentTargetPath(intent, features);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      context.go(target);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            intent.opensDocuments
                ? strings.sharedFileReceived
                : strings.sharedContentReceived,
          ),
        ),
      );
      if (!sharedIntentCanBeConsumedAtTarget(intent, target)) {
        ref.read(mobileSharedIntentProvider.notifier).clear(intent.id);
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final session = ref.watch(sessionControllerProvider).valueOrNull;
    final features = session?.bootstrap?.features ?? defaultMobileFeatures;
    final bootstrap = session?.bootstrap;
    final strings = ref.watch(appStringsProvider);
    final tabs = mobileAppTabsForFeatures(
      features,
      bootstrap: bootstrap,
      strings: strings,
    );

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

List<MobileAppTab> mobileAppTabsForFeatures(
  MobileFeatures features, {
  MobileBootstrap? bootstrap,
  AppStrings? strings,
}) {
  // Unit tests omit strings; default Chinese keeps existing expectations stable.
  final s = strings ?? AppStrings.forLanguage(appLanguageChinese);
  final twin = usesDigitalTwinAssistant(bootstrap);
  final assistantLabel = twin ? s.tabTwin : s.tabAssistant;
  // Bottom IA: assistant | documents | tasks | employees | account
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
      s.tabDocuments,
      Icons.description_outlined,
      'documents',
      selectedIcon: Icons.description,
    ),
    MobileAppTab(
      '/tasks',
      s.tabTasks,
      Icons.task_alt_outlined,
      'tasks',
      selectedIcon: Icons.task_alt,
    ),
    MobileAppTab(
      '/employees',
      s.tabEmployees,
      Icons.smart_toy_outlined,
      'employees',
      selectedIcon: Icons.smart_toy,
    ),
    MobileAppTab(
      '/account',
      s.tabAccount,
      Icons.person_outline,
      'account',
      selectedIcon: Icons.person,
    ),
  ];
  final tabs = all.where((tab) => tab.enabledBy(features)).toList();
  if (tabs.isEmpty) {
    return [
      MobileAppTab(
        '/account',
        s.tabAccount,
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
    // Prefer the primary documents tab when available.
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

// Labels exported for tests (Chinese canonical; effective UI uses AppStrings).
String get mobileAssistantTabLabelOfficial =>
    AppStrings.forLanguage(appLanguageChinese).tabAssistant;
String get mobileTwinTabLabel =>
    AppStrings.forLanguage(appLanguageChinese).tabTwin;
String get mobileDocumentsTabLabel =>
    AppStrings.forLanguage(appLanguageChinese).tabDocuments;
String get mobileTasksTabLabel =>
    AppStrings.forLanguage(appLanguageChinese).tabTasks;
String get mobileEmployeesTabLabel =>
    AppStrings.forLanguage(appLanguageChinese).tabEmployees;
String get mobileAccountTabLabel =>
    AppStrings.forLanguage(appLanguageChinese).tabAccount;
