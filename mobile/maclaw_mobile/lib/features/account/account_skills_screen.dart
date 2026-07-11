import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../shared/surface.dart';
import '../auth/session_controller.dart';
import 'account_agent_status_card.dart';

/// Read-only list of skills available to the Hub mobile full agent.
class AccountSkillsScreen extends ConsumerWidget {
  const AccountSkillsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final skills = ref.watch(accountSkillsProvider);
    return ScreenScaffold(
      title: 'Agent 技能',
      subtitle: 'Hub 官方助手可用的技能（含市场种子）。重活由 Hub 执行。',
      trailing: IconButton.filledTonal(
        tooltip: '刷新',
        onPressed: () => ref.invalidate(accountSkillsProvider),
        icon: const Icon(Icons.refresh),
      ),
      children: [
        skills.when(
          data: (list) {
            if (list == null) {
              return const Card(
                child: ListTile(
                  title: Text('无法加载技能'),
                  subtitle: Text('请确认已登录且 Hub 已启用官方 Agent。'),
                ),
              );
            }
            if (list.skills.isEmpty) {
              return Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const StatusBanner(
                    tone: StatusTone.info,
                    icon: Icons.info_outline,
                    message: '当前没有已安装技能。可从 Hub 技能种子目录重新同步，或通过助手 manage_skill 安装。',
                  ),
                  const SizedBox(height: 12),
                  FilledButton.tonalIcon(
                    onPressed: () => _reseed(context, ref),
                    icon: const Icon(Icons.cloud_sync_outlined),
                    label: const Text('重新同步种子技能'),
                  ),
                ],
              );
            }
            return Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                StatusBanner(
                  tone: StatusTone.success,
                  icon: Icons.check_circle_outline,
                  message: '已安装 ${list.count} 个技能',
                ),
                const SizedBox(height: 12),
                for (final skill in list.skills) ...[
                  Card(
                    child: ListTile(
                      leading: const Icon(Icons.auto_awesome_outlined),
                      title: Text(skill.name),
                      subtitle: Text(
                        [
                          if (skill.description.isNotEmpty) skill.description,
                          '${skill.type} · ${skill.status}'
                              '${skill.version.isEmpty ? '' : ' · v${skill.version}'}'
                              '${skill.stepCount > 0 ? ' · ${skill.stepCount} 步' : ''}',
                        ].join('\n'),
                      ),
                      isThreeLine: skill.description.isNotEmpty,
                    ),
                  ),
                  const SizedBox(height: 8),
                ],
                const SizedBox(height: 8),
                OutlinedButton.icon(
                  onPressed: () => _reseed(context, ref),
                  icon: const Icon(Icons.cloud_sync_outlined),
                  label: const Text('重新同步种子技能'),
                ),
              ],
            );
          },
          loading: () => const LoadingCard(label: '加载技能列表…'),
          error: (error, _) => Card(
            child: ListTile(
              leading: const Icon(Icons.error_outline),
              title: const Text('技能列表加载失败'),
              subtitle: Text('$error'),
            ),
          ),
        ),
      ],
    );
  }

  Future<void> _reseed(BuildContext context, WidgetRef ref) async {
    final client = ref.read(apiClientProvider);
    if (client == null) return;
    final messenger = ScaffoldMessenger.of(context);
    try {
      final list = await client.reseedAgentSkills();
      ref.invalidate(accountSkillsProvider);
      messenger.showSnackBar(
        SnackBar(content: Text('已同步，当前技能 ${list.count} 个')),
      );
    } on Object catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text('同步失败：$error')),
      );
    }
  }
}
