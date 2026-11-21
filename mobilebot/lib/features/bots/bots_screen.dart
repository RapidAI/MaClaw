import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_animate/flutter_animate.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'bot_models.dart';
import 'bot_api.dart';
import 'bots_controller.dart';
import '../../core/locale.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart';
import 'widgets.dart';

/// Bot 列表页 — 对应桌面 GUI 的 bot 侧栏。
/// 版式：品牌 header + 渐变主视觉 + 白色 Bot 卡片，不在顶栏重复标题。
class BotsScreen extends ConsumerWidget {
  const BotsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(botsProvider);
    return Scaffold(
      appBar: AppBar(
        title: Row(
          children: [
            const BrandMark(size: 26, tile: true),
            const SizedBox(width: AppSpacing.sm + 2),
            Text(context.S.tabBots),
          ],
        ),
        actions: [
          IconButton(
            tooltip: context.S.botsRetry,
            icon: const Icon(Icons.refresh),
            onPressed: () => ref.read(botsProvider.notifier).refresh(),
          ),
          const SizedBox(width: AppSpacing.xs),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _showCreateSheet(context, ref),
        icon: const Icon(Icons.add),
        label: Text(context.S.botsCreate),
      ),
      body: RefreshIndicator(
        onRefresh: () => ref.read(botsProvider.notifier).refresh(),
        child: _buildBody(context, ref, state),
      ),
    );
  }

  Widget _buildBody(BuildContext context, WidgetRef ref, BotsState state) {
    if (state.loading && state.bots.isEmpty) {
      return const SkeletonList(itemCount: 5);
    }
    if (!state.access.enabled) {
      return ListView(
        children: [
          const SizedBox(height: 120),
          EmptyState(
            icon: Icons.block,
            title: state.access.message.isNotEmpty
                ? state.access.message
                : context.S.botsAccessDisabled,
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
              child: Text(context.S.botsRetry),
            ),
          ),
        ],
      );
    }
    if (state.bots.isEmpty) {
      return ListView(
        children: [
          const SizedBox(height: 140),
          EmptyState(
            icon: Icons.smart_toy_outlined,
            title: context.S.botsEmptyTitle,
            hint: context.S.botsEmptyHint,
          ),
        ],
      );
    }
    return ListView.builder(
      padding: const EdgeInsets.only(bottom: 104),
      // header 占一行，卡片从第二行开始。
      itemCount: state.bots.length + 1,
      itemBuilder: (context, index) {
        if (index == 0) {
          return _HomeHeader(botCount: state.bots.length);
        }
        final bot = state.bots[index - 1];
        return Padding(
          padding: const EdgeInsets.fromLTRB(
            AppSpacing.lg,
            0,
            AppSpacing.lg,
            AppSpacing.sm + 2,
          ),
          child: _BotCard(bot: bot, index: index - 1),
        );
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

/// 列表顶部的主视觉：今天、正在跑的活儿、Bot 数量。工作状态接口不可用时
/// 只用本地数据，不让首页卡在加载上。
class _HomeHeader extends ConsumerWidget {
  final int botCount;

  const _HomeHeader({required this.botCount});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final S = context.S;
    final running = ref.watch(_runningCountProvider);
    final busy = running > 0;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        GradientHero(
          icon: busy ? Icons.bolt_rounded : Icons.wb_sunny_outlined,
          eyebrow: S.homeEyebrow,
          title: busy ? S.homeHeroRunning(running) : S.homeHeroBots(botCount),
          hint: busy ? S.homeHeroBots(botCount) : null,
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(
            AppSpacing.lg + AppSpacing.xs,
            AppSpacing.lg,
            AppSpacing.lg + AppSpacing.xs,
            AppSpacing.sm - 2,
          ),
          child: Row(
            children: [
              Text(
                S.homeSectionBots,
                style: TextStyle(
                  fontSize: 12.5,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 0.6,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
              const Spacer(),
              Text(
                '$botCount',
                style: TextStyle(
                  fontSize: 12.5,
                  fontWeight: FontWeight.w600,
                  color: Theme.of(
                    context,
                  ).colorScheme.onSurfaceVariant.withValues(alpha: 0.8),
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// 活动 run 数量 — 接口不可达时返回 0，不阻塞首页。
final _runningCountProvider = Provider<int>((ref) {
  final api = ref.watch(botApiProvider);
  if (api == null) return 0;
  var count = 0;
  for (final bot in ref.watch(botsProvider).bots) {
    final runs = ref.watch(_homeRunsProvider((api, bot.id))).value;
    count += runs?.length ?? 0;
  }
  return count;
});

final _homeRunsProvider = FutureProvider.autoDispose
    .family<List<ActiveRun>, (BotApi, String)>((ref, key) async {
      try {
        return await key.$1.activeRuns(key.$2);
      } on Object {
        return const <ActiveRun>[];
      }
    });

class _BotCard extends ConsumerWidget {
  final Bot bot;
  final int index;

  const _BotCard({required this.bot, this.index = 0});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final meta = <Widget>[
      if (bot.primary)
        AppBadge(
          label: context.S.botsPrimaryMark,
          color: scheme.primary,
          icon: Icons.star_rounded,
        ),
      AppBadge(
        label: desktopModeMark(bot.desktopMode, context.S),
        color: botHasComputer(bot.desktopMode)
            ? scheme.primary
            : scheme.onSurfaceVariant,
        icon: botHasComputer(bot.desktopMode) ? Icons.computer_rounded : null,
      ),
    ];
    return Card(
          clipBehavior: Clip.antiAlias,
          child: InkWell(
            onTap: () => context.push('/bot/${Uri.encodeComponent(bot.id)}'),
            child: Padding(
              padding: const EdgeInsets.fromLTRB(
                AppSpacing.lg,
                AppSpacing.md + 2,
                AppSpacing.md,
                AppSpacing.md + 2,
              ),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  BotAvatar(
                    bot: bot,
                    radius: 25,
                    // 主 Bot 有星点、有电脑的带电脑标识，与下方徽标一致。
                    showAttention: bot.primary,
                  ),
                  const SizedBox(width: AppSpacing.md + 2),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          bot.title,
                          style: theme.textTheme.titleMedium?.copyWith(
                            fontSize: 16.5,
                            fontWeight: FontWeight.w700,
                          ),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                        if (bot.description.isNotEmpty)
                          Padding(
                            padding: const EdgeInsets.only(top: 3),
                            child: Text(
                              bot.description,
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: scheme.onSurfaceVariant,
                                height: 1.35,
                              ),
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        if (meta.isNotEmpty)
                          Padding(
                            padding: const EdgeInsets.only(top: AppSpacing.sm),
                            child: Wrap(spacing: 6, runSpacing: 4, children: meta),
                          ),
                      ],
                    ),
                  ),
                  const SizedBox(width: AppSpacing.xs),
                  // 电脑画面入口保留为卡片上的次级按钮；整张卡片进聊天。
                  Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      if (botHasComputer(bot.desktopMode))
                        IconButton(
                          tooltip: context.S.botsDesktopTooltip,
                          visualDensity: VisualDensity.compact,
                          iconSize: 21,
                          color: scheme.onSurfaceVariant,
                          icon: const Icon(Icons.desktop_windows_outlined),
                          onPressed: () => context.push(
                            '/bot/${Uri.encodeComponent(bot.id)}/desktop',
                          ),
                        ),
                      Icon(
                        Icons.chevron_right,
                        size: 20,
                        color: scheme.onSurfaceVariant,
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        )
        .animate(delay: (index * 45).ms)
        .fadeIn(duration: 260.ms)
        .slideY(
          begin: 0.03,
          end: 0,
          duration: 260.ms,
          curve: Curves.easeOutCubic,
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
  bool _needsComputer = true;

  @override
  void initState() {
    super.initState();
    // Refresh so the computer switch locks against the live grant, not the
    // quota cached by the last list fetch (mirrors the desktop dialog, which
    // re-reads the quota when it opens).
    unawaited(
      Future.microtask(() => ref.read(botsProvider.notifier).refresh()),
    );
    _nameCtrl.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _nameCtrl.dispose();
    _descCtrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final bottom = MediaQuery.of(context).viewInsets.bottom;
    final quota = ref.watch(botsProvider).quota;
    final locked = quota.exhausted || quota.computerMissing;
    final needsComputer = locked ? false : _needsComputer;
    final taken = botNameTaken(_nameCtrl.text, ref.watch(botsProvider).bots);
    final quotaLabel = quota.label(context.S);
    return SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(20, 0, 20, 20 + bottom),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            context.S.botsCreate,
            style: Theme.of(context).textTheme.titleLarge,
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _nameCtrl,
            autofocus: true,
            decoration: InputDecoration(
              labelText: context.S.botsNameLabel,
              hintText: context.S.botsNameHint,
              border: const OutlineInputBorder(),
            ),
            maxLength: 40,
          ),
          TextField(
            controller: _descCtrl,
            decoration: InputDecoration(
              labelText: context.S.botsDescLabel,
              hintText: context.S.botsDescHint,
              border: const OutlineInputBorder(),
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
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: Text(context.S.botsNeedsComputer),
            subtitle: quota.computerMissing
                ? Text(context.S.botsNoComputerQuota)
                : (quotaLabel.isEmpty ? null : Text(quotaLabel)),
            value: needsComputer,
            onChanged: locked || _saving
                ? null
                : (value) => setState(() => _needsComputer = value),
          ),
          Wrap(
            spacing: 6,
            children: [
              for (final shape in botShapes)
                ChoiceChip(
                  label: Text(botShapeLabel(shape, context.S)),
                  visualDensity: VisualDensity.compact,
                  selected: _shape == shape,
                  onSelected: (_) => setState(() => _shape = shape),
                ),
            ],
          ),
          if (taken)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: Text(
                context.S.botsNameTaken,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
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
            onPressed: _saving || taken ? null : _create,
            child: _saving
                ? const SizedBox(
                    width: 20,
                    height: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Text(context.S.botsCreateAction),
          ),
        ],
      ),
    );
  }

  Future<void> _create() async {
    final name = _nameCtrl.text.trim();
    if (name.isEmpty) {
      setState(() => _error = context.S.botsNameRequired);
      return;
    }
    setState(() {
      _saving = true;
      _error = '';
    });
    try {
      final bot = await ref
          .read(botsProvider.notifier)
          .create(
            name,
            _descCtrl.text.trim(),
            color: _color,
            shape: _shape,
            needsComputer:
                _needsComputer &&
                !ref.read(botsProvider).quota.exhausted &&
                !ref.read(botsProvider).quota.computerMissing,
          );
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
