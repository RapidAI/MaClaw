import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../shared/surface.dart';
import '../../shared/theme.dart';
import 'bot.dart';
import 'bot_api.dart';
import 'bot_chat_screen.dart';
import 'bot_create_sheet.dart';
import 'bot_message.dart';
import 'bot_rail.dart';
import 'bots_controller.dart';

/// The Bot tab: a list of bots on the left rail, the selected bot's chat on the
/// right. On a narrow phone the same two pieces become a list that pushes a
/// full-screen chat, which is the shape the platform expects.
class BotsScreen extends ConsumerStatefulWidget {
  const BotsScreen({super.key});

  @override
  ConsumerState<BotsScreen> createState() => _BotsScreenState();
}

class _BotsScreenState extends ConsumerState<BotsScreen> {
  String? _selectedBotId;

  @override
  Widget build(BuildContext context) {
    final access = ref.watch(botAccessProvider);
    final bots = ref.watch(botListProvider);

    return access.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (error, stack) => _BotsUnavailable(
        message: botFailureMessage(error),
        onRetry: () => ref.invalidate(botAccessProvider),
      ),
      data: (value) {
        if (!value.enabled) {
          return _BotsUnavailable(
            // This text comes from whoever administers the tenant. Replacing it
            // with generic copy would hide the reason the feature is off.
            message: value.message.isEmpty
                ? botErrorCodeLabel('BOT_DISABLED')
                : value.message,
            onRetry: () => ref.invalidate(botAccessProvider),
          );
        }
        return bots.when(
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (error, stack) => _BotsUnavailable(
            message: botFailureMessage(error),
            onRetry: () => ref.invalidate(botListProvider),
          ),
          data: (items) => _buildBody(items),
        );
      },
    );
  }

  Widget _buildBody(List<Bot> bots) {
    // Keep the selection valid: a bot deleted elsewhere must not leave the chat
    // pinned to a row that no longer exists.
    final selected = _selectedBotId != null
        ? bots.where((bot) => bot.id == _selectedBotId).firstOrNull
        : null;
    final selectedId = selected?.id;

    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= botWideLayoutBreakpoint;
        final rail = _BotRailPane(
          bots: bots,
          selectedBotId: selectedId,
          onSelect: (botId) {
            setState(() => _selectedBotId = botId);
            if (!wide) _openChatPage(context, botId);
          },
          onCreate: _createBot,
        );
        if (wide) {
          final chat = selected == null
              ? _NoBotSelected(botCount: bots.length, onCreate: _createBot)
              : BotChatScreen(bot: selected);
          return Row(
            children: [
              SizedBox(width: 320, child: rail),
              const VerticalDivider(width: 1),
              Expanded(child: chat),
            ],
          );
        }
        return rail;
      },
    );
  }

  void _openChatPage(BuildContext context, String botId) {
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (context) => _BotChatPage(botId: botId),
      ),
    );
  }

  /// Create a bot, then open its chat.
  ///
  /// Uses the State's own [context] and checks `context.mounted` after each
  /// await: this method outlives the sheet it opened, so a `BuildContext`
  /// captured as a parameter could belong to a widget that has since gone.
  Future<void> _createBot() async {
    // Capture the messenger before the first await. Holding the context
    // instead would let the lint — rightly — worry that it belongs to a widget
    // that has since been unmounted.
    final messenger = ScaffoldMessenger.of(context);
    final navigator = Navigator.of(context);
    final result = await showModalBottomSheet<(String, String)>(
      context: context,
      isScrollControlled: true,
      builder: (context) => const BotCreateSheet(),
    );
    if (result == null) return;
    try {
      final bot = await ref
          .read(botListProvider.notifier)
          .create(name: result.$1, description: result.$2);
      if (!mounted) return;
      setState(() => _selectedBotId = bot.id);
      // On a wide layout the chat already sits beside the rail; on a phone the
      // rail is the whole page, so the chat has to be pushed.
      if (MediaQuery.sizeOf(context).width < botWideLayoutBreakpoint) {
        navigator.push(
          MaterialPageRoute<void>(
            builder: (context) => _BotChatPage(botId: bot.id),
          ),
        );
      }
    } on Object catch (error) {
      if (!mounted) return;
      messenger.showSnackBar(
        SnackBar(content: Text(botFailureMessage(error))),
      );
    }
  }
}

/// Chat pushed as its own route on a phone.
///
/// It reads the bot from the list so a rename made in the rail is reflected
/// here without passing a stale copy down the navigator.
class _BotChatPage extends ConsumerWidget {
  final String botId;

  const _BotChatPage({required this.botId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final bots = ref.watch(botListProvider).valueOrNull ?? const <Bot>[];
    final bot = bots.where((item) => item.id == botId).firstOrNull;
    if (bot == null) {
      return Scaffold(
        appBar: AppBar(title: const Text('Bot')),
        body: const Center(child: Text('该 Bot 已不存在')),
      );
    }
    return Scaffold(
      appBar: AppBar(
        title: Text(bot.title),
        actions: [
          IconButton(
            tooltip: '编辑',
            icon: const Icon(Icons.edit_outlined),
            onPressed: () => _renameBot(context, ref, bot),
          ),
          IconButton(
            tooltip: '删除',
            icon: const Icon(Icons.delete_outline),
            onPressed: () => _deleteBot(context, ref, bot),
          ),
        ],
      ),
      body: SafeArea(child: BotChatScreen(bot: bot)),
    );
  }

  static Future<void> _renameBot(
    BuildContext context,
    WidgetRef ref,
    Bot bot,
  ) async {
    final result = await showModalBottomSheet<(String, String)>(
      context: context,
      isScrollControlled: true,
      builder: (context) => BotCreateSheet(existing: bot),
    );
    if (result == null) return;
    try {
      await ref
          .read(botListProvider.notifier)
          .rename(bot.id, name: result.$1, description: result.$2);
    } on Object catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(botFailureMessage(error))),
      );
    }
  }

  static Future<void> _deleteBot(
    BuildContext context,
    WidgetRef ref,
    Bot bot,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除 Bot'),
        content: Text('确定删除「${bot.title}」？聊天记录也会一并清除。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await ref.read(botListProvider.notifier).remove(bot.id);
      if (!context.mounted) return;
      Navigator.of(context).maybePop();
    } on Object catch (error) {
      if (!context.mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(botFailureMessage(error))),
      );
    }
  }
}

class _BotRailPane extends ConsumerWidget {
  final List<Bot> bots;
  final String? selectedBotId;
  final ValueChanged<String> onSelect;
  final VoidCallback onCreate;

  const _BotRailPane({
    required this.bots,
    required this.selectedBotId,
    required this.onSelect,
    required this.onCreate,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final activity = ref.watch(botActivityProvider);
    final latest = ref.watch(botLatestMessagesProvider).valueOrNull ??
        const <String, BotMessage>{};
    final text = Theme.of(context).textTheme;
    final scheme = Theme.of(context).colorScheme;

    // The newest message of each bot is the rail preview, so a bot that is
    // mid-task shows what it is doing without being opened. A bot that never
    // talked falls back to its description.
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(
            MaClawColors.spaceLg,
            MaClawColors.spaceMd,
            MaClawColors.spaceSm,
            MaClawColors.spaceSm,
          ),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  'Bot',
                  style: text.headlineSmall?.copyWith(
                    fontWeight: FontWeight.w700,
                    letterSpacing: -0.01,
                  ),
                ),
              ),
              IconButton(
                tooltip: '新建 Bot',
                onPressed: onCreate,
                icon: const Icon(Icons.add_circle_outline),
              ),
            ],
          ),
        ),
        Expanded(
          child: bots.isEmpty
              ? Padding(
                  padding: const EdgeInsets.all(MaClawColors.spaceLg),
                  child: EmptyStatePanel(
                    icon: Icons.smart_toy_outlined,
                    title: '还没有 Bot',
                    message: '创建一个 Bot，让它在自己的云端桌面上替你干活。',
                    action: FilledButton.icon(
                      onPressed: onCreate,
                      icon: const Icon(Icons.add, size: 18),
                      label: const Text('新建 Bot'),
                    ),
                  ),
                )
              : RefreshIndicator(
                  onRefresh: () => ref.read(botListProvider.notifier).refresh(),
                  child: ListView.builder(
                    padding: const EdgeInsets.symmetric(
                      horizontal: MaClawColors.spaceSm,
                      vertical: MaClawColors.spaceXs,
                    ),
                    itemCount: bots.length,
                    itemBuilder: (context, index) {
                      final bot = bots[index];
                      final lastMessage = latest[bot.id];
                      final state = activity.isRunning(bot.id)
                          ? BotWorkState.running
                          : activity.needsUser(bot.id)
                              ? BotWorkState.waitingForUser
                              : BotWorkState.idle;
                      return BotRailCard(
                        bot: bot,
                        state: state,
                        selected: bot.id == selectedBotId,
                        preview: botRailPreview(lastMessage).isEmpty
                            ? bot.description
                            : botRailPreview(lastMessage),
                        onTap: () => onSelect(bot.id),
                      );
                    },
                  ),
                ),
        ),
        if (bots.isNotEmpty)
          Padding(
            padding: const EdgeInsets.fromLTRB(
              MaClawColors.spaceLg,
              0,
              MaClawColors.spaceLg,
              MaClawColors.spaceMd,
            ),
            child: Text(
              '共 ${bots.length} 个 Bot',
              style: text.labelSmall?.copyWith(color: scheme.onSurfaceVariant),
            ),
          ),
      ],
    );
  }
}

class _NoBotSelected extends StatelessWidget {
  final int botCount;
  final VoidCallback onCreate;

  const _NoBotSelected({required this.botCount, required this.onCreate});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(MaClawColors.spaceXl),
        child: botCount == 0
            ? EmptyStatePanel(
                icon: Icons.smart_toy_outlined,
                title: '还没有 Bot',
                message: '创建一个 Bot，让它在自己的云端桌面上替你干活。',
                action: FilledButton.icon(
                  onPressed: onCreate,
                  icon: const Icon(Icons.add, size: 18),
                  label: const Text('新建 Bot'),
                ),
              )
            : const EmptyStatePanel(
                icon: Icons.forum_outlined,
                title: '选择一个 Bot',
                message: '在左侧选择一个 Bot 开始对话。',
              ),
      ),
    );
  }
}

/// Shown when bots are unavailable, with the reason and a way to retry.
class _BotsUnavailable extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _BotsUnavailable({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(MaClawColors.spaceXl),
        child: EmptyStatePanel(
          icon: Icons.lock_outline,
          title: 'Bot 功能不可用',
          message: message,
          action: OutlinedButton.icon(
            onPressed: onRetry,
            icon: const Icon(Icons.refresh, size: 18),
            label: const Text('重试'),
          ),
        ),
      ),
    );
  }
}

/// Width at which the rail and the chat sit side by side.
///
/// Below it the rail is the whole page and a bot opens as a pushed route, so
/// both the layout and the navigation decision read this one threshold.
const double botWideLayoutBreakpoint = 720;

/// Widest preview a rail row shows before it truncates.
const int botRailPreviewMaxLength = 60;

/// Shorten a message for the rail preview: one line, no newlines, clipped.
String botRailPreview(BotMessage? message) {
  final raw = message?.content.trim() ?? '';
  if (raw.isEmpty) {
    if (message?.failed ?? false) return '上一条消息发送失败';
    return '';
  }
  final flattened = raw.replaceAll(RegExp(r'\s+'), ' ');
  if (flattened.length <= botRailPreviewMaxLength) return flattened;
  return '${flattened.substring(0, botRailPreviewMaxLength)}…';
}
