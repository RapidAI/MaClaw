import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:share_plus/share_plus.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_bootstrap.dart';
import '../../core/api/mobile_credits.dart';
import '../../core/security/mobile_redaction.dart';
import '../../l10n/app_strings.dart';
import '../../shared/surface.dart';
import '../assistant/assistant_employee_handoff.dart';
import '../auth/session_controller.dart';
import '../documents/document_draft.dart';
import '../documents/documents_controller.dart';
import 'digital_employee.dart';
import 'digital_employee_chat_screen.dart';
import 'digital_employees_controller.dart';

typedef DigitalEmployeeResultTextAction = Future<void> Function(String text);

final digitalEmployeeResultClipboardWriterProvider =
    Provider<DigitalEmployeeResultTextAction>(
  (ref) => (text) => Clipboard.setData(ClipboardData(text: text)),
);

final digitalEmployeeResultShareProvider =
    Provider<DigitalEmployeeResultTextAction>(
  (ref) => (text) async {
    await Share.share(text);
  },
);

class DigitalEmployeeMobileTaskDraft {
  final String prompt;
  final DigitalEmployeeMobileTaskType type;
  final bool requireManualConfirmation;

  const DigitalEmployeeMobileTaskDraft({
    required this.prompt,
    required this.type,
    required this.requireManualConfirmation,
  });

  String get taskTypeWireValue => digitalEmployeeMobileTaskTypeWireValue(type);

  Map<String, String> contextFor(
    DigitalEmployee employee, {
    String hubUrl = '',
    MobileBootstrap? bootstrap,
  }) {
    final context = {
      'source': 'maclaw_mobile',
      'handoff': 'mobile_emergency',
      'employee_id': employee.id,
      'employee_name': employee.name,
      'machine_id': employee.machineId,
      'machine_online_status': employee.onlineStatus,
      'access_policy': employee.accessPolicy,
      'access_policy_label': employee.accessPolicyLabel,
      'resident': employee.resident.toString(),
      'runtime_missing': employee.runtimeMissing.toString(),
      'task_type_label': digitalEmployeeMobileTaskTypeLabel(type),
      'manual_confirmation_required': requireManualConfirmation.toString(),
      'execution_boundary': requireManualConfirmation
          ? 'draft_only_until_mobile_user_confirms'
          : 'remote_policy_default',
      'manual_confirmation_scope':
          'destructive_or_high_risk_server_desktop_operations',
    };
    void putIfPresent(String key, String value) {
      final trimmed = value.trim();
      if (trimmed.isNotEmpty) context[key] = trimmed;
    }

    putIfPresent('hub_url', hubUrl);
    putIfPresent('discovered_hub_url', bootstrap?.connection.hubUrl ?? '');
    putIfPresent(
      'selected_hubcenter_url',
      bootstrap?.connection.selectedHubCenterUrl ?? '',
    );
    putIfPresent(
      'tenant_id',
      bootstrap?.connection.tenantId ?? bootstrap?.user.tenantId ?? '',
    );
    putIfPresent('llm_access_mode', bootstrap?.llmAccess.mode ?? '');
    putIfPresent('llm_access_status', bootstrap?.llmAccess.status ?? '');
    putIfPresent(
      'credits_account',
      trustedBootstrapCreditsAccount(bootstrap),
    );
    return context;
  }
}

class DigitalEmployeesScreen extends ConsumerStatefulWidget {
  const DigitalEmployeesScreen({super.key});

  @override
  ConsumerState<DigitalEmployeesScreen> createState() =>
      _DigitalEmployeesScreenState();
}

class _DigitalEmployeesScreenState
    extends ConsumerState<DigitalEmployeesScreen> {
  bool _consumingAssistantHandoff = false;
  bool _notifiedNoEmployeeForHandoff = false;

  void _showAccessPolicy(BuildContext context) {
    showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('数字员工访问策略'),
        content: const Text(
          '手机端只向 MaClaw 官方服务提交任务。远程服务器或电脑上的数字员工会按机器端策略领取任务；私有、按次授权或需要确认的能力仍由远程端控制，手机不会绕过审批或自动执行高风险操作。',
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

  Future<void> _consumeAssistantHandoffIfNeeded() async {
    if (_consumingAssistantHandoff) return;
    final handoff = ref.read(assistantEmployeeHandoffProvider);
    if (handoff == null) return;
    final employeesAsync = ref.read(digitalEmployeesProvider);
    if (!employeesAsync.hasValue) return;
    final employees = employeesAsync.requireValue;

    _consumingAssistantHandoff = true;
    try {
      if (!mounted) return;
      final target = employees.where((item) => item.canSubmitTask).firstOrNull;
      if (target == null) {
        // Keep handoff so a later refresh can still open the draft.
        if (!_notifiedNoEmployeeForHandoff && mounted) {
          _notifiedNoEmployeeForHandoff = true;
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(
              content: Text('已收到 AI 助手交接，但当前没有可派单的在线数字员工。可下拉刷新后再试。'),
            ),
          );
        }
        return;
      }
      _notifiedNoEmployeeForHandoff = false;
      // Clear only once we have a destination employee (avoids losing draft).
      ref.read(assistantEmployeeHandoffProvider.notifier).state = null;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('已从 AI 助手带入任务草稿 → ${target.name}')),
      );
      await _TaskButton.showTaskSheet(
        context,
        target,
        initialPrompt: handoff.taskPrompt,
      );
    } finally {
      _consumingAssistantHandoff = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    // Fire once when handoff arrives or employees finish loading — not on
    // every rebuild (avoids stacking post-frame callbacks).
    ref.listen<AssistantEmployeeHandoff?>(
      assistantEmployeeHandoffProvider,
      (previous, next) {
        if (next == null) return;
        unawaited(_consumeAssistantHandoffIfNeeded());
      },
    );
    ref.listen<AsyncValue<List<DigitalEmployee>>>(
      digitalEmployeesProvider,
      (previous, next) {
        if (!next.hasValue) return;
        if (ref.read(assistantEmployeeHandoffProvider) == null) return;
        unawaited(_consumeAssistantHandoffIfNeeded());
      },
    );

    final employees = ref.watch(digitalEmployeesProvider);
    final scope = ref.watch(digitalEmployeesScopeProvider);
    final shared = ref.watch(digitalEmployeesSharedFlagProvider);
    final handoff = ref.watch(assistantEmployeeHandoffProvider);
    final s = ref.watch(appStringsProvider);
    return ScreenScaffold(
      title: s.employeesTitle,
      subtitle: s.employeesSubtitle,
      trailing: IconButton.filledTonal(
        tooltip: '刷新',
        onPressed: () => ref.read(digitalEmployeesProvider.notifier).refresh(),
        icon: const Icon(Icons.refresh),
      ),
      children: [
        StatusBanner(
          tone: shared ? StatusTone.success : StatusTone.info,
          icon: shared ? Icons.groups_outlined : Icons.person_outline,
          message: shared || scope == 'shared'
              ? '只列出在线数字员工（scope=$scope 共享池可用）。离线不展示；仍受远程访问策略约束。'
              : '只列出在线数字员工（scope=own 仅自己的分身）。离线不展示；升级服务卡可查看共享池。',
        ),
        const SizedBox(height: 12),
        if (handoff != null)
          const StatusBanner(
            tone: StatusTone.info,
            icon: Icons.handshake_outlined,
            message: '正在处理来自 AI 助手的任务交接…',
          ),
        if (handoff != null) const SizedBox(height: 12),
        employees.when(
          data: (items) => items.isEmpty
              ? _EmptyEmployees(sharedAllowed: shared)
              : Column(
                  children: [
                    for (final employee in items) ...[
                      _DigitalEmployeeCard(employee: employee),
                      const SizedBox(height: 12),
                    ],
                  ],
                ),
          error: (error, _) => _EmployeeError(error: error),
          loading: () => const _EmployeeLoading(),
        ),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.security_outlined,
          title: '权限说明',
          subtitle: '私有或按次授权的数字员工会先向拥有者发起确认，手机不会绕过远程电脑策略。',
          actionLabel: '查看策略',
          onPressed: () => _showAccessPolicy(context),
        ),
        const SizedBox(height: 12),
        const _TaskStatusCard(),
        const SizedBox(height: 12),
        const _TaskHistoryCard(),
      ],
    );
  }
}

class _TaskHistoryCard extends ConsumerWidget {
  const _TaskHistoryCard();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final history = ref.watch(digitalEmployeeTaskHistoryProvider);
    return history.when(
      data: (tasks) {
        if (tasks.length <= 1) return const SizedBox.shrink();
        return Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '最近数字员工任务',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 8),
                for (final task in tasks.take(8))
                  ListTile(
                    dense: true,
                    contentPadding: EdgeInsets.zero,
                    leading: Icon(_taskHistoryIcon(task.taskType)),
                    title: Text(
                      task.prompt.trim().isEmpty ? task.taskId : task.prompt,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    subtitle: Text(
                      '${digitalEmployeeTaskTypeLabelFromWire(task.taskType)} · '
                      '${digitalEmployeeTaskStatusLabel(task.status)}',
                    ),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: () => ref
                        .read(digitalEmployeeTaskProvider.notifier)
                        .selectTask(task),
                  ),
              ],
            ),
          ),
        );
      },
      error: (error, _) => Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Text('最近任务加载失败：$error'),
        ),
      ),
      loading: () => const SizedBox.shrink(),
    );
  }
}

class _TaskStatusCard extends ConsumerWidget {
  const _TaskStatusCard();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final task = ref.watch(digitalEmployeeTaskProvider);
    return task.when(
      data: (value) {
        if (value == null) return const SizedBox.shrink();
        return Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('最近任务', style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: 6),
                Text('状态：${digitalEmployeeTaskStatusLabel(value.status)}'),
                if (digitalEmployeeTaskAwaitingAuthorization(value.status)) ...[
                  const SizedBox(height: 8),
                  _TaskAuthorizationNotice(task: value),
                ],
                if (value.prompt.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text('任务：${value.prompt}'),
                ],
                if (value.claimedBy.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text('领取者：${value.claimedBy}'),
                ],
                if (value.message.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text(
                    '说明：${value.message}',
                    style: TextStyle(
                      color: value.status == 'failed'
                          ? Theme.of(context).colorScheme.error
                          : null,
                    ),
                  ),
                ],
                if (value.result.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text(value.result),
                ],
                const SizedBox(height: 10),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    OutlinedButton.icon(
                      onPressed: () => ref
                          .read(digitalEmployeeTaskProvider.notifier)
                          .refreshTask(),
                      icon: const Icon(Icons.refresh),
                      label: const Text('刷新状态'),
                    ),
                    IconButton.outlined(
                      tooltip: '复制结果',
                      onPressed: value.result.isEmpty
                          ? null
                          : () => _copyTaskResult(
                                context,
                                ref,
                                redactMobileSensitiveText(value.result),
                              ),
                      icon: const Icon(Icons.content_copy_outlined),
                    ),
                    IconButton.outlined(
                      tooltip: '分享结果',
                      onPressed: value.result.isEmpty
                          ? null
                          : () => _shareTaskResult(
                                context,
                                ref,
                                redactMobileSensitiveText(value.result),
                              ),
                      icon: const Icon(Icons.ios_share_outlined),
                    ),
                    OutlinedButton.icon(
                      onPressed: value.result.isEmpty
                          ? null
                          : () => _createResultDraft(context, ref, value),
                      icon: const Icon(Icons.article_outlined),
                      label: const Text('整理为草稿'),
                    ),
                  ],
                ),
              ],
            ),
          ),
        );
      },
      error: (error, _) => Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Text('任务状态加载失败：$error'),
        ),
      ),
      loading: () => const Card(
        child: Padding(
          padding: EdgeInsets.all(16),
          child: LinearProgressIndicator(),
        ),
      ),
    );
  }

  Future<void> _copyTaskResult(
    BuildContext context,
    WidgetRef ref,
    String result,
  ) async {
    await ref.read(digitalEmployeeResultClipboardWriterProvider).call(result);
    if (!context.mounted) return;
    ScaffoldMessenger.of(context)
      ..clearSnackBars()
      ..showSnackBar(
        const SnackBar(content: Text('任务结果已复制')),
      );
  }

  Future<void> _shareTaskResult(
    BuildContext context,
    WidgetRef ref,
    String result,
  ) async {
    await ref.read(digitalEmployeeResultShareProvider).call(result);
    if (!context.mounted) return;
    ScaffoldMessenger.of(context)
      ..clearSnackBars()
      ..showSnackBar(
        const SnackBar(content: Text('任务结果已发送到系统分享')),
      );
  }

  Future<void> _createResultDraft(
    BuildContext context,
    WidgetRef ref,
    MobileDigitalEmployeeTask task,
  ) async {
    final markdown = digitalEmployeeTaskDocumentMarkdown(task);
    await ref.read(documentsControllerProvider.notifier).createDraft(
          title: '数字员工任务结果',
          template: DocumentTemplate.report,
          content: markdown,
        );
    if (!context.mounted) return;
    final documents = ref.read(documentsControllerProvider);
    final error = documents.error;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          documents.hasError ? '整理为草稿失败：$error' : '已整理为文档草稿',
        ),
      ),
    );
  }
}

String digitalEmployeeTaskDocumentMarkdown(MobileDigitalEmployeeTask task) {
  final prompt = redactMobileSensitiveText(task.prompt.trim());
  final message = redactMobileSensitiveText(task.message.trim());
  final result = redactMobileSensitiveText(task.result.trim());
  final buffer = StringBuffer()
    ..writeln('# 数字员工任务结果')
    ..writeln()
    ..writeln('## 任务')
    ..writeln(prompt.isEmpty ? '未提供任务说明。' : prompt)
    ..writeln()
    ..writeln('## 状态')
    ..writeln(digitalEmployeeTaskStatusLabel(task.status));
  if (task.claimedBy.trim().isNotEmpty) {
    buffer
      ..writeln()
      ..writeln('## 领取者')
      ..writeln(redactMobileSensitiveText(task.claimedBy.trim()));
  }
  if (message.isNotEmpty) {
    buffer
      ..writeln()
      ..writeln('## 说明')
      ..writeln(message);
  }
  buffer
    ..writeln()
    ..writeln('## 结果')
    ..writeln(result.isEmpty ? '暂无结果。' : result);
  return buffer.toString().trim();
}

class _EmployeeLoading extends StatelessWidget {
  const _EmployeeLoading();

  @override
  Widget build(BuildContext context) {
    return const LoadingCard(label: '正在加载数字员工…');
  }
}

class _EmployeeError extends StatelessWidget {
  final Object error;

  const _EmployeeError({required this.error});

  @override
  Widget build(BuildContext context) {
    return EmptyStatePanel(
      icon: Icons.error_outline,
      title: '数字员工加载失败',
      message: '$error',
    );
  }
}

class _EmptyEmployees extends StatelessWidget {
  final bool sharedAllowed;

  const _EmptyEmployees({this.sharedAllowed = false});

  @override
  Widget build(BuildContext context) {
    return EmptyStatePanel(
      icon: Icons.desktop_access_disabled_outlined,
      title: '暂无在线数字员工',
      message: sharedAllowed
          ? '仅展示在线员工。请确认电脑端分身已上线，或租户共享池中有在线员工后下拉刷新。'
          : '仅展示在线员工。请在电脑上登录同一账号并启用数字员工，待显示在线后刷新；升级服务卡可查看租户共享池。',
    );
  }
}

class _DigitalEmployeeCard extends StatelessWidget {
  final DigitalEmployee employee;

  const _DigitalEmployeeCard({required this.employee});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Card(
      child: InkWell(
        borderRadius: BorderRadius.circular(12),
        onTap: () => Navigator.of(context).push(
          MaterialPageRoute(
            builder: (_) => DigitalEmployeeChatScreen(employee: employee),
          ),
        ),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  CircleAvatar(
                    radius: 22,
                    backgroundColor: employee.online
                        ? scheme.secondaryContainer
                        : scheme.surfaceContainerHighest,
                    child: Icon(
                      employee.online
                          ? Icons.smart_toy_outlined
                          : Icons.cloud_off,
                      color: employee.online
                          ? scheme.onSecondaryContainer
                          : scheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          employee.name,
                          style: text.titleMedium?.copyWith(
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        const SizedBox(height: 2),
                        Text(
                          employee.machineId,
                          style: text.bodySmall?.copyWith(
                            color: scheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ),
                  _StatusChip(online: employee.online),
                  const SizedBox(width: 4),
                  Icon(
                    Icons.chevron_right,
                    color: scheme.onSurfaceVariant,
                  ),
                ],
              ),
              const SizedBox(height: 12),
              Text(
                employee.skillDescription,
                style: text.bodyMedium?.copyWith(height: 1.4),
              ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  _EmployeeInfoChip(
                    icon: Icons.security_outlined,
                    label: employee.accessPolicyLabel,
                  ),
                  _EmployeeInfoChip(
                    icon: Icons.memory_outlined,
                    label: employee.runtimeLabel,
                    emphasized: employee.runtimeMissing,
                  ),
                  _EmployeeInfoChip(
                    icon: Icons.power_settings_new_outlined,
                    label: employee.residencyLabel,
                  ),
                ],
              ),
              const SizedBox(height: 14),
              Row(
                children: [
                  Expanded(
                    child: _TaskButton(employee: employee),
                  ),
                  const SizedBox(width: 10),
                  IconButton.outlined(
                    tooltip: '分析日志/输出',
                    onPressed: employee.canSubmitTask
                        ? () => _TaskButton.showTaskSheet(
                              context,
                              employee,
                              initialPrompt:
                                  '请读取并分析远程服务器/电脑最近的后台会话输出和关键日志，重点说明异常、影响范围、排查依据和建议命令。高风险命令只给草案，不要自动执行。',
                            )
                        : null,
                    icon: const Icon(Icons.plagiarism_outlined),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _TaskButton extends ConsumerWidget {
  final DigitalEmployee employee;

  const _TaskButton({required this.employee});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final task = ref.watch(digitalEmployeeTaskProvider);
    return FilledButton.icon(
      onPressed: employee.canSubmitTask
          ? () => showTaskSheet(context, employee)
          : null,
      icon: const Icon(Icons.chat_outlined),
      label: Text(
        task.isLoading ? '提交中' : '发起任务',
      ),
    );
  }

  static Future<void> showTaskSheet(
    BuildContext context,
    DigitalEmployee employee, {
    String? initialPrompt,
  }) async {
    final draft = await showModalBottomSheet<DigitalEmployeeMobileTaskDraft>(
      context: context,
      isScrollControlled: true,
      builder: (context) => _DigitalEmployeeTaskSheet(
        employee: employee,
        initialPrompt: initialPrompt,
      ),
    );
    if (draft == null || draft.prompt.trim().isEmpty) return;
    if (!context.mounted) return;
    final container = ProviderScope.containerOf(context, listen: false);
    final session = await container.read(sessionControllerProvider.future);
    await container.read(digitalEmployeeTaskProvider.notifier).createTask(
          employeeId: employee.id,
          prompt: buildDigitalEmployeeMobilePrompt(
            type: draft.type,
            prompt: draft.prompt,
            requireManualConfirmation: draft.requireManualConfirmation,
          ),
          taskType: draft.taskTypeWireValue,
          context: draft.contextFor(
            employee,
            hubUrl: session.hubUrl,
            bootstrap: session.bootstrap,
          ),
        );
  }
}

enum DigitalEmployeeMobileTaskType {
  serverMaintenance,
  desktopAssist,
  documentWork,
  informationCheck,
}

String digitalEmployeeMobileTaskTypeLabel(DigitalEmployeeMobileTaskType type) {
  return switch (type) {
    DigitalEmployeeMobileTaskType.serverMaintenance => '服务器维护',
    DigitalEmployeeMobileTaskType.desktopAssist => '远程电脑',
    DigitalEmployeeMobileTaskType.documentWork => '文档处理',
    DigitalEmployeeMobileTaskType.informationCheck => '信息核查',
  };
}

String digitalEmployeeMobileTaskTypeWireValue(
  DigitalEmployeeMobileTaskType type,
) {
  return switch (type) {
    DigitalEmployeeMobileTaskType.serverMaintenance => 'server_maintenance',
    DigitalEmployeeMobileTaskType.desktopAssist => 'desktop_assist',
    DigitalEmployeeMobileTaskType.documentWork => 'document_work',
    DigitalEmployeeMobileTaskType.informationCheck => 'information_check',
  };
}

String digitalEmployeeTaskTypeLabelFromWire(String value) {
  return switch (value.trim()) {
    'server_maintenance' => '服务器维护',
    'desktop_assist' => '远程电脑',
    'document_work' => '文档处理',
    'information_check' => '信息核查',
    _ => '通用任务',
  };
}

IconData _taskHistoryIcon(String value) {
  return switch (value.trim()) {
    'server_maintenance' => Icons.dns_outlined,
    'desktop_assist' => Icons.desktop_windows_outlined,
    'document_work' => Icons.description_outlined,
    'information_check' => Icons.fact_check_outlined,
    _ => Icons.smart_toy_outlined,
  };
}

String buildDigitalEmployeeMobilePrompt({
  required DigitalEmployeeMobileTaskType type,
  required String prompt,
  bool requireManualConfirmation = true,
}) {
  final text = redactMobileSensitiveText(prompt.trim());
  final buffer = StringBuffer()
    ..writeln('【MaClaw Mobile 应急任务】')
    ..writeln('任务类型：${digitalEmployeeMobileTaskTypeLabel(type)}')
    ..writeln()
    ..writeln('移动端要求：')
    ..writeln('- 请先给结论、影响范围、证据和下一步，输出适合手机快速阅读。')
    ..writeln('- 假设用户在手机上应急处理，避免要求长时间盯屏或执行复杂连续操作。')
    ..writeln('- 如需操作远程服务器/电脑，请说明目标、风险、验证方式和回滚方式。');
  if (requireManualConfirmation) {
    buffer.writeln('- 高风险命令只生成命令草案，等待用户手动确认，不要自动执行。');
  }
  buffer
    ..writeln()
    ..writeln('用户说明：')
    ..writeln(text.isEmpty ? '请根据当前远程环境执行一次应急检查。' : text);
  return buffer.toString().trim();
}

class _DigitalEmployeeTaskSheet extends ConsumerStatefulWidget {
  final DigitalEmployee employee;
  final String? initialPrompt;

  const _DigitalEmployeeTaskSheet({
    required this.employee,
    this.initialPrompt,
  });

  @override
  ConsumerState<_DigitalEmployeeTaskSheet> createState() =>
      _DigitalEmployeeTaskSheetState();
}

class _DigitalEmployeeTaskSheetState
    extends ConsumerState<_DigitalEmployeeTaskSheet> {
  late final TextEditingController _promptController;
  var _taskType = DigitalEmployeeMobileTaskType.serverMaintenance;
  var _requireManualConfirmation = true;

  static const _templates = [
    '请检查远程电脑/服务器当前运行状态，列出异常、风险和建议操作。',
    '请查看最近的服务错误日志，整理可能原因和下一步排查命令。',
    '请检查磁盘、内存、CPU、网络连接状态，并给出应急处理建议。',
    '请帮我在远程电脑上整理指定目录/文件的关键信息，并返回摘要。',
  ];

  @override
  void initState() {
    super.initState();
    _promptController = TextEditingController(
      text: widget.initialPrompt ?? _templates.first,
    );
  }

  @override
  void dispose() {
    _promptController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final history = ref.watch(digitalEmployeePromptHistoryProvider);
    final bottom = MediaQuery.of(context).viewInsets.bottom;
    return Padding(
      padding: EdgeInsets.fromLTRB(16, 16, 16, 16 + bottom),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '发给 ${widget.employee.name}',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 12),
            Text('任务类型', style: Theme.of(context).textTheme.labelLarge),
            const SizedBox(height: 8),
            SegmentedButton<DigitalEmployeeMobileTaskType>(
              segments: const [
                ButtonSegment(
                  value: DigitalEmployeeMobileTaskType.serverMaintenance,
                  icon: Icon(Icons.dns_outlined),
                  label: Text('服务器'),
                ),
                ButtonSegment(
                  value: DigitalEmployeeMobileTaskType.desktopAssist,
                  icon: Icon(Icons.desktop_windows_outlined),
                  label: Text('电脑'),
                ),
                ButtonSegment(
                  value: DigitalEmployeeMobileTaskType.documentWork,
                  icon: Icon(Icons.description_outlined),
                  label: Text('文档'),
                ),
                ButtonSegment(
                  value: DigitalEmployeeMobileTaskType.informationCheck,
                  icon: Icon(Icons.fact_check_outlined),
                  label: Text('核查'),
                ),
              ],
              selected: {_taskType},
              onSelectionChanged: (selection) {
                setState(() => _taskType = selection.single);
              },
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _promptController,
              minLines: 4,
              maxLines: 8,
              decoration: const InputDecoration(
                labelText: '任务说明',
                alignLabelWithHint: true,
                prefixIcon: Icon(Icons.task_alt_outlined),
              ),
            ),
            const SizedBox(height: 8),
            CheckboxListTile(
              contentPadding: EdgeInsets.zero,
              value: _requireManualConfirmation,
              onChanged: (value) => setState(
                () => _requireManualConfirmation = value ?? true,
              ),
              title: const Text('高风险命令只给草案'),
              subtitle: const Text('远程端不要自动执行删除、重启、改权限等高风险操作。'),
              controlAffinity: ListTileControlAffinity.leading,
            ),
            const SizedBox(height: 12),
            Text('任务模板', style: Theme.of(context).textTheme.labelLarge),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final template in _templates)
                  ActionChip(
                    label: Text(template),
                    onPressed: () => _promptController.text = template,
                  ),
              ],
            ),
            const SizedBox(height: 12),
            history.when(
              data: (items) {
                final recent = items
                    .where((item) => item.employeeId == widget.employee.id)
                    .take(5)
                    .toList();
                if (recent.isEmpty) return const SizedBox.shrink();
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('最近任务', style: Theme.of(context).textTheme.labelLarge),
                    const SizedBox(height: 8),
                    for (final item in recent)
                      ListTile(
                        dense: true,
                        contentPadding: EdgeInsets.zero,
                        leading: const Icon(Icons.history),
                        title: Text(item.prompt),
                        onTap: () => _promptController.text = item.prompt,
                      ),
                  ],
                );
              },
              error: (error, _) => Text('最近任务加载失败：$error'),
              loading: () => const LinearProgressIndicator(),
            ),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: () => Navigator.of(context).pop(
                  DigitalEmployeeMobileTaskDraft(
                    prompt: _promptController.text,
                    type: _taskType,
                    requireManualConfirmation: _requireManualConfirmation,
                  ),
                ),
                icon: const Icon(Icons.send_outlined),
                label: const Text('提交任务'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

String digitalEmployeeTaskStatusLabel(String status) {
  return switch (status.trim().toLowerCase()) {
    'queued' => '等待远程领取',
    'claimed' => '远程处理中',
    'running' => '远程处理中',
    'in_progress' => '远程处理中',
    'approval_required' => '等待远程授权',
    'pending_approval' => '等待远程授权',
    'awaiting_approval' => '等待远程授权',
    'authorization_required' => '等待远程授权',
    'waiting_authorization' => '等待远程授权',
    'approval_denied' => '远程授权被拒绝',
    'authorization_denied' => '远程授权被拒绝',
    'rejected' => '远程授权被拒绝',
    'done' => '已完成',
    'completed' => '已完成',
    'failed' => '失败',
    _ => status,
  };
}

bool digitalEmployeeTaskAwaitingAuthorization(String status) {
  return switch (status.trim().toLowerCase()) {
    'approval_required' ||
    'pending_approval' ||
    'awaiting_approval' ||
    'authorization_required' ||
    'waiting_authorization' =>
      true,
    _ => false,
  };
}

class _TaskAuthorizationNotice extends StatelessWidget {
  final MobileDigitalEmployeeTask task;

  const _TaskAuthorizationNotice({required this.task});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final owner =
        task.claimedBy.trim().isEmpty ? '远程端拥有者' : task.claimedBy.trim();
    return DecoratedBox(
      decoration: BoxDecoration(
        color: scheme.tertiaryContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Padding(
        padding: const EdgeInsets.all(10),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              Icons.verified_user_outlined,
              color: scheme.onTertiaryContainer,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                '正在等待 $owner 在远程服务器/电脑上确认授权。手机端不会绕过远程策略；确认后可刷新查看结果。',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.onTertiaryContainer,
                    ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _EmployeeInfoChip extends StatelessWidget {
  final IconData icon;
  final String label;
  final bool emphasized;

  const _EmployeeInfoChip({
    required this.icon,
    required this.label,
    this.emphasized = false,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Chip(
      visualDensity: VisualDensity.compact,
      avatar: Icon(icon, size: 18),
      label: Text(label),
      backgroundColor:
          emphasized ? scheme.errorContainer : scheme.surfaceContainerHighest,
      labelStyle: emphasized
          ? TextStyle(color: scheme.onErrorContainer)
          : TextStyle(color: scheme.onSurfaceVariant),
      side: BorderSide(
        color: emphasized ? scheme.error : scheme.outlineVariant,
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  final bool online;

  const _StatusChip({required this.online});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Chip(
      visualDensity: VisualDensity.compact,
      label: Text(online ? '在线' : '离线'),
      avatar: Icon(online ? Icons.check_circle : Icons.radio_button_unchecked),
      backgroundColor:
          online ? scheme.secondaryContainer : scheme.surfaceContainerHighest,
    );
  }
}
