import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';

import '../bots/bot_api.dart';
import '../bots/bot_models.dart';
import '../bots/bots_controller.dart';
import '../bots/widgets.dart';

/// Per-bot work status — 对应 GUI 任务 tab：进行中的 run + 最近任务状态。
class BotStatusScreen extends ConsumerWidget {
  final Bot bot;

  const BotStatusScreen({super.key, required this.bot});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final api = ref.watch(botApiProvider);
    return Scaffold(
      appBar: AppBar(title: Text('${bot.title} · 任务状态')),
      body: api == null
          ? const Center(child: Text('未登录'))
          : RefreshIndicator(
              onRefresh: () async {
                ref.invalidate(_botStatusProvider(bot.id));
                try {
                  await ref.read(_botStatusProvider(bot.id).future);
                } on Object {
                  // Error state renders via data.when(error:).
                }
              },
              child: _BotStatusBody(bot: bot, api: api),
            ),
    );
  }
}

final _botStatusProvider = FutureProvider.autoDispose
    .family<({List<ActiveRun> runs, List<DesktopTaskStatus> tasks}), String>((
      ref,
      botId,
    ) async {
      final api = ref.watch(botApiProvider);
      if (api == null) {
        return (runs: const <ActiveRun>[], tasks: const <DesktopTaskStatus>[]);
      }
      final runs = await api.activeRuns(botId);
      final tasks = await api.taskStatuses(botId);
      return (runs: runs, tasks: tasks);
    });

class _BotStatusBody extends ConsumerWidget {
  final Bot bot;
  final BotApi api;

  const _BotStatusBody({required this.bot, required this.api});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final data = ref.watch(_botStatusProvider(bot.id));
    return data.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (error, _) => ListView(
        children: [
          const SizedBox(height: 120),
          Center(child: Text(failureMessage(error))),
        ],
      ),
      data: (status) => ListView(
        padding: const EdgeInsets.all(12),
        children: [
          _SectionTitle('进行中（${status.runs.length}）'),
          if (status.runs.isEmpty) const _EmptyHint('没有正在运行的任务'),
          for (final run in status.runs) _ActiveRunCard(bot: bot, run: run),
          const SizedBox(height: 20),
          _SectionTitle('最近任务（${status.tasks.length}）'),
          if (status.tasks.isEmpty) const _EmptyHint('暂无任务记录'),
          for (final task in status.tasks) _TaskStatusCard(task: task),
        ],
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  final String text;

  const _SectionTitle(this.text);

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 8, top: 4),
    child: Text(text, style: Theme.of(context).textTheme.titleSmall),
  );
}

class _EmptyHint extends StatelessWidget {
  final String text;

  const _EmptyHint(this.text);

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 12),
    child: Center(
      child: Text(
        text,
        style: TextStyle(color: Theme.of(context).colorScheme.onSurfaceVariant),
      ),
    ),
  );
}

class _ActiveRunCard extends ConsumerWidget {
  final Bot bot;
  final ActiveRun run;

  const _ActiveRunCard({required this.bot, required this.run});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final color = runStatusColor(run.status);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    run.task.isNotEmpty ? run.task : run.runId,
                    style: theme.textTheme.titleSmall,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                StateChip(label: run.paused ? '已暂停' : run.status, color: color),
              ],
            ),
            if (run.step.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text('当前：${run.step}', style: theme.textTheme.bodySmall),
              ),
            if (run.nextStep.isNotEmpty)
              Text('下一步：${run.nextStep}', style: theme.textTheme.bodySmall),
            if (run.startedAt != null)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  '开始于 ${DateFormat('MM-dd HH:mm').format(run.startedAt!.toLocal())}',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
            if (run.waitingForUser)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  '等待你的操作',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.tertiary,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
            const SizedBox(height: 8),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                if (run.paused)
                  TextButton(
                    onPressed: () => _act(context, ref, 'resume'),
                    child: const Text('继续'),
                  )
                else
                  TextButton(
                    onPressed: () => _act(context, ref, 'pause'),
                    child: const Text('暂停'),
                  ),
                TextButton(
                  onPressed: () => _act(context, ref, 'cancel'),
                  child: Text(
                    '停止',
                    style: TextStyle(color: theme.colorScheme.error),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _act(BuildContext context, WidgetRef ref, String op) async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    try {
      switch (op) {
        case 'pause':
          await api.pauseRun(bot.id, run.runId);
        case 'resume':
          await api.resumeRun(bot.id, run.runId);
        case 'cancel':
          await api.cancelRun(bot.id, run.runId);
      }
      ref.invalidate(_botStatusProvider(bot.id));
    } on Object catch (error) {
      if (context.mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    }
  }
}

class _TaskStatusCard extends StatelessWidget {
  final DesktopTaskStatus task;

  const _TaskStatusCard({required this.task});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    task.task.isNotEmpty ? task.task : task.runId,
                    style: theme.textTheme.titleSmall,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                StateChip(
                  label: task.status,
                  color: runStatusColor(task.status),
                ),
              ],
            ),
            if (task.whereWeAre.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  '进展：${task.whereWeAre}',
                  style: theme.textTheme.bodySmall,
                ),
              ),
            if (task.text.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(task.text, style: theme.textTheme.bodySmall),
              ),
            if (task.error.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  '错误：${task.error}',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.error,
                  ),
                ),
              ),
            if (task.openProblems.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  '遗留：${task.openProblems}',
                  style: theme.textTheme.bodySmall,
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// 全局工作状态 tab：所有 bot 的活动 run + 待展示的结果 + Bot 间动态。
class StatusScreen extends ConsumerStatefulWidget {
  const StatusScreen({super.key});

  @override
  ConsumerState<StatusScreen> createState() => _StatusScreenState();
}

class _StatusScreenState extends ConsumerState<StatusScreen> {
  int _segment = 0; // 0 = 任务, 1 = 动态

  @override
  Widget build(BuildContext context) {
    final bots = ref.watch(botsProvider).bots;
    return Scaffold(
      appBar: AppBar(
        title: const Text('工作状态'),
        actions: [
          IconButton(
            tooltip: 'Bot 房间',
            icon: const Icon(Icons.groups_outlined),
            onPressed: () => context.push('/rooms'),
          ),
        ],
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 0),
            child: SegmentedButton<int>(
              segments: const [
                ButtonSegment(value: 0, label: Text('任务')),
                ButtonSegment(value: 1, label: Text('动态')),
              ],
              selected: {_segment},
              onSelectionChanged: (v) => setState(() => _segment = v.first),
            ),
          ),
          Expanded(
            child: RefreshIndicator(
              onRefresh: () async {
                try {
                  if (_segment == 0) {
                    ref.invalidate(_globalStatusProvider);
                    await Future.wait([
                      ref.read(_globalStatusProvider.future),
                      ref.read(botsProvider.notifier).refresh(),
                    ]);
                  } else {
                    ref.invalidate(_feedProvider);
                    ref.invalidate(_saidProvider);
                    ref.invalidate(_assignmentsProvider);
                    await ref.read(_feedProvider.future);
                  }
                } on Object {
                  // Error state renders via data.when(error:).
                }
              },
              child: _segment == 0
                  ? _GlobalStatusBody(bots: bots)
                  : _FeedBody(bots: bots),
            ),
          ),
        ],
      ),
    );
  }
}

final _feedProvider = FutureProvider.autoDispose<List<FeedNote>>((ref) async {
  final api = ref.watch(botApiProvider);
  if (api == null) return const [];
  final items = await api.feed(all: true);
  // These are now displayed; mark the unshown ones so later polls skip them.
  final ids = [for (final n in items) n.id].where((id) => id.isNotEmpty);
  if (ids.isNotEmpty) {
    try {
      await api.markFeedShown(ids.toList());
    } on Object {
      // Marking is best-effort; the feed still renders.
    }
  }
  return items;
});

final _saidProvider = FutureProvider.autoDispose<List<SaidNote>>((ref) async {
  final api = ref.watch(botApiProvider);
  if (api == null) return const [];
  try {
    return await api.said();
  } on Object {
    return const [];
  }
});

final _assignmentsProvider = FutureProvider.autoDispose<List<BotAssignment>>((
  ref,
) async {
  final api = ref.watch(botApiProvider);
  if (api == null) return const [];
  try {
    return await api.assignments();
  } on Object {
    return const [];
  }
});

/// Download a file a bot attached to a feed/mail note and hand it to the
/// system share sheet (`GET /bots/{id}/inbox/{noteID}/files/{name}`).
/// Failures surface as a snackbar instead of an unhandled async error.
Future<void> shareInboxFile(
  BuildContext context,
  WidgetRef ref,
  String botId,
  String noteId,
  String name,
) async {
  final api = ref.read(botApiProvider);
  if (api == null) return;
  try {
    final file = await api.inboxFile(botId, noteId, name);
    if (file.data.isEmpty || !context.mounted) return;
    final bytes = base64Decode(file.data);
    final dir = await getTemporaryDirectory();
    final safe = file.displayName.replaceAll(RegExp(r'[\\/:*?"<>|]'), '_');
    final path = '${dir.path}${Platform.pathSeparator}$safe';
    await File(path).writeAsBytes(bytes, flush: true);
    await SharePlus.instance.share(
      ShareParams(
        files: [XFile(path, mimeType: file.mime.isNotEmpty ? file.mime : null)],
      ),
    );
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('下载附件失败：${failureMessage(error)}')),
      );
    }
  }
}

class _FeedBody extends ConsumerWidget {
  final List<Bot> bots;

  const _FeedBody({required this.bots});

  String _botName(String id) {
    for (final bot in bots) {
      if (bot.id == id) return bot.title;
    }
    return id.isEmpty ? '系统' : id;
  }

  String _kindLabel(FeedNote note) {
    if (note.kind == 'delegate') return '委托';
    return switch (note.topic) {
      'done' => '完成',
      'blocked' => '受阻',
      'progress' => '进展',
      'question' => '提问',
      'artifact' => '产物',
      'reply' => '回复',
      _ => note.kind.isNotEmpty ? note.kind : '动态',
    };
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final data = ref.watch(_feedProvider);
    final assignments =
        ref.watch(_assignmentsProvider).value ?? const <BotAssignment>[];
    final said = ref.watch(_saidProvider).value ?? const <SaidNote>[];
    return data.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (error, _) => ListView(
        children: [
          const SizedBox(height: 120),
          Center(child: Text(failureMessage(error))),
        ],
      ),
      data: (notes) {
        final feedIds = {for (final n in notes) n.id};
        final direct = [
          for (final s in said)
            if (!feedIds.contains(s.id)) s,
        ];
        if (notes.isEmpty && assignments.isEmpty && direct.isEmpty) {
          return ListView(
            children: [
              const SizedBox(height: 140),
              Icon(Icons.forum_outlined, size: 56, color: Colors.grey.shade400),
              const SizedBox(height: 16),
              const Center(child: Text('还没有 Bot 之间的动态')),
            ],
          );
        }
        final theme = Theme.of(context);
        final openAssignments = [
          for (final a in assignments)
            if (a.state != 'completed' && a.state != 'failed') a,
        ];
        final saidCount = direct.isEmpty ? 0 : direct.length + 1;
        final assignCount = openAssignments.isEmpty
            ? 0
            : openAssignments.length + 1;
        return ListView.builder(
          padding: const EdgeInsets.all(12),
          itemCount: notes.length + saidCount + assignCount,
          itemBuilder: (context, index) {
            if (direct.isNotEmpty) {
              if (index == 0) {
                return Padding(
                  padding: const EdgeInsets.only(bottom: 4),
                  child: Text('Bot 对你说', style: theme.textTheme.titleSmall),
                );
              }
              if (index <= direct.length) {
                return _SaidCard(
                  note: direct[direct.length - index],
                  botName: _botName,
                );
              }
              index -= saidCount;
            }
            if (openAssignments.isNotEmpty) {
              if (index == 0) {
                return Padding(
                  padding: const EdgeInsets.only(bottom: 4),
                  child: Text('协作任务', style: theme.textTheme.titleSmall),
                );
              }
              if (index <= openAssignments.length) {
                return _AssignmentCard(
                  assignment: openAssignments[index - 1],
                  botName: _botName,
                );
              }
              index -= assignCount;
            }
            final note = notes[notes.length - 1 - index];
            return Card(
              child: ListTile(
                leading: CircleAvatar(
                  radius: 16,
                  backgroundColor: theme.colorScheme.secondaryContainer,
                  child: Icon(
                    note.kind == 'delegate'
                        ? Icons.send_outlined
                        : Icons.chat_bubble_outline,
                    size: 16,
                    color: theme.colorScheme.onSecondaryContainer,
                  ),
                ),
                title: Text(
                  '${_botName(note.from)} → ${_botName(note.target)}',
                  style: theme.textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
                ),
                subtitle: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (note.text.isNotEmpty)
                      Text(
                        note.text,
                        maxLines: 3,
                        overflow: TextOverflow.ellipsis,
                      ),
                    if (note.artifacts.isNotEmpty)
                      Padding(
                        padding: const EdgeInsets.only(top: 4),
                        child: Wrap(
                          spacing: 6,
                          runSpacing: 4,
                          children: [
                            for (final name in note.artifacts)
                              ActionChip(
                                avatar: const Icon(Icons.attach_file, size: 14),
                                label: Text(
                                  name,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                ),
                                onPressed: () => shareInboxFile(
                                  context,
                                  ref,
                                  note.target,
                                  note.id,
                                  name,
                                ),
                              ),
                          ],
                        ),
                      ),
                    Text(
                      [
                        _kindLabel(note),
                        if (note.at != null)
                          DateFormat('MM-dd HH:mm').format(note.at!.toLocal()),
                      ].join(' · '),
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
                isThreeLine: true,
                trailing: note.kind == 'delegate' && note.runId.isEmpty
                    ? TextButton(
                        onPressed: () async {
                          final api = ref.read(botApiProvider);
                          if (api == null) return;
                          try {
                            await api.crossNoteStart(note.target, note.id);
                            ref.invalidate(_feedProvider);
                          } on Object catch (error) {
                            if (context.mounted) {
                              ScaffoldMessenger.of(context).showSnackBar(
                                SnackBar(content: Text(failureMessage(error))),
                              );
                            }
                          }
                        },
                        child: const Text('启动'),
                      )
                    : null,
                onTap: note.target.isNotEmpty
                    ? () => context.push(
                        '/bot/${Uri.encodeComponent(note.target)}',
                      )
                    : null,
              ),
            );
          },
        );
      },
    );
  }
}

/// A direct "say" note a bot addressed to the person (BotSaidInline).
class _SaidCard extends ConsumerWidget {
  final SaidNote note;
  final String Function(String id) botName;

  const _SaidCard({required this.note, required this.botName});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return Card(
      child: ListTile(
        leading: CircleAvatar(
          radius: 16,
          backgroundColor: theme.colorScheme.tertiaryContainer,
          child: Icon(
            Icons.record_voice_over_outlined,
            size: 16,
            color: theme.colorScheme.onTertiaryContainer,
          ),
        ),
        title: Text(
          botName(note.from),
          style: theme.textTheme.bodyMedium?.copyWith(
            fontWeight: FontWeight.w600,
          ),
        ),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(note.text, maxLines: 3, overflow: TextOverflow.ellipsis),
            if (note.files.isNotEmpty)
              Wrap(
                spacing: 6,
                children: [
                  for (final name in note.files)
                    ActionChip(
                      avatar: const Icon(Icons.attach_file, size: 14),
                      label: Text(
                        name,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      onPressed: () => shareInboxFile(
                        context,
                        ref,
                        note.from,
                        note.id,
                        name,
                      ),
                    ),
                ],
              ),
            if (note.createdAt != null)
              Text(
                DateFormat('MM-dd HH:mm').format(note.createdAt!.toLocal()),
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
          ],
        ),
        isThreeLine: true,
        onTap: note.from.isNotEmpty
            ? () => context.push('/bot/${Uri.encodeComponent(note.from)}')
            : null,
      ),
    );
  }
}

/// One open delegation on the collab board.
class _AssignmentCard extends StatelessWidget {
  final BotAssignment assignment;
  final String Function(String id) botName;

  const _AssignmentCard({required this.assignment, required this.botName});

  String _stateLabel(String state) => switch (state) {
    'queued' => '排队中',
    'running' => '进行中',
    'held' => '已挂起',
    'waiting_human' => '等待人工',
    'blocked' => '受阻',
    'completed' => '已完成',
    'failed' => '失败',
    'accepted' => '已验收',
    'returned' => '已退回',
    _ => state,
  };

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final a = assignment;
    return Card(
      child: ListTile(
        leading: CircleAvatar(
          radius: 16,
          backgroundColor: theme.colorScheme.primaryContainer,
          child: Icon(
            Icons.assignment_outlined,
            size: 16,
            color: theme.colorScheme.onPrimaryContainer,
          ),
        ),
        title: Text(
          '${botName(a.ownerBot)} → ${botName(a.targetBot)}',
          style: theme.textTheme.bodyMedium?.copyWith(
            fontWeight: FontWeight.w600,
          ),
        ),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (a.task.isNotEmpty)
              Text(a.task, maxLines: 2, overflow: TextOverflow.ellipsis),
            if (a.openQuestion.isNotEmpty)
              Text(
                '问题：${a.openQuestion}',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
            Text(
              [
                _stateLabel(a.state),
                if (a.createdAt != null)
                  DateFormat('MM-dd HH:mm').format(a.createdAt!.toLocal()),
              ].join(' · '),
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
        isThreeLine: true,
        onTap: a.targetBot.isNotEmpty
            ? () => context.push('/bot/${Uri.encodeComponent(a.targetBot)}')
            : null,
      ),
    );
  }
}

final _globalStatusProvider =
    FutureProvider.autoDispose<
      ({List<ActiveRun> runs, List<PendingDelivery> deliveries})
    >((ref) async {
      final api = ref.watch(botApiProvider);
      final bots = ref.watch(botsProvider).bots;
      if (api == null) {
        return (
          runs: const <ActiveRun>[],
          deliveries: const <PendingDelivery>[],
        );
      }
      final runs = <ActiveRun>[];
      for (final bot in bots) {
        try {
          runs.addAll(await api.activeRuns(bot.id));
        } on Object {
          // One failing bot must not hide the others.
        }
      }
      var deliveries = const <PendingDelivery>[];
      try {
        deliveries = await api.pendingDeliveries();
      } on Object {
        // Same isolation: a pending-pull outage must not blank the runs.
      }
      return (runs: runs, deliveries: deliveries);
    });

class _GlobalStatusBody extends ConsumerWidget {
  final List<Bot> bots;

  const _GlobalStatusBody({required this.bots});

  String _botName(String id) {
    for (final bot in bots) {
      if (bot.id == id) return bot.title;
    }
    return id;
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final data = ref.watch(_globalStatusProvider);
    return data.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (error, _) => ListView(
        children: [
          const SizedBox(height: 120),
          Center(child: Text(failureMessage(error))),
        ],
      ),
      data: (status) {
        if (status.runs.isEmpty && status.deliveries.isEmpty) {
          return ListView(
            children: [
              const SizedBox(height: 140),
              Icon(
                Icons.check_circle_outline,
                size: 56,
                color: Colors.grey.shade400,
              ),
              const SizedBox(height: 16),
              const Center(child: Text('一切安静，没有进行中的任务')),
            ],
          );
        }
        return ListView(
          padding: const EdgeInsets.all(12),
          children: [
            _SectionTitle('进行中（${status.runs.length}）'),
            for (final run in status.runs)
              Card(
                child: ListTile(
                  leading: SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: runStatusColor(run.status),
                    ),
                  ),
                  title: Text(
                    run.task.isNotEmpty ? run.task : run.runId,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  subtitle: Text(
                    '${_botName(run.botId)} · ${run.step.isNotEmpty ? run.step : run.status}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push(
                    '/bot/${Uri.encodeComponent(run.botId)}/status',
                  ),
                ),
              ),
            const SizedBox(height: 20),
            _SectionTitle('待查看结果（${status.deliveries.length}）'),
            for (final delivery in status.deliveries)
              Card(
                child: ListTile(
                  leading: Icon(
                    Icons.mark_chat_unread_outlined,
                    color: runStatusColor(delivery.status),
                  ),
                  title: Text(
                    delivery.task.isNotEmpty ? delivery.task : delivery.runId,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  subtitle: Text(
                    '${_botName(delivery.botId)} · ${delivery.reply.text.isNotEmpty ? delivery.reply.text : delivery.status}',
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push(
                    '/bot/${Uri.encodeComponent(delivery.botId)}',
                  ),
                ),
              ),
          ],
        );
      },
    );
  }
}
