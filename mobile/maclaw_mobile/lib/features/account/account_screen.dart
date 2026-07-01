import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/mobile_bootstrap.dart';
import '../../core/api/official_service.dart';
import '../../core/settings/app_preferences.dart';
import '../../core/storage/mobile_local_store.dart';
import '../../shared/surface.dart';
import '../assistant/assistant_controller.dart';
import '../auth/session_controller.dart';
import '../digital_employees/digital_employees_controller.dart';
import '../documents/documents_controller.dart';
import '../servers/servers_controller.dart';

class AccountScreen extends ConsumerWidget {
  const AccountScreen({super.key});

  void _showPrivacyInfo(BuildContext context) {
    showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('凭据与隐私'),
        content: const Text(
          '登录 Token、SSH 密码、私钥和私钥口令仅保存在系统安全存储中。终端输出或日志发送给 AI 分析前，需要用户手动触发。',
        ),
        actions: [
          FilledButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('知道了'),
          ),
        ],
      ),
    );
  }

  Future<void> _clearLocalCache(BuildContext context, WidgetRef ref) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('清理本地缓存？'),
        content: const Text(
          '将删除搜索历史、最近文档草稿、服务器配置、命令历史和数字员工任务模板，并清理已保存的 SSH 密码/私钥。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('清理'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    final store = ref.read(mobileLocalStoreProvider);
    final profiles = await store.loadServerProfiles();
    final vault = ref.read(secureVaultProvider);
    await Future.wait([
      for (final profile in profiles) ...[
        vault.deleteServerPassword(profile.id),
        vault.deleteServerPrivateKey(profile.id),
      ],
    ]);
    await store.clearLocalCache();
    ref
      ..invalidate(searchHistoryProvider)
      ..invalidate(documentsControllerProvider)
      ..invalidate(serverProfilesProvider)
      ..invalidate(serverCommandsProvider)
      ..invalidate(digitalEmployeePromptHistoryProvider);

    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('本地缓存和已保存 SSH 凭据已清理')),
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionControllerProvider).valueOrNull;
    final bootstrap = session?.bootstrap;
    final preferences = ref.watch(appPreferencesProvider);
    return ScreenScaffold(
      title: '我的',
      subtitle: '官方服务绑定、额度、模型/搜索状态、凭据和本地缓存。',
      trailing: IconButton.filledTonal(
        tooltip: '退出登录',
        onPressed: () => ref.read(sessionControllerProvider.notifier).signOut(),
        icon: const Icon(Icons.logout),
      ),
      children: [
        if (bootstrap == null)
          ActionTile(
            icon: Icons.login_outlined,
            title: '未登录',
            subtitle: '移动端只支持接入 MaClaw 官方服务。',
            actionLabel: '去登录',
            onPressed: () =>
                ref.read(sessionControllerProvider.notifier).signOut(),
          )
        else ...[
          _AccountSummaryCard(
            bootstrap: bootstrap,
            serviceUrl: maclawOfficialServiceUrl,
          ),
          const SizedBox(height: 12),
          _ServiceStatusCard(bootstrap: bootstrap),
          const SizedBox(height: 12),
          _FeatureStatusCard(features: bootstrap.features),
          const SizedBox(height: 12),
          _LimitStatusCard(limits: bootstrap.limits),
        ],
        const SizedBox(height: 12),
        _PreferenceCard(preferences: preferences),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.security_outlined,
          title: '凭据与隐私',
          subtitle: 'Token、SSH 密码、私钥口令保存在系统安全存储中。',
          actionLabel: '查看',
          onPressed: () => _showPrivacyInfo(context),
        ),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.cleaning_services_outlined,
          title: '本地缓存',
          subtitle: '搜索历史、文档草稿、导出记录、服务器配置和命令历史。',
          actionLabel: '清理缓存',
          onPressed: () => _clearLocalCache(context, ref),
        ),
      ],
    );
  }
}

class _PreferenceCard extends ConsumerWidget {
  final AsyncValue<AppPreferences> preferences;

  const _PreferenceCard({required this.preferences});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: preferences.when(
          data: (value) => Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(
                    Icons.settings_outlined,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                  const SizedBox(width: 8),
                  Text('主题与语言', style: Theme.of(context).textTheme.titleMedium),
                ],
              ),
              const SizedBox(height: 12),
              SegmentedButton<ThemeMode>(
                segments: const [
                  ButtonSegment(
                    value: ThemeMode.system,
                    icon: Icon(Icons.phone_android_outlined),
                    label: Text('系统'),
                  ),
                  ButtonSegment(
                    value: ThemeMode.light,
                    icon: Icon(Icons.light_mode_outlined),
                    label: Text('浅色'),
                  ),
                  ButtonSegment(
                    value: ThemeMode.dark,
                    icon: Icon(Icons.dark_mode_outlined),
                    label: Text('深色'),
                  ),
                ],
                selected: {value.themeMode},
                onSelectionChanged: (next) => ref
                    .read(appPreferencesProvider.notifier)
                    .setThemeMode(next.first),
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                value: value.language,
                items: const [
                  DropdownMenuItem(
                    value: appLanguageChinese,
                    child: Text('简体中文'),
                  ),
                  DropdownMenuItem(
                    value: appLanguageEnglish,
                    child: Text('English'),
                  ),
                ],
                onChanged: (next) {
                  if (next == null) return;
                  ref.read(appPreferencesProvider.notifier).setLanguage(next);
                },
                decoration: const InputDecoration(
                  labelText: '语音输入语言',
                  prefixIcon: Icon(Icons.language_outlined),
                ),
              ),
            ],
          ),
          error: (error, _) => Text('偏好设置加载失败：$error'),
          loading: () => const LinearProgressIndicator(),
        ),
      ),
    );
  }
}

class _AccountSummaryCard extends StatelessWidget {
  final MobileBootstrap bootstrap;
  final String serviceUrl;

  const _AccountSummaryCard({
    required this.bootstrap,
    required this.serviceUrl,
  });

  @override
  Widget build(BuildContext context) {
    final identity = bootstrap.user.email.isEmpty
        ? bootstrap.user.userId
        : bootstrap.user.email;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.verified_user_outlined,
                  color: Theme.of(context).colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Text('账号绑定', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 12),
            _InfoRow(label: '用户', value: identity),
            _InfoRow(label: '租户', value: bootstrap.user.tenantId),
            _InfoRow(label: '官方服务', value: serviceUrl),
          ],
        ),
      ),
    );
  }
}

class _ServiceStatusCard extends StatelessWidget {
  final MobileBootstrap bootstrap;

  const _ServiceStatusCard({required this.bootstrap});

  @override
  Widget build(BuildContext context) {
    final services = bootstrap.services;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.hub_outlined,
                  color: Theme.of(context).colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Text('服务状态', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 12),
            _StatusPill(label: 'Hub', value: services.hubStatus),
            const SizedBox(height: 10),
            _InfoRow(label: '模型状态', value: services.llmStatusPath),
            _InfoRow(label: '模型列表', value: services.modelsPath),
            _InfoRow(label: '联网搜索', value: services.searchPath),
            _InfoRow(label: '文档服务', value: services.documentsPath),
            _InfoRow(label: '数字员工', value: services.digitalEmployeesPath),
          ],
        ),
      ),
    );
  }
}

class _FeatureStatusCard extends StatelessWidget {
  final MobileFeatures features;

  const _FeatureStatusCard({required this.features});

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.tune_outlined,
                  color: Theme.of(context).colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Text('功能开关', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                _FeatureChip(label: '查信息', enabled: features.search),
                _FeatureChip(label: '应急文档', enabled: features.documents),
                _FeatureChip(label: '本地 SSH', enabled: features.localSsh),
                _FeatureChip(
                  label: '数字员工',
                  enabled: features.digitalEmployees,
                ),
                _FeatureChip(
                  label: '通知',
                  enabled: features.pushNotifications,
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _LimitStatusCard extends StatelessWidget {
  final MobileLimits limits;

  const _LimitStatusCard({required this.limits});

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.speed_outlined,
                  color: Theme.of(context).colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Text('额度与限制', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 12),
            _InfoRow(label: '上传上限', value: _formatBytes(limits.maxUploadBytes)),
            _InfoRow(label: '并发导出任务', value: '${limits.maxExportJobs}'),
          ],
        ),
      ),
    );
  }

  String _formatBytes(int value) {
    if (value <= 0) return '未配置';
    final mb = value / (1024 * 1024);
    return '${mb.toStringAsFixed(mb >= 10 ? 0 : 1)} MB';
  }
}

class _InfoRow extends StatelessWidget {
  final String label;
  final String value;

  const _InfoRow({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 92,
            child: Text(
              label,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
          ),
          Expanded(child: Text(value.isEmpty ? '未配置' : value)),
        ],
      ),
    );
  }
}

class _StatusPill extends StatelessWidget {
  final String label;
  final String value;

  const _StatusPill({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final online = value.toLowerCase() == 'online';
    return Chip(
      avatar: Icon(
        online ? Icons.check_circle_outline : Icons.info_outline,
        color: online ? scheme.primary : scheme.onSurfaceVariant,
      ),
      label: Text('$label：$value'),
    );
  }
}

class _FeatureChip extends StatelessWidget {
  final String label;
  final bool enabled;

  const _FeatureChip({required this.label, required this.enabled});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return FilterChip(
      selected: enabled,
      onSelected: null,
      avatar: Icon(
        enabled ? Icons.check_circle_outline : Icons.remove_circle_outline,
        color: enabled ? scheme.primary : scheme.onSurfaceVariant,
      ),
      label: Text(label),
    );
  }
}
