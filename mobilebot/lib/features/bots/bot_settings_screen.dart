import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'bot_models.dart';
import 'bots_controller.dart';
import '../../core/locale.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart';
import 'widgets.dart';

/// Bot 设置 — 对应桌面 GUI 的 BotEditDialog / BotStyleDialog /
/// BotComputerSettingsDialog：改名、人格与记忆、电脑模式、删除。
class BotSettingsScreen extends ConsumerStatefulWidget {
  final Bot bot;

  const BotSettingsScreen({super.key, required this.bot});

  @override
  ConsumerState<BotSettingsScreen> createState() => _BotSettingsScreenState();
}

class _BotSettingsScreenState extends ConsumerState<BotSettingsScreen> {
  late final TextEditingController _nameCtrl;
  late final TextEditingController _descCtrl;
  late String _desktopMode;
  late String _color;
  late String _shape;
  late bool _primary;
  bool _saving = false;
  List<BotSchedule> _schedules = const [];
  String _scheduleNote = '';

  @override
  void initState() {
    super.initState();
    _loadSchedules();
    _nameCtrl = TextEditingController(text: widget.bot.name);
    _descCtrl = TextEditingController(text: widget.bot.description);
    _desktopMode = switch (widget.bot.desktopMode) {
      'isolated' => 'isolated',
      'none' => 'none',
      _ => 'shared',
    };
    _color = botColors.contains(widget.bot.color.toLowerCase())
        ? widget.bot.color.toLowerCase()
        : botColors.first;
    _shape = botShapes.contains(widget.bot.shape) ? widget.bot.shape : 'blob';
    _primary = widget.bot.primary;
  }

  @override
  void dispose() {
    _nameCtrl.dispose();
    _descCtrl.dispose();
    super.dispose();
  }

  Future<void> _loadSchedules() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    try {
      final items = await api.schedules(widget.bot.id);
      if (mounted) {
        setState(() {
          _schedules = items;
          _scheduleNote = '';
        });
      }
    } on Object catch (error) {
      if (mounted) setState(() => _scheduleNote = failureMessage(error));
    }
  }

  Future<void> _deleteSchedule(BotSchedule item) async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    final name = item.name.isEmpty ? item.action : item.name;
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(context.S.stDeleteScheduleTitle),
        content: Text(context.S.stDeleteScheduleBody(name)),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: Text(context.S.cancel),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(context.S.delete),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await api.deleteSchedule(widget.bot.id, item.id);
      await _loadSchedules();
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    }
  }

  /// A none bot cannot take a computer once the grant is already used up,
  /// nor when the account has no assigned cloud computer (a LAN pin would
  /// only help at creation time, not for an existing node-less bot).
  bool _computerChoiceEnabled(String mode) {
    if (mode == 'none') return true;
    if (widget.bot.desktopMode == 'none') {
      final quota = ref.read(botsProvider).quota;
      if (quota.exhausted || quota.cloudMissing) return false;
    }
    return true;
  }

  Future<void> _save() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    final name = _nameCtrl.text.trim();
    if (name.isEmpty) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(context.S.stNameEmpty)));
      return;
    }
    if (botNameTaken(
      name,
      ref.read(botsProvider).bots,
      exceptId: widget.bot.id,
    )) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(context.S.stNameTaken)));
      return;
    }
    setState(() => _saving = true);
    try {
      await api.updateBot(
        widget.bot.id,
        name: name,
        description: _descCtrl.text.trim(),
      );
      // Mode and appearance are independent endpoints — send them together.
      // The server stores "shared" as empty (omitempty), so normalize before
      // comparing or every save re-PUTs the mode for shared bots.
      final currentMode = widget.bot.desktopMode.isEmpty
          ? 'shared'
          : widget.bot.desktopMode;
      await Future.wait([
        if (_desktopMode != currentMode)
          api.setDesktopMode(widget.bot.id, _desktopMode),
        if (_color != widget.bot.color || _shape != widget.bot.shape)
          api.setAppearance(widget.bot.id, color: _color, shape: _shape),
      ]);
      await ref.read(botsProvider.notifier).refresh();
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.S.stSaved)));
        context.pop();
      }
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
        setState(() => _saving = false);
      }
    }
  }

  Future<void> _makePrimary() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    setState(() => _saving = true);
    try {
      await api.setPrimary(widget.bot.id);
      await ref.read(botsProvider.notifier).refresh();
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.S.stSetPrimaryDone)));
        // widget.bot is the pre-refresh copy — track the flag locally so the
        // subtitle/check icon update without re-entering the screen.
        setState(() {
          _primary = true;
          _saving = false;
        });
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _saving = false);
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    }
  }

  /// Durable preferences the bot honours across tasks. Hub stores them as
  /// memory entries (append-only, no read endpoint), so the UI offers
  /// "记录一条偏好" and "撤回全部".
  Future<void> _preferenceSheet() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    final ctrl = TextEditingController();
    try {
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        showDragHandle: true,
        builder: (context) => Padding(
          padding: EdgeInsets.fromLTRB(
            AppSpacing.xl,
            0,
            AppSpacing.xl,
            AppSpacing.xxl + MediaQuery.viewInsetsOf(context).bottom,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                context.S.stLongTermPrefs,
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: AppSpacing.xs),
              Text(
                context.S.stPrefsHint,
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: AppSpacing.md),
              TextField(
                controller: ctrl,
                minLines: 2,
                maxLines: 4,
                decoration: InputDecoration(
                  hintText: context.S.stPrefsFieldHint,
                  border: const OutlineInputBorder(
                    borderRadius: BorderRadius.all(
                      Radius.circular(AppRadius.md),
                    ),
                  ),
                ),
              ),
              const SizedBox(height: AppSpacing.md),
              Row(
                children: [
                  TextButton.icon(
                    onPressed: () async {
                      final S = context.S;
                      final navigator = Navigator.of(context);
                      // Irreversible — wipe every recorded preference.
                      final confirmed = await showDialog<bool>(
                        context: context,
                        builder: (context) => AlertDialog(
                          title: Text(context.S.stWithdrawPrefsTitle),
                          content: Text(context.S.stWithdrawPrefsBody),
                          actions: [
                            TextButton(
                              onPressed: () => Navigator.of(context).pop(false),
                              child: Text(context.S.cancel),
                            ),
                            FilledButton(
                              onPressed: () => Navigator.of(context).pop(true),
                              child: Text(context.S.stWithdraw),
                            ),
                          ],
                        ),
                      );
                      if (confirmed != true || !context.mounted) return;
                      try {
                        await api.withdrawPreference(widget.bot.id);
                        navigator.pop();
                        if (mounted) {
                          ScaffoldMessenger.of(this.context).showSnackBar(
                            SnackBar(content: Text(S.stWithdrawDone)),
                          );
                        }
                      } on Object catch (error) {
                        navigator.pop();
                        if (mounted) {
                          ScaffoldMessenger.of(this.context).showSnackBar(
                            SnackBar(content: Text(failureMessage(error))),
                          );
                        }
                      }
                    },
                    icon: const Icon(Icons.delete_outline, size: 18),
                    label: Text(context.S.stWithdrawAll),
                  ),
                  const Spacer(),
                  FilledButton(
                    onPressed: () async {
                      final text = ctrl.text.trim();
                      final navigator = Navigator.of(context);
                      if (text.isEmpty) return;
                      final S = context.S;
                      try {
                        await api.savePreference(widget.bot.id, text);
                        navigator.pop();
                        if (mounted) {
                          ScaffoldMessenger.of(this.context).showSnackBar(
                            SnackBar(content: Text(S.stPrefRecorded)),
                          );
                        }
                      } on Object catch (error) {
                        navigator.pop();
                        if (mounted) {
                          ScaffoldMessenger.of(this.context).showSnackBar(
                            SnackBar(content: Text(failureMessage(error))),
                          );
                        }
                      }
                    },
                    child: Text(context.S.stRecord),
                  ),
                ],
              ),
            ],
          ),
        ),
      );
    } finally {
      ctrl.dispose();
    }
  }

  Future<void> _delete() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(context.S.stDeleteBotTitle),
        content: Text(context.S.stDeleteBotBody(widget.bot.title)),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(context.S.cancel),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(context.S.delete),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    try {
      await ref.read(botsProvider.notifier).remove(widget.bot);
      if (mounted) context.go('/');
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Row(
          children: [
            const BrandMark(size: 26, tile: true),
            const SizedBox(width: AppSpacing.sm + 2),
            Text(context.S.stScreenTitle),
          ],
        ),
        actions: [
          TextButton(
            onPressed: _saving ? null : _save,
            child: _saving
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Text(context.S.save),
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(
          AppSpacing.lg,
          AppSpacing.md,
          AppSpacing.lg,
          AppSpacing.xxl,
        ),
        children: [
          Card(
            child: Padding(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: Column(
                children: [
                  TextField(
                    controller: _nameCtrl,
                    decoration: InputDecoration(
                      labelText: context.S.stNameLabel,
                    ),
                  ),
                  const SizedBox(height: AppSpacing.md),
                  TextField(
                    controller: _descCtrl,
                    decoration: InputDecoration(
                      labelText: context.S.stDescLabel,
                    ),
                    maxLines: 3,
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: AppSpacing.sm),
          Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(
                AppSpacing.lg,
                AppSpacing.md,
                AppSpacing.sm,
                AppSpacing.md,
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    context.S.stScheduleTitle,
                    style: theme.textTheme.titleSmall,
                  ),
                  const SizedBox(height: AppSpacing.xs),
                  if (_scheduleNote.isNotEmpty)
                    Text(
                      _scheduleNote,
                      style: Theme.of(context).textTheme.bodySmall,
                    )
                  else if (_schedules.isEmpty)
                    Text(
                      context.S.stNoSchedule,
                      style: Theme.of(context).textTheme.bodySmall,
                    )
                  else
                    for (final item in _schedules)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        title: Text(
                          item.name.isEmpty ? item.action : item.name,
                        ),
                        subtitle: Text(item.when(context.S)),
                        trailing: IconButton(
                          tooltip: context.S.delete,
                          icon: const Icon(Icons.delete_outline),
                          onPressed: () => _deleteSchedule(item),
                        ),
                      ),
                ],
              ),
            ),
          ),
          const SizedBox(height: AppSpacing.sm),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    context.S.stAppearance,
                    style: theme.textTheme.titleSmall,
                  ),
                  const SizedBox(height: AppSpacing.md),
                  Wrap(
                    spacing: AppSpacing.sm + 2,
                    runSpacing: AppSpacing.sm,
                    children: [
                      for (final hex in botColors)
                        GestureDetector(
                          onTap: () => setState(() => _color = hex),
                          child: Container(
                            width: 34,
                            height: 34,
                            decoration: BoxDecoration(
                              color: hexColor(hex),
                              shape: BoxShape.circle,
                              border: _color == hex
                                  ? Border.all(
                                      color: theme.colorScheme.onSurface,
                                      width: 2.5,
                                    )
                                  : null,
                            ),
                            child: _color == hex
                                ? const Icon(
                                    Icons.check,
                                    color: Colors.white,
                                    size: 18,
                                  )
                                : null,
                          ),
                        ),
                    ],
                  ),
                  const SizedBox(height: AppSpacing.md),
                  Wrap(
                    spacing: AppSpacing.sm,
                    children: [
                      for (final shape in botShapes)
                        ChoiceChip(
                          label: Text(botShapeLabel(shape, context.S)),
                          selected: _shape == shape,
                          onSelected: (_) => setState(() => _shape = shape),
                        ),
                    ],
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: AppSpacing.sm),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    context.S.stComputerSection,
                    style: theme.textTheme.titleSmall,
                  ),
                  const SizedBox(height: AppSpacing.md),
                  SizedBox(
                    width: double.infinity,
                    child: SegmentedButton<String>(
                      segments: [
                        ButtonSegment(
                          value: 'shared',
                          label: Text(context.S.stModeShared),
                          icon: const Icon(Icons.computer),
                          enabled: _computerChoiceEnabled('shared'),
                        ),
                        ButtonSegment(
                          value: 'isolated',
                          label: Text(context.S.stModeIsolated),
                          icon: const Icon(Icons.desktop_windows),
                          enabled: _computerChoiceEnabled('isolated'),
                        ),
                        ButtonSegment(
                          value: 'none',
                          label: Text(context.S.stModeNone),
                          icon: Icon(Icons.desktop_access_disabled_outlined),
                        ),
                      ],
                      selected: {_desktopMode},
                      onSelectionChanged: (v) {
                        final next = v.first;
                        if (!_computerChoiceEnabled(next)) return;
                        setState(() => _desktopMode = next);
                      },
                    ),
                  ),
                  const SizedBox(height: AppSpacing.sm),
                  Text(
                    switch (_desktopMode) {
                      'none' => context.S.stModeNoneHint,
                      'isolated' => context.S.stModeIsolatedHint,
                      _ => context.S.stModeSharedHint,
                    },
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                  if (ref.watch(botsProvider).quota.label(context.S).isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(top: AppSpacing.xs),
                      child: Text(
                        ref.watch(botsProvider).quota.label(context.S),
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ),
          const SizedBox(height: AppSpacing.sm),
          Card(
            child: Column(
              children: [
                ListTile(
                  leading: const Icon(Icons.psychology_outlined),
                  title: Text(context.S.stPersonaMemory),
                  subtitle: Text(context.S.stPersonaMemoryHint),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push(
                    '/bot/${Uri.encodeComponent(widget.bot.id)}/style',
                  ),
                ),
                const Divider(
                  height: 1,
                  indent: AppSpacing.lg,
                  endIndent: AppSpacing.lg,
                ),
                ListTile(
                  leading: const Icon(Icons.tune),
                  title: Text(context.S.stLongTermPrefs),
                  subtitle: Text(context.S.stLongTermPrefsHint),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: _preferenceSheet,
                ),
                if (_desktopMode != 'none') ...[
                  const Divider(height: 1, indent: 16, endIndent: 16),
                  ListTile(
                    leading: const Icon(Icons.smart_display_outlined),
                    title: Text(context.S.stDemoRecord),
                    subtitle: Text(context.S.stDemoRecordHint),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: () => context.push(
                      '/bot/${Uri.encodeComponent(widget.bot.id)}/demo',
                    ),
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(height: AppSpacing.sm),
          Card(
            child: ListTile(
              leading: const Icon(Icons.star_outline),
              title: Text(context.S.stPrimaryTitle),
              subtitle: Text(
                _primary
                    ? context.S.stIsPrimaryHint
                    : context.S.stMakePrimaryHint,
              ),
              trailing: _primary
                  ? Icon(Icons.check_circle, color: theme.colorScheme.primary)
                  : TextButton(
                      onPressed: _saving ? null : _makePrimary,
                      child: Text(context.S.stMakePrimary),
                    ),
            ),
          ),
          const SizedBox(height: AppSpacing.xl),
          FilledButton.tonalIcon(
            style: FilledButton.styleFrom(
              foregroundColor: theme.colorScheme.error,
            ),
            onPressed: _delete,
            icon: const Icon(Icons.delete_outline),
            label: Text(context.S.stDeleteBot),
          ),
        ],
      ),
    );
  }
}

/// 人格与记忆 — `GET/PUT /api/v1/bots/{id}/style` 的 soul / memory 覆写。
class BotStyleScreen extends ConsumerStatefulWidget {
  final Bot bot;

  const BotStyleScreen({super.key, required this.bot});

  @override
  ConsumerState<BotStyleScreen> createState() => _BotStyleScreenState();
}

class _BotStyleScreenState extends ConsumerState<BotStyleScreen> {
  final _soulCtrl = TextEditingController();
  final _memoryCtrl = TextEditingController();
  bool _loading = true;
  bool _enabled = true;
  bool _saving = false;
  String _error = '';
  List<Map<String, dynamic>> _history = const [];

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _soulCtrl.dispose();
    _memoryCtrl.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final api = ref.read(botApiProvider);
    if (api == null) {
      // No session — stop the spinner instead of hanging forever.
      setState(() => _loading = false);
      return;
    }
    try {
      final view = await api.getStyle(widget.bot.id);
      if (!mounted) return;
      final current = view['current'] is Map
          ? Map<String, dynamic>.from(view['current'] as Map)
          : const <String, dynamic>{};
      setState(() {
        _enabled = view['enabled'] != false;
        _soulCtrl.text = (current['soul'] ?? '').toString();
        _memoryCtrl.text = (current['memory'] ?? '').toString();
        _history = [
          for (final item in syncList(view['history']))
            if (item is Map) Map<String, dynamic>.from(item),
        ];
        _loading = false;
      });
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = failureMessage(error);
      });
    }
  }

  Future<void> _save() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    setState(() => _saving = true);
    try {
      await api.setStyle(
        widget.bot.id,
        soul: _soulCtrl.text,
        memory: _memoryCtrl.text,
      );
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.S.stSaved)));
        context.pop();
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = failureMessage(error);
        });
      }
    }
  }

  Future<void> _rollback(int version) async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    setState(() => _saving = true);
    try {
      await api.rollbackStyle(widget.bot.id, version);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(context.S.stRolledBack(version.toString()))),
      );
      await _load();
      if (mounted) setState(() => _saving = false);
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
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Row(
          children: [
            const BrandMark(size: 26, tile: true),
            const SizedBox(width: AppSpacing.sm + 2),
            Text(context.S.stPersonaMemory),
          ],
        ),
        actions: [
          TextButton(
            onPressed: _saving || _loading || !_enabled ? null : _save,
            child: Text(context.S.save),
          ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : !_enabled
          ? Center(child: Text(context.S.stPersonaDisabled))
          : ListView(
              padding: const EdgeInsets.all(AppSpacing.lg),
              children: [
                if (_error.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(bottom: AppSpacing.md),
                    child: Text(
                      _error,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  ),
                TextField(
                  controller: _soulCtrl,
                  decoration: InputDecoration(
                    labelText: context.S.stPersonaLabel,
                    hintText: context.S.stPersonaFieldHint,
                    border: const OutlineInputBorder(
                      borderRadius: BorderRadius.all(
                        Radius.circular(AppRadius.md),
                      ),
                    ),
                    alignLabelWithHint: true,
                  ),
                  maxLines: 6,
                ),
                const SizedBox(height: AppSpacing.md),
                TextField(
                  controller: _memoryCtrl,
                  decoration: InputDecoration(
                    labelText: context.S.stMemoryLabel,
                    hintText: context.S.stMemoryFieldHint,
                    border: const OutlineInputBorder(
                      borderRadius: BorderRadius.all(
                        Radius.circular(AppRadius.md),
                      ),
                    ),
                    alignLabelWithHint: true,
                  ),
                  maxLines: 6,
                ),
                if (_history.isNotEmpty) ...[
                  const SizedBox(height: AppSpacing.xxl),
                  Text(
                    context.S.stHistoryTitle,
                    style: theme.textTheme.titleSmall,
                  ),
                  const SizedBox(height: AppSpacing.sm),
                  for (final item in _history.reversed)
                    ListTile(
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      leading: const Icon(Icons.history, size: 20),
                      title: Text(context.S.stVersion(item['version'])),
                      subtitle: Text(
                        DateTime.tryParse(
                              (item['updated_at'] ?? '').toString(),
                            )?.toLocal().toString().substring(0, 16) ??
                            '',
                        style: theme.textTheme.bodySmall,
                      ),
                      trailing: TextButton(
                        onPressed: _saving
                            ? null
                            : () => _rollback(syncInt(item['version'])),
                        child: Text(context.S.stRollback),
                      ),
                    ),
                ],
              ],
            ),
    );
  }
}
