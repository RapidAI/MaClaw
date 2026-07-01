import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

class AppShell extends StatelessWidget {
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
  Widget build(BuildContext context) {
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
