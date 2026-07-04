import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../core/api/mobile_bootstrap.dart';
import '../core/shared_intents/mobile_shared_intent.dart';
import '../core/shared_intents/shared_intent_bootstrap.dart';
import '../features/auth/session_controller.dart';

class AppShell extends ConsumerWidget {
  final Widget child;

  const AppShell({super.key, required this.child});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final features =
        ref.watch(sessionControllerProvider).valueOrNull?.bootstrap?.features ??
            defaultMobileFeatures;
    final tabs = mobileAppTabsForFeatures(features);

    ref.listen(mobileSharedIntentProvider, (previous, next) {
      if (next == null) return;
      final target = sharedIntentTargetPath(next, features);
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!context.mounted) return;
        context.go(target);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              next.opensDocuments ? _sharedFileMessage : _sharedContentMessage,
            ),
          ),
        );
        if (!sharedIntentCanBeConsumedAtTarget(next, target)) {
          ref.read(mobileSharedIntentProvider.notifier).clear(next.id);
        }
      });
    });

    final location = GoRouterState.of(context).uri.path;
    final index = tabs.indexWhere((tab) => location.startsWith(tab.path));
    final selectedIndex = index < 0 ? 0 : index;

    return Scaffold(
      body: SafeArea(child: child),
      bottomNavigationBar: NavigationBar(
        selectedIndex: selectedIndex,
        onDestinationSelected: (next) => context.go(tabs[next].path),
        destinations: [
          for (final tab in tabs)
            NavigationDestination(
              icon: Icon(tab.icon),
              selectedIcon: Icon(tab.icon, fill: 1),
              label: tab.label,
            ),
        ],
      ),
    );
  }
}

const defaultMobileFeatures = MobileFeatures(
  search: true,
  documents: true,
  localSsh: true,
  digitalEmployees: true,
  pushNotifications: false,
);

const _lookupTabLabel = '\u67e5\u4fe1\u606f';
const _documentsTabLabel = '\u6587\u6863';
const _remoteTabLabel = '\u8fdc\u7a0b';
const _employeesTabLabel = '\u5458\u5de5';
const _accountTabLabel = '\u6211\u7684';
const _sharedFileMessage = '\u5df2\u63a5\u6536\u5206\u4eab\u6587\u4ef6';
const _sharedContentMessage = '\u5df2\u63a5\u6536\u5206\u4eab\u5185\u5bb9';

const _allMobileTabs = [
  MobileAppTab(
    '/assistant',
    _lookupTabLabel,
    Icons.manage_search_outlined,
    'search',
  ),
  MobileAppTab(
    '/documents',
    _documentsTabLabel,
    Icons.description_outlined,
    'documents',
  ),
  MobileAppTab('/servers', _remoteTabLabel, Icons.lan_outlined, 'local_ssh'),
  MobileAppTab(
    '/employees',
    _employeesTabLabel,
    Icons.smart_toy_outlined,
    'employees',
  ),
  MobileAppTab('/account', _accountTabLabel, Icons.person_outline, 'account'),
];

List<MobileAppTab> mobileAppTabsForFeatures(MobileFeatures features) {
  final tabs = _allMobileTabs.where((tab) => tab.enabledBy(features)).toList();
  if (tabs.isEmpty) {
    return const [
      MobileAppTab(
        '/account',
        _accountTabLabel,
        Icons.person_outline,
        'account',
      ),
    ];
  }
  return tabs;
}

String mobileInitialPathForFeatures(MobileFeatures features) {
  return mobileAppTabsForFeatures(features).first.path;
}

bool mobilePathEnabledForFeatures(String path, MobileFeatures features) {
  return mobileAppTabsForFeatures(features)
      .any((tab) => path.startsWith(tab.path));
}

String sharedIntentTargetPath(
  MobileSharedIntent intent,
  MobileFeatures features,
) {
  if (intent.opensDocuments) {
    if (features.documents) {
      return '/documents';
    }
    return _firstEnabledPathExcept(features, {'/assistant', '/documents'});
  }
  if (intent.opensAssistant && features.search) {
    return '/assistant';
  }
  return mobileInitialPathForFeatures(features);
}

bool sharedIntentCanBeConsumedAtTarget(
  MobileSharedIntent intent,
  String targetPath,
) {
  if (intent.opensDocuments) {
    return targetPath.startsWith('/documents');
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

  const MobileAppTab(this.path, this.label, this.icon, this.feature);

  bool enabledBy(MobileFeatures features) {
    return switch (feature) {
      'search' => features.search,
      'documents' => features.documents,
      'local_ssh' => features.localSsh,
      'employees' => features.digitalEmployees,
      _ => true,
    };
  }
}
