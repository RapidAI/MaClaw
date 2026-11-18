import 'package:flutter/material.dart';
import 'package:flutter_animate/flutter_animate.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/providers.dart';
import '../../core/locale.dart';
import '../../core/ui_kit.dart';
import '../bots/bots_controller.dart' show failureMessage;
import 'remote_api.dart';
import 'remote_models.dart';

final remoteApiProvider = Provider<RemoteControlApi?>((ref) {
  final client = ref.watch(apiClientProvider);
  return client == null ? null : RemoteControlApi(client);
});

final remoteStateProvider = FutureProvider<RemoteControlState>((ref) async {
  final api = ref.watch(remoteApiProvider);
  if (api == null) return const RemoteControlState();
  return api.state();
});

/// The grant currently selected in the machine dropdown. Null means "pick the
/// first usable one" — resolved inside build so a refreshed state can swap in
/// a removed grant's neighbour.
final selectedGrantProvider = StateProvider<RcGrant?>((ref) => null);

final remoteTasksProvider = FutureProvider.autoDispose
    .family<List<RemoteTask>, String>((ref, grantId) async {
      final api = ref.watch(remoteApiProvider);
      if (api == null) return const [];
      return api.listTasks(grantId);
    });

/// 远程控制 — 查看/操作自己名下或受邀机器的桌面任务。
/// 下拉选择远程机，下面列出它的任务；可进详情发消息/停任务/取产出物。
class RemoteScreen extends ConsumerStatefulWidget {
  const RemoteScreen({super.key});

  @override
  ConsumerState<RemoteScreen> createState() => _RemoteScreenState();
}

class _RemoteScreenState extends ConsumerState<RemoteScreen> {
  late final AppLifecycleListener _lifecycle;

  @override
  void initState() {
    super.initState();
    // Grants, invites and who's controlling my machines all change while the
    // app sits in the background — refresh the snapshot on resume instead of
    // waiting for a manual pull.
    _lifecycle = AppLifecycleListener(
      onStateChange: (state) {
        if (state == AppLifecycleState.resumed) {
          ref.invalidate(remoteStateProvider);
        }
      },
    );
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(remoteStateProvider);
    return Scaffold(
      appBar: AppBar(
        title: Text(context.S.rcTitle),
        actions: [
          IconButton(
            tooltip: context.S.rcManageDevices,
            icon: const Icon(Icons.devices_outlined),
            onPressed: () => showModalBottomSheet<void>(
              context: context,
              isScrollControlled: true,
              showDragHandle: true,
              builder: (_) => const _ManageSheet(),
            ),
          ),
          IconButton(
            tooltip: context.S.rcInviteOthers,
            icon: const Icon(Icons.person_add_alt_1),
            onPressed: () => showModalBottomSheet<void>(
              context: context,
              isScrollControlled: true,
              showDragHandle: true,
              builder: (_) => const _InviteSheet(),
            ),
          ),
          IconButton(
            tooltip: context.S.refresh,
            icon: const Icon(Icons.refresh),
            onPressed: () => ref.invalidate(remoteStateProvider),
          ),
        ],
      ),
      body: state.when(
        loading: () => const SkeletonList(itemCount: 5),
        error: (error, _) => RefreshIndicator(
          onRefresh: () async => ref.invalidate(remoteStateProvider),
          child: ListView(
            physics: const AlwaysScrollableScrollPhysics(),
            children: [
              SizedBox(
                height: MediaQuery.of(context).size.height * 0.6,
                child: _RemoteError(
                  message: failureMessage(error),
                  onRetry: () => ref.invalidate(remoteStateProvider),
                ),
              ),
            ],
          ),
        ),
        data: (snapshot) => _RemoteBody(snapshot: snapshot),
      ),
    );
  }
}

class _RemoteError extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _RemoteError({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return EmptyState(
      icon: Icons.cloud_off,
      title: message,
      action: FilledButton.tonal(
        onPressed: onRetry,
        child: Text(context.S.retry),
      ),
    );
  }
}

class _RemoteBody extends ConsumerWidget {
  final RemoteControlState snapshot;

  const _RemoteBody({required this.snapshot});

  RcGrant? _selected(WidgetRef ref) {
    final usable = snapshot.usableControlled;
    final wanted = ref.watch(selectedGrantProvider);
    for (final g in usable) {
      if (g.id == wanted?.id) return g;
    }
    return usable.isEmpty ? null : usable.first;
  }

  Future<void> _refreshAll(WidgetRef ref, RcGrant? selected) async {
    ref.invalidate(remoteStateProvider);
    if (selected != null) {
      ref.invalidate(remoteTasksProvider(selected.id));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final selected = _selected(ref);
    final usable = snapshot.usableControlled;
    return RefreshIndicator(
      onRefresh: () => _refreshAll(ref, selected),
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (snapshot.invites.isNotEmpty) ...[
            SectionHeader(title: context.S.rcSectionInvites),
            for (final grant in snapshot.invites)
              _IncomingInviteCard(grant: grant, machines: snapshot.machines),
            const SizedBox(height: 16),
          ],
          SectionHeader(title: context.S.rcSectionMachines),
          if (usable.isEmpty)
            Card(
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Text(
                  context.S.rcNoMachines,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
            )
          else
            _MachineDropdown(
              grants: usable,
              selected: selected,
              onChanged: (grant) =>
                  ref.read(selectedGrantProvider.notifier).state = grant,
            ),
          const SizedBox(height: 12),
          if (selected != null) _TaskSection(grant: selected),
        ],
      ),
    );
  }
}

/// 设备与授权管理 — 从主页面收进底栏弹层，保持主页干净。
/// 这里集中放：正在远控我的、等待对方处理、我可以控制（撤权）、我的设备（配对）。
class _ManageSheet extends ConsumerWidget {
  const _ManageSheet();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final snapshot = ref.watch(remoteStateProvider).valueOrNull;
    return DraggableScrollableSheet(
      expand: false,
      initialChildSize: 0.75,
      maxChildSize: 0.95,
      builder: (context, scrollCtrl) => ListView(
        controller: scrollCtrl,
        padding: const EdgeInsets.fromLTRB(20, 0, 20, 24),
        children: [
          Text(context.S.rcManageDevices, style: theme.textTheme.titleMedium),
          const SizedBox(height: 16),
          if (snapshot == null)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 40),
              child: Center(child: CircularProgressIndicator()),
            )
          else ...[
            if (snapshot.controlled.isNotEmpty) ...[
              Text(
                context.S.rcControlledByOthers,
                style: theme.textTheme.titleSmall,
              ),
              const SizedBox(height: 8),
              for (final grant in snapshot.controlled)
                _ControlledRow(grant: grant),
              const SizedBox(height: 16),
            ],
            if (snapshot.pending.isNotEmpty) ...[
              Text(
                context.S.rcPendingOutgoing,
                style: theme.textTheme.titleSmall,
              ),
              const SizedBox(height: 8),
              for (final grant in snapshot.pending)
                _PendingOutgoingRow(grant: grant),
              const SizedBox(height: 16),
            ],
            if (snapshot.usableControlled.isNotEmpty) ...[
              Text(context.S.rcICanControl, style: theme.textTheme.titleSmall),
              const SizedBox(height: 8),
              for (final grant in snapshot.usableControlled)
                _GrantRow(grant: grant, revocable: true),
              const SizedBox(height: 16),
            ],
            if (snapshot.machines.isNotEmpty) ...[
              Text(context.S.rcMyDevices, style: theme.textTheme.titleSmall),
              const SizedBox(height: 8),
              for (final machine in snapshot.machines)
                _MachineRow(machine: machine),
            ],
            if (snapshot.controlled.isEmpty &&
                snapshot.pending.isEmpty &&
                snapshot.usableControlled.isEmpty &&
                snapshot.machines.isEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 32),
                child: Center(
                  child: Text(
                    context.S.rcNoRecords,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
              ),
          ],
        ],
      ),
    );
  }
}

class _MachineDropdown extends StatelessWidget {
  final List<RcGrant> grants;
  final RcGrant? selected;
  final ValueChanged<RcGrant?> onChanged;

  const _MachineDropdown({
    required this.grants,
    required this.selected,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return DropdownButtonFormField<RcGrant>(
      initialValue: selected,
      decoration: const InputDecoration(
        border: OutlineInputBorder(),
        isDense: true,
        prefixIcon: Icon(Icons.computer),
      ),
      items: [
        for (final grant in grants)
          DropdownMenuItem(
            value: grant,
            // Menu items lay out under unbounded width — Expanded would
            // throw. Constrain the label instead of flexing it.
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                StatusDot(
                  size: 8,
                  color: grant.online
                      ? theme.colorScheme.primary
                      : theme.colorScheme.outline,
                ),
                const SizedBox(width: 8),
                ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 180),
                  child: Text(
                    grant.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                const SizedBox(width: 8),
                Text(
                  grant.permissionLabel(context.S),
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
      ],
      onChanged: onChanged,
    );
  }
}

/// Runs one grant action and resyncs the snapshot; shared by every
/// accept/reject/ignore/revoke button below.
Future<void> _runGrantAction(
  BuildContext context,
  WidgetRef ref,
  Future<void> Function(RemoteControlApi api) call,
) async {
  final api = ref.read(remoteApiProvider);
  if (api == null) return;
  try {
    await call(api);
    ref.invalidate(remoteStateProvider);
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
    }
  }
}

/// An incoming invite (`invites` bucket): another account asks to control one
/// of MY machines. Accepting binds a desktop — on the phone that means picking
/// which registered machine answers.
class _IncomingInviteCard extends ConsumerWidget {
  final RcGrant grant;
  final List<RcMachine> machines;

  const _IncomingInviteCard({required this.grant, required this.machines});

  Future<void> _accept(BuildContext context, WidgetRef ref) async {
    if (machines.isEmpty) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(context.S.rcNoBindableMachine)));
      return;
    }
    final machine = await showDialog<RcMachine>(
      context: context,
      builder: (context) => SimpleDialog(
        title: Text(context.S.rcPickMachineTitle),
        children: [
          for (final m in machines)
            SimpleDialogOption(
              onPressed: () => Navigator.of(context).pop(m),
              child: Row(
                children: [
                  Icon(
                    Icons.computer,
                    size: 18,
                    color: m.online ? null : Theme.of(context).disabledColor,
                  ),
                  const SizedBox(width: 8),
                  Expanded(child: Text(m.title)),
                  if (!m.online)
                    Text(
                      context.S.rcOffline,
                      style: Theme.of(context).textTheme.labelSmall,
                    ),
                ],
              ),
            ),
        ],
      ),
    );
    if (machine == null || !context.mounted) return;
    await _runGrantAction(
      context,
      ref,
      (api) => api.acceptGrant(grant.id, bindMachineId: machine.id),
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final who = grant.fromMachine.isNotEmpty
        ? grant.fromMachine
        : (grant.account.isNotEmpty ? grant.account : context.S.rcCounterparty);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              context.S.rcWantsToControl(who, grant.permissionLabel(context.S)),
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                FilledButton.tonal(
                  onPressed: () => _accept(context, ref),
                  child: Text(context.S.rcAccept),
                ),
                const SizedBox(width: 8),
                TextButton(
                  onPressed: () => _runGrantAction(
                    context,
                    ref,
                    (api) => api.rejectGrant(grant.id),
                  ),
                  child: Text(context.S.rcReject),
                ),
                TextButton(
                  onPressed: () => _runGrantAction(
                    context,
                    ref,
                    (api) => api.ignoreGrant(grant.id),
                  ),
                  child: Text(context.S.rcIgnore),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

/// An outgoing invite (`pending` bucket) — I asked to control someone else's
/// machine and am waiting for them to accept.
class _PendingOutgoingRow extends ConsumerWidget {
  final RcGrant grant;

  const _PendingOutgoingRow({required this.grant});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return Card(
      child: ListTile(
        dense: true,
        leading: Icon(
          Icons.hourglass_top,
          size: 18,
          color: theme.colorScheme.onSurfaceVariant,
        ),
        title: Text(
          grant.account.isNotEmpty ? grant.account : grant.title,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        subtitle: Text(
          context.S.rcPendingPermission(grant.permissionLabel(context.S)),
        ),
        trailing: TextButton(
          child: Text(context.S.cancel),
          onPressed: () =>
              _runGrantAction(context, ref, (api) => api.revokeGrant(grant.id)),
        ),
      ),
    );
  }
}

/// One of my machines currently being controlled (`controlled` bucket).
class _ControlledRow extends ConsumerWidget {
  final RcGrant grant;

  const _ControlledRow({required this.grant});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final who = grant.fromMachine.isNotEmpty
        ? grant.fromMachine
        : (grant.account.isNotEmpty ? grant.account : context.S.rcCounterparty);
    return Card(
      child: ListTile(
        dense: true,
        leading: Icon(Icons.circle, size: 10, color: theme.colorScheme.primary),
        title: Text(who, maxLines: 1, overflow: TextOverflow.ellipsis),
        subtitle: Text(
          '${context.S.rcControlling(grant.machineName.isNotEmpty ? grant.machineName : context.S.rcMyMachine)} · ${grant.permissionLabel(context.S)}',
        ),
        trailing: TextButton(
          child: Text(context.S.rcEnd),
          onPressed: () =>
              _runGrantAction(context, ref, (api) => api.revokeGrant(grant.id)),
        ),
      ),
    );
  }
}

class _GrantRow extends ConsumerWidget {
  final RcGrant grant;
  final bool revocable;

  const _GrantRow({required this.grant, this.revocable = false});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return Card(
      child: ListTile(
        dense: true,
        leading: Icon(
          Icons.circle,
          size: 10,
          color: grant.online
              ? theme.colorScheme.primary
              : theme.colorScheme.outline,
        ),
        title: Text(grant.title),
        subtitle: Text(
          [
            grant.permissionLabel(context.S),
            if (grant.status != 'active') grant.status,
            if (grant.sameAccount) context.S.rcSameAccount,
          ].join(' · '),
        ),
        trailing: revocable
            ? TextButton(
                child: Text(context.S.rcRevoke),
                onPressed: () => _runGrantAction(
                  context,
                  ref,
                  (api) => api.revokeGrant(grant.id),
                ),
              )
            : null,
      ),
    );
  }
}

/// One of my registered desktops. Pairing creates a same-account grant where
/// the account itself is the controller (mobile has no machine id) — after
/// that the machine shows up in the selector above.
class _MachineRow extends ConsumerWidget {
  final RcMachine machine;

  const _MachineRow({required this.machine});

  Future<void> _pair(BuildContext context, WidgetRef ref) async {
    final permission = await showDialog<String>(
      context: context,
      builder: (context) {
        var selected = 'view';
        return StatefulBuilder(
          builder: (context, setDialogState) => AlertDialog(
            title: Text(context.S.rcPairTitle(machine.title)),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(context.S.rcPairHint),
                const SizedBox(height: 12),
                SegmentedButton<String>(
                  segments: [
                    ButtonSegment(
                      value: 'view',
                      label: Text(context.S.rcViewOnly),
                    ),
                    ButtonSegment(
                      value: 'operate',
                      label: Text(context.S.rcOperate),
                    ),
                  ],
                  selected: {selected},
                  onSelectionChanged: (v) =>
                      setDialogState(() => selected = v.first),
                ),
              ],
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(context).pop(),
                child: Text(context.S.cancel),
              ),
              FilledButton(
                onPressed: () => Navigator.of(context).pop(selected),
                child: Text(context.S.rcPair),
              ),
            ],
          ),
        );
      },
    );
    if (permission == null || !context.mounted) return;
    await _runGrantAction(
      context,
      ref,
      (api) => api.sameAccount(machine.id, permission),
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return Card(
      child: ListTile(
        dense: true,
        leading: Icon(
          Icons.computer,
          color: machine.online
              ? theme.colorScheme.primary
              : theme.colorScheme.outline,
        ),
        title: Text(machine.title),
        subtitle: Text(
          machine.online ? context.S.rcOnline : context.S.rcOffline,
        ),
        trailing: TextButton(
          onPressed: () => _pair(context, ref),
          child: Text(context.S.rcPair),
        ),
      ),
    );
  }
}

/// Invite another account — they pick which of their desktops accepts.
class _InviteSheet extends ConsumerStatefulWidget {
  const _InviteSheet();

  @override
  ConsumerState<_InviteSheet> createState() => _InviteSheetState();
}

class _InviteSheetState extends ConsumerState<_InviteSheet> {
  final _accountCtrl = TextEditingController();
  String _permission = 'view';
  bool _saving = false;
  String _error = '';

  @override
  void dispose() {
    _accountCtrl.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    final account = _accountCtrl.text.trim();
    if (account.isEmpty) {
      setState(() => _error = context.S.rcIdentityRequired);
      return;
    }
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      final api = ref.read(remoteApiProvider);
      if (api == null) throw StateError('not signed in');
      await api.invite(account, _permission);
      ref.invalidate(remoteStateProvider);
      if (mounted) Navigator.of(context).pop();
    } on Object catch (error) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = failureMessage(error);
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final bottom = MediaQuery.of(context).viewInsets.bottom;
    return SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(20, 0, 20, 20 + bottom),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            context.S.rcInviteOthers,
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 8),
          Text(
            context.S.rcInviteHint,
            style: Theme.of(context).textTheme.bodySmall,
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _accountCtrl,
            keyboardType: TextInputType.emailAddress,
            decoration: InputDecoration(
              labelText: context.S.rcIdentityLabel,
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 12),
          SegmentedButton<String>(
            segments: [
              ButtonSegment(value: 'view', label: Text(context.S.rcViewOnly)),
              ButtonSegment(value: 'operate', label: Text(context.S.rcOperate)),
            ],
            selected: {_permission},
            onSelectionChanged: (v) => setState(() => _permission = v.first),
          ),
          if (_error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                _error,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          const SizedBox(height: 12),
          FilledButton(
            onPressed: _saving ? null : _send,
            child: _saving
                ? const SizedBox(
                    width: 20,
                    height: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Text(context.S.rcSendInvite),
          ),
        ],
      ),
    );
  }
}

/// 打开新建任务底栏；创建成功后直接进任务详情。
Future<void> _createRemoteTask(BuildContext context, RcGrant grant) async {
  final created = await showModalBottomSheet<RemoteTask>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (_) => CreateRemoteTaskSheet(grant: grant),
  );
  if (created == null || !context.mounted || created.path.isEmpty) return;
  GoRouter.maybeOf(context)?.push(
    '/remote/task',
    extra: (grant: grant, taskPath: created.path, taskName: created.title),
  );
}

class _TaskSection extends ConsumerWidget {
  final RcGrant grant;

  const _TaskSection({required this.grant});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tasks = ref.watch(remoteTasksProvider(grant.id));
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SectionHeader(
          title: context.S.rcTasksTitle,
          trailing: grant.canOperate
              ? FilledButton.tonalIcon(
                  onPressed: () => _createRemoteTask(context, grant),
                  icon: const Icon(Icons.add, size: 18),
                  label: Text(context.S.rcNewTask),
                )
              : Tooltip(
                  message: context.S.rcViewOnlyTooltip,
                  triggerMode: TooltipTriggerMode.tap,
                  child: Chip(
                    avatar: const Icon(Icons.visibility_outlined, size: 14),
                    label: Text(context.S.rcViewOnly),
                    visualDensity: VisualDensity.compact,
                  ),
                ),
        ),
        tasks.when(
          loading: () => const Padding(
            padding: EdgeInsets.symmetric(vertical: 4),
            child: SkeletonBox(
              width: double.infinity,
              height: 76,
              radius: BorderRadius.all(Radius.circular(18)),
            ),
          ),
          error: (error, _) => Card(
            child: Padding(
              padding: const EdgeInsets.all(12),
              child: Text(
                failureMessage(error),
                style: TextStyle(color: theme.colorScheme.error),
              ),
            ),
          ),
          data: (items) {
            if (items.isEmpty) {
              return Padding(
                padding: const EdgeInsets.symmetric(vertical: 24),
                child: EmptyState(
                  icon: Icons.task_alt,
                  title: context.S.rcNoTasks,
                  action: grant.canOperate
                      ? FilledButton.tonalIcon(
                          onPressed: () => _createRemoteTask(context, grant),
                          icon: const Icon(Icons.add, size: 18),
                          label: Text(context.S.rcNewFirstTask),
                        )
                      : null,
                ),
              );
            }
            final sorted = [...items]
              ..sort((a, b) {
                if (a.pinned != b.pinned) return a.pinned ? -1 : 1;
                return b.lastActivity.compareTo(a.lastActivity);
              });
            return Column(
              children: [
                for (final task in sorted) _TaskCard(grant: grant, task: task),
              ],
            );
          },
        ),
      ],
    );
  }
}

class _TaskCard extends ConsumerWidget {
  final RcGrant grant;
  final RemoteTask task;

  const _TaskCard({required this.grant, required this.task});

  /// 任务类型 → 图标与着色芯片。coding_dev 是编程任务；其余（含空）
  /// 都是普通对话任务。
  (IconData, Color) _typeOf(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final wf = task.workflowType.toLowerCase();
    if (wf.contains('coding') || wf.contains('dev')) {
      return (Icons.code_rounded, scheme.tertiary);
    }
    return (Icons.chat_bubble_outline_rounded, scheme.primary);
  }

  /// activeStatus 的终态 → (kind, label, color)。kind 是给图标选择用的
  /// 语义键（与本地化文本解耦）；label 走翻译。
  (String, String, Color) _stateOf(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final S = context.S;
    final status = task.activeStatus.toLowerCase();
    if (task.running) {
      return (
        'running',
        task.activePhase.isNotEmpty ? task.activePhase : S.rcStateRunning,
        scheme.primary,
      );
    }
    if (status.contains('fail') || status.contains('error')) {
      return ('failed', S.stStateFailed, const Color(0xFFD64545));
    }
    if (status.contains('cancel')) {
      return ('cancelled', S.rcStateCancelled, scheme.onSurfaceVariant);
    }
    if (status.contains('interrupt')) {
      return ('interrupted', S.rcStateInterrupted, const Color(0xFFD97B42));
    }
    if (status.contains('completed') || status.contains('done')) {
      return ('completed', S.stStateCompleted, const Color(0xFF6AA84F));
    }
    if (task.activeStatus.isNotEmpty) {
      return ('other', task.activeStatus, scheme.onSurfaceVariant);
    }
    return ('notStarted', S.rcStateNotStarted, scheme.onSurfaceVariant);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final (typeIcon, typeColor) = _typeOf(context);
    final (stateKind, stateLabel, stateColor) = _stateOf(context);
    return Card(
          child: InkWell(
            borderRadius: BorderRadius.circular(16),
            onTap: () => context.push(
              '/remote/task',
              extra: (grant: grant, taskPath: task.path, taskName: task.title),
            ),
            child: Padding(
              padding: const EdgeInsets.all(14),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // 任务类型芯片 — tinted rounded square, hue carries the type.
                  Container(
                    width: 42,
                    height: 42,
                    decoration: BoxDecoration(
                      color: typeColor.withValues(alpha: 0.13),
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Icon(typeIcon, size: 21, color: typeColor),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Expanded(
                              child: Text(
                                task.title,
                                style: theme.textTheme.titleSmall,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                            if (task.pinned)
                              Padding(
                                padding: const EdgeInsets.only(left: 4),
                                child: Icon(
                                  Icons.push_pin,
                                  size: 14,
                                  color: theme.colorScheme.primary,
                                ),
                              ),
                          ],
                        ),
                        if (task.preview.isNotEmpty)
                          Padding(
                            padding: const EdgeInsets.only(top: 3),
                            child: Text(
                              task.preview,
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: theme.colorScheme.onSurfaceVariant,
                              ),
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        Padding(
                          padding: const EdgeInsets.only(top: 6),
                          child: Row(
                            children: [
                              // 状态徽章 — 运行中带脉冲点，终端态用语义色。
                              AppBadge(
                                label: stateLabel,
                                color: stateColor,
                                icon: task.running
                                    ? Icons.circle
                                    : _stateIcon(stateKind),
                              ),
                              if (task.pendingReview) ...[
                                const SizedBox(width: 6),
                                AppBadge(
                                  label: context.S.rcPendingReview,
                                  color: theme.colorScheme.tertiary,
                                  icon: Icons.rate_review_outlined,
                                ),
                              ],
                              const Spacer(),
                              if (task.lastActivity.isNotEmpty)
                                Text(
                                  task.lastActivity,
                                  style: theme.textTheme.labelMedium,
                                ),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 4),
                  Icon(
                    Icons.chevron_right,
                    size: 18,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ],
              ),
            ),
          ),
        )
        .animate(delay: (task.id.hashCode.abs() % 3 * 40).ms)
        .fadeIn(duration: 240.ms);
  }

  IconData? _stateIcon(String kind) => switch (kind) {
    'completed' => Icons.check_circle_outline,
    'failed' => Icons.error_outline,
    'cancelled' || 'interrupted' => Icons.stop_circle_outlined,
    _ => null,
  };
}

/// 在远程机上新建任务 — 名称 + 模式 + 远程工作目录（经 list_dirs 浏览）。
class CreateRemoteTaskSheet extends ConsumerStatefulWidget {
  final RcGrant grant;

  const CreateRemoteTaskSheet({super.key, required this.grant});

  @override
  ConsumerState<CreateRemoteTaskSheet> createState() =>
      _CreateRemoteTaskSheetState();
}

class _CreateRemoteTaskSheetState extends ConsumerState<CreateRemoteTaskSheet> {
  final _nameCtrl = TextEditingController();
  final _dirCtrl = TextEditingController();
  String _mode = '';
  bool _saving = false;
  String _error = '';

  @override
  void dispose() {
    _nameCtrl.dispose();
    _dirCtrl.dispose();
    super.dispose();
  }

  Future<void> _pickDir() async {
    final picked = await showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => _RemoteDirPicker(grant: widget.grant),
    );
    if (picked != null && picked.isNotEmpty && mounted) {
      _dirCtrl.text = picked;
    }
  }

  Future<void> _create() async {
    final name = _nameCtrl.text.trim();
    if (name.isEmpty) {
      setState(() => _error = context.S.rcNameRequired);
      return;
    }
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      final api = ref.read(remoteApiProvider);
      if (api == null) throw StateError('not signed in');
      final created = await api.createTask(
        widget.grant.id,
        name: name,
        mode: _mode,
        workDir: _dirCtrl.text.trim(),
      );
      ref.invalidate(remoteTasksProvider(widget.grant.id));
      if (mounted) Navigator.of(context).pop(created);
    } on Object catch (error) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = failureMessage(error);
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final bottom = MediaQuery.of(context).viewInsets.bottom;
    return SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(20, 0, 20, 20 + bottom),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            context.S.rcCreateTaskOn(widget.grant.title),
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _nameCtrl,
            decoration: InputDecoration(
              labelText: context.S.rcTaskNameLabel,
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 12),
          SegmentedButton<String>(
            segments: [
              ButtonSegment(value: '', label: Text(context.S.rcModeChat)),
              ButtonSegment(
                value: 'coding_dev',
                label: Text(context.S.rcModeCoding),
              ),
            ],
            selected: {_mode},
            onSelectionChanged: (v) => setState(() => _mode = v.first),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _dirCtrl,
            decoration: InputDecoration(
              labelText: context.S.rcWorkDirLabel,
              hintText: context.S.rcWorkDirHint,
              border: const OutlineInputBorder(),
              suffixIcon: IconButton(
                tooltip: context.S.rcBrowseTheirDir,
                icon: const Icon(Icons.folder_open),
                onPressed: _saving ? null : _pickDir,
              ),
            ),
          ),
          if (_error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                _error,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          const SizedBox(height: 12),
          FilledButton(
            onPressed: _saving ? null : _create,
            child: _saving
                ? const SizedBox(
                    width: 20,
                    height: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Text(context.S.rcCreate),
          ),
        ],
      ),
    );
  }
}

/// 浏览远程机目录 — `list_dirs` 翻页，点选回填到工作目录。
class _RemoteDirPicker extends ConsumerStatefulWidget {
  final RcGrant grant;

  const _RemoteDirPicker({required this.grant});

  @override
  ConsumerState<_RemoteDirPicker> createState() => _RemoteDirPickerState();
}

class _RemoteDirPickerState extends ConsumerState<_RemoteDirPicker> {
  RemoteDirList? _page;
  String _error = '';
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _browse('');
  }

  Future<void> _browse(String path) async {
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final api = ref.read(remoteApiProvider);
      if (api == null) throw StateError('not signed in');
      final page = await api.listDirs(widget.grant.id, path);
      if (mounted) setState(() => _page = page);
    } on Object catch (error) {
      if (mounted) setState(() => _error = failureMessage(error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final page = _page;
    return SizedBox(
      height: MediaQuery.of(context).size.height * 0.6,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 20),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    page == null ? context.S.rcBrowseDir : page.path,
                    style: theme.textTheme.titleSmall,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (page != null)
                  FilledButton.tonal(
                    onPressed: () => Navigator.of(context).pop(page.path),
                    child: Text(context.S.rcPickThis),
                  ),
              ],
            ),
          ),
          const SizedBox(height: 8),
          if (_error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: Text(
                _error,
                style: TextStyle(color: theme.colorScheme.error),
              ),
            ),
          if (page != null && (page.roots.isNotEmpty || page.home.isNotEmpty))
            SizedBox(
              height: 40,
              child: ListView(
                scrollDirection: Axis.horizontal,
                padding: const EdgeInsets.symmetric(horizontal: 16),
                children: [
                  if (page.home.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(right: 6),
                      child: ActionChip(
                        avatar: const Icon(Icons.home_outlined, size: 16),
                        label: Text(context.S.rcHomeDir),
                        onPressed: () => _browse(page.home),
                      ),
                    ),
                  for (final root in page.roots)
                    Padding(
                      padding: const EdgeInsets.only(right: 6),
                      child: ActionChip(
                        avatar: const Icon(Icons.storage_outlined, size: 16),
                        label: Text(root),
                        onPressed: () => _browse(root),
                      ),
                    ),
                ],
              ),
            ),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : ListView(
                    children: [
                      if (page != null && page.parent.isNotEmpty)
                        ListTile(
                          dense: true,
                          leading: const Icon(Icons.arrow_upward, size: 18),
                          title: Text(context.S.rcParent),
                          onTap: () => _browse(page.parent),
                        ),
                      if (page != null &&
                          page.defaultDir.isNotEmpty &&
                          page.defaultDir != page.path)
                        ListTile(
                          dense: true,
                          leading: const Icon(Icons.home_outlined, size: 18),
                          title: Text(context.S.rcJumpDefault),
                          onTap: () => _browse(page.defaultDir),
                        ),
                      if (page != null)
                        for (final dir in page.dirs)
                          ListTile(
                            dense: true,
                            leading: const Icon(
                              Icons.folder_outlined,
                              size: 18,
                            ),
                            title: Text(dir.name),
                            onTap: () => _browse(dir.path),
                          ),
                      if (page != null && page.truncated)
                        Padding(
                          padding: const EdgeInsets.all(12),
                          child: Text(
                            context.S.rcTooManyDirs,
                            style: theme.textTheme.labelSmall?.copyWith(
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                        ),
                      if (page != null &&
                          page.dirs.isEmpty &&
                          page.parent.isEmpty)
                        Padding(
                          padding: const EdgeInsets.all(20),
                          child: Text(context.S.rcNoSubdirs),
                        ),
                    ],
                  ),
          ),
        ],
      ),
    );
  }
}
