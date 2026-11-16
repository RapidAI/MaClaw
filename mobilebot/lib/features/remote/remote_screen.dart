import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/providers.dart';
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
        title: const Text('远程控制'),
        actions: [
          IconButton(
            tooltip: '设备与授权',
            icon: const Icon(Icons.devices_outlined),
            onPressed: () => showModalBottomSheet<void>(
              context: context,
              isScrollControlled: true,
              showDragHandle: true,
              builder: (_) => const _ManageSheet(),
            ),
          ),
          IconButton(
            tooltip: '邀请他人',
            icon: const Icon(Icons.person_add_alt_1),
            onPressed: () => showModalBottomSheet<void>(
              context: context,
              isScrollControlled: true,
              showDragHandle: true,
              builder: (_) => const _InviteSheet(),
            ),
          ),
          IconButton(
            tooltip: '刷新',
            icon: const Icon(Icons.refresh),
            onPressed: () => ref.invalidate(remoteStateProvider),
          ),
        ],
      ),
      body: state.when(
        loading: () => const Center(child: CircularProgressIndicator()),
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
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(message),
          const SizedBox(height: 12),
          FilledButton.tonal(onPressed: onRetry, child: const Text('重试')),
        ],
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
            Text('邀请我', style: theme.textTheme.titleSmall),
            const SizedBox(height: 8),
            for (final grant in snapshot.invites)
              _IncomingInviteCard(grant: grant, machines: snapshot.machines),
            const SizedBox(height: 16),
          ],
          Text('远程机', style: theme.textTheme.titleSmall),
          const SizedBox(height: 8),
          if (usable.isEmpty)
            Card(
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Text(
                  '还没有可控制的机器。\n在下面的「我的设备」里挑一台配对，或让别的账号邀请你。',
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
          Text('设备与授权', style: theme.textTheme.titleMedium),
          const SizedBox(height: 16),
          if (snapshot == null)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 40),
              child: Center(child: CircularProgressIndicator()),
            )
          else ...[
            if (snapshot.controlled.isNotEmpty) ...[
              Text('正在远控我的电脑', style: theme.textTheme.titleSmall),
              const SizedBox(height: 8),
              for (final grant in snapshot.controlled)
                _ControlledRow(grant: grant),
              const SizedBox(height: 16),
            ],
            if (snapshot.pending.isNotEmpty) ...[
              Text('等待对方处理', style: theme.textTheme.titleSmall),
              const SizedBox(height: 8),
              for (final grant in snapshot.pending)
                _PendingOutgoingRow(grant: grant),
              const SizedBox(height: 16),
            ],
            if (snapshot.usableControlled.isNotEmpty) ...[
              Text('我可以控制', style: theme.textTheme.titleSmall),
              const SizedBox(height: 8),
              for (final grant in snapshot.usableControlled)
                _GrantRow(grant: grant, revocable: true),
              const SizedBox(height: 16),
            ],
            if (snapshot.machines.isNotEmpty) ...[
              Text('我的设备', style: theme.textTheme.titleSmall),
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
                    '暂无设备与授权记录',
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
                Icon(
                  Icons.circle,
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
                  grant.permissionLabel,
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
      ).showSnackBar(const SnackBar(content: Text('没有可绑定的电脑，请在桌面端接受')));
      return;
    }
    final machine = await showDialog<RcMachine>(
      context: context,
      builder: (context) => SimpleDialog(
        title: const Text('让哪台电脑接受这次远控？'),
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
                    Text('离线', style: Theme.of(context).textTheme.labelSmall),
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
        : (grant.account.isNotEmpty ? grant.account : '对方');
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '$who 想远控你的电脑（${grant.permissionLabel}）',
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                FilledButton.tonal(
                  onPressed: () => _accept(context, ref),
                  child: const Text('接受'),
                ),
                const SizedBox(width: 8),
                TextButton(
                  onPressed: () => _runGrantAction(
                    context,
                    ref,
                    (api) => api.rejectGrant(grant.id),
                  ),
                  child: const Text('拒绝'),
                ),
                TextButton(
                  onPressed: () => _runGrantAction(
                    context,
                    ref,
                    (api) => api.ignoreGrant(grant.id),
                  ),
                  child: const Text('忽略'),
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
        subtitle: Text('等待对方处理 · ${grant.permissionLabel}'),
        trailing: TextButton(
          child: const Text('取消'),
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
        : (grant.account.isNotEmpty ? grant.account : '对方');
    return Card(
      child: ListTile(
        dense: true,
        leading: Icon(Icons.circle, size: 10, color: theme.colorScheme.primary),
        title: Text(who, maxLines: 1, overflow: TextOverflow.ellipsis),
        subtitle: Text(
          '正在远控 ${grant.machineName.isNotEmpty ? grant.machineName : '我的电脑'}'
          ' · ${grant.permissionLabel}',
        ),
        trailing: TextButton(
          child: const Text('结束'),
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
            grant.permissionLabel,
            if (grant.status != 'active') grant.status,
            if (grant.sameAccount) '同账号',
          ].join(' · '),
        ),
        trailing: revocable
            ? TextButton(
                child: const Text('取消授权'),
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
            title: Text('配对 ${machine.title}'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text('配对后这台机器会出现在「远程机」里。'),
                const SizedBox(height: 12),
                SegmentedButton<String>(
                  segments: const [
                    ButtonSegment(value: 'view', label: Text('仅查看')),
                    ButtonSegment(value: 'operate', label: Text('可操作')),
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
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: () => Navigator.of(context).pop(selected),
                child: const Text('配对'),
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
        subtitle: Text(machine.online ? '在线' : '离线'),
        trailing: TextButton(
          onPressed: () => _pair(context, ref),
          child: const Text('配对'),
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
      setState(() => _error = '请输入对方帐号（邮箱或手机号）');
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
          Text('邀请他人', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          Text(
            '对方在其中一台电脑上接受后，你就可以远控那台电脑。',
            style: Theme.of(context).textTheme.bodySmall,
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _accountCtrl,
            keyboardType: TextInputType.emailAddress,
            decoration: const InputDecoration(
              labelText: '对方帐号（邮箱或手机号）',
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 12),
          SegmentedButton<String>(
            segments: const [
              ButtonSegment(value: 'view', label: Text('仅查看')),
              ButtonSegment(value: 'operate', label: Text('可操作')),
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
                : const Text('发送邀请'),
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
        Row(
          children: [
            Text('任务', style: theme.textTheme.titleSmall),
            const Spacer(),
            if (grant.canOperate)
              FilledButton.tonalIcon(
                onPressed: () => _createRemoteTask(context, grant),
                icon: const Icon(Icons.add, size: 18),
                label: const Text('新建任务'),
              )
            else
              Tooltip(
                message: '当前是「仅查看」授权，不能新建任务',
                triggerMode: TooltipTriggerMode.tap,
                child: Chip(
                  avatar: const Icon(Icons.visibility_outlined, size: 14),
                  label: const Text('仅查看'),
                  visualDensity: VisualDensity.compact,
                ),
              ),
          ],
        ),
        const SizedBox(height: 8),
        tasks.when(
          loading: () => const Padding(
            padding: EdgeInsets.symmetric(vertical: 24),
            child: Center(child: CircularProgressIndicator()),
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
              return Card(
                child: Padding(
                  padding: const EdgeInsets.all(20),
                  child: Column(
                    children: [
                      Icon(
                        Icons.task_alt,
                        size: 36,
                        color: theme.colorScheme.outline,
                      ),
                      const SizedBox(height: 8),
                      Text(
                        '这台机器还没有任务',
                        style: theme.textTheme.bodyMedium?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                      if (grant.canOperate) ...[
                        const SizedBox(height: 12),
                        FilledButton.tonalIcon(
                          onPressed: () => _createRemoteTask(context, grant),
                          icon: const Icon(Icons.add, size: 18),
                          label: const Text('新建第一个任务'),
                        ),
                      ],
                    ],
                  ),
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

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return Card(
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: () => context.push(
          '/remote/task',
          extra: (grant: grant, taskPath: task.path, taskName: task.title),
        ),
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        if (task.pinned)
                          Padding(
                            padding: const EdgeInsets.only(right: 4),
                            child: Icon(
                              Icons.push_pin,
                              size: 14,
                              color: theme.colorScheme.primary,
                            ),
                          ),
                        if (task.pendingReview)
                          Padding(
                            padding: const EdgeInsets.only(right: 4),
                            child: Icon(
                              Icons.rate_review_outlined,
                              size: 14,
                              color: theme.colorScheme.tertiary,
                            ),
                          ),
                        Expanded(
                          child: Text(
                            task.title,
                            style: theme.textTheme.titleSmall,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                      ],
                    ),
                    if (task.preview.isNotEmpty)
                      Padding(
                        padding: const EdgeInsets.only(top: 4),
                        child: Text(
                          task.preview,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                    if (task.activePhase.isNotEmpty ||
                        task.lastActivity.isNotEmpty)
                      Padding(
                        padding: const EdgeInsets.only(top: 4),
                        child: Text(
                          [
                            if (task.activePhase.isNotEmpty) task.activePhase,
                            if (task.activeStatus.isNotEmpty) task.activeStatus,
                            task.lastActivity,
                          ].where((s) => s.isNotEmpty).join(' · '),
                          style: theme.textTheme.labelSmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                  ],
                ),
              ),
              if (task.running)
                Padding(
                  padding: const EdgeInsets.only(left: 8, top: 4),
                  child: SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: theme.colorScheme.primary,
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
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
      setState(() => _error = '请输入任务名称');
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
            '在 ${widget.grant.title} 上新建任务',
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _nameCtrl,
            decoration: const InputDecoration(
              labelText: '任务名称',
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 12),
          SegmentedButton<String>(
            segments: const [
              ButtonSegment(value: '', label: Text('对话')),
              ButtonSegment(value: 'coding_dev', label: Text('编程')),
            ],
            selected: {_mode},
            onSelectionChanged: (v) => setState(() => _mode = v.first),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _dirCtrl,
            decoration: InputDecoration(
              labelText: '工作目录（可选）',
              hintText: '留空用对方默认目录',
              border: const OutlineInputBorder(),
              suffixIcon: IconButton(
                tooltip: '浏览对方目录',
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
                : const Text('创建'),
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
                    page == null ? '浏览目录' : page.path,
                    style: theme.textTheme.titleSmall,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (page != null)
                  FilledButton.tonal(
                    onPressed: () => Navigator.of(context).pop(page.path),
                    child: const Text('选这个'),
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
                        label: const Text('主目录'),
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
                          title: const Text('上一级'),
                          onTap: () => _browse(page.parent),
                        ),
                      if (page != null &&
                          page.defaultDir.isNotEmpty &&
                          page.defaultDir != page.path)
                        ListTile(
                          dense: true,
                          leading: const Icon(Icons.home_outlined, size: 18),
                          title: const Text('跳到默认目录'),
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
                            '子目录太多，只显示了前一部分',
                            style: theme.textTheme.labelSmall?.copyWith(
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                        ),
                      if (page != null &&
                          page.dirs.isEmpty &&
                          page.parent.isEmpty)
                        const Padding(
                          padding: EdgeInsets.all(20),
                          child: Text('这个目录没有子文件夹'),
                        ),
                    ],
                  ),
          ),
        ],
      ),
    );
  }
}
