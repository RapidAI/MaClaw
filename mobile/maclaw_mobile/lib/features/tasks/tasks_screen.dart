import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_bootstrap.dart';
import '../../l10n/app_strings.dart';
import '../../shared/surface.dart';
import '../account/account_agent_status_card.dart';
import '../auth/session_controller.dart';
import '../digital_employees/digital_employee.dart';
import '../digital_employees/digital_employees_controller.dart';
import '../documents/documents_controller.dart';
import 'mobile_jobs_provider.dart';

/// Unified long-running task center (bottom tab "Tasks" / "后台").
class TasksScreen extends ConsumerWidget {
  const TasksScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final documents = ref.watch(documentsControllerProvider);
    final employeeTask = ref.watch(digitalEmployeeTaskProvider);
    final employeeHistory = ref.watch(digitalEmployeeTaskHistoryProvider);
    final hubJobs = ref.watch(mobileJobsProvider);
    final liveQuota = ref.watch(documentQuotaProvider);
    final bootstrap =
        ref.watch(sessionControllerProvider).valueOrNull?.bootstrap;
    final limits = mergeDocumentQuotaLimits(
      bootstrap?.limits,
      liveQuota.valueOrNull,
    );
    final showQuota = bootstrap?.limits != null || liveQuota.valueOrNull != null;
    final s = ref.watch(appStringsProvider);

    return ScreenScaffold(
      title: s.tasksTitle,
      subtitle: s.tasksSubtitle,
      trailing: IconButton.filledTonal(
        tooltip: s.refresh,
        onPressed: () {
          ref.invalidate(mobileJobsProvider);
          ref.invalidate(documentQuotaProvider);
          ref
              .read(documentsControllerProvider.notifier)
              .refreshUploadTask(silent: true);
          ref
              .read(documentsControllerProvider.notifier)
              .refreshExportJob(silent: true);
          ref
              .read(digitalEmployeeTaskProvider.notifier)
              .refreshTask(silent: true);
          ref.read(digitalEmployeeTaskHistoryProvider.notifier).refresh();
        },
        icon: const Icon(Icons.refresh),
      ),
      children: [
        if (showQuota) ...[
          _QuotaCard(
            limits: limits,
            refreshing: liveQuota.isLoading,
          ),
          const SizedBox(height: 12),
        ],
        const _AgentBackendSummaryCard(),
        const SizedBox(height: 12),
        hubJobs.when(
          data: (list) => _UnifiedJobsCard(list: list),
          loading: () => LoadingCard(label: s.loadingHubJobs),
          error: (error, _) => Card(
            child: ListTile(
              leading: const Icon(Icons.error_outline),
              title: Text(s.hubJobsLoadFailed),
              subtitle: Text('$error'),
            ),
          ),
        ),
        const SizedBox(height: 12),
        documents.when(
          data: (state) => _DocumentTasksCard(state: state),
          loading: () => LoadingCard(label: s.loadingDocumentTasks),
          error: (error, _) => Card(
            child: ListTile(
              leading: const Icon(Icons.error_outline),
              title: Text(s.documentTasksLoadFailed),
              subtitle: Text('$error'),
            ),
          ),
        ),
        const SizedBox(height: 12),
        employeeTask.when(
          data: (task) => _EmployeeTaskCard(
            task: task,
            history: employeeHistory.valueOrNull ?? const [],
          ),
          loading: () => LoadingCard(label: s.loadingEmployeeTasks),
          error: (error, _) => Card(
            child: ListTile(
              leading: const Icon(Icons.error_outline),
              title: Text(s.employeeTasksLoadFailed),
              subtitle: Text('$error'),
            ),
          ),
        ),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.description_outlined,
          title: s.openDocumentsEditor,
          subtitle: s.openDocumentsEditorHint,
          actionLabel: s.enter,
          onPressed: () => context.go('/documents'),
        ),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.smart_toy_outlined,
          title: s.employeesTitle,
          subtitle: s.employeesShortcutHint,
          actionLabel: s.enter,
          onPressed: () => context.go('/employees'),
        ),
      ],
    );
  }
}

class _AgentBackendSummaryCard extends ConsumerWidget {
  const _AgentBackendSummaryCard();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final knowledge = ref.watch(accountKnowledgeStatusProvider);
    final health = ref.watch(accountMcpHealthProvider);
    final knowledgeLine = knowledge.when(
      data: (st) {
        if (st == null || !st.available) {
          return s.knowledgeNotReady;
        }
        return s.knowledgeSummary(st.sources, st.cards, st.mode);
      },
      loading: () => s.knowledgeLoading,
      error: (_, __) => s.knowledgeUnavailable,
    );
    final skills = ref.watch(accountSkillsProvider);
    final mcpLine = health.when(
      data: (h) {
        if (h == null) return s.mcpNotProbed;
        return s.mcpSummary(h.healthyCount, h.serverCount, h.availableTools);
      },
      loading: () => s.mcpProbing,
      error: (_, __) => s.mcpProbeFailed,
    );
    final skillsLine = skills.when(
      data: (st) => st == null ? s.skillsUnknown : s.skillsCount(st.count),
      loading: () => s.skillsLoading,
      error: (_, __) => s.skillsUnavailable,
    );
    return Card(
      child: ListTile(
        leading: const Icon(Icons.hub_outlined),
        title: Text(s.officialAgentBackend),
        subtitle: Text('$knowledgeLine\n$mcpLine\n$skillsLine'),
        isThreeLine: true,
        trailing: IconButton(
          tooltip: s.refreshAgentStatus,
          onPressed: () {
            ref.invalidate(accountKnowledgeStatusProvider);
            ref.invalidate(accountSkillsProvider);
            ref.read(accountMcpHealthProvider.notifier).probe();
          },
          icon: const Icon(Icons.refresh),
        ),
      ),
    );
  }
}

class _QuotaCard extends ConsumerWidget {
  final MobileLimits limits;
  final bool refreshing;

  const _QuotaCard({
    required this.limits,
    this.refreshing = false,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final quota = limits.effectiveDocumentQuotaBytes;
    final used = limits.documentQuotaUsedBytes.clamp(0, quota);
    final ratio = quota <= 0 ? 0.0 : used / quota;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    s.documentStorage,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                if (refreshing)
                  const SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
              ],
            ),
            const SizedBox(height: 8),
            LinearProgressIndicator(value: ratio.clamp(0.0, 1.0)),
            const SizedBox(height: 8),
            Text(
              '${formatMobileFileSize(used)} / ${formatMobileFileSize(quota)}'
              '${limits.documentQuotaBytes <= 0 ? s.defaultFreeQuota : ''}',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

class _UnifiedJobsCard extends ConsumerStatefulWidget {
  final MobileJobsList? list;

  const _UnifiedJobsCard({required this.list});

  @override
  ConsumerState<_UnifiedJobsCard> createState() => _UnifiedJobsCardState();
}

class _UnifiedJobsCardState extends ConsumerState<_UnifiedJobsCard> {
  /// empty = all kinds
  String _kindFilter = '';
  bool _activeOnly = false;

  List<(String id, String label)> _kindFilters(AppStrings s) => [
        ('', s.filterAll),
        ('assistant', s.filterAssistant),
        ('document', s.filterDocument),
        ('digital_employee', s.filterEmployee),
        ('ssh', 'SSH'),
      ];

  List<MobileJob> _filtered(List<MobileJob> jobs) {
    return jobs.where((job) {
      if (_activeOnly && !job.isActive) return false;
      final kind = job.kind.trim().toLowerCase();
      switch (_kindFilter) {
        case '':
          return true;
        case 'document':
          return kind.startsWith('document_');
        case 'ssh':
          return kind.startsWith('ssh_');
        default:
          return kind == _kindFilter;
      }
    }).toList();
  }

  @override
  Widget build(BuildContext context) {
    final s = ref.watch(appStringsProvider);
    final list = widget.list;
    if (list == null) {
      return Card(
        child: ListTile(
          leading: const Icon(Icons.cloud_off_outlined),
          title: Text(s.hubJobsTitle),
          subtitle: Text(s.hubJobsUnavailableHint),
        ),
      );
    }
    final filtered = _filtered(list.jobs);
    final activeInFilter = filtered.where((j) => j.isActive).length;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.playlist_play_outlined),
              title: Text(s.unifiedJobs),
              subtitle: Text(
                list.jobs.isEmpty
                    ? s.noHubLongJobs
                    : s.hubJobsCountLine(
                        list.count,
                        list.activeCount,
                        filtered: filtered.length != list.jobs.length
                            ? filtered.length
                            : null,
                      ),
              ),
              trailing: FilterChip(
                label: Text(_activeOnly ? s.activeOnly : s.includeFinished),
                selected: _activeOnly,
                onSelected: (v) => setState(() => _activeOnly = v),
                visualDensity: VisualDensity.compact,
              ),
            ),
            Wrap(
              spacing: 6,
              runSpacing: 4,
              children: [
                for (final (id, label) in _kindFilters(s))
                  ChoiceChip(
                    label: Text(label),
                    selected: _kindFilter == id,
                    onSelected: (_) => setState(() => _kindFilter = id),
                    visualDensity: VisualDensity.compact,
                  ),
              ],
            ),
            const SizedBox(height: 8),
            if (list.jobs.isEmpty)
              ListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(s.noRecentHubJobs),
                subtitle: Text(s.noRecentHubJobsHint),
              )
            else if (filtered.isEmpty)
              ListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(s.filterNoResults),
                subtitle: Text(
                  _activeOnly
                      ? s.filterNoActive(activeInFilter)
                      : s.filterTryOther,
                ),
              )
            else
              for (final job in filtered.take(12))
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                  leading: Icon(_statusIcon(job.status)),
                  title: Text(
                    job.title.isEmpty ? job.jobId : job.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  subtitle: Text(
                    [
                      s.jobKindLabel(job.kind),
                      s.jobStatusLabel(job.status),
                      if (job.message.isNotEmpty) job.message,
                    ].join(' · '),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                  trailing: job.deepLink.isEmpty
                      ? null
                      : TextButton(
                          onPressed: () => context.go(job.deepLink),
                          child: Text(s.openLabel),
                        ),
                ),
          ],
        ),
      ),
    );
  }

  IconData _statusIcon(String status) {
    final st = status.toLowerCase();
    if (st.contains('fail') || st.contains('error')) return Icons.error_outline;
    if (st.contains('ready') ||
        st.contains('done') ||
        st.contains('complete') ||
        st.contains('success')) {
      return Icons.check_circle_outline;
    }
    if (st.contains('cancel')) return Icons.cancel_outlined;
    if (st.contains('run') || st.contains('process') || st.contains('queue')) {
      return Icons.timelapse;
    }
    return Icons.circle_outlined;
  }
}

/// Kept for tests / other callers that still import the free functions.
String mobileJobKindLabel(String kind, {bool isZh = true}) {
  return AppStrings.forLanguage(isZh ? 'zh' : 'en').jobKindLabel(kind);
}

String mobileJobStatusLabel(String status, {bool isZh = true}) {
  return AppStrings.forLanguage(isZh ? 'zh' : 'en').jobStatusLabel(status);
}

class _DocumentTasksCard extends ConsumerWidget {
  final DocumentsState state;

  const _DocumentTasksCard({required this.state});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final upload = state.uploadTask;
    final exportJob = state.exportJob;
    final draft = state.draft;
    final rows = <Widget>[
      ListTile(
        contentPadding: EdgeInsets.zero,
        leading: const Icon(Icons.description_outlined),
        title: Text(s.documentTasks),
        subtitle: Text(
          draft == null
              ? s.noActiveDraft
              : s.currentDraftLine(
                  draft.title.isEmpty ? draft.id : draft.title,
                ),
        ),
      ),
    ];
    if (upload != null) {
      rows.add(
        ListTile(
          contentPadding: EdgeInsets.zero,
          leading: Icon(_statusIcon(upload.status)),
          title: Text(
            s.importTaskLine(
              upload.filename.isEmpty ? upload.taskId : upload.filename,
            ),
          ),
          subtitle: Text('${upload.status} · ${upload.message}'),
          trailing: TextButton(
            onPressed: () => context.go('/documents'),
            child: Text(s.details),
          ),
        ),
      );
    }
    if (exportJob != null) {
      rows.add(
        ListTile(
          contentPadding: EdgeInsets.zero,
          leading: Icon(_statusIcon(exportJob.status)),
          title: Text(s.exportTaskLine(exportJob.jobId)),
          subtitle: Text(exportJob.status),
          trailing: TextButton(
            onPressed: () => context.go('/documents'),
            child: Text(s.details),
          ),
        ),
      );
    }
    if (upload == null && exportJob == null) {
      rows.add(
        ListTile(
          contentPadding: EdgeInsets.zero,
          title: Text(s.noActiveImportExport),
          subtitle: Text(s.noActiveImportExportHint),
        ),
      );
    }
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(children: rows),
      ),
    );
  }
}

class _EmployeeTaskCard extends ConsumerWidget {
  final MobileDigitalEmployeeTask? task;
  final List<MobileDigitalEmployeeTask> history;

  const _EmployeeTaskCard({
    required this.task,
    required this.history,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(appStringsProvider);
    final scheme = Theme.of(context).colorScheme;
    final active = task;
    final running =
        active != null && digitalEmployeeTaskIsRunning(active.status);
    final preview = active == null
        ? ''
        : digitalEmployeeTaskProgressPreview(
            result: active.result,
            message: active.message,
          );
    final statusLabel = active == null
        ? s.noRecentEmployeeTask
        : s.employeeTaskStatusLabel(active.status);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: Icon(
                running ? Icons.sync : Icons.smart_toy_outlined,
                color: running ? scheme.primary : null,
              ),
              title: Text(s.digitalEmployeeTasks),
              subtitle: active == null
                  ? Text(s.noRecentEmployeeTask)
                  : Text(
                      '${active.taskId} · $statusLabel',
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
              trailing: TextButton(
                onPressed: () => context.go('/employees'),
                child: Text(s.employeesPage),
              ),
            ),
            if (active != null && preview.isNotEmpty) ...[
              Text(
                preview,
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
              ),
              if (running) ...[
                const SizedBox(height: 8),
                ClipRRect(
                  borderRadius: BorderRadius.circular(999),
                  child: LinearProgressIndicator(
                    minHeight: 3,
                    backgroundColor: scheme.surfaceContainerHighest,
                  ),
                ),
              ],
              const SizedBox(height: 4),
            ],
            if (history.isNotEmpty) ...[
              const Divider(),
              Text(
                s.recentHistoryCount(history.length.clamp(0, 5)),
                style: Theme.of(context).textTheme.labelLarge,
              ),
              for (final item in history.take(5))
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                  title: Text(
                    item.taskId,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  subtitle: Text(
                    [
                      s.employeeTaskStatusLabel(item.status),
                      if (digitalEmployeeTaskProgressPreview(
                        result: item.result,
                        message: item.message,
                      ).isNotEmpty)
                        digitalEmployeeTaskProgressPreview(
                          result: item.result,
                          message: item.message,
                        ),
                    ].join(' · '),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
            ],
          ],
        ),
      ),
    );
  }
}

IconData _statusIcon(String status) {
  final value = status.toLowerCase();
  if (value.contains('ready') ||
      value.contains('done') ||
      value.contains('complete')) {
    return Icons.check_circle_outline;
  }
  if (value.contains('fail') || value.contains('error')) {
    return Icons.error_outline;
  }
  return Icons.hourglass_top_outlined;
}
