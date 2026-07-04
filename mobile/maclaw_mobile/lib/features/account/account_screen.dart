import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/mobile_bootstrap.dart';
import '../../core/api/mobile_credits.dart';
import '../../core/api/mobile_realtime_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../core/settings/app_preferences.dart';
import '../../core/storage/mobile_local_store.dart';
import '../../shared/surface.dart';
import '../assistant/assistant_controller.dart';
import '../auth/session_controller.dart';
import '../digital_employees/digital_employees_controller.dart';
import '../documents/documents_controller.dart';
import '../servers/servers_controller.dart';
import 'llm_qr_authorization_screen.dart';

final mobileLlmServiceStatusProvider =
    FutureProvider.autoDispose<LlmServiceStatus?>((ref) async {
  final session = ref.watch(sessionControllerProvider).valueOrNull;
  final client = ref.watch(apiClientProvider);
  if (session == null || client == null || session.bootstrap == null) {
    return null;
  }
  return client.llmServiceStatus(session.bootstrap!.services.llmStatusPath);
});

class AccountScreen extends ConsumerWidget {
  const AccountScreen({super.key});

  void _showPrivacyInfo(BuildContext context) {
    showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('凭据与隐私'),
        content: const Text(
          '登录 Token、SSH 密码、私钥和私钥口令仅保存在系统安全存储中。终端输出或日志发送给 AI 分析前，需要用户手动确认。',
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

  Future<void> _clearLocalWorkCache(BuildContext context, WidgetRef ref) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('清理本机工作记录？'),
        content: const Text(
          '将删除搜索历史、文档草稿、导入/导出任务、常用命令、数字员工提示、最近任务和本机偏好设置。本操作不会退出官方服务，也不会删除服务器配置或 SSH 凭据。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('清理记录'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    await ref.read(mobileLocalStoreProvider).clearLocalWorkCache();
    ref
      ..invalidate(searchHistoryProvider)
      ..invalidate(documentsControllerProvider)
      ..invalidate(documentDraftHistoryProvider)
      ..invalidate(serverCommandsProvider)
      ..invalidate(digitalEmployeePromptHistoryProvider)
      ..invalidate(digitalEmployeeTaskProvider)
      ..invalidate(digitalEmployeeTaskHistoryProvider)
      ..invalidate(appPreferencesProvider);

    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('本机工作记录已清理，登录态、服务器配置和 SSH 凭据已保留')),
    );
  }

  Future<void> _clearServerAccessData(
    BuildContext context,
    WidgetRef ref,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除服务器资料和 SSH 凭据？'),
        content: const Text(
          '将删除本机保存的服务器 Host、端口、用户名、标签、备注，以及对应 SSH 密码、私钥和私钥口令。官方服务登录不会受影响。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('删除'),
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
    await store.clearServerProfiles();
    ref
      ..invalidate(serverProfilesProvider)
      ..invalidate(serverCommandsProvider);

    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('服务器资料和已保存 SSH 凭据已删除')),
    );
  }

  Future<void> _requestNotificationPermission(
    BuildContext context,
    WidgetRef ref,
  ) async {
    try {
      final result = await ref
          .read(mobileNotificationServiceProvider)
          .requestPermissions();
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(result.message)),
      );
    } catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('通知权限请求失败：$error')),
      );
    }
  }

  Future<void> _refreshOfficialService(
    BuildContext context,
    WidgetRef ref,
  ) async {
    try {
      await ref.read(sessionControllerProvider.notifier).refreshBootstrap();
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('官方服务状态已刷新')),
      );
    } catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('刷新官方服务状态失败：$error')),
      );
    }
  }

  Future<void> _testRealtimeChannel(
    BuildContext context,
    WidgetRef ref,
    MobileBootstrap? bootstrap,
  ) async {
    final services = bootstrap?.services;
    if (services == null || !services.realtimeConfigured) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('官方服务尚未下发实时通道配置')),
      );
      return;
    }
    try {
      await ref
          .read(mobileRealtimeClientProvider)
          .pingOnce(path: services.realtimePath);
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('实时通道自检成功')),
      );
    } catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('实时通道自检失败：$error')),
      );
    }
  }

  Future<void> _openLlmQrAuthorization(BuildContext context) async {
    await Navigator.of(context).push<bool>(
      MaterialPageRoute(
        builder: (context) => const LlmQrAuthorizationScreen(),
      ),
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionControllerProvider).valueOrNull;
    final bootstrap = session?.bootstrap;
    final preferences = ref.watch(appPreferencesProvider);
    final llmServiceStatus = ref.watch(mobileLlmServiceStatusProvider);
    return ScreenScaffold(
      title: '我的',
      subtitle: '官方服务绑定、额度、模型/搜索状态、凭据和本地隐私数据。',
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
            serviceUrl: session?.hubUrl ?? '',
          ),
          const SizedBox(height: 12),
          _HubConnectionCard(
            bootstrap: bootstrap,
            sessionHubUrl: session?.hubUrl ?? '',
            llmServiceStatus: llmServiceStatus,
          ),
          const SizedBox(height: 12),
          ActionTile(
            icon: Icons.qr_code_scanner_outlined,
            title: '第三方 LLM 授权',
            subtitle:
                '默认使用 MaClaw 官方 LLM；如需接入第三方 LLM，只能扫描 MaClaw GUI 生成的授权二维码。',
            actionLabel: '扫码授权',
            onPressed: () => _openLlmQrAuthorization(context),
          ),
          const SizedBox(height: 12),
          _ServiceStatusCard(bootstrap: bootstrap),
          const SizedBox(height: 12),
          ActionTile(
            icon: Icons.sync_outlined,
            title: '刷新官方服务状态',
            subtitle: '重新获取额度、模型/搜索状态、实时通道和功能开关。',
            actionLabel: '刷新',
            onPressed: () => _refreshOfficialService(context, ref),
          ),
          const SizedBox(height: 12),
          ActionTile(
            icon: Icons.sensors_outlined,
            title: '实时通道自检',
            subtitle: '连接官方 WebSocket 并发送一次移动端 ping，用于确认长任务和数字员工状态通道可用。',
            actionLabel: '开始自检',
            onPressed: () => _testRealtimeChannel(context, ref, bootstrap),
          ),
          const SizedBox(height: 12),
          _FeatureStatusCard(features: bootstrap.features),
          const SizedBox(height: 12),
          _LimitStatusCard(limits: bootstrap.limits),
        ],
        const SizedBox(height: 12),
        _PreferenceCard(preferences: preferences),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.notifications_active_outlined,
          title: '通知权限',
          subtitle: _notificationSubtitle(bootstrap?.features),
          actionLabel: '请求权限',
          onPressed: () => _requestNotificationPermission(context, ref),
        ),
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
          title: '本机工作记录',
          subtitle: '清理搜索、文档、导出、命令历史、数字员工临时记录和本机偏好，保留登录态、服务器配置和 SSH 凭据。',
          actionLabel: '清理记录',
          onPressed: () => _clearLocalWorkCache(context, ref),
        ),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.key_off_outlined,
          title: '服务器资料与 SSH 凭据',
          subtitle: '删除本机服务器配置，以及对应 SSH 密码、私钥和私钥口令。',
          actionLabel: '删除资料',
          onPressed: () => _clearServerAccessData(context, ref),
        ),
      ],
    );
  }

  String _notificationSubtitle(MobileFeatures? features) {
    final serviceState = features?.pushNotifications == true
        ? '官方服务已开启通知能力'
        : '官方服务未声明 Push 能力，本机仍可用于前台本地提醒';
    return '$serviceState；用于文档导出、AI 长任务、数字员工任务和 SSH 连接异常提醒。';
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
                initialValue: value.language,
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
    final identity = _mobileAccountIdentity(bootstrap.user);
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
            _InfoRow(label: identity.label, value: identity.value),
            if (identity.creditsHint.isNotEmpty)
              _InfoRow(label: '额度账户', value: identity.creditsHint),
            _InfoRow(label: '租户', value: bootstrap.user.tenantId),
            _InfoRow(label: '官方服务', value: serviceUrl),
          ],
        ),
      ),
    );
  }

  _MobileAccountIdentity _mobileAccountIdentity(MobileUser user) {
    final explicitPhone = user.phoneNumber.trim();
    final creditsAccount = trustedPhoneCreditsAccount(user.creditsAccount);
    if (explicitPhone.isNotEmpty) {
      return _MobileAccountIdentity(
        label: '手机号',
        value: 'phone:${_maskPhoneNumber(explicitPhone)}',
        creditsHint: _creditsHint(creditsAccount),
      );
    }
    final rawIdentity = [
      user.accountId,
      user.email,
      user.userId,
    ].map((value) => value.trim()).firstWhere(
          (value) => value.isNotEmpty,
          orElse: () => '',
        );
    final lower = rawIdentity.toLowerCase();
    if (lower.startsWith('phone:')) {
      final phone = rawIdentity.substring(rawIdentity.indexOf(':') + 1);
      return _MobileAccountIdentity(
        label: '手机号',
        value: 'phone:${_maskPhoneNumber(phone)}',
        creditsHint: _creditsHint(
          creditsAccount.isEmpty ? rawIdentity : creditsAccount,
        ),
      );
    }
    return _MobileAccountIdentity(
      label: '账号',
      value: rawIdentity,
      creditsHint: creditsAccount.isEmpty
          ? ''
          : 'MaClaw 官方 credits: ${_maskCreditsAccount(creditsAccount)}',
    );
  }

  String _creditsHint(String creditsAccount) {
    if (creditsAccount.trim().isEmpty) {
      return 'MaClaw 官方 credits 使用该手机号账户';
    }
    return 'MaClaw 官方 credits 使用 ${_maskCreditsAccount(creditsAccount)}';
  }
}

class _MobileAccountIdentity {
  final String label;
  final String value;
  final String creditsHint;

  const _MobileAccountIdentity({
    required this.label,
    required this.value,
    required this.creditsHint,
  });
}

class _ServiceStatusCard extends StatelessWidget {
  final MobileBootstrap bootstrap;

  const _ServiceStatusCard({required this.bootstrap});

  @override
  Widget build(BuildContext context) {
    final services = bootstrap.services;
    final criticalReady = _serviceStatusHealthy(services.hubStatus) &&
        _serviceStatusHealthy(services.llmStatus) &&
        _serviceStatusHealthy(services.searchStatus) &&
        _serviceStatusHealthy(services.documentsStatus) &&
        _serviceStatusHealthy(services.digitalEmployeesStatus) &&
        services.realtimeConfigured;
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
            Text(
              criticalReady ? '应急能力可用' : '部分能力需要检查',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: criticalReady
                        ? Theme.of(context).colorScheme.primary
                        : Theme.of(context).colorScheme.error,
                  ),
            ),
            const SizedBox(height: 8),
            _StatusPill(label: 'Hub', value: services.hubStatus),
            _StatusPill(label: '模型/LLM', value: services.llmStatus),
            _StatusPill(label: '联网搜索', value: services.searchStatus),
            _StatusPill(label: '文档服务', value: services.documentsStatus),
            _StatusPill(
              label: '数字员工',
              value: services.digitalEmployeesStatus,
            ),
            _StatusPill(
              label: '实时',
              value: services.realtimeConfigured ? 'configured' : 'missing',
            ),
            const SizedBox(height: 12),
            _InfoRow(label: '模型状态接口', value: services.llmStatusPath),
            _InfoRow(label: '模型列表', value: services.modelsPath),
            _InfoRow(label: '联网搜索接口', value: services.searchPath),
            _InfoRow(label: '文档服务接口', value: services.documentsPath),
            _InfoRow(label: '数字员工接口', value: services.digitalEmployeesPath),
            _InfoRow(label: '实时通道', value: services.realtimePath),
          ],
        ),
      ),
    );
  }
}

class _HubConnectionCard extends StatelessWidget {
  final MobileBootstrap bootstrap;
  final String sessionHubUrl;
  final AsyncValue<LlmServiceStatus?> llmServiceStatus;

  const _HubConnectionCard({
    required this.bootstrap,
    required this.sessionHubUrl,
    required this.llmServiceStatus,
  });

  @override
  Widget build(BuildContext context) {
    final connection = bootstrap.connection;
    final llmAccess = bootstrap.llmAccess;
    final hubUrl =
        connection.hubUrl.isEmpty ? sessionHubUrl : connection.hubUrl;
    final tenantId = connection.tenantId.isEmpty
        ? bootstrap.user.tenantId
        : connection.tenantId;
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
                Text('Hub 接入', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 12),
            _InfoRow(
              label: '候选',
              value: connection.hubCenterCandidates.isEmpty
                  ? '未下发'
                  : connection.hubCenterCandidates.join(' / '),
            ),
            _InfoRow(
              label: 'HubCenter',
              value: connection.selectedHubCenterUrl,
            ),
            _InfoRow(label: 'Hub', value: hubUrl),
            _InfoRow(label: 'Hub ID', value: connection.hubId),
            _InfoRow(label: '租户', value: tenantId),
            _InfoRow(label: 'LLM', value: _llmAccessLabel(llmAccess)),
            _InfoRow(
              label: 'LLM 状态',
              value: _serviceStatusLabel(llmAccess.status),
            ),
            if (llmAccess.desktopQrDelegated)
              _InfoRow(label: '授权', value: _llmAuthorizationLabel(llmAccess)),
            const Divider(height: 24),
            Text('官方 credits', style: Theme.of(context).textTheme.titleSmall),
            const SizedBox(height: 8),
            _LlmCreditsStatusRows(
              status: llmServiceStatus,
              fallbackAccount: trustedBootstrapCreditsAccount(bootstrap),
            ),
          ],
        ),
      ),
    );
  }
}

class _LlmCreditsStatusRows extends StatelessWidget {
  final AsyncValue<LlmServiceStatus?> status;
  final String fallbackAccount;

  const _LlmCreditsStatusRows({
    required this.status,
    required this.fallbackAccount,
  });

  @override
  Widget build(BuildContext context) {
    return status.when(
      data: (value) {
        if (value == null) {
          return _InfoRow(
            label: '额度',
            value: fallbackAccount.isEmpty
                ? '未下发'
                : _maskCreditsAccount(fallbackAccount),
          );
        }
        final inactive = value.inactiveReasons.join('；');
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _InfoRow(label: '服务', value: value.active ? '可用' : '不可用'),
            if (fallbackAccount.isNotEmpty)
              _InfoRow(
                label: '额度账户',
                value: _maskCreditsAccount(fallbackAccount),
              ),
            _InfoRow(
              label: '可用额度',
              value: _formatCredits(value.creditsAvailable),
            ),
            _InfoRow(
              label: '剩余额度',
              value: _formatCredits(value.creditsRemaining),
            ),
            _InfoRow(label: '已用额度', value: _formatCredits(value.creditsUsed)),
            if (value.creditsTotal > 0)
              _InfoRow(label: '总额度', value: _formatCredits(value.creditsTotal)),
            if (value.defaultModel.isNotEmpty)
              _InfoRow(label: '默认模型', value: value.defaultModel),
            if (value.serviceGroupNames.isNotEmpty)
              _InfoRow(
                label: '服务组',
                value: value.serviceGroupNames.join(' / '),
              ),
            if (value.nearestExpiresAt.isNotEmpty)
              _InfoRow(label: '到期', value: value.nearestExpiresAt),
            if (inactive.isNotEmpty) _InfoRow(label: '提示', value: inactive),
          ],
        );
      },
      error: (error, _) => _InfoRow(label: '额度', value: '读取失败：$error'),
      loading: () => const _InfoRow(label: '额度', value: '正在读取...'),
    );
  }
}

String _maskCreditsAccount(String account) {
  final value = account.trim();
  if (!value.toLowerCase().startsWith('phone:')) return value;
  final phone = value.substring(value.indexOf(':') + 1);
  return 'phone:${_maskPhoneNumber(phone)}';
}

String _maskPhoneNumber(String phone) {
  final digits = phone.replaceAll(RegExp(r'\D'), '');
  if (digits.length <= 7) return digits;
  return '${digits.substring(0, 3)}****${digits.substring(digits.length - 4)}';
}

String _formatCredits(double value) {
  if (value == value.roundToDouble()) {
    return value.toStringAsFixed(0);
  }
  return value.toStringAsFixed(2);
}

String _llmAuthorizationLabel(MobileLlmAccess access) {
  final parts = [
    if (access.authorizationId.isNotEmpty) access.authorizationId,
    if (access.authorizedBy.isNotEmpty) '来自 ${access.authorizedBy}',
    if (access.authorizedAt != null) access.authorizedAt!.toLocal().toString(),
  ];
  return parts.isEmpty ? '桌面 GUI 二维码授权' : parts.join(' · ');
}

String _llmAccessLabel(MobileLlmAccess access) {
  if (access.desktopQrDelegated) {
    return access.authorizationId.isEmpty
        ? '桌面 GUI 二维码授权的第三方 LLM'
        : '桌面 GUI 二维码授权的第三方 LLM（${access.authorizationId}）';
  }
  if (access.official) return 'MaClaw 官方 LLM';
  return access.mode.isEmpty ? '未声明' : access.mode;
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
            _InfoRow(
              label: '上传上限',
              value: _limitBytesLabel(limits.maxUploadBytes),
            ),
            _InfoRow(label: '并发导出任务', value: '${limits.maxExportJobs}'),
          ],
        ),
      ),
    );
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
    final lower = value.toLowerCase();
    final healthy = _serviceStatusHealthy(lower);
    return Tooltip(
      message: value.isEmpty ? 'empty' : value,
      child: Chip(
        avatar: Icon(
          healthy ? Icons.check_circle_outline : Icons.info_outline,
          color: healthy ? scheme.primary : scheme.error,
        ),
        label: Text('$label：${_serviceStatusLabel(value)}'),
        backgroundColor:
            healthy ? scheme.secondaryContainer : scheme.errorContainer,
        labelStyle: TextStyle(
          color:
              healthy ? scheme.onSecondaryContainer : scheme.onErrorContainer,
        ),
      ),
    );
  }
}

String _limitBytesLabel(int value) {
  if (value <= 0) return '未配置';
  return formatMobileFileSize(value);
}

bool _serviceStatusHealthy(String value) {
  final lower = value.toLowerCase().trim();
  return lower == 'online' ||
      lower == 'configured' ||
      lower == 'available' ||
      lower == 'ready' ||
      lower == 'enabled';
}

String _serviceStatusLabel(String value) {
  return switch (value.toLowerCase().trim()) {
    'online' => '在线',
    'configured' => '已配置',
    'available' => '可用',
    'ready' => '就绪',
    'enabled' => '已开启',
    'missing' => '未配置',
    'offline' => '离线',
    'unavailable' => '不可用',
    'disabled' => '已关闭',
    '' => '未配置',
    _ => value,
  };
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
      label: Text('$label：${enabled ? '已开启' : '未开启'}'),
    );
  }
}
