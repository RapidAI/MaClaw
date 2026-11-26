import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/locale.dart';
import '../../core/providers.dart';
import '../../core/theme.dart';
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

/// Account-level cloud agents. Not a machine grant's task list.
class CloudScreen extends ConsumerWidget {
  const CloudScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
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
                  onPressed: () => _createTask(context, ref),
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
                        trailing: const Icon(Icons.chevron_right),
                        onTap: () => context.push(
                          '/cloud/task',
                          extra: (instance: instance, task: task),
                        ),
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

  Future<void> _createTask(BuildContext context, WidgetRef ref) async {
    final title = await _ask(context, context.S.cloudNewTask);
    if (title == null || title.trim().isEmpty || !context.mounted) return;
    final api = ref.read(cloudApiProvider);
    if (api == null) return;
    try {
      final grantId = await api.openGrant(instance.id);
      final task = await api.createTask(grantId, title: title.trim());
      ref.invalidate(cloudTasksProvider(instance.id));
      if (!context.mounted || task.id.isEmpty) return;
      context.push('/cloud/task', extra: (instance: instance, task: task));
    } catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(failureMessage(error))),
      );
    }
  }
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
  CloudTaskDetail? _detail;
  Object? _error;
  var _loading = true;
  var _sending = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _input.dispose();
    super.dispose();
  }

  Future<CloudApi?> _api() async => ref.read(cloudApiProvider);

  Future<void> _load() async {
    final api = await _api();
    if (api == null) return;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final grantId = await api.openGrant(widget.instance.id);
      final detail = await api.getTask(grantId, widget.task.id);
      if (!mounted) return;
      setState(() {
        _detail = detail;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _error = error;
        _loading = false;
      });
    }
  }

  Future<void> _send() async {
    final text = _input.text.trim();
    if (text.isEmpty || _sending) return;
    final api = await _api();
    if (api == null) return;
    setState(() => _sending = true);
    try {
      final grantId = await api.openGrant(widget.instance.id);
      await api.sendMessage(grantId, widget.task.id, text);
      _input.clear();
      await _load();
    } catch (error) {
      if (!mounted) return;
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
      final grantId = await api.openGrant(widget.instance.id);
      await api.stopTask(grantId, widget.task.id);
      await _load();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(failureMessage(error))),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final title = widget.task.title.isEmpty
        ? context.S.cloudUntitled
        : widget.task.title;
    final messages = _detail?.messages ?? const <CloudMessage>[];
    return Scaffold(
      appBar: AppBar(
        title: Text(title),
        actions: [
          IconButton(
            tooltip: context.S.stop,
            onPressed: _stopTask,
            icon: const Icon(Icons.stop_circle_outlined),
          ),
        ],
      ),
      body: Column(
        children: [
          if (widget.instance.status.isNotEmpty ||
              (_detail?.turnStatus.isNotEmpty ?? false))
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
                    if (_detail != null && _detail!.turnStatus.isNotEmpty)
                      cloudTurnLabel(context.S, _detail!.turnStatus),
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
                    padding: const EdgeInsets.all(AppSpacing.lg),
                    itemCount: messages.length,
                    itemBuilder: (context, index) {
                      final row = messages[index];
                      final mine = row.role == 'user';
                      return Align(
                        alignment: mine
                            ? Alignment.centerRight
                            : Alignment.centerLeft,
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
                          child: Text(
                            row.content.isEmpty ? row.role : row.content,
                          ),
                        ),
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
