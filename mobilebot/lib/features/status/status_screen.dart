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
import '../../core/locale.dart';
import '../../l10n/gen/app_localizations.dart';
import '../../core/ui_kit.dart';
import '../bots/widgets.dart' show runStatusColor;

/// Per-bot work status — 对应 GUI 任务 tab：进行中的 run + 最近任务状态。
class BotStatusScreen extends ConsumerWidget {
  final Bot bot;

  const BotStatusScreen({super.key, required this.bot});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final api = ref.watch(botApiProvider);
    return Scaffold(
      appBar: AppBar(title: Text(context.S.botStatusTitle(bot.title))),
      body: api == null
          ? Center(child: Text(context.S.notSignedIn))
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
      final runsFuture = api.activeRuns(botId);
      final tasksFuture = api.taskStatuses(botId);
      return (runs: await runsFuture, tasks: await tasksFuture);
    });

class _BotStatusBody extends ConsumerWidget {
  final Bot bot;
  final BotApi api;

  const _BotStatusBody({required this.bot, required this.api});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final data = ref.watch(_botStatusProvider(bot.id));
    return data.when(
      loading: () => const SkeletonList(itemCount: 3),
      error: (error, _) => ListView(
        children: [
          const SizedBox(height: 120),
          EmptyState(
            icon: Icons.cloud_off,
            title: failureMessage(error),
            action: FilledButton.tonal(
              onPressed: () => ref.invalidate(_botStatusProvider(bot.id)),
              child: Text(context.S.retry),
            ),
          ),
        ],
      ),
      data: (status) => ListView(
        padding: const EdgeInsets.all(12),
        children: [
          SectionHeader(title: context.S.stRunningCount(status.runs.length)),
          if (status.runs.isEmpty) _MutedHint(context.S.stNoRunningTasks),
          for (final run in status.runs) _ActiveRunCard(bot: bot, run: run),
          const SizedBox(height: 20),
          SectionHeader(title: context.S.stRecentCount(status.tasks.length)),
          if (status.tasks.isEmpty) _MutedHint(context.S.stNoTaskHistory),
          for (final task in status.tasks) _TaskStatusCard(task: task),
        ],
      ),
    );
  }
}

/// Inline muted hint — the light placeholder for sections that need one
/// sentence, not the full EmptyState treatment.
class _MutedHint extends StatelessWidget {
  final String text;

  const _MutedHint(this.text);

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 12),
    child: Center(
      child: Text(text, style: Theme.of(context).textTheme.bodySmall),
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
                StateChip(
                  label: run.paused ? context.S.stPaused : run.status,
                  color: color,
                ),
              ],
            ),
            if (run.step.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  context.S.stCurrent(run.step),
                  style: theme.textTheme.bodySmall,
                ),
              ),
            if (run.nextStep.isNotEmpty)
              Text(
                context.S.stNext(run.nextStep),
                style: theme.textTheme.bodySmall,
              ),
            if (run.startedAt != null)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  context.S.stStartedAt(
                    DateFormat('MM-dd HH:mm').format(run.startedAt!.toLocal()),
                  ),
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
            if (run.waitingForUser)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text(
                  context.S.stWaitingForYou,
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
                    child: Text(context.S.resume),
                  )
                else
                  TextButton(
                    onPressed: () => _act(context, ref, 'pause'),
                    child: Text(context.S.pause),
                  ),
                TextButton(
                  onPressed: () => _confirmCancel(context, ref),
                  child: Text(
                    context.S.stop,
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

  /// Stopping a run kills work in progress on the bot's computer — confirm
  /// first, same as the remote task screen does.
  Future<void> _confirmCancel(BuildContext context, WidgetRef ref) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(context.S.stStopRunTitle),
        content: Text(
          context.S.stStopRunBody(run.task.isNotEmpty ? run.task : run.runId),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: Text(context.S.cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(context.S.stop),
          ),
        ],
      ),
    );
    if (ok == true && context.mounted) {
      await _act(context, ref, 'cancel');
    }
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
                  context.S.stProgress(task.whereWeAre),
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
                  context.S.stError(task.error),
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.error,
                  ),
                ),
              ),
            if (task.openProblems.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  context.S.stLeftover(task.openProblems),
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
        title: Text(context.S.stTitle),
        actions: [
          IconButton(
            tooltip: context.S.stBotRooms,
            icon: const Icon(Icons.groups_outlined),
            onPressed: () => context.push('/rooms'),
          ),
        ],
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 0),
            child: SizedBox(
              width: double.infinity,
              child: SegmentedButton<int>(
                segments: [
                  ButtonSegment(
                    value: 0,
                    label: Text(context.S.stSegmentTasks),
                  ),
                  ButtonSegment(value: 1, label: Text(context.S.stSegmentFeed)),
                ],
                selected: {_segment},
                onSelectionChanged: (v) => setState(() => _segment = v.first),
              ),
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
    // Keep the requested artifact name (extension included): a server-side
    // empty name would otherwise save an extensionless "文件" the system
    // share sheet cannot open.
    final rawName = file.name.trim().isNotEmpty ? file.name.trim() : name;
    final safe = rawName.replaceAll(RegExp(r'[\\/:*?"<>|]'), '_');
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
        SnackBar(
          content: Text(context.S.stDownloadFailed(failureMessage(error))),
        ),
      );
    }
  }
}

class _FeedBody extends ConsumerWidget {
  final List<Bot> bots;

  const _FeedBody({required this.bots});

  String _botName(String id, AppLocalizations S) {
    for (final bot in bots) {
      if (bot.id == id) return bot.title;
    }
    return id.isEmpty ? S.stSystemName : id;
  }

  String _kindLabel(FeedNote note, AppLocalizations S) {
    if (note.kind == 'delegate') return S.stKindDelegate;
    return switch (note.topic) {
      'done' => S.stKindDone,
      'blocked' => S.stKindBlocked,
      'progress' => S.stKindProgress,
      'question' => S.stKindQuestion,
      'artifact' => S.stKindArtifact,
      'reply' => S.stKindReply,
      _ => note.kind.isNotEmpty ? note.kind : S.stKindDefault,
    };
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final data = ref.watch(_feedProvider);
    final assignments =
        ref.watch(_assignmentsProvider).value ?? const <BotAssignment>[];
    final said = ref.watch(_saidProvider).value ?? const <SaidNote>[];
    return data.when(
      loading: () => const SkeletonList(itemCount: 4),
      error: (error, _) => ListView(
        children: [
          const SizedBox(height: 120),
          EmptyState(
            icon: Icons.cloud_off,
            title: failureMessage(error),
            action: FilledButton.tonal(
              onPressed: () => ref.invalidate(_feedProvider),
              child: Text(context.S.retry),
            ),
          ),
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
              EmptyState(
                icon: Icons.forum_outlined,
                title: context.S.stEmptyFeed,
              ),
            ],
          );
        }
        final theme = Theme.of(context);
        final S = context.S;
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
                return SectionHeader(title: context.S.stSaidHeader);
              }
              if (index <= direct.length) {
                return _SaidCard(
                  note: direct[direct.length - index],
                  botName: (id) => _botName(id, S),
                );
              }
              index -= saidCount;
            }
            if (openAssignments.isNotEmpty) {
              if (index == 0) {
                return SectionHeader(title: context.S.stCollabHeader);
              }
              if (index <= openAssignments.length) {
                return _AssignmentCard(
                  assignment: openAssignments[index - 1],
                  botName: (id) => _botName(id, S),
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
                  '${_botName(note.from, S)} → ${_botName(note.target, S)}',
                  style: theme.textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
                ),
                subtitle: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (note.text.isNotEmpty)
                      LinkifiedText(
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
                        _kindLabel(note, S),
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
                        child: Text(context.S.start),
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
            LinkifiedText(
              note.text,
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
            ),
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

  String _stateLabel(String state, AppLocalizations S) => switch (state) {
    'queued' => S.stStateQueued,
    'running' => S.stStateRunning,
    'held' => S.stStateHeld,
    'waiting_human' => S.stStateWaitingHuman,
    'blocked' => S.stStateBlocked,
    'completed' => S.stStateCompleted,
    'failed' => S.stStateFailed,
    'accepted' => S.stStateAccepted,
    'returned' => S.stStateReturned,
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
                context.S.stQuestion(a.openQuestion),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
            Text(
              [
                _stateLabel(a.state, context.S),
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
      // Fan out across bots in parallel — a serial loop makes the page
      // wait on N round trips even though the runs are independent. One
      // failing bot (or a pending-pull outage) must not hide the rest.
      final runListsFuture = Future.wait([
        for (final bot in bots)
          api.activeRuns(bot.id).onError((_, _) => const <ActiveRun>[]),
      ]);
      final deliveriesFuture = api.pendingDeliveries().onError(
        (_, _) => const <PendingDelivery>[],
      );
      final runLists = await runListsFuture;
      final deliveries = await deliveriesFuture;
      return (
        runs: [for (final list in runLists) ...list],
        deliveries: deliveries,
      );
    });

class _GlobalStatusBody extends ConsumerWidget {
  final List<Bot> bots;

  const _GlobalStatusBody({required this.bots});

  String _botName(String id, AppLocalizations S) {
    for (final bot in bots) {
      if (bot.id == id) return bot.title;
    }
    return id.isEmpty ? S.stSystemName : id;
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final data = ref.watch(_globalStatusProvider);
    final S = context.S;
    return data.when(
      loading: () => const SkeletonList(itemCount: 4),
      error: (error, _) => ListView(
        children: [
          const SizedBox(height: 120),
          EmptyState(
            icon: Icons.cloud_off,
            title: failureMessage(error),
            action: FilledButton.tonal(
              onPressed: () => ref.invalidate(_globalStatusProvider),
              child: Text(context.S.retry),
            ),
          ),
        ],
      ),
      data: (status) {
        if (status.runs.isEmpty && status.deliveries.isEmpty) {
          return ListView(
            children: [
              const SizedBox(height: 140),
              EmptyState(
                icon: Icons.check_circle_outline,
                title: context.S.stAllQuiet,
              ),
            ],
          );
        }
        return ListView(
          padding: const EdgeInsets.all(12),
          children: [
            SectionHeader(title: context.S.stRunningCount(status.runs.length)),
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
                    '${_botName(run.botId, S)} · ${run.step.isNotEmpty ? run.step : run.status}',
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
            SectionHeader(
              title: context.S.stDeliveriesCount(status.deliveries.length),
            ),
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
                    '${_botName(delivery.botId, S)} · ${delivery.reply.text.isNotEmpty ? delivery.reply.text : delivery.status}',
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
