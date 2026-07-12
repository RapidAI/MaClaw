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
    final s = ref.read(appStringsProvider);
    showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(s.accessPolicyTitle),
        content: Text(s.accessPolicyBody),
        actions: [
          FilledButton(
            onPressed: () => Navigator.of(context).pop(),
            child: Text(s.ok),
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
          final s = ref.read(appStringsProvider);
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(s.handoffNoEmployee)),
          );
        }
        return;
      }
      _notifiedNoEmployeeForHandoff = false;
      // Clear only once we have a destination employee (avoids losing draft).
      ref.read(assistantEmployeeHandoffProvider.notifier).state = null;
      final s = ref.read(appStringsProvider);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(s.handoffDraftTo(target.name))),
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
        tooltip: s.refresh,
        onPressed: () => ref.read(digitalEmployeesProvider.notifier).refresh(),
        icon: const Icon(Icons.refresh),
      ),
      children: [
        StatusBanner(
          tone: shared ? StatusTone.success : StatusTone.info,
          icon: shared ? Icons.groups_outlined : Icons.person_outline,
          message: shared || scope == 'shared'
              ? s.employeesOnlineSharedHint
              : s.employeesOnlineOwnHint,
        ),
        const SizedBox(height: 12),
        if (handoff != null)
          StatusBanner(
            tone: StatusTone.info,
            icon: Icons.handshake_outlined,
            message: s.handoffInProgress,
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
          title: s.accessPolicyActionTitle,
          subtitle: s.accessPolicyActionSubtitle,
          actionLabel: s.viewPolicy,
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
    final s = ref.watch(appStringsProvider);
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
                  s.recentEmployeeTasks,
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
                      '${digitalEmployeeTaskTypeLabelFromWire(task.taskType, isZh: s.isZh)} · '
                      '${digitalEmployeeTaskStatusLabel(task.status, isZh: s.isZh)}',
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
          child: Text(s.recentTasksLoadFailed(error)),
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
    final s = ref.watch(appStringsProvider);
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
                Text(s.recentTasks, style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: 6),
                Text(s.statusLine(
                  digitalEmployeeTaskStatusLabel(value.status, isZh: s.isZh),
                ),),
                if (digitalEmployeeTaskIsRunning(value.status)) ...[
                  const SizedBox(height: 8),
                  ClipRRect(
                    borderRadius: BorderRadius.circular(999),
                    child: LinearProgressIndicator(
                      minHeight: 3,
                      backgroundColor:
                          Theme.of(context).colorScheme.surfaceContainerHighest,
                    ),
                  ),
                ],
                if (digitalEmployeeTaskAwaitingAuthorization(value.status)) ...[
                  const SizedBox(height: 8),
                  _TaskAuthorizationNotice(task: value),
                ],
                if (value.prompt.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text(s.taskLine(value.prompt)),
                ],
                if (value.claimedBy.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text(s.claimedByLine(value.claimedBy)),
                ],
                if (value.message.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text(
                    s.noteLine(value.message),
                    style: TextStyle(
                      color: value.status == 'failed'
                          ? Theme.of(context).colorScheme.error
                          : null,
                    ),
                  ),
                ],
                if (value.result.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text(
                    // While running, result is progressive agent text from Hub.
                    value.result,
                    style: digitalEmployeeTaskIsRunning(value.status)
                        ? Theme.of(context).textTheme.bodyMedium?.copyWith(
                              color: Theme.of(context)
                                  .colorScheme
                                  .onSurfaceVariant,
                            )
                        : null,
                  ),
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
                      label: Text(s.refreshStatus),
                    ),
                    IconButton.outlined(
                      tooltip: s.copyResult,
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
                      tooltip: s.shareResultTooltip,
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
                      label: Text(s.makeDraftFromResult),
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
          child: Text(s.taskStatusLoadFailedDetail(error)),
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
        SnackBar(content: Text(ref.read(appStringsProvider).taskResultCopied)),
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
        SnackBar(content: Text(ref.read(appStringsProvider).taskResultShared)),
      );
  }

  Future<void> _createResultDraft(
    BuildContext context,
    WidgetRef ref,
    MobileDigitalEmployeeTask task,
  ) async {
    final s = ref.read(appStringsProvider);
    final markdown =
        digitalEmployeeTaskDocumentMarkdown(task, isZh: s.isZh);
    await ref.read(documentsControllerProvider.notifier).createDraft(
          title: s.employeeTaskResultTitle,
          template: DocumentTemplate.report,
          content: markdown,
        );
    if (!context.mounted) return;
    final documents = ref.read(documentsControllerProvider);
    final error = documents.error;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          documents.hasError
              ? s.draftFromResultFailed(error ?? '')
              : s.draftFromResultOk,
        ),
      ),
    );
  }
}

String digitalEmployeeTaskDocumentMarkdown(
  MobileDigitalEmployeeTask task, {
  bool isZh = true,
}) {
  final s = AppStrings.forLanguage(isZh ? 'zh' : 'en');
  final prompt = redactMobileSensitiveText(task.prompt.trim());
  final message = redactMobileSensitiveText(task.message.trim());
  final result = redactMobileSensitiveText(task.result.trim());
  final buffer = StringBuffer()
    ..writeln('# ${s.employeeTaskResultTitle}')
    ..writeln()
    ..writeln(isZh ? '## 任务' : '## Task')
    ..writeln(
      prompt.isEmpty
          ? (isZh ? '未提供任务说明。' : 'No task description provided.')
          : prompt,
    )
    ..writeln()
    ..writeln(isZh ? '## 状态' : '## Status')
    ..writeln(digitalEmployeeTaskStatusLabel(task.status, isZh: isZh));
  if (task.claimedBy.trim().isNotEmpty) {
    buffer
      ..writeln()
      ..writeln(isZh ? '## 领取者' : '## Claimed by')
      ..writeln(redactMobileSensitiveText(task.claimedBy.trim()));
  }
  if (message.isNotEmpty) {
    buffer
      ..writeln()
      ..writeln(isZh ? '## 说明' : '## Notes')
      ..writeln(message);
  }
  buffer
    ..writeln()
    ..writeln(isZh ? '## 结果' : '## Result')
    ..writeln(
      result.isEmpty ? (isZh ? '暂无结果。' : 'No result yet.') : result,
    );
  return buffer.toString().trim();
}

class _EmployeeLoading extends ConsumerWidget {
  const _EmployeeLoading();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    return LoadingCard(label: s.loadingEmployees);
  }
}

class _EmployeeError extends ConsumerWidget {
  final Object error;

  const _EmployeeError({required this.error});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    return EmptyStatePanel(
      icon: Icons.error_outline,
      title: s.employeesLoadFailed,
      message: '$error',
    );
  }
}

class _EmptyEmployees extends ConsumerWidget {
  final bool sharedAllowed;

  const _EmptyEmployees({this.sharedAllowed = false});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    return EmptyStatePanel(
      icon: Icons.desktop_access_disabled_outlined,
      title: s.noOnlineEmployees,
      message: sharedAllowed
          ? s.noOnlineEmployeesSharedHint
          : s.noOnlineEmployeesOwnHint,
    );
  }
}

class _DigitalEmployeeCard extends ConsumerWidget {
  final DigitalEmployee employee;

  const _DigitalEmployeeCard({required this.employee});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
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
                    label: employee.accessPolicyLabelFor(isZh: s.isZh),
                  ),
                  _EmployeeInfoChip(
                    icon: Icons.memory_outlined,
                    label: employee.runtimeLabelFor(isZh: s.isZh),
                    emphasized: employee.runtimeMissing,
                  ),
                  _EmployeeInfoChip(
                    icon: Icons.power_settings_new_outlined,
                    label: employee.residencyLabelFor(isZh: s.isZh),
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
                    tooltip: s.analyzeLogOutput,
                    onPressed: employee.canSubmitTask
                        ? () => _TaskButton.showTaskSheet(
                              context,
                              employee,
                              initialPrompt: s.analyzeLogPrompt,
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
    final s = ref.watch(appStringsProvider);
    final task = ref.watch(digitalEmployeeTaskProvider);
    return FilledButton.icon(
      onPressed: employee.canSubmitTask
          ? () => showTaskSheet(context, employee)
          : null,
      icon: const Icon(Icons.chat_outlined),
      label: Text(
        task.isLoading ? s.submittingTask : s.submitTask,
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

String digitalEmployeeMobileTaskTypeLabel(DigitalEmployeeMobileTaskType type, {bool isZh = true}) {
  final wire = digitalEmployeeMobileTaskTypeWireValue(type);
  return AppStrings.forLanguage(isZh ? 'zh' : 'en').employeeTaskTypeLabel(wire);
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

String digitalEmployeeTaskTypeLabelFromWire(String value, {bool isZh = true}) {
  return AppStrings.forLanguage(isZh ? 'zh' : 'en').employeeTaskTypeLabel(value);
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
  var _seededTemplate = false;

  @override
  void initState() {
    super.initState();
    _promptController = TextEditingController(text: widget.initialPrompt ?? '');
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_seededTemplate) return;
    _seededTemplate = true;
    if (_promptController.text.trim().isEmpty) {
      final s = ref.read(appStringsProvider);
      _promptController.text = s.employeeTemplateStatus;
    }
  }

  @override
  void dispose() {
    _promptController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final s = ref.watch(appStringsProvider);
    final templates = [
      s.employeeTemplateStatus,
      s.employeeTemplateLogs,
      s.employeeTemplateResources,
      s.employeeTemplateFiles,
    ];
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
              s.sendToEmployee(widget.employee.name),
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 12),
            Text(s.taskTypeLabel, style: Theme.of(context).textTheme.labelLarge),
            const SizedBox(height: 8),
            SegmentedButton<DigitalEmployeeMobileTaskType>(
              segments: [
                ButtonSegment(
                  value: DigitalEmployeeMobileTaskType.serverMaintenance,
                  icon: const Icon(Icons.dns_outlined),
                  label: Text(s.taskTypeServer),
                ),
                ButtonSegment(
                  value: DigitalEmployeeMobileTaskType.desktopAssist,
                  icon: const Icon(Icons.desktop_windows_outlined),
                  label: Text(s.taskTypeDesktop),
                ),
                ButtonSegment(
                  value: DigitalEmployeeMobileTaskType.documentWork,
                  icon: const Icon(Icons.description_outlined),
                  label: Text(s.taskTypeDocument),
                ),
                ButtonSegment(
                  value: DigitalEmployeeMobileTaskType.informationCheck,
                  icon: const Icon(Icons.fact_check_outlined),
                  label: Text(s.taskTypeCheck),
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
              decoration: InputDecoration(
                labelText: s.taskDescription,
                alignLabelWithHint: true,
                prefixIcon: const Icon(Icons.task_alt_outlined),
              ),
            ),
            const SizedBox(height: 8),
            CheckboxListTile(
              contentPadding: EdgeInsets.zero,
              value: _requireManualConfirmation,
              onChanged: (value) => setState(
                () => _requireManualConfirmation = value ?? true,
              ),
              title: Text(s.highRiskDraftOnly),
              subtitle: Text(s.highRiskDraftOnlyHint),
              controlAffinity: ListTileControlAffinity.leading,
            ),
            const SizedBox(height: 12),
            Text(s.taskTemplates, style: Theme.of(context).textTheme.labelLarge),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final template in templates)
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
                    Text(s.recentTasks, style: Theme.of(context).textTheme.labelLarge),
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
              error: (error, _) => Text(s.recentTasksLoadFailed(error)),
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
                label: Text(s.submitTaskButton),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

String digitalEmployeeTaskStatusLabel(String status, {bool isZh = true}) {
  return AppStrings.forLanguage(isZh ? 'zh' : 'en').employeeTaskStatusLabel(status);
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

class _TaskAuthorizationNotice extends ConsumerWidget {
  final MobileDigitalEmployeeTask task;

  const _TaskAuthorizationNotice({required this.task});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final scheme = Theme.of(context).colorScheme;
    final owner =
        task.claimedBy.trim().isEmpty ? s.remoteOwner : task.claimedBy.trim();
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
                s.awaitingAuthorization(owner),
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

class _StatusChip extends ConsumerWidget {
  final bool online;

  const _StatusChip({required this.online});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final scheme = Theme.of(context).colorScheme;
    return Chip(
      visualDensity: VisualDensity.compact,
      label: Text(online ? s.online : s.offline),
      avatar: Icon(online ? Icons.check_circle : Icons.radio_button_unchecked),
      backgroundColor:
          online ? scheme.secondaryContainer : scheme.surfaceContainerHighest,
    );
  }
}
