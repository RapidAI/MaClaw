import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/account_store.dart';
import '../../core/providers.dart';
import '../bots/bot_models.dart';
import '../bots/bots_controller.dart';

/// 我的 — account card, multi-account switcher, logout.
class AccountScreen extends ConsumerWidget {
  const AccountScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionProvider);
    final active = session.active;
    return Scaffold(
      appBar: AppBar(title: const Text('我的')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (active != null) _AccountCard(account: active, active: true),
          const SizedBox(height: 12),
          const _CreditsCard(),
          const SizedBox(height: 12),
          Card(
            child: ListTile(
              leading: const Icon(Icons.psychology_alt_outlined),
              title: const Text('模型设置'),
              subtitle: const Text('对所有 Bot 生效的模型提供方'),
              trailing: const Icon(Icons.chevron_right),
              onTap: () => context.push('/llm-settings'),
            ),
          ),
          const SizedBox(height: 24),
          Row(
            children: [
              Text('账号（${session.accounts.length}）',
                  style: Theme.of(context).textTheme.titleSmall),
              const Spacer(),
              TextButton.icon(
                onPressed: () => context.push('/login?mode=add'),
                icon: const Icon(Icons.person_add_alt_1, size: 18),
                label: const Text('添加账号'),
              ),
            ],
          ),
          for (final account in session.accounts)
            _AccountTile(account: account, isActive: account.key == active?.key),
          const SizedBox(height: 24),
          if (active != null)
            FilledButton.tonalIcon(
              style: FilledButton.styleFrom(
                foregroundColor: Theme.of(context).colorScheme.error,
              ),
              onPressed: () => _confirmLogout(context, ref, active),
              icon: const Icon(Icons.logout),
              label: const Text('退出当前账号'),
            ),
          const SizedBox(height: 24),
          Center(
            child: Text(
              'MobileBot · 桌面 Bot 的移动工作台',
              style: Theme.of(context)
                  .textTheme
                  .bodySmall
                  ?.copyWith(color: Theme.of(context).colorScheme.onSurfaceVariant),
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _confirmLogout(
      BuildContext context, WidgetRef ref, Account account) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('退出当前账号？'),
        content: Text('将移除本机上「${account.label}」的登录信息。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('退出'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      await ref.read(sessionProvider.notifier).signOutActive();
    }
  }
}

final _creditsProvider = FutureProvider.autoDispose<BotCredits?>((ref) async {
  final api = ref.watch(botApiProvider);
  if (api == null) return null;
  try {
    return await api.credits();
  } on Object {
    return null;
  }
});

/// Bot 用量额度 — `GET /api/v1/bots/credits`。
class _CreditsCard extends ConsumerWidget {
  const _CreditsCard();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final credits = ref.watch(_creditsProvider).value;
    if (credits == null || !credits.applies) {
      return const SizedBox.shrink();
    }
    final used = credits.used;
    final total = credits.unlimited ? 0.0 : credits.limit;
    final progress = total > 0 ? (used / total).clamp(0.0, 1.0) : 0.0;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.bolt, size: 18, color: theme.colorScheme.primary),
                const SizedBox(width: 6),
                Text('Bot 额度', style: theme.textTheme.titleSmall),
              ],
            ),
            const SizedBox(height: 10),
            if (total > 0) ...[
              LinearProgressIndicator(value: progress),
              const SizedBox(height: 6),
              Text(
                '已用 ${used.toStringAsFixed(1)} / ${total.toStringAsFixed(0)}，剩余 ${credits.remaining.toStringAsFixed(1)}',
                style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant),
              ),
            ] else
              Text(
                credits.unlimited ? '不限量' : '已用 ${used.toStringAsFixed(1)}',
                style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant),
              ),
          ],
        ),
      ),
    );
  }
}

class _AccountCard extends StatelessWidget {
  final Account account;
  final bool active;

  const _AccountCard({required this.account, this.active = false});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          children: [
            CircleAvatar(
              radius: 26,
              backgroundColor: theme.colorScheme.primary,
              child: Text(
                account.label.isNotEmpty
                    ? account.label.substring(0, 1).toUpperCase()
                    : '?',
                style: const TextStyle(
                    color: Colors.white,
                    fontSize: 22,
                    fontWeight: FontWeight.w600),
              ),
            ),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(account.label, style: theme.textTheme.titleMedium),
                  const SizedBox(height: 2),
                  Text(
                    account.subtitle,
                    style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant),
                  ),
                  Text(
                    account.hubUrl,
                    style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ],
              ),
            ),
            if (active)
              Icon(Icons.check_circle, color: theme.colorScheme.primary),
          ],
        ),
      ),
    );
  }
}

class _AccountTile extends ConsumerWidget {
  final Account account;
  final bool isActive;

  const _AccountTile({required this.account, required this.isActive});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        leading: CircleAvatar(
          backgroundColor: isActive
              ? theme.colorScheme.primary
              : theme.colorScheme.surfaceContainerHighest,
          child: Text(
            account.label.isNotEmpty
                ? account.label.substring(0, 1).toUpperCase()
                : '?',
            style: TextStyle(
              color: isActive
                  ? theme.colorScheme.onPrimary
                  : theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ),
        title: Text(account.label,
            maxLines: 1, overflow: TextOverflow.ellipsis),
        subtitle: Text(account.subtitle,
            maxLines: 1, overflow: TextOverflow.ellipsis),
        trailing: isActive
            ? Text('当前',
                style: TextStyle(
                    color: theme.colorScheme.primary,
                    fontWeight: FontWeight.w600))
            : IconButton(
                icon: const Icon(Icons.delete_outline, size: 20),
                tooltip: '移除账号',
                onPressed: () => _remove(context, ref),
              ),
        onTap: isActive
            ? null
            : () => ref
                .read(sessionProvider.notifier)
                .switchAccount(account.key),
      ),
    );
  }

  Future<void> _remove(BuildContext context, WidgetRef ref) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('移除账号？'),
        content: Text('将移除本机上「${account.label}」的登录信息。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('移除'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      await ref.read(sessionProvider.notifier).removeAccount(account.key);
    }
  }
}
