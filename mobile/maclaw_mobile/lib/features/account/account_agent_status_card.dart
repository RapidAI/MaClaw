import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../auth/session_controller.dart';
import 'account_mcp_screen.dart';
import 'account_skills_screen.dart';

final accountKnowledgeStatusProvider =
    FutureProvider.autoDispose<MobileAgentKnowledgeStatus?>((ref) async {
  final client = ref.watch(apiClientProvider);
  if (client == null) return null;
  try {
    return await client.getAgentKnowledgeStatus();
  } on Object {
    return null;
  }
});

final accountMcpConfigProvider =
    FutureProvider.autoDispose<MobileAgentMcpConfig>((ref) async {
  final client = ref.watch(apiClientProvider);
  if (client == null) {
    throw StateError('请先登录官方服务。');
  }
  return client.getAgentMcpConfig();
});

/// Lazy MCP health: null until user probes (avoids auto network on every page open).
final accountMcpHealthProvider =
    NotifierProvider.autoDispose<AccountMcpHealthNotifier, AsyncValue<MobileAgentMcpHealth?>>(
  AccountMcpHealthNotifier.new,
);

class AccountMcpHealthNotifier
    extends AutoDisposeNotifier<AsyncValue<MobileAgentMcpHealth?>> {
  @override
  AsyncValue<MobileAgentMcpHealth?> build() => const AsyncData(null);

  Future<void> probe() async {
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(StateError('请先登录官方服务。'), StackTrace.current);
      return;
    }
    state = const AsyncLoading();
    state = await AsyncValue.guard(client.probeAgentMcpHealth);
  }

  void clear() {
    state = const AsyncData(null);
  }
}

final accountSkillsProvider =
    FutureProvider.autoDispose<MobileAgentSkillsList?>((ref) async {
  final client = ref.watch(apiClientProvider);
  if (client == null) return null;
  try {
    return await client.listAgentSkills();
  } on Object {
    return null;
  }
});

/// Compact Agent MCP + knowledge + skills status for the Account tab.
class AccountAgentStatusCard extends ConsumerWidget {
  const AccountAgentStatusCard({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final knowledge = ref.watch(accountKnowledgeStatusProvider);
    final mcp = ref.watch(accountMcpConfigProvider);
    final health = ref.watch(accountMcpHealthProvider);
    final skills = ref.watch(accountSkillsProvider);

    final knowledgeText = knowledge.when(
      data: (s) {
        if (s == null) return '知识库状态未知';
        if (!s.available) return s.message.isEmpty ? '知识库不可用' : s.message;
        return '知识库就绪（${s.mode}）· 来源 ${s.sources} · 卡片 ${s.cards}';
      },
      loading: () => '知识库状态加载中…',
      error: (_, __) => '知识库状态加载失败',
    );

    final healthLine = health.when(
      data: (h) {
        if (h == null) return ' · 健康未探测';
        return ' · 健康 ${h.healthyCount}/${h.serverCount} · 工具 ${h.availableTools}';
      },
      loading: () => ' · 探测中…',
      error: (_, __) => ' · 探测失败',
    );

    final mcpText = mcp.when(
      data: (c) => '远程 MCP ${c.servers.length} 个'
          '${c.localMcpAllowed ? '' : ' · 本地 MCP 已禁用'}'
          '$healthLine',
      loading: () => 'MCP 配置加载中…',
      error: (_, __) => 'MCP 配置不可用（需官方 Agent）',
    );

    final skillsText = skills.when(
      data: (s) {
        if (s == null) return '技能：未知';
        return '技能：已安装 ${s.count} 个';
      },
      loading: () => '技能：加载中…',
      error: (_, __) => '技能：不可用',
    );

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '官方 Agent',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 8),
            Text(knowledgeText, style: Theme.of(context).textTheme.bodyMedium),
            const SizedBox(height: 4),
            Text(mcpText, style: Theme.of(context).textTheme.bodyMedium),
            const SizedBox(height: 4),
            Text(skillsText, style: Theme.of(context).textTheme.bodyMedium),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton.tonalIcon(
                  onPressed: () {
                    Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => const AccountMcpScreen(),
                      ),
                    );
                  },
                  icon: const Icon(Icons.extension_outlined),
                  label: const Text('管理 MCP'),
                ),
                FilledButton.tonalIcon(
                  onPressed: () {
                    Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => const AccountSkillsScreen(),
                      ),
                    );
                  },
                  icon: const Icon(Icons.auto_awesome_outlined),
                  label: const Text('技能列表'),
                ),
                OutlinedButton.icon(
                  onPressed: () {
                    ref.read(accountMcpHealthProvider.notifier).probe();
                    ref.invalidate(accountKnowledgeStatusProvider);
                    ref.invalidate(accountSkillsProvider);
                  },
                  icon: const Icon(Icons.monitor_heart_outlined),
                  label: const Text('探测健康'),
                ),
                OutlinedButton.icon(
                  onPressed: () => _showIngestNoteDialog(context, ref),
                  icon: const Icon(Icons.note_add_outlined),
                  label: const Text('写入备忘'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

Future<void> _showIngestNoteDialog(BuildContext context, WidgetRef ref) async {
  final client = ref.read(apiClientProvider);
  if (client == null) {
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('请先登录官方服务。')),
    );
    return;
  }
  final titleCtrl = TextEditingController();
  final textCtrl = TextEditingController();
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) {
      return AlertDialog(
        title: const Text('写入知识库备忘'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: titleCtrl,
                decoration: const InputDecoration(
                  labelText: '标题（可选）',
                  border: OutlineInputBorder(),
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: textCtrl,
                minLines: 4,
                maxLines: 8,
                decoration: const InputDecoration(
                  labelText: '内容',
                  hintText: '会被官方助手在对话中检索召回',
                  border: OutlineInputBorder(),
                ),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('入库'),
          ),
        ],
      );
    },
  );
  final title = titleCtrl.text;
  final text = textCtrl.text;
  titleCtrl.dispose();
  textCtrl.dispose();
  if (ok != true || !context.mounted) return;
  if (text.trim().isEmpty) {
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('内容不能为空')),
    );
    return;
  }
  try {
    final result = await client.ingestAgentKnowledge(
      text: text,
      title: title,
    );
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          result.ok
              ? '已写入知识库（${result.runeCount} 字）'
              : '写入未确认',
        ),
      ),
    );
    ref.invalidate(accountKnowledgeStatusProvider);
  } on Object catch (e) {
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('写入失败：$e')),
    );
  }
}
