import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'bots_controller.dart';

/// 当前模型 — 只读展示 `GET /api/v1/bots/llm` 的各阶段生效配置
/// （前台/后台/电脑操作/界面定位）。编辑入口留在桌面端。
class BotLLMScreen extends ConsumerStatefulWidget {
  const BotLLMScreen({super.key});

  @override
  ConsumerState<BotLLMScreen> createState() => _BotLLMScreenState();
}

class _BotLLMScreenState extends ConsumerState<BotLLMScreen> {
  bool _loading = true;
  String _mode = 'default';
  String _warning = '';
  String _error = '';
  Map<String, dynamic> _effective = const {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final api = ref.read(botApiProvider);
    if (api == null) {
      setState(() => _loading = false);
      return;
    }
    try {
      final view = await api.getLLMSettings();
      if (!mounted) return;
      setState(() {
        _mode = (view['mode'] as String? ?? 'default') == 'custom'
            ? 'custom'
            : 'default';
        _warning = (view['warning'] as String? ?? '').toString();
        _effective = view['effective'] is Map
            ? Map<String, dynamic>.from(view['effective'] as Map)
            : const {};
        _loading = false;
      });
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = failureMessage(error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('当前模型')),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.all(16),
              children: [
                Row(
                  children: [
                    Icon(
                      _mode == 'custom' ? Icons.tune : Icons.auto_awesome,
                      size: 18,
                      color: theme.colorScheme.primary,
                    ),
                    const SizedBox(width: 8),
                    Text(
                      _mode == 'custom' ? '自定义模型服务' : '平台默认模型',
                      style: theme.textTheme.titleSmall,
                    ),
                  ],
                ),
                if (_warning.isNotEmpty)
                  Container(
                    margin: const EdgeInsets.only(top: 12),
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: theme.colorScheme.tertiaryContainer,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: Text(
                      _warning,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onTertiaryContainer,
                      ),
                    ),
                  ),
                if (_error.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Text(
                      _error,
                      style: TextStyle(color: theme.colorScheme.error),
                    ),
                  ),
                if (_effective.isNotEmpty) ...[
                  const SizedBox(height: 20),
                  Text('各阶段生效', style: theme.textTheme.titleSmall),
                  const SizedBox(height: 4),
                  for (final role in const [
                    ('foreground', '前台对话'),
                    ('background', '后台任务'),
                    ('computer_use', '电脑操作'),
                    ('grounder', '界面定位'),
                  ])
                    _EffectiveRoleRow(
                      label: role.$2,
                      role: _effective[role.$1],
                    ),
                ],
                if (_effective.isEmpty && _error.isEmpty && _warning.isEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 20),
                    child: Text(
                      '暂无生效配置',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
              ],
            ),
    );
  }
}

class _EffectiveRoleRow extends StatelessWidget {
  final String label;
  final Object? role;

  const _EffectiveRoleRow({required this.label, this.role});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    if (role is! Map) return const SizedBox.shrink();
    final map = Map<String, dynamic>.from(role as Map);
    final unused = map['unused'] == true;
    final official = map['official'] == true;
    final detail = unused
        ? '未启用'
        : official
            ? '官方模型'
            : [
                (map['name'] as String? ?? '').toString(),
                (map['model'] as String? ?? '').toString(),
              ].where((s) => s.isNotEmpty).join(' · ');
    return ListTile(
      dense: true,
      contentPadding: EdgeInsets.zero,
      title: Text(label, style: theme.textTheme.bodyMedium),
      trailing: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 210),
        child: Text(
          detail.isEmpty ? '—' : detail,
          textAlign: TextAlign.end,
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: theme.textTheme.bodySmall
              ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ),
    );
  }
}
