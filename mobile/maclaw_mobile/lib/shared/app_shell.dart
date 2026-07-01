import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../core/shared_intents/shared_intent_bootstrap.dart';

class AppShell extends ConsumerWidget {
  final Widget child;

  const AppShell({super.key, required this.child});

  static const _tabs = [
    _AppTab('/assistant', '查信息', Icons.manage_search_outlined),
    _AppTab('/documents', '文档', Icons.description_outlined),
    _AppTab('/servers', '远程', Icons.lan_outlined),
    _AppTab('/employees', '员工', Icons.smart_toy_outlined),
    _AppTab('/account', '我的', Icons.person_outline),
  ];

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    ref.listen(mobileSharedIntentProvider, (previous, next) {
      if (next == null) return;
      final target = next.opensDocuments ? '/documents' : '/assistant';
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!context.mounted) return;
        context.go(target);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(next.opensDocuments ? '已接收分享文件' : '已接收分享内容'),
          ),
        );
      });
    });

    final location = GoRouterState.of(context).uri.path;
    final index = _tabs.indexWhere((tab) => location.startsWith(tab.path));
    final selectedIndex = index < 0 ? 0 : index;

    return Scaffold(
      body: SafeArea(child: child),
      bottomNavigationBar: NavigationBar(
        selectedIndex: selectedIndex,
        onDestinationSelected: (next) => context.go(_tabs[next].path),
        destinations: [
          for (final tab in _tabs)
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

class _AppTab {
  final String path;
  final String label;
  final IconData icon;

  const _AppTab(this.path, this.label, this.icon);
}
