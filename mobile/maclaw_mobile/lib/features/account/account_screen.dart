import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_bootstrap.dart';
import '../../core/api/mobile_credits.dart';
import '../../core/api/mobile_realtime_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../core/notifications/mobile_push_sync.dart';
import '../../core/settings/app_preferences.dart';
import '../../core/storage/mobile_local_store.dart';
import '../../l10n/app_strings.dart';
import '../../shared/surface.dart';
import '../assistant/assistant_controller.dart';
import '../auth/session_controller.dart';
import '../digital_employees/digital_employees_controller.dart';
import '../documents/documents_controller.dart';
import '../servers/servers_controller.dart';
import '../tasks/mobile_jobs_provider.dart';
import 'account_agent_status_card.dart';
import 'card_store_sheet.dart';
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

final accountRealtimeCheckProvider = StateProvider<String?>((ref) => null);

class AccountScreen extends ConsumerWidget {
  const AccountScreen({super.key});

  void _showPrivacyInfo(BuildContext context) {
    showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('凭据与隐私'),
        content: const Text(
          '登录 Token 保存在系统安全存储中。服务器档案 metadata 缓存在手机；可选的 SSH 密钥加密存于 Hub 凭据库（hub_exec），密钥不下发手机。后台会话输出或日志发送给 AI 分析前，需要用户手动确认。',
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
          '将删除助手历史、文档草稿、导入/导出任务、常用命令、数字员工提示、最近任务和本机偏好设置。本操作不会退出官方服务，也不会删除手机侧服务器档案缓存。',
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
      const SnackBar(content: Text('本机工作记录已清理，登录态和服务器档案缓存已保留')),
    );
  }

  Future<void> _clearServerAccessData(
    BuildContext context,
    WidgetRef ref,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('清理服务器档案缓存？'),
        content: const Text(
          '将删除手机本机缓存的服务器 Host、端口、用户名、标签和备注，并清理历史版本可能留下的本机 SSH 凭据残留。官方服务登录不会受影响。',
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
      for (final profile in profiles)
        vault.clearLegacyServerCredentials(profile.id),
    ]);
    await store.clearServerProfiles();
    ref
      ..invalidate(serverProfilesProvider)
      ..invalidate(serverCommandsProvider);

    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('服务器档案缓存已清理')),
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
      // Bind this install to Hub for pending/remote fan-out after opt-in.
      final client = ref.read(apiClientProvider);
      final bootstrap =
          ref.read(sessionControllerProvider).valueOrNull?.bootstrap;
      await registerMobilePushDevice(
        client: client,
        services: bootstrap?.services,
      );
      await syncMobilePushPending(
        client: client,
        notify: ref.read(mobileNotificationServiceProvider),
        features: bootstrap?.features,
        services: bootstrap?.services,
      );
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
      ref.invalidate(documentQuotaProvider);
      ref.invalidate(entitlementsCapsProvider);
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
    ref.read(accountRealtimeCheckProvider.notifier).state = 'checking';
    final services = bootstrap?.services;
    if (services == null || !services.realtimeConfigured) {
      ref.read(accountRealtimeCheckProvider.notifier).state = 'failed';
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('官方服务尚未下发实时通道配置')),
      );
      return;
    }
    try {
      await ref
          .read(mobileRealtimeClientProvider)
          .pingOnce(path: services.realtimePath);
      ref.read(accountRealtimeCheckProvider.notifier).state = 'success';
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('实时通道自检成功')),
      );
    } catch (error) {
      ref.read(accountRealtimeCheckProvider.notifier).state = 'failed';
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

  Future<void> _revokeThirdPartyLlmAuthorization(
    BuildContext context,
    WidgetRef ref,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('撤销第三方 LLM 授权？'),
        content: const Text(
          '撤销后，移动端会立即恢复使用该手机号账户的 MaClaw 官方 credits。'
          '之后仍可通过桌面 GUI 重新扫码授权。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('撤销授权'),
          ),
        ],
      ),
    );
    if (confirmed != true || !context.mounted) return;
    try {
      await ref
          .read(sessionControllerProvider.notifier)
          .revokeThirdPartyLlmAuthorization();
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('已恢复使用 MaClaw 官方 LLM credits')),
      );
    } catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('撤销第三方 LLM 授权失败：$error')),
      );
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionControllerProvider).valueOrNull;
    final bootstrap = session?.bootstrap;
    final preferences = ref.watch(appPreferencesProvider);
    final llmServiceStatus = ref.watch(mobileLlmServiceStatusProvider);
    final realtimeCheck = ref.watch(accountRealtimeCheckProvider);
    final s = ref.watch(appStringsProvider);
    return ScreenScaffold(
      title: s.accountTitle,
      subtitle: s.accountSubtitle,
      trailing: IconButton.filledTonal(
        tooltip: '退出登录',
        onPressed: () => ref.read(sessionControllerProvider.notifier).signOut(),
        icon: const Icon(Icons.logout),
      ),
      children: [
        if (realtimeCheck != null) ...[
          StatusBanner(
            tone: realtimeCheck == 'success'
                ? StatusTone.success
                : realtimeCheck == 'failed'
                    ? StatusTone.danger
                    : StatusTone.info,
            icon: realtimeCheck == 'success'
                ? Icons.check_circle_outline
                : realtimeCheck == 'failed'
                    ? Icons.error_outline
                    : Icons.sync,
            title: _realtimeCheckTitle(realtimeCheck),
            message: _realtimeCheckSubtitle(realtimeCheck),
          ),
          const SizedBox(height: 12),
        ],
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
          if (!isMobileLlmConfigured(bootstrap)) ...[
            ActionTile(
              icon: Icons.settings_suggest_outlined,
              title: '配置 MaClaw LLM 服务',
              subtitle:
                  '当前账户尚未绑定可用 LLM。官方服务通过手机号账户 credits 使用；如需第三方服务，请扫描 MaClaw GUI 生成的授权二维码。',
              actionLabel: '打开配置',
              onPressed: () => _openLlmQrAuthorization(context),
            ),
            const SizedBox(height: 12),
          ],
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
                '默认使用 MaClaw 官方 LLM；如需接入第三方 LLM，只能扫描或粘贴 MaClaw 桌面 GUI 生成的授权二维码。',
            actionLabel: '扫码授权',
            onPressed: () => _openLlmQrAuthorization(context),
          ),
          if (bootstrap.llmAccess.desktopQrDelegated) ...[
            const SizedBox(height: 12),
            ActionTile(
              icon: Icons.undo_outlined,
              title: '恢复官方 LLM',
              subtitle: '撤销当前桌面 GUI 二维码授权，恢复使用手机号账户绑定的 MaClaw 官方 credits。',
              actionLabel: '撤销授权',
              onPressed: () => _revokeThirdPartyLlmAuthorization(context, ref),
            ),
          ],
          const SizedBox(height: 12),
          _ServiceStatusCard(bootstrap: bootstrap),
          const SizedBox(height: 12),
          ActionTile(
            icon: Icons.sync_outlined,
            title: '刷新官方服务状态',
            subtitle: '重新获取额度、模型/助手联网状态、实时通道和功能开关。',
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
          const _RealtimeLiveStatusCard(),
          const SizedBox(height: 12),
          const AccountAgentStatusCard(),
          const SizedBox(height: 12),
          _FeatureStatusCard(features: bootstrap.features),
          const SizedBox(height: 12),
          _LimitStatusCard(
            limits: mergeDocumentQuotaLimits(
              bootstrap.limits,
              ref.watch(documentQuotaProvider).valueOrNull,
            ),
            liveQuota: ref.watch(documentQuotaProvider).valueOrNull,
          ),
          const SizedBox(height: 12),
          const _LivePlanCapsCard(),
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
          subtitle: 'Token 保存在系统安全存储中；服务器 SSH 凭据由 MaClaw GUI/agent 管理。',
          actionLabel: '查看',
          onPressed: () => _showPrivacyInfo(context),
        ),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.cleaning_services_outlined,
          title: '本机工作记录',
          subtitle: '清理助手历史、文档、导出、命令历史、数字员工临时记录和本机偏好，保留登录态和服务器档案缓存。',
          actionLabel: '清理记录',
          onPressed: () => _clearLocalWorkCache(context, ref),
        ),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.key_off_outlined,
          title: '服务器档案缓存',
          subtitle: '删除手机本机缓存的服务器档案；真实 SSH 凭据仍由 MaClaw GUI/agent 管理。',
          actionLabel: '清理缓存',
          onPressed: () => _clearServerAccessData(context, ref),
        ),
      ],
    );
  }

  String _realtimeCheckTitle(String status) {
    return switch (status) {
      'checking' => '\u5b9e\u65f6\u901a\u9053\u68c0\u67e5\u4e2d',
      'success' => '\u5b9e\u65f6\u901a\u9053\u53ef\u7528',
      _ => '\u5b9e\u65f6\u901a\u9053\u68c0\u67e5\u5931\u8d25',
    };
  }

  String _realtimeCheckSubtitle(String status) {
    return switch (status) {
      'checking' =>
        '\u6b63\u5728\u8fde\u63a5\u5b98\u65b9 WebSocket\uff0c\u8bf7\u7a0d\u5019\u2026',
      'success' =>
        '\u5df2\u53d1\u9001\u79fb\u52a8\u7aef ping\uff0c\u53ef\u7ee7\u7eed\u4f7f\u7528\u957f\u4efb\u52a1\u548c\u6570\u5b57\u5458\u5de5\u72b6\u6001\u901a\u9053\u3002',
      _ =>
        '\u8bf7\u68c0\u67e5\u5b98\u65b9 Hub \u7f51\u7edc\u914d\u7f6e\u540e\u91cd\u8bd5\u3002',
    };
  }

  String _notificationSubtitle(MobileFeatures? features) {
    final remote = features?.pushNotifications == true
        ? '远程 Push 已配置（Webhook/FCM）'
        : '远程 Push 未配置';
    final pending = features?.pushPendingSync != false
        ? '冷启动可同步离线完成队列'
        : '离线队列未开启';
    return '$remote；$pending；本机本地通知用于文档/助手/员工/SSH 终态与进度。';
  }
}

class _PreferenceCard extends ConsumerWidget {
  final AsyncValue<AppPreferences> preferences;

  const _PreferenceCard({required this.preferences});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: preferences.when(
          data: (value) => Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SectionHeader(
                icon: Icons.settings_outlined,
                title: s.themeAndLanguage,
              ),
              const SizedBox(height: 12),
              SegmentedButton<ThemeMode>(
                segments: [
                  ButtonSegment(
                    value: ThemeMode.system,
                    icon: const Icon(Icons.phone_android_outlined),
                    label: Text(s.themeSystem),
                  ),
                  ButtonSegment(
                    value: ThemeMode.light,
                    icon: const Icon(Icons.light_mode_outlined),
                    label: Text(s.themeLight),
                  ),
                  ButtonSegment(
                    value: ThemeMode.dark,
                    icon: const Icon(Icons.dark_mode_outlined),
                    label: Text(s.themeDark),
                  ),
                ],
                selected: {value.themeMode},
                onSelectionChanged: (next) => ref
                    .read(appPreferencesProvider.notifier)
                    .setThemeMode(next.first),
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                key: ValueKey<String>('lang-${value.language}'),
                initialValue: value.language,
                items: [
                  DropdownMenuItem(
                    value: appLanguageSystem,
                    child: Text(s.languageSystem),
                  ),
                  DropdownMenuItem(
                    value: appLanguageChinese,
                    child: Text(s.languageChinese),
                  ),
                  DropdownMenuItem(
                    value: appLanguageEnglish,
                    child: Text(s.languageEnglish),
                  ),
                ],
                onChanged: (next) {
                  if (next == null) return;
                  ref.read(appPreferencesProvider.notifier).setLanguage(next);
                },
                decoration: InputDecoration(
                  labelText: s.speechLanguage,
                  prefixIcon: const Icon(Icons.language_outlined),
                  helperText: s.languageHint,
                ),
              ),
            ],
          ),
          error: (error, _) => Text('${s.preferencesLoadFailed}：$error'),
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
            const SectionHeader(
              icon: Icons.verified_user_outlined,
              title: '账号绑定',
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
            const SectionHeader(
              icon: Icons.hub_outlined,
              title: '服务状态',
            ),
            const SizedBox(height: 12),
            Text(
              criticalReady ? '应急能力可用' : '部分能力需要检查',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: criticalReady
                        ? Theme.of(context).colorScheme.secondary
                        : Theme.of(context).colorScheme.error,
                    fontWeight: FontWeight.w600,
                  ),
            ),
            const SizedBox(height: 8),
            _StatusPill(label: 'Hub', value: services.hubStatus),
            _StatusPill(label: '模型/LLM', value: services.llmStatus),
            _StatusPill(label: '助手联网', value: services.searchStatus),
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
            _InfoRow(label: '助手联网接口', value: services.searchPath),
            _InfoRow(label: '文档服务接口', value: services.documentsPath),
            _InfoRow(label: '数字员工接口', value: services.digitalEmployeesPath),
            _InfoRow(label: '实时通道', value: services.realtimePath),
          ],
        ),
      ),
    );
  }
}

class _HubConnectionCard extends ConsumerWidget {
  final MobileBootstrap bootstrap;
  final String sessionHubUrl;
  final AsyncValue<LlmServiceStatus?> llmServiceStatus;

  const _HubConnectionCard({
    required this.bootstrap,
    required this.sessionHubUrl,
    required this.llmServiceStatus,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
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
            const SectionHeader(
              icon: Icons.hub_outlined,
              title: 'Hub 接入',
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
            _InfoRow(
              label: '套餐',
              value: _planLabel(bootstrap.entitlements.plan),
            ),
            if (bootstrap.entitlements.serviceActive ||
                bootstrap.entitlements.hasServiceCardGrant)
              _InfoRow(
                label: '服务授权',
                value: [
                  if (bootstrap.entitlements.hasServiceCardGrant) '授权卡',
                  if (bootstrap.entitlements.serviceGroupCount > 0)
                    '组 ${bootstrap.entitlements.serviceGroupCount}',
                  if (bootstrap.entitlements.creditsAvailable > 0)
                    '可用 ${_formatCredits(bootstrap.entitlements.creditsAvailable)}',
                ].where((s) => s.isNotEmpty).join(' · '),
              ),
            _InfoRow(
              label: 'Hub SSH',
              value: bootstrap.entitlements.hubSshExec ||
                      bootstrap.features.backendSshSessions
                  ? (bootstrap.entitlements.hubSshExec
                      ? '支持 hub_exec + 桌面 claim'
                      : '桌面 claim')
                  : '未启用',
            ),
            _InfoRow(
              label: '共享数字员工',
              value: bootstrap.entitlements.sharedEmployees
                  ? '已开通（租户/共享池可见）'
                  : '仅自己的分身（免费档）',
            ),
            _InfoRow(
              label: '云端 Agent',
              value: bootstrap.entitlements.mobileAgent ? '可用' : '不可用',
            ),
            if (bootstrap.entitlements.documentQuotaBytes > 0)
              _InfoRow(
                label: '文档配额(套餐)',
                value: formatMobileFileSize(
                  bootstrap.entitlements.documentQuotaBytes,
                ),
              ),
            if (bootstrap.entitlements.maxExportJobs > 0)
              _InfoRow(
                label: '并发导出(套餐)',
                value: '${bootstrap.entitlements.maxExportJobs}',
              ),
            if (bootstrap.entitlements.hubFileDownloadMaxBytes > 0)
              _InfoRow(
                label: 'hub_exec 下载上限',
                value: formatMobileFileSize(
                  bootstrap.entitlements.hubFileDownloadMaxBytes,
                ),
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
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              children: [
                TextButton.icon(
                  onPressed: () => _showRedeemServiceCardDialog(context),
                  icon: const Icon(Icons.card_giftcard_outlined, size: 18),
                  label: const Text('兑换服务卡'),
                ),
                TextButton.icon(
                  onPressed: () {
                    final client = ref.read(apiClientProvider);
                    if (client == null) {
                      ScaffoldMessenger.of(context).showSnackBar(
                        const SnackBar(content: Text('请先登录官方服务')),
                      );
                      return;
                    }
                    unawaited(
                      showMobileCardStoreSheet(
                        context,
                        client: client,
                        account: bootstrap.user.email.isNotEmpty
                            ? bootstrap.user.email
                            : bootstrap.user.creditsAccount,
                        tenantId: bootstrap.user.tenantId.isNotEmpty
                            ? bootstrap.user.tenantId
                            : bootstrap.connection.tenantId,
                      ),
                    );
                  },
                  icon: const Icon(Icons.storefront_outlined, size: 18),
                  label: const Text('购买服务卡'),
                ),
              ],
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
            const SectionHeader(
              icon: Icons.tune_outlined,
              title: '功能开关',
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                _FeatureChip(label: '助手联网', enabled: features.search),
                _FeatureChip(label: '应急文档', enabled: features.documents),
                _FeatureChip(
                  label: '后台 SSH',
                  enabled: features.backendSshSessions,
                ),
                _FeatureChip(
                  label: '数字员工',
                  enabled: features.digitalEmployees,
                ),
                const _FeatureChip(label: '本地通知', enabled: true),
                if (features.pushNotifications)
                  const _FeatureChip(label: '远程 Push', enabled: true),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

/// Shows whether the long-lived realtime socket (pty_input / task push) is up.
class _RealtimeLiveStatusCard extends ConsumerWidget {
  const _RealtimeLiveStatusCard();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sender = ref.watch(mobileRealtimeSenderProvider);
    final bootstrap =
        ref.watch(sessionControllerProvider).valueOrNull?.bootstrap;
    final configured = bootstrap?.services.realtimeConfigured ?? false;
    final live = sender != null;
    final scheme = Theme.of(context).colorScheme;
    final tone = !configured
        ? scheme.outline
        : live
            ? scheme.primary
            : scheme.error;
    final title = !configured
        ? '实时通道未配置'
        : live
            ? '实时通道已连接'
            : '实时通道未连接（将自动重连）';
    final binaryPty = ref.watch(mobileRealtimeBinaryPtyProvider);
    final body = !configured
        ? 'bootstrap 未下发 realtime_path。'
        : live
            ? (binaryPty
                ? 'WebSocket 在线：MCP1 二进制 PTY + 任务推送可用。'
                : 'WebSocket 在线：任务推送与 hub_exec pty_input 可用。')
            : '暂无可用 sender；后台会自动重连，也可点「实时通道自检」。';
    return Card(
      child: ListTile(
        leading: Icon(Icons.sensors, color: tone),
        title: Text(title),
        subtitle: Text(body),
        trailing: Chip(
          visualDensity: VisualDensity.compact,
          avatar: Icon(Icons.circle, size: 10, color: tone),
          label: Text(live ? '在线' : (configured ? '离线' : '未配置')),
        ),
      ),
    );
  }
}

/// In-memory ops token for caps PUT (never persisted).
final capsAdminTokenMemoryProvider = StateProvider<String>((ref) => '');

/// Live Hub plan matrix (GET /api/mobile/entitlements/caps).
class _LivePlanCapsCard extends ConsumerWidget {
  const _LivePlanCapsCard();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final capsAsync = ref.watch(entitlementsCapsProvider);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SectionHeader(
              icon: Icons.workspace_premium_outlined,
              title: '套餐权益（实时）',
              action: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  IconButton(
                    tooltip: '运维覆盖 caps',
                    onPressed: () => unawaited(
                      showMobileCapsAdminSheet(context, ref),
                    ),
                    icon: const Icon(Icons.admin_panel_settings_outlined, size: 20),
                  ),
                  IconButton(
                    tooltip: '刷新权益',
                    onPressed: () => ref.invalidate(entitlementsCapsProvider),
                    icon: const Icon(Icons.refresh, size: 20),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 8),
            capsAsync.when(
              data: (caps) {
                if (caps == null) {
                  return Text(
                    '未登录或 Hub 暂不可用',
                    style: Theme.of(context).textTheme.bodySmall,
                  );
                }
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    _InfoRow(label: '套餐', value: _planLabel(caps.plan)),
                    _InfoRow(
                      label: '云端 Agent',
                      value: caps.mobileAgent ? '可用' : '不可用',
                    ),
                    _InfoRow(
                      label: '文档 AI',
                      value: caps.documentAi ? '可用' : '不可用',
                    ),
                    _InfoRow(
                      label: '共享员工',
                      value: caps.sharedEmployees ? '已开通' : '仅自己的分身',
                    ),
                    _InfoRow(
                      label: 'Hub SSH',
                      value: caps.hubSshExec ? 'hub_exec 可用' : '未启用',
                    ),
                    if (caps.documentQuotaBytes > 0)
                      _InfoRow(
                        label: '文档配额',
                        value: formatMobileFileSize(caps.documentQuotaBytes),
                      ),
                    if (caps.maxUploadBytes > 0)
                      _InfoRow(
                        label: '上传上限',
                        value: formatMobileFileSize(caps.maxUploadBytes),
                      ),
                    if (caps.maxExportJobs > 0)
                      _InfoRow(
                        label: '并发导出',
                        value: '${caps.maxExportJobs}',
                      ),
                    if (caps.hubFileDownloadMaxBytes > 0)
                      _InfoRow(
                        label: 'hub_exec 下载上限',
                        value: formatMobileFileSize(
                          caps.hubFileDownloadMaxBytes,
                        ),
                      ),
                    if (caps.hubFileDownloadChunked)
                      _InfoRow(
                        label: '分块下载',
                        value: caps.hubFileChunkRawBytes > 0
                            ? '已启用 · 块 ${formatMobileFileSize(caps.hubFileChunkRawBytes)}'
                                '${caps.hubFileSingleShotBytes > 0 ? ' · 单次≤${formatMobileFileSize(caps.hubFileSingleShotBytes)}' : ''}'
                            : '已启用',
                      ),
                    if (caps.hasRuntimeOverrides) ...[
                      const SizedBox(height: 8),
                      Text(
                        '运行时覆盖（进程内）',
                        style: Theme.of(context).textTheme.labelMedium,
                      ),
                      const SizedBox(height: 4),
                      for (final e in caps.runtimeOverrides.entries)
                        if (e.value > 0)
                          Padding(
                            padding: const EdgeInsets.only(bottom: 2),
                            child: Text(
                              '${e.key}=${e.value}',
                              style: Theme.of(context)
                                  .textTheme
                                  .bodySmall
                                  ?.copyWith(fontFamily: 'monospace'),
                            ),
                          ),
                    ],
                    if (caps.envOverrides.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Text(
                        '运维 env / admin token 键名',
                        style: Theme.of(context).textTheme.labelMedium,
                      ),
                      const SizedBox(height: 4),
                      for (final e in caps.envOverrides.entries)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 2),
                          child: Text(
                            '${e.key}: ${e.value}',
                            style: Theme.of(context)
                                .textTheme
                                .bodySmall
                                ?.copyWith(fontFamily: 'monospace'),
                          ),
                        ),
                    ],
                    Align(
                      alignment: Alignment.centerLeft,
                      child: TextButton.icon(
                        onPressed: () => unawaited(
                          showMobileCapsAdminSheet(context, ref),
                        ),
                        icon: const Icon(Icons.tune, size: 18),
                        label: const Text('运维覆盖（进程内）'),
                      ),
                    ),
                    if (caps.serverTime.isNotEmpty) ...[
                      const SizedBox(height: 6),
                      Text(
                        'server_time ${caps.serverTime}',
                        style: Theme.of(context).textTheme.labelSmall?.copyWith(
                              color: Theme.of(context)
                                  .colorScheme
                                  .onSurfaceVariant,
                            ),
                      ),
                    ],
                  ],
                );
              },
              loading: () => const Padding(
                padding: EdgeInsets.symmetric(vertical: 8),
                child: LinearProgressIndicator(),
              ),
              error: (e, _) => Text(
                '权益加载失败：$e',
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

Future<void> showMobileCapsAdminSheet(
  BuildContext context,
  WidgetRef ref,
) async {
  final client = ref.read(apiClientProvider);
  if (client == null) {
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('请先登录官方服务')),
    );
    return;
  }
  final caps = ref.read(entitlementsCapsProvider).valueOrNull;
  await showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (ctx) {
      return Padding(
        padding: EdgeInsets.only(
          bottom: MediaQuery.viewInsetsOf(ctx).bottom,
        ),
        child: _CapsAdminSheetBody(
          client: client,
          initialRuntime: caps?.runtimeOverrides ?? const {},
          rememberedToken: ref.read(capsAdminTokenMemoryProvider),
          onTokenRemembered: (token) {
            ref.read(capsAdminTokenMemoryProvider.notifier).state = token;
          },
          onApplied: () {
            ref.invalidate(entitlementsCapsProvider);
            ref.invalidate(documentQuotaProvider);
            unawaited(
              ref.read(sessionControllerProvider.notifier).refreshBootstrap(),
            );
          },
        ),
      );
    },
  );
}

class _CapsAdminSheetBody extends StatefulWidget {
  final ApiClient client;
  final Map<String, int> initialRuntime;
  final String rememberedToken;
  final ValueChanged<String> onTokenRemembered;
  final VoidCallback onApplied;

  const _CapsAdminSheetBody({
    required this.client,
    required this.initialRuntime,
    required this.rememberedToken,
    required this.onTokenRemembered,
    required this.onApplied,
  });

  @override
  State<_CapsAdminSheetBody> createState() => _CapsAdminSheetBodyState();
}

class _CapsAdminSheetBodyState extends State<_CapsAdminSheetBody> {
  late final TextEditingController _tokenCtrl;
  late final TextEditingController _docFreeCtrl;
  late final TextEditingController _docPaidCtrl;
  late final TextEditingController _exportFreeCtrl;
  late final TextEditingController _exportPaidCtrl;
  late final TextEditingController _hubDlCtrl;
  bool _busy = false;
  String? _status;

  @override
  void initState() {
    super.initState();
    final rt = widget.initialRuntime;
    _tokenCtrl = TextEditingController(text: widget.rememberedToken);
    _docFreeCtrl = TextEditingController(
      text: _positiveOrEmpty(rt['doc_free_mib']),
    );
    _docPaidCtrl = TextEditingController(
      text: _positiveOrEmpty(rt['doc_paid_mib']),
    );
    _exportFreeCtrl = TextEditingController(
      text: _positiveOrEmpty(rt['export_free']),
    );
    _exportPaidCtrl = TextEditingController(
      text: _positiveOrEmpty(rt['export_paid']),
    );
    _hubDlCtrl = TextEditingController(
      text: _positiveOrEmpty(rt['hub_file_download_mib']),
    );
  }

  String _positiveOrEmpty(int? v) =>
      (v != null && v > 0) ? '$v' : '';

  int? _parsePositive(TextEditingController c) {
    final n = int.tryParse(c.text.trim());
    if (n == null || n <= 0) return null;
    return n;
  }

  @override
  void dispose() {
    _tokenCtrl.dispose();
    _docFreeCtrl.dispose();
    _docPaidCtrl.dispose();
    _exportFreeCtrl.dispose();
    _exportPaidCtrl.dispose();
    _hubDlCtrl.dispose();
    super.dispose();
  }

  Future<void> _submit({required bool clear}) async {
    final token = _tokenCtrl.text.trim();
    if (token.isEmpty) {
      setState(() => _status = '请填写 X-Maclaw-Caps-Admin-Token');
      return;
    }
    setState(() {
      _busy = true;
      _status = null;
    });
    try {
      final result = await widget.client.putEntitlementsCaps(
        adminToken: token,
        clear: clear,
        docFreeMib: clear ? null : _parsePositive(_docFreeCtrl),
        docPaidMib: clear ? null : _parsePositive(_docPaidCtrl),
        exportFree: clear ? null : _parsePositive(_exportFreeCtrl),
        exportPaid: clear ? null : _parsePositive(_exportPaidCtrl),
        hubFileDownloadMib: clear ? null : _parsePositive(_hubDlCtrl),
      );
      widget.onTokenRemembered(token);
      widget.onApplied();
      if (!mounted) return;
      setState(() {
        _busy = false;
        _status = clear
            ? '已清空 runtime 覆盖'
            : '已应用 · effective doc_free=${result.effective['doc_free_bytes'] ?? 0}'
                ' hub_dl=${result.effective['hub_file_download_bytes'] ?? 0}';
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _status = '失败：$e';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('运维 caps 覆盖', style: theme.textTheme.titleMedium),
            const SizedBox(height: 4),
            Text(
              '需 Hub 环境变量 MACLAW_MOBILE_CAPS_ADMIN_TOKEN。覆盖仅进程内有效，'
              '优先于 env；清空后回退 env/默认。Token 仅存本次 App 内存。',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _tokenCtrl,
              obscureText: true,
              decoration: const InputDecoration(
                labelText: 'Admin Token',
                border: OutlineInputBorder(),
                isDense: true,
              ),
            ),
            const SizedBox(height: 10),
            _capsField(_docFreeCtrl, 'doc_free_mib（免费文档配额 MiB）'),
            _capsField(_docPaidCtrl, 'doc_paid_mib（付费文档配额 MiB）'),
            _capsField(_exportFreeCtrl, 'export_free（免费并发导出）'),
            _capsField(_exportPaidCtrl, 'export_paid（付费并发导出）'),
            _capsField(_hubDlCtrl, 'hub_file_download_mib（hub_exec 下载上限）'),
            if (_status != null) ...[
              const SizedBox(height: 8),
              Text(
                _status!,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: _status!.startsWith('失败')
                      ? theme.colorScheme.error
                      : theme.colorScheme.primary,
                ),
              ),
            ],
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: _busy ? null : () => unawaited(_submit(clear: true)),
                    child: const Text('清空覆盖'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: FilledButton(
                    onPressed:
                        _busy ? null : () => unawaited(_submit(clear: false)),
                    child: _busy
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Text('应用'),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _capsField(TextEditingController c, String label) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: TextField(
        controller: c,
        keyboardType: TextInputType.number,
        decoration: InputDecoration(
          labelText: label,
          border: const OutlineInputBorder(),
          isDense: true,
          helperText: '留空则不改该字段',
        ),
      ),
    );
  }
}

class _LimitStatusCard extends StatelessWidget {
  final MobileLimits limits;
  final MobileDocumentQuota? liveQuota;

  const _LimitStatusCard({
    required this.limits,
    this.liveQuota,
  });

  @override
  Widget build(BuildContext context) {
    final quota = limits.effectiveDocumentQuotaBytes;
    final used = limits.documentQuotaUsedBytes.clamp(0, quota);
    final remaining = liveQuota != null
        ? liveQuota!.documentQuotaRemaining.clamp(0, quota)
        : (quota - used).clamp(0, quota);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const SectionHeader(
              icon: Icons.speed_outlined,
              title: '额度与限制',
            ),
            const SizedBox(height: 12),
            _InfoRow(
              label: '文档空间',
              value:
                  '${formatMobileFileSize(used)} / ${formatMobileFileSize(quota)}'
                  '（剩余 ${formatMobileFileSize(remaining)}）',
            ),
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

String _planLabel(String plan) {
  return switch (plan.toLowerCase().trim()) {
    'official' => '官方服务',
    'desktop_delegate' => '桌面委托 LLM',
    'service_card' => '服务卡/授权卡',
    'paid' => '付费',
    'free' || '' => '免费',
    _ => plan,
  };
}

Future<void> _showRedeemServiceCardDialog(BuildContext context) async {
  final codeCtrl = TextEditingController();
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) {
      return AlertDialog(
        title: const Text('兑换服务卡'),
        content: TextField(
          controller: codeCtrl,
          autofocus: true,
          decoration: const InputDecoration(
            labelText: '卡密',
            hintText: '粘贴授权卡 / 服务卡代码',
            border: OutlineInputBorder(),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('兑换'),
          ),
        ],
      );
    },
  );
  final code = codeCtrl.text.trim();
  codeCtrl.dispose();
  if (ok != true || !context.mounted) return;
  if (code.isEmpty) {
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('请输入卡密')),
    );
    return;
  }
  // Redeem via ProviderContainer is awkward from free function — use element.
  final container = ProviderScope.containerOf(context);
  final client = container.read(apiClientProvider);
  if (client == null) {
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('请先登录官方服务')),
    );
    return;
  }
  try {
    final result = await client.redeemLLMServiceCard(code);
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          result.message.isNotEmpty ? result.message : '兑换成功',
        ),
      ),
    );
    container.invalidate(mobileLlmServiceStatusProvider);
    container.invalidate(sessionControllerProvider);
  } on Object catch (e) {
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('兑换失败：$e')),
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
      label: Text('$label：${enabled ? '已开启' : '未开启'}'),
    );
  }
}
