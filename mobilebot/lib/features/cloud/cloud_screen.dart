import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../components/mermaid/mermaid.dart';
import '../../core/locale.dart';
import '../../core/providers.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart' show linkifyMarkdownUrls, openExternalUrl;
import '../bots/bots_controller.dart' show failureMessage;
import 'cloud_api.dart';
import 'cloud_models.dart';

final cloudApiProvider = Provider<CloudApi?>((ref) {
  final client = ref.watch(apiClientProvider);
  return client == null ? null : CloudApi(client);
});

final cloudInstancesProvider = FutureProvider.autoDispose<CloudInstanceList>((
  ref,
) async {
  final api = ref.watch(cloudApiProvider);
  if (api == null) return const CloudInstanceList();
  return api.listInstances();
});

final cloudTasksProvider = FutureProvider.autoDispose
    .family<List<CloudTask>, String>((ref, instanceId) async {
      final api = ref.watch(cloudApiProvider);
      if (api == null || instanceId.isEmpty) return const [];
      final grantId = await api.openGrant(instanceId);
      if (grantId.isEmpty) return const [];
      return api.listTasks(grantId);
    });

/// The task the chat should reopen after an app restart. The route passes the
/// records in memory, so without this a killed app has no way back into a
/// conversation — the reported "exit and cannot return" gap.
const _cloudLastTaskKey = 'mobilebot.cloud.lastTask';

Future<void> saveCloudLastTask(String instanceId, String taskId) async {
  final prefs = await SharedPreferences.getInstance();
  await prefs.setString(
    _cloudLastTaskKey,
    jsonEncode({'instance_id': instanceId, 'task_id': taskId}),
  );
}

Future<({String instanceId, String taskId})?> loadCloudLastTask() async {
  final prefs = await SharedPreferences.getInstance();
  final raw = prefs.getString(_cloudLastTaskKey);
  if (raw == null || raw.isEmpty) return null;
  try {
    final decoded = jsonDecode(raw);
    if (decoded is! Map) return null;
    final instanceId = (decoded['instance_id'] ?? '').toString();
    final taskId = (decoded['task_id'] ?? '').toString();
    if (instanceId.isEmpty || taskId.isEmpty) return null;
    return (instanceId: instanceId, taskId: taskId);
  } catch (_) {
    return null;
  }
}

Future<void> clearCloudLastTask() async {
  final prefs = await SharedPreferences.getInstance();
  await prefs.remove(_cloudLastTaskKey);
}

/// One auto-open per app run: the first visit to the cloud tab returns to the
/// last conversation. Tab switches afterwards keep showing the instance list
/// instead of yanking the user back into a chat.
var _cloudAutoOpenDone = false;

/// Test hook: the auto-open guard is module state, so a suite that exercises
/// the reopen path must clear it between cases.
@visibleForTesting
void resetCloudAutoOpenForTests() => _cloudAutoOpenDone = false;

/// Account-level cloud agents. Not a machine grant's task list.
class CloudScreen extends ConsumerStatefulWidget {
  const CloudScreen({super.key});

  @override
  ConsumerState<CloudScreen> createState() => _CloudScreenState();
}

class _CloudScreenState extends ConsumerState<CloudScreen> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _restoreLastTask());
  }

  Future<void> _restoreLastTask() async {
    if (_cloudAutoOpenDone || !mounted) return;
    final saved = await loadCloudLastTask();
    if (saved == null) return;
    final CloudInstanceList data;
    try {
      // Await, don't peek: the postFrame callback can beat the list fetch.
      data = await ref.read(cloudInstancesProvider.future);
    } catch (_) {
      return;
    }
    if (!data.enabled) return;
    CloudInstance? instance;
    for (final item in data.instances) {
      if (item.id == saved.instanceId) instance = item;
    }
    if (instance == null) {
      await clearCloudLastTask();
      return;
    }
    try {
      final tasks = await ref.read(cloudTasksProvider(instance.id).future);
      CloudTask? task;
      for (final item in tasks) {
        if (item.id == saved.taskId) task = item;
      }
      if (task == null) {
        await clearCloudLastTask();
        return;
      }
      _cloudAutoOpenDone = true;
      if (!mounted) return;
      await context.push('/cloud/task', extra: (instance: instance, task: task));
    } catch (_) {
      // The grant or task list failed; leave the saved entry for the next try.
    }
  }

  @override
  Widget build(BuildContext context) {
    final list = ref.watch(cloudInstancesProvider);
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(context.S.cloudTitle),
            Text(
              context.S.rcCloudAccountSlot,
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: context.S.refresh,
            onPressed: () => ref.invalidate(cloudInstancesProvider),
            icon: const Icon(Icons.refresh),
          ),
        ],
      ),
      floatingActionButton: list.maybeWhen(
        data: (data) => data.enabled
            ? FloatingActionButton.extended(
                onPressed: () => _createInstance(context, ref),
                icon: const Icon(Icons.add),
                label: Text(context.S.cloudNewInstance),
              )
            : null,
        orElse: () => null,
      ),
      body: list.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (error, _) => _CloudMessage(
          text: failureMessage(error),
          action: context.S.retry,
          onAction: () => ref.invalidate(cloudInstancesProvider),
        ),
        data: (data) {
          if (!data.enabled && data.instances.isEmpty) {
            return _CloudMessage(text: context.S.cloudDisabled);
          }
          if (data.instances.isEmpty) {
            return _CloudMessage(text: context.S.cloudNoInstances);
          }
          return RefreshIndicator(
            onRefresh: () async => ref.invalidate(cloudInstancesProvider),
            child: ListView.separated(
              padding: const EdgeInsets.all(AppSpacing.lg),
              itemCount: data.instances.length,
              separatorBuilder: (_, _) => const SizedBox(height: AppSpacing.sm),
              itemBuilder: (context, index) =>
                  _InstanceCard(instance: data.instances[index]),
            ),
          );
        },
      ),
    );
  }
}

class _InstanceCard extends ConsumerWidget {
  const _InstanceCard({required this.instance});

  final CloudInstance instance;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final tasks = ref.watch(cloudTasksProvider(instance.id));
    final status = instance.statusLabel(context.S);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.lg),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    instance.name.isEmpty
                        ? context.S.cloudUntitled
                        : instance.name,
                    style: theme.textTheme.titleSmall,
                  ),
                ),
                if (status.isNotEmpty)
                  Text(
                    status,
                    style: theme.textTheme.labelMedium?.copyWith(
                      color: theme.colorScheme.primary,
                    ),
                  ),
              ],
            ),
            const SizedBox(height: AppSpacing.sm),
            Row(
              children: [
                if (instance.canStop)
                  TextButton(
                    onPressed: () => _act(
                      context,
                      ref,
                      () => ref
                          .read(cloudApiProvider)!
                          .stopInstance(instance.id),
                    ),
                    child: Text(context.S.stop),
                  ),
                if (instance.canResume)
                  TextButton(
                    onPressed: () => _act(
                      context,
                      ref,
                      () => ref
                          .read(cloudApiProvider)!
                          .resumeInstance(instance.id),
                    ),
                    child: Text(context.S.resume),
                  ),
                TextButton(
                  onPressed: () => showCloudCreateTask(context, ref, instance),
                  child: Text(context.S.cloudNewTask),
                ),
              ],
            ),
            tasks.when(
              loading: () => const LinearProgressIndicator(),
              error: (error, _) => Text(
                _taskError(context, error),
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.error,
                ),
              ),
              data: (rows) {
                if (rows.isEmpty) {
                  return Text(
                    context.S.cloudNoTasks,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  );
                }
                return Column(
                  children: [
                    for (final task in rows)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        title: Text(
                          task.title.isEmpty
                              ? context.S.cloudUntitled
                              : task.title,
                        ),
                        subtitle: task.updatedAt.isEmpty
                            ? null
                            : Text(
                                task.updatedAt,
                                style: theme.textTheme.labelSmall?.copyWith(
                                  color: theme.colorScheme.onSurfaceVariant,
                                ),
                              ),
                        trailing: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            ...?_taskBadge(context, task),
                            const Icon(Icons.chevron_right),
                          ],
                        ),
                        onTap: () => openCloudTask(context, instance, task),
                      ),
                  ],
                );
              },
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _act(
    BuildContext context,
    WidgetRef ref,
    Future<CloudInstance> Function() call,
  ) async {
    try {
      await call();
      ref.invalidate(cloudInstancesProvider);
    } catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(failureMessage(error))),
      );
    }
  }

}

/// Opens one cloud-task chat and remembers it as the reopen target.
Future<void> openCloudTask(
  BuildContext context,
  CloudInstance instance,
  CloudTask task,
) async {
  await saveCloudLastTask(instance.id, task.id);
  if (!context.mounted) return;
  await context.push('/cloud/task', extra: (instance: instance, task: task));
}

/// Shared by the instance card and the chat drawer: ask for a title, create
/// the task, refresh the list and open its chat.
Future<void> showCloudCreateTask(
  BuildContext context,
  WidgetRef ref,
  CloudInstance instance,
) async {
  final title = await _ask(context, context.S.cloudNewTask);
  if (title == null || title.trim().isEmpty || !context.mounted) return;
  final api = ref.read(cloudApiProvider);
  if (api == null) return;
  try {
    final grantId = await api.openGrant(instance.id);
    final task = await api.createTask(grantId, title: title.trim());
    ref.invalidate(cloudTasksProvider(instance.id));
    if (!context.mounted || task.id.isEmpty) return;
    await openCloudTask(context, instance, task);
  } catch (error) {
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(failureMessage(error))),
    );
  }
}

List<Widget>? _taskBadge(BuildContext context, CloudTask task) {
  final badge = cloudTaskBadge(
    context.S,
    Theme.of(context).colorScheme,
    task,
  );
  if (badge == null) return null;
  return [
    Container(
      margin: const EdgeInsets.only(right: AppSpacing.xs),
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: badge.color.withValues(alpha: 0.14),
        borderRadius: BorderRadius.circular(AppRadius.sm),
      ),
      child: Text(
        badge.label,
        style: Theme.of(context).textTheme.labelSmall?.copyWith(
          color: badge.color,
          fontWeight: FontWeight.w600,
        ),
      ),
    ),
  ];
}

class CloudTaskScreen extends ConsumerStatefulWidget {
  const CloudTaskScreen({super.key, required this.instance, required this.task});

  final CloudInstance instance;
  final CloudTask task;

  @override
  ConsumerState<CloudTaskScreen> createState() => _CloudTaskScreenState();
}

class _CloudTaskScreenState extends ConsumerState<CloudTaskScreen> {
  final _input = TextEditingController();
  final _scaffoldKey = GlobalKey<ScaffoldState>();
  final _scroll = ScrollController();
  CloudTaskDetail? _detail;
  Object? _error;
  var _loading = true;
  var _sending = false;
  String? _grantId;
  Timer? _fastTimer;
  Timer? _slowTimer;
  var _pollInFlight = false;
  String? _titleOverride;

  /// Optimistic user echoes not yet seen in a fetched page. The runtime
  /// dedupes by client message id, so the next load folds them in.
  final _pending = <String>[];

  /// Set when the user deletes the open task from the drawer; the chat then
  /// pops instead of polling a gone task.
  var _deleted = false;

  /// Captured in didChangeDependencies: reading Localizations inside the
  /// initState-path load would depend on an inherited widget too early.
  String _needsMachine = '';

  static const _pollFast = Duration(seconds: 3);
  static const _pollSlow = Duration(seconds: 15);
  static const _defaultPage = 40;
  static const _maxPage = 400;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _needsMachine = context.S.cloudGrantNeedsMachine;
  }

  @override
  void dispose() {
    _fastTimer?.cancel();
    _slowTimer?.cancel();
    _scroll.dispose();
    _input.dispose();
    super.dispose();
  }

  Future<CloudApi?> _api() async => ref.read(cloudApiProvider);

  /// The grant is opened once per screen and reused by every poll; a fresh
  /// openGrant per tick would spam the hub.
  Future<String?> _grant(CloudApi api) async {
    final cached = _grantId;
    if (cached != null && cached.isNotEmpty) return cached;
    final grantId = await api.openGrant(widget.instance.id);
    if (grantId.isNotEmpty) _grantId = grantId;
    return grantId.isEmpty ? null : grantId;
  }

  /// Fast polling while a turn is running or a send is in flight (3s is the
  /// only moving picture, same as the remote screen without SSE); slow
  /// refresh otherwise keeps status and artifacts warm.
  void _schedulePoll() {
    _fastTimer?.cancel();
    _slowTimer?.cancel();
    _fastTimer = null;
    _slowTimer = null;
    final busy =
        _sending ||
        _pending.isNotEmpty ||
        (_detail?.turnStatus == cloudTurnRunning);
    final timer = Timer.periodic(busy ? _pollFast : _pollSlow, (_) {
      if (!_pollInFlight && mounted && !_deleted) _load();
    });
    if (busy) {
      _fastTimer = timer;
    } else {
      _slowTimer = timer;
    }
  }

  Future<void> _load() async {
    if (_pollInFlight || _deleted) return;
    _pollInFlight = true;
    final firstLoad = _detail == null;
    try {
      final api = await _api();
      if (api == null) return;
      final grantId = await _grant(api);
      if (grantId == null) {
        throw StateError(_needsMachine);
      }
      // 首载与轮询都显式窗口化（0 = 服务端全量兼容行为，长对话会撞 1MB
      // 中继上限）；已载更宽的窗口时按原样带上，避免轮询把它缩回去。
      final loaded = _detail?.messages.length ?? 0;
      final detail = await api.getTask(
        grantId,
        widget.task.id,
        historyLimit: loaded > _defaultPage ? loaded : _defaultPage,
      );
      if (!mounted) return;
      setState(() {
        _detail = _reconcile(detail);
        _loading = false;
        _error = null;
      });
      _jumpToBottomIfNeeded(firstLoad);
      _schedulePoll();
    } catch (error) {
      if (!mounted) return;
      // Transient poll failures keep the last snapshot on screen. Only a
      // first load paints the full-page error.
      if (_detail == null) {
        setState(() {
          _error = error;
          _loading = false;
        });
      } else {
        _schedulePoll();
      }
    } finally {
      _pollInFlight = false;
    }
  }

  /// Drops pending echoes the server window now covers; leftovers stay at
  /// the bottom of the transcript until then.
  CloudTaskDetail _reconcile(CloudTaskDetail detail) {
    if (_pending.isEmpty) return detail;
    final landed = {
      for (final row in detail.messages)
        if (row.role == 'user') row.content,
    };
    _pending.removeWhere(landed.contains);
    return detail;
  }

  void _jumpToBottomIfNeeded(bool force) {
    if (!_scroll.hasClients) return;
    final position = _scroll.position;
    final nearBottom =
        position.maxScrollExtent - position.pixels < 400 ||
        position.maxScrollExtent == 0;
    if (!force && !nearBottom) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scroll.hasClients) return;
      _scroll.jumpTo(_scroll.position.maxScrollExtent);
    });
  }

  List<CloudMessage> get _visibleMessages {
    final detail = _detail;
    if (detail == null) return const <CloudMessage>[];
    final rows = [...detail.messages];
    for (final text in _pending) {
      rows.add(
        CloudMessage(role: 'user', content: text, requestId: 'pending'),
      );
    }
    return rows;
  }

  bool get _hasEarlier {
    final detail = _detail;
    return detail != null && detail.messages.length < detail.messageTotal;
  }

  bool get _showTranscriptHeader {
    final detail = _detail;
    return detail != null &&
        detail.messages.isNotEmpty &&
        detail.messageTotal > 0;
  }

  Future<void> _loadEarlier() async {
    final current = _detail;
    if (current == null || _pollInFlight || _deleted) return;
    if (current.messages.length >= current.messageTotal) return;
    final api = await _api();
    if (api == null) return;
    _pollInFlight = true;
    try {
      final grantId = await _grant(api);
      if (grantId == null) return;
      var limit = current.messages.length * 2;
      if (limit < _defaultPage) limit = _defaultPage;
      if (limit > _maxPage) limit = _maxPage;
      final page = await api.getTask(
        grantId,
        widget.task.id,
        historyLimit: limit,
      );
      if (!mounted) return;
      final earlier = _earlierThan(current.messages, page.messages);
      setState(() {
        _detail = CloudTaskDetail(
          taskId: current.taskId,
          messages: [...earlier, ...current.messages],
          turnStatus: page.turnStatus,
          messageTotal: page.messageTotal,
        );
      });
      _schedulePoll();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(failureMessage(error))),
      );
    } finally {
      _pollInFlight = false;
    }
  }

  /// The page window shares this screen's tail; everything before it is the
  /// earlier rows. The key keeps a request split across a page boundary from
  /// rendering twice.
  List<CloudMessage> _earlierThan(
    List<CloudMessage> current,
    List<CloudMessage> page,
  ) {
    final overlap = page.length - current.length;
    if (overlap <= 0) return const <CloudMessage>[];
    final seen = {for (final row in current) row.key};
    return page
        .take(overlap)
        .where((row) => !seen.contains(row.key))
        .toList();
  }

  Future<void> _send() async {
    final text = _input.text.trim();
    if (text.isEmpty || _sending) return;
    final api = await _api();
    if (api == null) return;
    setState(() {
      _sending = true;
      _pending.add(text);
    });
    _input.clear();
    _schedulePoll();
    _jumpToBottomIfNeeded(true);
    try {
      final grantId = await _grant(api);
      if (grantId == null) throw StateError(_needsMachine);
      await api.sendMessage(
        grantId,
        widget.task.id,
        text,
        clientMessageId: 'm${DateTime.now().microsecondsSinceEpoch}',
      );
      await _load();
    } catch (error) {
      if (!mounted) return;
      setState(() => _pending.remove(text));
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(failureMessage(error))),
      );
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  Future<void> _stopTask() async {
    final api = await _api();
    if (api == null) return;
    try {
      final grantId = await _grant(api);
      if (grantId == null) return;
      await api.stopTask(grantId, widget.task.id);
      await _load();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(failureMessage(error))),
      );
    }
  }

  Future<void> _onTaskDeleted(CloudTask task) async {
    await clearCloudLastTask();
    if (task.id != widget.task.id) return;
    _deleted = true;
    _fastTimer?.cancel();
    _slowTimer?.cancel();
    if (!mounted) return;
    context.pop();
  }

  void _onTaskRenamed(CloudTask task, String title) {
    if (task.id == widget.task.id) {
      setState(() => _titleOverride = title);
    }
    ref.invalidate(cloudTasksProvider(widget.instance.id));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final title = _titleOverride ??
        (widget.task.title.isEmpty ? context.S.cloudUntitled : widget.task.title);
    final messages = _visibleMessages;
    final detail = _detail;
    return Scaffold(
      key: _scaffoldKey,
      endDrawer: Drawer(
        child: _CloudTaskDrawer(
          instance: widget.instance,
          currentTaskId: widget.task.id,
          onTaskDeleted: _onTaskDeleted,
          onTaskRenamed: _onTaskRenamed,
        ),
      ),
      appBar: AppBar(
        leading: IconButton(
          tooltip: context.S.cloudSessionList,
          onPressed: () => _scaffoldKey.currentState?.openEndDrawer(),
          icon: const Icon(Icons.segment),
        ),
        title: Text(title, overflow: TextOverflow.ellipsis),
        actions: [
          IconButton(
            tooltip: context.S.stop,
            onPressed: _deleted ? null : _stopTask,
            icon: const Icon(Icons.stop_circle_outlined),
          ),
        ],
      ),
      body: Column(
        children: [
          if (widget.instance.status.isNotEmpty ||
              (detail?.turnStatus.isNotEmpty ?? false))
            Padding(
              padding: const EdgeInsets.fromLTRB(
                AppSpacing.lg,
                AppSpacing.sm,
                AppSpacing.lg,
                0,
              ),
              child: Align(
                alignment: Alignment.centerLeft,
                child: Text(
                  [
                    if (widget.instance.status.isNotEmpty)
                      widget.instance.statusLabel(context.S),
                    if (detail != null && detail.turnStatus.isNotEmpty)
                      cloudTurnLabel(context.S, detail.turnStatus),
                  ].join(' · '),
                  style: theme.textTheme.labelMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
            ),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : _error != null
                ? _CloudMessage(
                    text: failureMessage(_error!),
                    action: context.S.retry,
                    onAction: _load,
                  )
                : messages.isEmpty
                ? _CloudMessage(text: context.S.cloudNoHistory)
                : ListView.builder(
                    controller: _scroll,
                    padding: const EdgeInsets.all(AppSpacing.lg),
                    // 计数行在有已载窗口时恒显示（含全量 4/4），按钮仅在有
                    // 更早页时出现——远控详情页同一套信息结构。
                    itemCount:
                        messages.length + (_showTranscriptHeader ? 1 : 0),
                    itemBuilder: (context, index) {
                      if (_showTranscriptHeader && index == 0) {
                        return _CloudLoadEarlier(
                          shown: detail!.messages.length,
                          total: detail.messageTotal,
                          hasEarlier: _hasEarlier,
                          onPressed: _loadEarlier,
                        );
                      }
                      final row = messages[
                          _showTranscriptHeader ? index - 1 : index];
                      return _CloudTranscriptBubble(
                        row: row,
                        theme: theme,
                      );
                    },
                  ),
          ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.all(AppSpacing.md),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _input,
                      minLines: 1,
                      maxLines: 4,
                      decoration: InputDecoration(
                        hintText: context.S.cloudComposerHint,
                      ),
                      onSubmitted: (_) => _send(),
                    ),
                  ),
                  IconButton(
                    tooltip: context.S.cloudSend,
                    onPressed: _sending ? null : _send,
                    icon: _sending
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.send),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Hub's cloud grant still requires a logged-in computer. A phone with no
/// machine id hits that gap; show it instead of a raw server sentence.
String _taskError(BuildContext context, Object error) {
  final raw = failureMessage(error);
  if (raw.contains('登录这台电脑') || raw.contains('INVALID')) {
    return context.S.cloudGrantNeedsMachine;
  }
  return raw.isEmpty ? context.S.cloudGrantNeedsMachine : raw;
}

class _CloudMessage extends StatelessWidget {
  const _CloudMessage({required this.text, this.action, this.onAction});

  final String text;
  final String? action;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.lg),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(text, textAlign: TextAlign.center),
            if (action != null) ...[
              const SizedBox(height: AppSpacing.md),
              FilledButton.tonal(onPressed: onAction, child: Text(action!)),
            ],
          ],
        ),
      ),
    );
  }
}

/// One cloud-task transcript row. Assistant replies use the shared markdown
/// body (links, code, mermaid). Persisted process rows (thinking, progress)
/// render inside the same bubble; a user row can carry them too when its
/// turn never produced a reply. Plain Text keeps user wording and error rows
/// from being reinterpreted as markup.
class _CloudTranscriptBubble extends StatelessWidget {
  const _CloudTranscriptBubble({required this.row, required this.theme});

  final CloudMessage row;
  final ThemeData theme;

  @override
  Widget build(BuildContext context) {
    final mine = row.role == 'user';
    final plain = mine || row.asError;
    final text = row.content.isEmpty ? row.role : row.content;
    final thinking = mine ? '' : row.thinkingText;
    final progress = row.progressRows.toList();
    return Align(
      alignment: mine ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        margin: const EdgeInsets.only(bottom: AppSpacing.sm),
        padding: const EdgeInsets.all(AppSpacing.md),
        constraints: const BoxConstraints(maxWidth: 320),
        decoration: BoxDecoration(
          color: row.asError
              ? theme.colorScheme.errorContainer
              : mine
              ? theme.colorScheme.primaryContainer
              : theme.colorScheme.surfaceContainerHighest,
          borderRadius: BorderRadius.circular(AppRadius.md),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (thinking.isNotEmpty) _CloudThinkingBlock(
              text: thinking,
              theme: theme,
            ),
            for (final event in progress)
              _CloudProgressLine(text: event.text, theme: theme),
            if (text.isNotEmpty)
              plain
                  ? Text(text)
                  : _CloudMarkdownText(text: text, theme: theme),
          ],
        ),
      ),
    );
  }
}

/// Collapsible reasoning block restored from the sealed thinking event.
/// Collapsed by default — the answer is the primary content.
class _CloudThinkingBlock extends StatefulWidget {
  const _CloudThinkingBlock({required this.text, required this.theme});

  final String text;
  final ThemeData theme;

  @override
  State<_CloudThinkingBlock> createState() => _CloudThinkingBlockState();
}

class _CloudThinkingBlockState extends State<_CloudThinkingBlock> {
  var _open = false;

  @override
  Widget build(BuildContext context) {
    final theme = widget.theme;
    return InkWell(
      onTap: () => setState(() => _open = !_open),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                Icons.psychology_outlined,
                size: 14,
                color: theme.colorScheme.onSurfaceVariant,
              ),
              const SizedBox(width: AppSpacing.xs),
              Text(
                context.S.cloudThinking,
                style: theme.textTheme.labelMedium?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
              Icon(
                _open ? Icons.expand_less : Icons.expand_more,
                size: 16,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ],
          ),
          if (_open) ...[
            const SizedBox(height: AppSpacing.xs),
            Text(
              widget.text,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
                height: 1.4,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// One persisted progress line of a round. Muted and indented so the answer
/// stays the visual focus.
class _CloudProgressLine extends StatelessWidget {
  const _CloudProgressLine({required this.text, required this.theme});

  final String text;
  final ThemeData theme;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 2),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: 2),
            child: Icon(
              Icons.subdirectory_arrow_right,
              size: 14,
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(width: AppSpacing.xs),
          Expanded(
            child: Text(
              text,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// The 查看更早 header above a paged transcript. The shown-count line stays
/// once the window covers everything; the button only offers an earlier page.
class _CloudLoadEarlier extends StatelessWidget {
  const _CloudLoadEarlier({
    required this.shown,
    required this.total,
    required this.hasEarlier,
    required this.onPressed,
  });

  final int shown;
  final int total;
  final bool hasEarlier;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: Column(
        children: [
          Text(
            context.S.cloudShownCount(shown, total),
            style: Theme.of(context).textTheme.labelSmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          if (hasEarlier)
            TextButton.icon(
              key: const ValueKey('cloud-load-earlier'),
              onPressed: onPressed,
              icon: const Icon(Icons.expand_less, size: 18),
              label: Text(context.S.cloudLoadEarlier),
            ),
        ],
      ),
    );
  }
}

/// The right-hand drawer of a cloud-task chat: this instance's task list with
/// status chips, swipe-to-delete, long-press rename, and the create button.
/// Closing it with a rightward swipe is the Drawer's native gesture.
class _CloudTaskDrawer extends ConsumerWidget {
  const _CloudTaskDrawer({
    required this.instance,
    required this.currentTaskId,
    required this.onTaskDeleted,
    required this.onTaskRenamed,
  });

  final CloudInstance instance;
  final String currentTaskId;
  final Future<void> Function(CloudTask task) onTaskDeleted;
  final void Function(CloudTask task, String title) onTaskRenamed;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final tasks = ref.watch(cloudTasksProvider(instance.id));
    return SafeArea(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(
              AppSpacing.lg,
              AppSpacing.lg,
              AppSpacing.md,
              AppSpacing.sm,
            ),
            child: Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        instance.name.isEmpty
                            ? context.S.cloudUntitled
                            : instance.name,
                        style: theme.textTheme.titleSmall,
                      ),
                      if (instance.status.isNotEmpty)
                        Text(
                          instance.statusLabel(context.S),
                          style: theme.textTheme.labelSmall?.copyWith(
                            color: theme.colorScheme.primary,
                          ),
                        ),
                    ],
                  ),
                ),
                IconButton(
                  tooltip: context.S.cloudManageInstances,
                  onPressed: () => context.go('/cloud'),
                  icon: const Icon(Icons.settings_outlined),
                ),
              ],
            ),
          ),
          const Divider(height: 1),
          Expanded(
            child: tasks.when(
              loading: () => const Center(child: CircularProgressIndicator()),
              error: (error, _) => _CloudMessage(
                text: failureMessage(error),
                action: context.S.retry,
                onAction: () =>
                    ref.invalidate(cloudTasksProvider(instance.id)),
              ),
              data: (rows) {
                if (rows.isEmpty) {
                  return Padding(
                    padding: const EdgeInsets.all(AppSpacing.lg),
                    child: Text(
                      context.S.cloudNoTasks,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  );
                }
                return ListView(
                  children: [
                    for (final task in rows)
                      _CloudDrawerTile(
                        task: task,
                        current: task.id == currentTaskId,
                        onDelete: () => _deleteTask(context, ref, task),
                        onRename: () => _renameTask(context, ref, task),
                        onOpen: () {
                          Navigator.pop(context);
                          openCloudTask(context, instance, task);
                        },
                      ),
                  ],
                );
              },
            ),
          ),
          Padding(
            padding: const EdgeInsets.all(AppSpacing.md),
            child: SizedBox(
              width: double.infinity,
              child: FilledButton.tonalIcon(
                onPressed: () {
                  Navigator.pop(context);
                  showCloudCreateTask(context, ref, instance);
                },
                icon: const Icon(Icons.add),
                label: Text(context.S.cloudNewTask),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Future<bool> _confirmDelete(BuildContext context, CloudTask task) async {
    final title = task.title.isEmpty ? context.S.cloudUntitled : task.title;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(context.S.cloudDeleteTask),
        content: Text(context.S.cloudDeleteTaskConfirm(title)),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: Text(context.S.cancel),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            onPressed: () => Navigator.pop(context, true),
            child: Text(context.S.cloudDelete),
          ),
        ],
      ),
    );
    return confirmed == true;
  }

  Future<bool> _deleteTask(
    BuildContext context,
    WidgetRef ref,
    CloudTask task,
  ) async {
    if (!await _confirmDelete(context, task)) return false;
    final api = ref.read(cloudApiProvider);
    if (api == null) return false;
    try {
      final grantId = await api.openGrant(instance.id);
      await api.deleteTask(grantId, task.id);
      ref.invalidate(cloudTasksProvider(instance.id));
      await onTaskDeleted(task);
      if (!context.mounted) return true;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(context.S.cloudTaskDeleted)),
      );
      return true;
    } catch (error) {
      if (!context.mounted) return false;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(failureMessage(error))),
      );
      return false;
    }
  }

  Future<void> _renameTask(
    BuildContext context,
    WidgetRef ref,
    CloudTask task,
  ) async {
    final controller = TextEditingController(text: task.title);
    final title = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(context.S.cloudRenameTask),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: InputDecoration(hintText: context.S.cloudNameHint),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: Text(context.S.cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, controller.text),
            child: Text(context.S.cloudRename),
          ),
        ],
      ),
    );
    if (title == null || title.trim().isEmpty || title.trim() == task.title) {
      return;
    }
    final api = ref.read(cloudApiProvider);
    if (api == null) return;
    try {
      final grantId = await api.openGrant(instance.id);
      await api.renameTask(grantId, task.id, title);
      ref.invalidate(cloudTasksProvider(instance.id));
      onTaskRenamed(task, title.trim());
    } catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(failureMessage(error))),
      );
    }
  }
}

/// One drawer row. Swipe (endToStart) deletes with a confirm dialog —
/// [onDelete] runs the dialog plus the delete and reports whether the row is
/// really gone, so a failed call does not fling the tile away. Long-press
/// renames. The current task is marked.
class _CloudDrawerTile extends StatelessWidget {
  const _CloudDrawerTile({
    required this.task,
    required this.current,
    required this.onDelete,
    required this.onRename,
    required this.onOpen,
  });

  final CloudTask task;
  final bool current;
  final Future<bool> Function() onDelete;
  final Future<void> Function() onRename;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Dismissible(
      key: ValueKey('cloud-task-${task.id}'),
      direction: DismissDirection.endToStart,
      confirmDismiss: (_) => onDelete(),
      // The row leaves the tree through the invalidated provider, not here.
      onDismissed: (_) {},
      background: Container(
        color: theme.colorScheme.errorContainer,
        alignment: Alignment.centerRight,
        padding: const EdgeInsets.only(right: AppSpacing.lg),
        child: Icon(Icons.delete_outline, color: theme.colorScheme.error),
      ),
      child: ListTile(
        selected: current,
        title: Text(
          task.title.isEmpty ? context.S.cloudUntitled : task.title,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        subtitle: task.updatedAt.isEmpty
            ? null
            : Text(
                task.updatedAt,
                style: theme.textTheme.labelSmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            ...?_taskBadge(context, task),
            const Icon(Icons.chevron_right),
          ],
        ),
        onTap: onOpen,
        onLongPress: onRename,
      ),
    );
  }
}

/// Assistant markdown body. Kept out of the list itemBuilder so linkify,
/// selection and the shared style sheet stay in one widget.
class _CloudMarkdownText extends StatelessWidget {
  const _CloudMarkdownText({required this.text, required this.theme});

  final String text;
  final ThemeData theme;

  @override
  Widget build(BuildContext context) {
    return MermaidMarkdown(
      data: linkifyMarkdownUrls(text),
      selectable: true,
      onTapLink: (label, href, title) {
        if (href != null) openExternalUrl(href);
      },
      styleSheet: _style(theme),
    );
  }

  static MarkdownStyleSheet _style(ThemeData theme) {
    // The assistant bubble is already surfaceContainerHighest. A same-color
    // code chip disappears in dark mode, so code lifts one surface step.
    final codeSurface = theme.colorScheme.surfaceContainerHigh;
    return MarkdownStyleSheet.fromTheme(theme).copyWith(
      p: theme.textTheme.bodyMedium?.copyWith(height: 1.45),
      h1: theme.textTheme.titleSmall,
      h2: theme.textTheme.titleSmall,
      h3: theme.textTheme.titleSmall,
      a: theme.textTheme.bodyMedium?.copyWith(
        color: theme.colorScheme.primary,
        fontWeight: FontWeight.w600,
        decoration: TextDecoration.underline,
      ),
      code: theme.textTheme.bodySmall?.copyWith(
        fontFamily: 'monospace',
        backgroundColor: codeSurface,
      ),
      codeblockDecoration: BoxDecoration(
        color: codeSurface,
        borderRadius: BorderRadius.circular(AppRadius.sm),
      ),
      blockSpacing: 6,
    );
  }
}

Future<String?> _ask(BuildContext context, String title) {
  final controller = TextEditingController();
  return showDialog<String>(
    context: context,
    builder: (context) => AlertDialog(
      title: Text(title),
      content: TextField(
        controller: controller,
        autofocus: true,
        decoration: InputDecoration(hintText: context.S.cloudNameHint),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: Text(context.S.cancel),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(context, controller.text),
          child: Text(context.S.cloudCreate),
        ),
      ],
    ),
  );
}

Future<void> _createInstance(BuildContext context, WidgetRef ref) async {
  final name = await _ask(context, context.S.cloudNewInstance);
  if (name == null || name.trim().isEmpty || !context.mounted) return;
  final api = ref.read(cloudApiProvider);
  if (api == null) return;
  try {
    await api.createInstance(name: name.trim());
    ref.invalidate(cloudInstancesProvider);
  } catch (error) {
    if (!context.mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(failureMessage(error))),
    );
  }
}
