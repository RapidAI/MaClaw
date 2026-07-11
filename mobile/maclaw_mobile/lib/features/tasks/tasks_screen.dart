import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_bootstrap.dart';
import '../../l10n/app_strings.dart';
import '../../shared/surface.dart';
import '../account/account_agent_status_card.dart';
import '../auth/session_controller.dart';
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
          loading: () => const LoadingCard(label: '加载 Hub 任务…'),
          error: (error, _) => Card(
            child: ListTile(
              leading: const Icon(Icons.error_outline),
              title: const Text('Hub 任务加载失败'),
              subtitle: Text('$error'),
            ),
          ),
        ),
        const SizedBox(height: 12),
        documents.when(
          data: (state) => _DocumentTasksCard(state: state),
          loading: () => const LoadingCard(label: '加载文档任务…'),
          error: (error, _) => Card(
            child: ListTile(
              leading: const Icon(Icons.error_outline),
              title: const Text('文档任务加载失败'),
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
          loading: () => const LoadingCard(label: '加载员工任务…'),
          error: (error, _) => Card(
            child: ListTile(
              leading: const Icon(Icons.error_outline),
              title: const Text('员工任务加载失败'),
              subtitle: Text('$error'),
            ),
          ),
        ),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.description_outlined,
          title: '打开文档编辑',
          subtitle: '查看草稿、轻编辑、导入导出（二级页面）',
          actionLabel: '进入',
          onPressed: () => context.go('/documents'),
        ),
        const SizedBox(height: 12),
        ActionTile(
          icon: Icons.smart_toy_outlined,
          title: '数字员工',
          subtitle: '与分身交谈、查看派单',
          actionLabel: '进入',
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
    final knowledge = ref.watch(accountKnowledgeStatusProvider);
    final health = ref.watch(accountMcpHealthProvider);
    final knowledgeLine = knowledge.when(
      data: (s) {
        if (s == null || !s.available) {
          return '知识库：未就绪';
        }
        return '知识库：来源 ${s.sources} · 卡片 ${s.cards}（${s.mode}）';
      },
      loading: () => '知识库：加载中…',
      error: (_, __) => '知识库：不可用',
    );
    final skills = ref.watch(accountSkillsProvider);
    final mcpLine = health.when(
      data: (h) {
        if (h == null) return 'MCP：未探测（点刷新探测）';
        return 'MCP：健康 ${h.healthyCount}/${h.serverCount} · 工具 ${h.availableTools}';
      },
      loading: () => 'MCP：探测中…',
      error: (_, __) => 'MCP：探测失败',
    );
    final skillsLine = skills.when(
      data: (s) => s == null ? '技能：未知' : '技能：${s.count} 个',
      loading: () => '技能：加载中…',
      error: (_, __) => '技能：不可用',
    );
    return Card(
      child: ListTile(
        leading: const Icon(Icons.hub_outlined),
        title: const Text('官方 Agent 后台'),
        subtitle: Text('$knowledgeLine\n$mcpLine\n$skillsLine'),
        isThreeLine: true,
        trailing: IconButton(
          tooltip: '刷新 Agent 状态',
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

class _QuotaCard extends StatelessWidget {
  final MobileLimits limits;
  final bool refreshing;

  const _QuotaCard({
    required this.limits,
    this.refreshing = false,
  });

  @override
  Widget build(BuildContext context) {
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
                    '文档空间',
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
              '${limits.documentQuotaBytes <= 0 ? '（默认免费额度）' : ''}',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

class _UnifiedJobsCard extends StatefulWidget {
  final MobileJobsList? list;

  const _UnifiedJobsCard({required this.list});

  @override
  State<_UnifiedJobsCard> createState() => _UnifiedJobsCardState();
}

class _UnifiedJobsCardState extends State<_UnifiedJobsCard> {
  /// empty = all kinds
  String _kindFilter = '';
  bool _activeOnly = false;

  static const _kindFilters = <(String id, String label)>[
    ('', '全部'),
    ('assistant', '助手'),
    ('document', '文档'),
    ('digital_employee', '员工'),
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
    final list = widget.list;
    if (list == null) {
      return const Card(
        child: ListTile(
          leading: Icon(Icons.cloud_off_outlined),
          title: Text('Hub 任务列表'),
          subtitle: Text('未登录或 Hub 暂不可用。下方仍显示本机缓存的文档/员工任务。'),
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
              title: const Text('统一任务'),
              subtitle: Text(
                list.jobs.isEmpty
                    ? '暂无 Hub 侧长任务'
                    : '共 ${list.count} 条 · 进行中 ${list.activeCount}'
                        '${filtered.length != list.jobs.length ? ' · 筛选 ${filtered.length}' : ''}',
              ),
              trailing: FilterChip(
                label: Text(_activeOnly ? '仅进行中' : '含已结束'),
                selected: _activeOnly,
                onSelected: (v) => setState(() => _activeOnly = v),
                visualDensity: VisualDensity.compact,
              ),
            ),
            Wrap(
              spacing: 6,
              runSpacing: 4,
              children: [
                for (final (id, label) in _kindFilters)
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
              const ListTile(
                contentPadding: EdgeInsets.zero,
                title: Text('没有进行中或最近的 Hub 任务'),
                subtitle: Text('导入/导出、员工派单、SSH 长命令会出现在这里。'),
              )
            else if (filtered.isEmpty)
              ListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('当前筛选无结果'),
                subtitle: Text(
                  _activeOnly
                      ? '没有进行中的任务（筛选内进行中 $activeInFilter）'
                      : '试试「全部」或其他类型',
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
                      mobileJobKindLabel(job.kind),
                      mobileJobStatusLabel(job.status),
                      if (job.message.isNotEmpty) job.message,
                    ].join(' · '),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                  trailing: job.deepLink.isEmpty
                      ? null
                      : TextButton(
                          onPressed: () => context.go(job.deepLink),
                          child: const Text('打开'),
                        ),
                ),
          ],
        ),
      ),
    );
  }

  IconData _statusIcon(String status) {
    final s = status.toLowerCase();
    if (s.contains('fail') || s.contains('error')) return Icons.error_outline;
    if (s.contains('ready') ||
        s.contains('done') ||
        s.contains('complete') ||
        s.contains('success')) {
      return Icons.check_circle_outline;
    }
    if (s.contains('cancel')) return Icons.cancel_outlined;
    if (s.contains('run') || s.contains('process') || s.contains('queue')) {
      return Icons.timelapse;
    }
    return Icons.circle_outlined;
  }
}

String mobileJobKindLabel(String kind) {
  return switch (kind.trim().toLowerCase()) {
    'document_upload' => '文档导入',
    'document_export' => '文档导出',
    'document_process' => '文档处理',
    'digital_employee' => '数字员工',
    'ssh_command' => 'SSH 命令',
    'ssh_file' => 'SSH 文件',
    'ssh_session' => 'SSH 会话',
    'assistant' => 'AI 助手',
    _ => kind.isEmpty ? '任务' : kind,
  };
}

String mobileJobStatusLabel(String status) {
  final s = status.trim().toLowerCase();
  return switch (s) {
    'queued' || 'pending' => '排队中',
    'running' || 'processing' => '进行中',
    'ready' || 'done' || 'completed' || 'success' => '已完成',
    'failed' || 'error' => '失败',
    'cancelled' || 'canceled' => '已取消',
    'agent_claimed' => '已接管',
    'kill_requested' => '终止中',
    'wait_requested' => '等待中',
    _ => status.isEmpty ? '未知' : status,
  };
}

class _DocumentTasksCard extends StatelessWidget {
  final DocumentsState state;

  const _DocumentTasksCard({required this.state});

  @override
  Widget build(BuildContext context) {
    final upload = state.uploadTask;
    final exportJob = state.exportJob;
    final draft = state.draft;
    final rows = <Widget>[
      ListTile(
        contentPadding: EdgeInsets.zero,
        leading: const Icon(Icons.description_outlined),
        title: const Text('文档任务'),
        subtitle: Text(
          draft == null
              ? '暂无活动草稿'
              : '当前草稿：${draft.title.isEmpty ? draft.id : draft.title}',
        ),
      ),
    ];
    if (upload != null) {
      rows.add(
        ListTile(
          contentPadding: EdgeInsets.zero,
          leading: Icon(_statusIcon(upload.status)),
          title: Text(
            '导入 · ${upload.filename.isEmpty ? upload.taskId : upload.filename}',
          ),
          subtitle: Text('${upload.status} · ${upload.message}'),
          trailing: TextButton(
            onPressed: () => context.go('/documents'),
            child: const Text('详情'),
          ),
        ),
      );
    }
    if (exportJob != null) {
      rows.add(
        ListTile(
          contentPadding: EdgeInsets.zero,
          leading: Icon(_statusIcon(exportJob.status)),
          title: Text('导出 · ${exportJob.jobId}'),
          subtitle: Text(exportJob.status),
          trailing: TextButton(
            onPressed: () => context.go('/documents'),
            child: const Text('详情'),
          ),
        ),
      );
    }
    if (upload == null && exportJob == null) {
      rows.add(
        const ListTile(
          contentPadding: EdgeInsets.zero,
          title: Text('没有进行中的导入/导出'),
          subtitle: Text('从文档页导入文件或导出后，进度会出现在这里。'),
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

class _EmployeeTaskCard extends StatelessWidget {
  final MobileDigitalEmployeeTask? task;
  final List<MobileDigitalEmployeeTask> history;

  const _EmployeeTaskCard({
    required this.task,
    required this.history,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.smart_toy_outlined),
              title: const Text('数字员工任务'),
              subtitle: task == null
                  ? const Text('暂无最近任务')
                  : Text('${task!.taskId} · ${task!.status}'),
              trailing: TextButton(
                onPressed: () => context.go('/employees'),
                child: const Text('员工页'),
              ),
            ),
            if (history.isNotEmpty) ...[
              const Divider(),
              Text(
                '最近 ${history.length.clamp(0, 5)} 条',
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
                  subtitle: Text(item.status),
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
