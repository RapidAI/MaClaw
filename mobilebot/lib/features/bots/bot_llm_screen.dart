import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'bots_controller.dart';
import '../../core/locale.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart';

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
      if (mounted) setState(() => _loading = false);
      return;
    }
    try {
      final view = await api.getLLMSettings();
      if (!mounted) return;
      setState(() {
        _mode = (view['mode'] ?? 'default').toString() == 'custom'
            ? 'custom'
            : 'default';
        _warning = (view['warning'] ?? '').toString();
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
      appBar: AppBar(
        title: Row(
          children: [
            const BrandMark(size: 26, tile: true),
            const SizedBox(width: AppSpacing.sm + 2),
            Text(context.S.acCurrentModel),
          ],
        ),
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: _load,
              child: ListView(
                physics: const AlwaysScrollableScrollPhysics(),
                padding: const EdgeInsets.all(AppSpacing.lg),
                children: [
                  Row(
                    children: [
                      Icon(
                        _mode == 'custom' ? Icons.tune : Icons.auto_awesome,
                        size: 18,
                        color: theme.colorScheme.primary,
                      ),
                      const SizedBox(width: AppSpacing.sm),
                      Text(
                        _mode == 'custom'
                            ? context.S.llmCustom
                            : context.S.llmPlatformDefault,
                        style: theme.textTheme.titleSmall,
                      ),
                    ],
                  ),
                  if (_warning.isNotEmpty)
                    Container(
                      margin: const EdgeInsets.only(top: AppSpacing.md),
                      padding: const EdgeInsets.all(AppSpacing.md),
                      decoration: BoxDecoration(
                        color: theme.colorScheme.tertiaryContainer,
                        borderRadius: BorderRadius.circular(AppRadius.md),
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
                      padding: const EdgeInsets.only(top: AppSpacing.md),
                      child: Text(
                        _error,
                        style: TextStyle(color: theme.colorScheme.error),
                      ),
                    ),
                  if (_effective.isNotEmpty) ...[
                    const SizedBox(height: AppSpacing.xl),
                    Text(
                      context.S.llmStages,
                      style: theme.textTheme.titleSmall,
                    ),
                    const SizedBox(height: AppSpacing.sm),
                    Card(
                      child: Padding(
                        padding: const EdgeInsets.symmetric(
                          vertical: AppSpacing.xs,
                        ),
                        child: Column(
                          children: [
                            for (final role in [
                              ('foreground', context.S.llmForeground),
                              ('background', context.S.llmBackground),
                              ('computer_use', context.S.llmComputerUse),
                              ('grounder', context.S.llmGrounder),
                            ])
                              _EffectiveRoleRow(
                                label: role.$2,
                                role: _effective[role.$1],
                              ),
                            ],
                          ),
                        ),
                      ),
                    ],
                  if (_effective.isEmpty && _error.isEmpty && _warning.isEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: AppSpacing.xl),
                      child: Text(
                        context.S.llmNoConfig,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                ],
              ),
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
        ? context.S.llmNotEnabled
        : official
        ? context.S.llmOfficialModel
        : [
            (map['name'] ?? '').toString(),
            (map['model'] ?? '').toString(),
          ].where((s) => s.isNotEmpty).join(' · ');
    return ListTile(
      dense: true,
      contentPadding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.lg,
      ),
      title: Text(label, style: theme.textTheme.bodyMedium),
      trailing: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 210),
        child: Text(
          detail.isEmpty ? '—' : detail,
          textAlign: TextAlign.end,
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ),
    );
  }
}
