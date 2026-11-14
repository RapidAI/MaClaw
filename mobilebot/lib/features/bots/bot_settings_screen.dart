import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'bot_models.dart';
import 'bots_controller.dart';
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

  @override
  void initState() {
    super.initState();
    _nameCtrl = TextEditingController(text: widget.bot.name);
    _descCtrl = TextEditingController(text: widget.bot.description);
    _desktopMode =
        widget.bot.desktopMode == 'isolated' ? 'isolated' : 'shared';
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

  Future<void> _save() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    setState(() => _saving = true);
    try {
      await api.updateBot(
        widget.bot.id,
        name: _nameCtrl.text,
        description: _descCtrl.text,
      );
      if (_desktopMode != widget.bot.desktopMode) {
        await api.setDesktopMode(widget.bot.id, _desktopMode);
      }
      if (_color != widget.bot.color || _shape != widget.bot.shape) {
        await api.setAppearance(widget.bot.id,
            color: _color, shape: _shape);
      }
      await ref.read(botsProvider.notifier).refresh();
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(const SnackBar(content: Text('已保存')));
        context.pop();
      }
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(failureMessage(error))));
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
        ScaffoldMessenger.of(context)
            .showSnackBar(const SnackBar(content: Text('已设为主 Bot')));
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
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(failureMessage(error))));
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
            20, 0, 20, 24 + MediaQuery.viewInsetsOf(context).bottom),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('长期偏好',
                style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 4),
            const Text(
              '写下的偏好会长期生效，之后的每个任务都会遵循（例如"结果用表格呈现""截图带上关键步骤"）。',
              style: TextStyle(fontSize: 12),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: ctrl,
              minLines: 2,
              maxLines: 4,
              decoration: const InputDecoration(
                hintText: '记录一条偏好…',
                border: OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                TextButton.icon(
                  onPressed: () async {
                    final navigator = Navigator.of(context);
                    try {
                      await api.withdrawPreference(widget.bot.id);
                      navigator.pop();
                      if (mounted) {
                        ScaffoldMessenger.of(this.context).showSnackBar(
                            const SnackBar(content: Text('已撤回全部偏好')));
                      }
                    } on Object catch (error) {
                      navigator.pop();
                      if (mounted) {
                        ScaffoldMessenger.of(this.context).showSnackBar(
                            SnackBar(content: Text(failureMessage(error))));
                      }
                    }
                  },
                  icon: const Icon(Icons.delete_outline, size: 18),
                  label: const Text('撤回全部偏好'),
                ),
                const Spacer(),
                FilledButton(
                  onPressed: () async {
                    final text = ctrl.text.trim();
                    final navigator = Navigator.of(context);
                    if (text.isEmpty) return;
                    try {
                      await api.savePreference(widget.bot.id, text);
                      navigator.pop();
                      if (mounted) {
                        ScaffoldMessenger.of(this.context).showSnackBar(
                            const SnackBar(content: Text('偏好已记录')));
                      }
                    } on Object catch (error) {
                      navigator.pop();
                      if (mounted) {
                        ScaffoldMessenger.of(this.context).showSnackBar(
                            SnackBar(content: Text(failureMessage(error))));
                      }
                    }
                  },
                  child: const Text('记录'),
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
        title: const Text('删除 Bot？'),
        content: Text('将删除「${widget.bot.title}」及其会话记录，此操作不可撤销。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: Theme.of(context).colorScheme.error,
            ),
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('删除'),
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
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('Bot 设置'),
        actions: [
          TextButton(
            onPressed: _saving ? null : _save,
            child: _saving
                ? const SizedBox(
                    width: 16, height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2))
                : const Text('保存'),
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          TextField(
            controller: _nameCtrl,
            decoration: const InputDecoration(
              labelText: '名称',
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _descCtrl,
            decoration: const InputDecoration(
              labelText: '简介',
              border: OutlineInputBorder(),
            ),
            maxLines: 3,
          ),
          const SizedBox(height: 24),
          Text('外观', style: theme.textTheme.titleSmall),
          const SizedBox(height: 8),
          Wrap(
            spacing: 10,
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
                              color: theme.colorScheme.onSurface, width: 2.5)
                          : null,
                    ),
                    child: _color == hex
                        ? const Icon(Icons.check,
                            color: Colors.white, size: 18)
                        : null,
                  ),
                ),
            ],
          ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            children: [
              for (final shape in botShapes)
                ChoiceChip(
                  label: Text(botShapeLabel(shape)),
                  selected: _shape == shape,
                  onSelected: (_) => setState(() => _shape = shape),
                ),
            ],
          ),
          const SizedBox(height: 24),
          Text('电脑', style: theme.textTheme.titleSmall),
          const SizedBox(height: 8),
          SegmentedButton<String>(
            segments: const [
              ButtonSegment(
                value: 'shared',
                label: Text('共享电脑'),
                icon: Icon(Icons.computer),
              ),
              ButtonSegment(
                value: 'isolated',
                label: Text('独立电脑'),
                icon: Icon(Icons.desktop_windows),
              ),
            ],
            selected: {_desktopMode},
            onSelectionChanged: (v) =>
                setState(() => _desktopMode = v.first),
          ),
          const SizedBox(height: 8),
          Text(
            _desktopMode == 'isolated'
                ? 'Bot 使用自己的独立电脑画面'
                : 'Bot 使用你账号下的共享电脑画面',
            style: theme.textTheme.bodySmall
                ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
          const SizedBox(height: 24),
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.psychology_outlined),
            title: const Text('人格与记忆'),
            subtitle: const Text('自定义 Bot 的性格、语气和长期记忆'),
            trailing: const Icon(Icons.chevron_right),
            onTap: () => context.push('/bot/${widget.bot.id}/style'),
          ),
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.tune),
            title: const Text('长期偏好'),
            subtitle: const Text('记录 Bot 在每个任务里都要遵循的偏好'),
            trailing: const Icon(Icons.chevron_right),
            onTap: _preferenceSheet,
          ),
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.smart_display_outlined),
            title: const Text('演示录制'),
            subtitle: const Text('录一段操作过程，提炼成 Bot 的技能（需独立电脑）'),
            trailing: const Icon(Icons.chevron_right),
            onTap: () => context.push('/bot/${widget.bot.id}/demo'),
          ),
          const SizedBox(height: 24),
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: const Icon(Icons.star_outline),
            title: const Text('主 Bot'),
            subtitle: Text(_primary
                ? '已是主 Bot，其他 Bot 的委托由它派发'
                : '设为主 Bot 后，它可以给团队里其他 Bot 派任务'),
            trailing: _primary
                ? Icon(Icons.check_circle,
                    color: theme.colorScheme.primary)
                : TextButton(
                    onPressed: _saving ? null : _makePrimary,
                    child: const Text('设为主 Bot'),
                  ),
          ),
          const Divider(height: 40),
          FilledButton.tonalIcon(
            style: FilledButton.styleFrom(
              foregroundColor: theme.colorScheme.error,
            ),
            onPressed: _delete,
            icon: const Icon(Icons.delete_outline),
            label: const Text('删除这个 Bot'),
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
    if (api == null) return;
    try {
      final view = await api.getStyle(widget.bot.id);
      if (!mounted) return;
      final current = view['current'] is Map
          ? Map<String, dynamic>.from(view['current'] as Map)
          : const <String, dynamic>{};
      setState(() {
        _enabled = view['enabled'] != false;
        _soulCtrl.text = (current['soul'] as String? ?? '').toString();
        _memoryCtrl.text = (current['memory'] as String? ?? '').toString();
        _history = [
          for (final item in (view['history'] as List? ?? const []))
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
        ScaffoldMessenger.of(context)
            .showSnackBar(const SnackBar(content: Text('已保存')));
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
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('已回滚到版本 $version')));
        await _load();
        setState(() => _saving = false);
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

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('人格与记忆'),
        actions: [
          TextButton(
            onPressed: _saving || _loading || !_enabled ? null : _save,
            child: const Text('保存'),
          ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : !_enabled
              ? const Center(child: Text('当前 Hub 未开启人格设置'))
              : ListView(
                  padding: const EdgeInsets.all(16),
                  children: [
                    if (_error.isNotEmpty)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 12),
                        child: Text(_error,
                            style: TextStyle(
                                color:
                                    Theme.of(context).colorScheme.error)),
                      ),
                    TextField(
                      controller: _soulCtrl,
                      decoration: const InputDecoration(
                        labelText: '人格',
                        hintText: '这个 Bot 是谁，怎么说话',
                        border: OutlineInputBorder(),
                        alignLabelWithHint: true,
                      ),
                      maxLines: 6,
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _memoryCtrl,
                      decoration: const InputDecoration(
                        labelText: '长期记忆',
                        hintText: 'Bot 应记住的事（偏好、约定、背景）',
                        border: OutlineInputBorder(),
                        alignLabelWithHint: true,
                      ),
                      maxLines: 6,
                    ),
                    if (_history.isNotEmpty) ...[
                      const SizedBox(height: 24),
                      Text('历史版本',
                          style: theme.textTheme.titleSmall),
                      const SizedBox(height: 8),
                      for (final item in _history.reversed)
                        ListTile(
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                          leading: const Icon(Icons.history, size: 20),
                          title: Text('版本 ${item['version']}'),
                          subtitle: Text(
                            DateTime.tryParse(
                                            item['updated_at']
                                                    as String? ??
                                                '')
                                        ?.toLocal()
                                        .toString()
                                        .substring(0, 16) ??
                                    '',
                            style: theme.textTheme.bodySmall,
                          ),
                          trailing: TextButton(
                            onPressed: _saving
                                ? null
                                : () => _rollback(
                                    (item['version'] as num? ?? 0)
                                        .toInt()),
                            child: const Text('回滚'),
                          ),
                        ),
                    ],
                  ],
                ),
    );
  }
}
