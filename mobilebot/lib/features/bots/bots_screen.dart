import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'bot_models.dart';
import 'bots_controller.dart';
import 'widgets.dart';

/// Bot 列表页 — 对应桌面 GUI 的 bot 侧栏。
class BotsScreen extends ConsumerWidget {
  const BotsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(botsProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('我的 Bot'),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: () => ref.read(botsProvider.notifier).refresh(),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _showCreateSheet(context, ref),
        icon: const Icon(Icons.add),
        label: const Text('新建 Bot'),
      ),
      body: RefreshIndicator(
        onRefresh: () => ref.read(botsProvider.notifier).refresh(),
        child: _buildBody(context, ref, state),
      ),
    );
  }

  Widget _buildBody(BuildContext context, WidgetRef ref, BotsState state) {
    if (state.loading && state.bots.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }
    if (!state.access.enabled) {
      return ListView(
        children: [
          const SizedBox(height: 120),
          EmptyState(
            icon: Icons.block,
            title: state.access.message.isNotEmpty
                ? state.access.message
                : '当前账号未开通 Bot 功能',
          ),
        ],
      );
    }
    if (state.error.isNotEmpty && state.bots.isEmpty) {
      return ListView(
        children: [
          const SizedBox(height: 120),
          EmptyState(
            icon: Icons.cloud_off,
            title: state.error,
            action: FilledButton.tonal(
              onPressed: () => ref.read(botsProvider.notifier).refresh(),
              child: const Text('重试'),
            ),
          ),
        ],
      );
    }
    if (state.bots.isEmpty) {
      return ListView(
        children: const [
          SizedBox(height: 140),
          EmptyState(
            icon: Icons.smart_toy_outlined,
            title: '还没有 Bot',
            hint: '点击右下角创建一个',
          ),
        ],
      );
    }
    return ListView.separated(
      padding: const EdgeInsets.fromLTRB(12, 12, 12, 96),
      itemCount: state.bots.length,
      separatorBuilder: (_, _) => const SizedBox(height: 8),
      itemBuilder: (context, index) {
        final bot = state.bots[index];
        return _BotCard(bot: bot);
      },
    );
  }

  Future<void> _showCreateSheet(BuildContext context, WidgetRef ref) async {
    final created = await showModalBottomSheet<Bot>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (context) => const CreateBotSheet(),
    );
    if (created != null && context.mounted) {
      context.push('/bot/${Uri.encodeComponent(created.id)}');
    }
  }
}

class _BotCard extends ConsumerWidget {
  final Bot bot;

  const _BotCard({required this.bot});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => context.push('/bot/${Uri.encodeComponent(bot.id)}'),
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Row(
            children: [
              BotAvatar(bot: bot, radius: 24),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Flexible(
                          child: Text(
                            bot.title,
                            style: theme.textTheme.titleMedium,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        if (bot.primary)
                          Container(
                            margin: const EdgeInsets.only(left: 6),
                            padding: const EdgeInsets.symmetric(
                              horizontal: 5,
                              vertical: 1,
                            ),
                            decoration: BoxDecoration(
                              color: theme.colorScheme.primaryContainer,
                              borderRadius: BorderRadius.circular(4),
                            ),
                            child: Text(
                              '主',
                              style: theme.textTheme.labelSmall?.copyWith(
                                color: theme.colorScheme.onPrimaryContainer,
                              ),
                            ),
                          ),
                      ],
                    ),
                    if (bot.description.isNotEmpty)
                      Text(
                        bot.description,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                  ],
                ),
              ),
              Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  IconButton(
                    tooltip: '电脑画面',
                    visualDensity: VisualDensity.compact,
                    icon: const Icon(Icons.desktop_windows_outlined),
                    onPressed: () => context.push(
                      '/bot/${Uri.encodeComponent(bot.id)}/desktop',
                    ),
                  ),
                  Icon(
                    Icons.chevron_right,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// 新建 Bot 底部表单。
class CreateBotSheet extends ConsumerStatefulWidget {
  const CreateBotSheet({super.key});

  @override
  ConsumerState<CreateBotSheet> createState() => _CreateBotSheetState();
}

class _CreateBotSheetState extends ConsumerState<CreateBotSheet> {
  final _nameCtrl = TextEditingController();
  final _descCtrl = TextEditingController();
  bool _saving = false;
  String _error = '';
  String _color = botColors[0];
  String _shape = 'blob';

  @override
  void dispose() {
    _nameCtrl.dispose();
    _descCtrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final bottom = MediaQuery.of(context).viewInsets.bottom;
    return Padding(
      padding: EdgeInsets.fromLTRB(20, 0, 20, 20 + bottom),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('新建 Bot', style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 16),
          TextField(
            controller: _nameCtrl,
            autofocus: true,
            decoration: const InputDecoration(
              labelText: '名称',
              hintText: '给 Bot 起个名字',
              border: OutlineInputBorder(),
            ),
            maxLength: 40,
          ),
          TextField(
            controller: _descCtrl,
            decoration: const InputDecoration(
              labelText: '简介（可选）',
              hintText: '这个 Bot 帮你做什么',
              border: OutlineInputBorder(),
            ),
            maxLines: 2,
          ),
          const SizedBox(height: 12),
          SizedBox(
            height: 28,
            child: ListView(
              scrollDirection: Axis.horizontal,
              children: [
                for (final hex in botColors)
                  GestureDetector(
                    onTap: () => setState(() => _color = hex),
                    child: Container(
                      width: 28,
                      height: 28,
                      margin: const EdgeInsets.only(right: 8),
                      decoration: BoxDecoration(
                        color: hexColor(hex),
                        shape: BoxShape.circle,
                        border: _color == hex
                            ? Border.all(
                                color: Theme.of(context).colorScheme.onSurface,
                                width: 2,
                              )
                            : null,
                      ),
                      child: _color == hex
                          ? const Icon(
                              Icons.check,
                              color: Colors.white,
                              size: 14,
                            )
                          : null,
                    ),
                  ),
              ],
            ),
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 6,
            children: [
              for (final shape in botShapes)
                ChoiceChip(
                  label: Text(
                    botShapeLabel(shape),
                    style: const TextStyle(fontSize: 12),
                  ),
                  visualDensity: VisualDensity.compact,
                  selected: _shape == shape,
                  onSelected: (_) => setState(() => _shape = shape),
                ),
            ],
          ),
          if (_error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: Text(
                _error,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ),
          const SizedBox(height: 8),
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

  Future<void> _create() async {
    final name = _nameCtrl.text.trim();
    if (name.isEmpty) {
      setState(() => _error = '请输入名称');
      return;
    }
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      final bot = await ref
          .read(botsProvider.notifier)
          .create(name, _descCtrl.text.trim(), color: _color, shape: _shape);
      if (mounted) Navigator.of(context).pop(bot);
    } on Object catch (error) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = failureMessage(error);
        });
      }
    }
  }
}
