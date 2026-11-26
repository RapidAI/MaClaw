import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/account_store.dart';
import '../../core/providers.dart';
import '../../core/locale.dart';
import '../../core/platform.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart';
import '../bots/bot_models.dart';
import '../bots/bots_controller.dart';
import '../auth/qr_login_screen.dart';
import 'about_sheet.dart';
import 'update_dialog.dart';
import 'voice_settings_sheet.dart';

/// 我的 — account card, multi-account switcher, logout.
class AccountScreen extends ConsumerWidget {
  const AccountScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionProvider);
    final active = session.active;
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Row(
          children: [
            const BrandMark(size: 26, tile: true),
            const SizedBox(width: AppSpacing.sm + 2),
            Text(context.S.tabMe),
          ],
        ),
        actions: [
          if (active != null)
            IconButton(
              tooltip: '授权电脑端',
              icon: const Icon(Icons.qr_code_scanner),
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) =>
                      const QrLoginScreen(mode: QrScanMode.thirdPartyAuthorize),
                ),
              ),
            ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(AppSpacing.lg),
        children: [
          if (active != null) _AccountCard(account: active, active: true),
          const SizedBox(height: AppSpacing.md),
          const _CreditsCard(),
          const SizedBox(height: AppSpacing.md),
          Card(
            child: Column(
              children: [
                ListTile(
                  leading: _SettingsIcon(
                    icon: Icons.connected_tv_outlined,
                    color: theme.colorScheme.primary,
                  ),
                  title: Text(context.S.tabRemote),
                  subtitle: Text(context.S.rcMachineTasks),
                  trailing: const Icon(Icons.chevron_right, size: 20),
                  onTap: () => context.go('/remote'),
                ),
                const Divider(indent: 68),
                ListTile(
                  leading: _SettingsIcon(
                    icon: Icons.cloud_outlined,
                    color: const Color(0xFF3B82F6),
                  ),
                  title: Text(context.S.tabCloud),
                  subtitle: Text(context.S.rcCloudTasks),
                  trailing: const Icon(Icons.chevron_right, size: 20),
                  onTap: () => context.push('/cloud'),
                ),
                const Divider(indent: 68),
                ListTile(
                  leading: _SettingsIcon(
                    icon: Icons.psychology_alt_outlined,
                    color: theme.colorScheme.primary,
                  ),
                  title: Text(context.S.acCurrentModel),
                  subtitle: Text(context.S.acCurrentModelHint),
                  trailing: const Icon(Icons.chevron_right, size: 20),
                  onTap: () => context.push('/llm-settings'),
                ),
                if (supportsVoiceInput || supportsVoiceOutput) ...[
                  const Divider(indent: 68),
                  ListTile(
                    leading: _SettingsIcon(
                      icon: Icons.record_voice_over_outlined,
                      color: theme.colorScheme.tertiary,
                    ),
                    title: Text(context.S.voiceTitle),
                    subtitle: Text(context.S.acVoiceEngineHint),
                    trailing: const Icon(Icons.chevron_right, size: 20),
                    onTap: () => showModalBottomSheet<void>(
                      context: context,
                      isScrollControlled: true,
                      showDragHandle: true,
                      builder: (_) => const VoiceSettingsSheet(),
                    ),
                  ),
                ],
                if (supportsSideloadUpdate) ...[
                  const Divider(indent: 68),
                  ListTile(
                    leading: _SettingsIcon(
                      icon: Icons.system_update_alt,
                      color: theme.colorScheme.secondary,
                    ),
                    title: Text(context.S.checkUpdate),
                    subtitle: Text(context.S.acCheckUpdateHint),
                    trailing: const Icon(Icons.chevron_right, size: 20),
                    onTap: () => showUpdateSheet(context),
                  ),
                ],
                const Divider(indent: 68),
                ListTile(
                  leading: _SettingsIcon(
                    icon: Icons.language,
                    color: theme.colorScheme.primary,
                  ),
                  title: Text(context.S.settingsLanguage),
                  subtitle: Text(() {
                    final lang = ref.watch(appLanguageProvider);
                    return lang == AppLanguage.system
                        ? context.S.languageSystem
                        : languageDisplayName(lang);
                  }()),
                  trailing: const Icon(Icons.chevron_right, size: 20),
                  onTap: () => _pickLanguage(context, ref),
                ),
                const Divider(indent: 68),
                ListTile(
                  leading: _SettingsIcon(
                    icon: Icons.info_outline,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                  title: Text(context.S.aboutTitle),
                  trailing: const Icon(Icons.chevron_right, size: 20),
                  onTap: () => showAboutSheet(context),
                ),
              ],
            ),
          ),
          SectionHeader(
            title: context.S.acAccountsCount(session.accounts.length),
            trailing: TextButton.icon(
              onPressed: () => context.push('/login?mode=add'),
              icon: const Icon(Icons.person_add_alt_1, size: 18),
              label: Text(context.S.addAccountTitle),
            ),
          ),
          for (final account in session.accounts)
            _AccountTile(
              account: account,
              isActive: account.key == active?.key,
            ),
          const SizedBox(height: AppSpacing.xxl),
          if (active != null)
            FilledButton.tonalIcon(
              style: FilledButton.styleFrom(
                foregroundColor: Theme.of(context).colorScheme.error,
              ),
              onPressed: () => _confirmLogout(context, ref, active),
              icon: const Icon(Icons.logout),
              label: Text(context.S.acSignOut),
            ),
          const SizedBox(height: AppSpacing.xxl),
          Center(
            child: Text(
              context.S.acFooter,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ],
      ),
    );
  }

  /// 语言选择 sheet — 每个选项用它自己的语言显示名字。
  Future<void> _pickLanguage(BuildContext context, WidgetRef ref) async {
    final current = ref.read(appLanguageProvider);
    final chosen = await showModalBottomSheet<AppLanguage>(
      context: context,
      showDragHandle: true,
      builder: (sheetContext) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (final lang in AppLanguage.values)
              ListTile(
                leading: lang == AppLanguage.system
                    ? Icon(
                        Icons.settings_suggest_outlined,
                        color: Theme.of(sheetContext).colorScheme.primary,
                      )
                    : null,
                title: Text(
                  lang == AppLanguage.system
                      ? sheetContext.S.languageSystem
                      : languageDisplayName(lang),
                ),
                trailing: lang == current
                    ? Icon(
                        Icons.check,
                        color: Theme.of(sheetContext).colorScheme.primary,
                      )
                    : null,
                onTap: () => Navigator.of(sheetContext).pop(lang),
              ),
          ],
        ),
      ),
    );
    if (chosen != null && chosen != current) {
      await ref.read(appLanguageProvider.notifier).set(chosen);
    }
  }

  Future<void> _confirmLogout(
    BuildContext context,
    WidgetRef ref,
    Account account,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(context.S.acSignOutConfirm),
        content: Text(context.S.acRemoveBody(account.label)),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(context.S.cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(context.S.acSignOutAction),
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
        padding: const EdgeInsets.all(AppSpacing.lg),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.bolt, size: 18, color: theme.colorScheme.primary),
                const SizedBox(width: AppSpacing.sm),
                Text(context.S.acCredits, style: theme.textTheme.titleSmall),
              ],
            ),
            const SizedBox(height: AppSpacing.md),
            if (total > 0) ...[
              ClipRRect(
                borderRadius: AppRadius.pill,
                child: LinearProgressIndicator(value: progress),
              ),
              const SizedBox(height: AppSpacing.sm),
              Text(
                context.S.acCreditsUsedRemain(
                  used.toStringAsFixed(1),
                  total.toStringAsFixed(0),
                  credits.remaining.toStringAsFixed(1),
                ),
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ] else
              Text(
                credits.unlimited
                    ? context.S.acCreditsUnlimited
                    : context.S.acCreditsUsed(used.toStringAsFixed(1)),
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
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
    final scheme = theme.colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.lg),
        child: Row(
          children: [
            _AccountMark(
              label: account.label.isNotEmpty
                  ? account.label.characters.first.toUpperCase()
                  : '?',
              filled: true,
              size: 56,
            ),
            const SizedBox(width: AppSpacing.md + 2),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(account.label, style: theme.textTheme.titleMedium),
                  const SizedBox(height: AppSpacing.xs / 2),
                  Text(
                    account.subtitle,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                  Text(
                    account.hubUrl,
                    style: theme.textTheme.labelMedium,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ],
              ),
            ),
            if (active) Icon(Icons.check_circle, color: scheme.primary),
          ],
        ),
      ),
    );
  }
}

/// Rounded initial tile so an account reads as a workspace card, not a contact.
class _AccountMark extends StatelessWidget {
  final String label;
  final bool filled;
  final double size;

  const _AccountMark({
    required this.label,
    required this.filled,
    this.size = 40,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: filled
            ? scheme.primary
            : scheme.primary.withValues(alpha: 0.14),
        borderRadius: BorderRadius.circular(
          size >= 56 ? AppRadius.md : AppRadius.sm,
        ),
      ),
      child: Text(
        label,
        style: Theme.of(context).textTheme.titleSmall?.copyWith(
          color: filled ? scheme.onPrimary : scheme.primary,
          fontWeight: FontWeight.w700,
          fontSize: size >= 56 ? 22 : null,
        ),
      ),
    );
  }
}

/// Rounded tinted chip behind a settings tile's leading icon — the modern
/// grouped-settings look (one hue per row).
class _SettingsIcon extends StatelessWidget {
  final IconData icon;
  final Color color;

  const _SettingsIcon({required this.icon, required this.color});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 40,
      height: 40,
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.14),
        borderRadius: BorderRadius.circular(AppRadius.sm),
      ),
      child: Icon(icon, size: 21, color: color),
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
      margin: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: ListTile(
        leading: _AccountMark(
          label: account.label.isNotEmpty
              ? account.label.characters.first.toUpperCase()
              : '?',
          filled: isActive,
        ),
        title: Text(
          account.label,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        subtitle: Text(
          account.subtitle,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        trailing: isActive
            ? Text(
                context.S.acCurrent,
                style: TextStyle(
                  color: theme.colorScheme.primary,
                  fontWeight: FontWeight.w600,
                ),
              )
            : IconButton(
                icon: const Icon(Icons.delete_outline, size: 20),
                tooltip: context.S.acRemoveAccount,
                onPressed: () => _remove(context, ref),
              ),
        onTap: isActive
            ? null
            : () =>
                  ref.read(sessionProvider.notifier).switchAccount(account.key),
      ),
    );
  }

  Future<void> _remove(BuildContext context, WidgetRef ref) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(context.S.acRemoveConfirm),
        content: Text(context.S.acRemoveBody(account.label)),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(context.S.cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(context.S.acRemove),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      await ref.read(sessionProvider.notifier).removeAccount(account.key);
    }
  }
}
