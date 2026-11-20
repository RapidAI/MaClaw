import 'package:flutter/material.dart';
import 'package:flutter_animate/flutter_animate.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/providers.dart';
import '../../core/locale.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart';
import '../bots/bots_controller.dart' show failureMessage;
import 'remote_api.dart';
import 'remote_models.dart';
import 'task_spaces.dart';

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
/// [workDir] 非空时预填工作目录（空间头「+」入口按空间路径预填）。
Future<void> _createRemoteTask(
  BuildContext context,
  RcGrant grant, {
  String workDir = '',
}) async {
  final created = await showModalBottomSheet<RemoteTask>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (_) => CreateRemoteTaskSheet(grant: grant, initialWorkDir: workDir),
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
                  // 新建入口只保留列表顶部的 SectionHeader trailing 按钮，
                  // 空态不再重复放一个（否则页面出现两个「新建任务」）。
                ),
              );
            }
            // WorkBuddy 风格：任务按工作目录聚成「空间」，每个空间一行
            // 可折叠的头，组内任务仍是紧凑卡片。
            final spaces = groupTasksIntoSpaces(items);
            // 默认只展开"最活跃"空间：groupTasksIntoSpaces 已把 running
            // 空间排最前、其余按 lastActivity 倒序，第一个即最活跃。
            // items 非空时 spaces 必然非空；用 firstOrNull 守住这条
            // 不变量，将来有人改动分组逻辑也不会抛 Bad state。
            final activeKey = spaces.firstOrNull?.key;
            return Column(
              children: [
                for (final space in spaces)
                  _SpaceGroup(
                    // 按空间 key 绑定 element，空间重排后折叠状态不错配。
                    key: ValueKey('rc-space-${space.key}'),
                    grant: grant,
                    space: space,
                    initiallyExpanded: space.key == activeKey,
                  ),
              ],
            );
          },
        ),
      ],
    );
  }
}

/// 一个空间（目录）及其任务的折叠组：一行可折叠的头 + 组内紧凑任务行。
/// 头行右侧：运行计数徽章 → 「+」在该空间新建任务（预填目录）。
class _SpaceGroup extends StatefulWidget {
  final RcGrant grant;
  final TaskSpace space;

  /// 初次构建时是否展开 — 由上层按"最活跃空间"传入。
  final bool initiallyExpanded;

  const _SpaceGroup({
    super.key,
    required this.grant,
    required this.space,
    required this.initiallyExpanded,
  });

  @override
  State<_SpaceGroup> createState() => _SpaceGroupState();
}

class _SpaceGroupState extends State<_SpaceGroup> {
  late bool _expanded;

  @override
  void initState() {
    super.initState();
    _expanded = widget.initiallyExpanded;
  }

  @override
  void didUpdateWidget(_SpaceGroup oldWidget) {
    super.didUpdateWidget(oldWidget);
    // 刷新后"最活跃空间"变了：initiallyExpanded 由 false 变 true 时展开；
    // true→false 不主动收起（用户手动展开过的状态保留）。
    // didUpdateWidget 本身就会触发 rebuild，这里直接改状态即可。
    if (!oldWidget.initiallyExpanded && widget.initiallyExpanded) {
      _expanded = true;
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final space = widget.space;
    final title = space.isUngrouped ? context.S.rcSpaceOther : space.label;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // 无障碍：给整行可点击区标 button 角色 + 展开/折叠态，读屏报
        // "按钮，已展开/已折叠"。container:true 使该节点独立成组，不吞并
        // 子级——计数 Semantics 与「+」IconButton 的 button 仍各自暴露。
        Semantics(
          button: true,
          expanded: _expanded,
          container: true,
          child: InkWell(
            borderRadius: BorderRadius.circular(8),
            onTap: () => setState(() => _expanded = !_expanded),
            child: Padding(
            padding: const EdgeInsets.fromLTRB(4, 8, 4, 6),
            child: Row(
              children: [
                Icon(
                  _expanded
                      ? Icons.keyboard_arrow_down
                      : Icons.keyboard_arrow_right,
                  size: 18,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: 4),
                Icon(
                  Icons.folder_outlined,
                  size: 16,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: 6),
                Expanded(
                  child: Tooltip(
                    message: space.fullPath.isNotEmpty ? space.fullPath : title,
                    child: Text(
                      title,
                      style: theme.textTheme.titleSmall,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ),
                // 状态汇总：有运行中任务时显示计数小徽章（primary），
                // 没有就不显示。
                if (space.runningCount > 0) ...[
                  const SizedBox(width: 6),
                  _RunningBadge(spaceKey: space.key, count: space.runningCount),
                ],
                const SizedBox(width: 6),
                // 窄屏（360dp）下蒙古语 "N даалгавар" 或长数字会挤压标题：
                // Flexible + ellipsis 让计数在空间不足时缩略而不是溢出；
                // 缩略后用 Semantics 向读屏报完整计数，语义不丢。
                Flexible(
                  child: Semantics(
                    label: context.S.rcSpaceTaskCount(space.tasks.length),
                    child: Text(
                      context.S.rcSpaceTaskCount(space.tasks.length),
                      style: theme.textTheme.labelMedium?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ),
                // 「+」在该空间新建任务（预填工作目录）。未分组空间没有目录
                // 可预填，不显示；view-only grant 也没有新建权限。
                if (widget.grant.canOperate && !space.isUngrouped) ...[
                  const SizedBox(width: 2),
                  SizedBox(
                    width: 30,
                    height: 30,
                    child: IconButton(
                      tooltip: context.S.rcNewTask,
                      icon: const Icon(Icons.add, size: 18),
                      visualDensity: VisualDensity.compact,
                      padding: EdgeInsets.zero,
                      onPressed: () => _createRemoteTask(
                        context,
                        widget.grant,
                        workDir: space.fullPath,
                      ),
                    ),
                  ),
                ],
              ],
            ),
          ),
          ),
        ),
        if (_expanded)
          // 组内缩进：头文本从左 4dp（InkWell 的 fromLTRB(4,…)）起，
          // 卡片列右移 4dp 与头文本起点对齐，形成轻量"组内"层级；
          // 不嵌套 Card/不加阴影，层级全靠 4dp 缩进表达。
          Padding(
            padding: const EdgeInsets.only(left: 4),
            child: Column(
              children: [
                for (final task in space.tasks)
                  _TaskCard(grant: widget.grant, task: task),
              ],
            ),
          ),
      ],
    );
  }
}

/// 空间头的运行中任务计数徽章：primary 底 + onPrimary 数字，
/// 仅在 runningCount > 0 时渲染（调用处已用 if 保证）。
class _RunningBadge extends StatelessWidget {
  final String spaceKey;
  final int count;
  const _RunningBadge({required this.spaceKey, required this.count});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      key: ValueKey('rc-space-running-badge-$spaceKey'),
      constraints: const BoxConstraints(minWidth: 18),
      height: 18,
      padding: const EdgeInsets.symmetric(horizontal: 5),
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: theme.colorScheme.primary,
        borderRadius: BorderRadius.circular(9),
      ),
      child: Text(
        '$count',
        style: theme.textTheme.labelSmall?.copyWith(
          color: theme.colorScheme.onPrimary,
          fontWeight: FontWeight.w600,
          height: 1,
        ),
      ),
    );
  }
}

class _TaskCard extends ConsumerWidget {
  final RcGrant grant;
  final RemoteTask task;

  const _TaskCard({required this.grant, required this.task});

  /// 任务类型 → 图标、着色与本地化短标签。判定走 [RemoteTask.kind]
  /// （工作流类型或标签上的编程身份），不再用 workflow 子串猜测。
  /// 非编程的 other 工作流仍用对话气泡，避免产品设计/报告被画成代码任务。
  (IconData, Color, String) _typeOf(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final S = context.S;
    if (task.isCoding) {
      return (Icons.code_rounded, scheme.tertiary, S.rcKindCoding);
    }
    return (Icons.chat_bubble_outline_rounded, scheme.primary, S.rcKindChat);
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
    final (typeIcon, typeColor, typeLabel) = _typeOf(context);
    final (stateKind, stateLabel, stateColor) = _stateOf(context);
    return Card(
          child: InkWell(
            borderRadius: BorderRadius.circular(16),
            onTap: () => context.push(
              '/remote/task',
              extra: (grant: grant, taskPath: task.path, taskName: task.title),
            ),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 9),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // 任务类型芯片 — tinted rounded square, hue carries the type.
                  Container(
                    width: 32,
                    height: 32,
                    decoration: BoxDecoration(
                      color: typeColor.withValues(alpha: 0.13),
                      borderRadius: BorderRadius.circular(9),
                    ),
                    child: Icon(typeIcon, size: 17, color: typeColor),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // 320dp + 超长任务名：Wrap 换行。Flexible 不能直接放进
                        // Wrap，用行宽上限代替，标题省略、置顶图标落到下一行。
                        LayoutBuilder(
                          builder: (context, titleConstraints) {
                            final maxTitle = titleConstraints.maxWidth.isFinite
                                ? titleConstraints.maxWidth
                                : MediaQuery.sizeOf(context).width;
                            return Wrap(
                              spacing: 4,
                              runSpacing: 2,
                              crossAxisAlignment: WrapCrossAlignment.center,
                              children: [
                                ConstrainedBox(
                                  constraints: BoxConstraints(
                                    maxWidth: maxTitle,
                                  ),
                                  child: Text(
                                    task.title,
                                    style: theme.textTheme.titleSmall,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ),
                                if (task.pinned)
                                  Icon(
                                    Icons.push_pin,
                                    size: 14,
                                    color: theme.colorScheme.primary,
                                  ),
                              ],
                            );
                          },
                        ),

                        if (task.preview.isNotEmpty)
                          Padding(
                            padding: const EdgeInsets.only(top: 2),
                            child: Text(
                              task.preview,
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: theme.colorScheme.onSurfaceVariant,
                              ),
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        Padding(
                          padding: const EdgeInsets.only(top: 4),
                          child: LayoutBuilder(
                            builder: (context, constraints) {
                              final maxChip = constraints.maxWidth.isFinite
                                  ? constraints.maxWidth
                                  : MediaQuery.sizeOf(context).width;
                              return Wrap(
                                spacing: 6,
                                runSpacing: 4,
                                crossAxisAlignment: WrapCrossAlignment.center,
                                children: [
                                  _metaChip(
                                    label: typeLabel,
                                    color: typeColor,
                                    maxWidth: maxChip,
                                  ),
                                  _metaChip(
                                    label: stateLabel,
                                    color: stateColor,
                                    maxWidth: maxChip,
                                    icon: task.running
                                        ? Icons.circle
                                        : _stateIcon(stateKind),
                                  ),
                                  if (task.pendingReview)
                                    _metaChip(
                                      label: context.S.rcPendingReview,
                                      color: theme.colorScheme.tertiary,
                                      maxWidth: maxChip,
                                      icon: Icons.rate_review_outlined,
                                    ),
                                  if (task.lastActivity.isNotEmpty)
                                    ConstrainedBox(
                                      constraints: BoxConstraints(
                                        maxWidth: maxChip,
                                      ),
                                      child: Text(
                                        task.lastActivity,
                                        maxLines: 1,
                                        overflow: TextOverflow.ellipsis,
                                        style: theme.textTheme.labelMedium,
                                      ),
                                    ),
                                ],
                              );
                            },
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

  /// Badge capped to the meta row so a long localized label ellipsizes
  /// instead of painting past the card.
  Widget _metaChip({
    required String label,
    required Color color,
    required double maxWidth,
    IconData? icon,
  }) {
    final foreground = color;
    return ConstrainedBox(
      constraints: BoxConstraints(maxWidth: maxWidth),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (icon != null) ...[
              Icon(icon, size: 12, color: foreground),
              const SizedBox(width: 4),
            ],
            Flexible(
              child: Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                softWrap: false,
                style: TextStyle(
                  fontSize: 11,
                  color: foreground,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  IconData? _stateIcon(String kind) => switch (kind) {
    'completed' => Icons.check_circle_outline,
    'failed' => Icons.error_outline,
    'cancelled' || 'interrupted' => Icons.stop_circle_outlined,
    _ => null,
  };
}

/// 在远程机上新建任务 — 名称 + 模式 + 远程工作目录（经 list_dirs 浏览）。
/// [initialWorkDir] 非空时预填目录输入框（空间头「+」入口传入空间路径）。
class CreateRemoteTaskSheet extends ConsumerStatefulWidget {
  final RcGrant grant;
  final String initialWorkDir;

  const CreateRemoteTaskSheet({
    super.key,
    required this.grant,
    this.initialWorkDir = '',
  });

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
  void initState() {
    super.initState();
    _dirCtrl.text = widget.initialWorkDir;
  }

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
