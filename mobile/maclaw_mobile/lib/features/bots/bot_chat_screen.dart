import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../shared/surface.dart';
import '../../shared/theme.dart';
import 'bot.dart';
import 'bot_api.dart';
import 'bot_inline_cards.dart';
import 'bot_message.dart';
import 'bot_rail.dart';
import 'bots_controller.dart';

/// Chat with one bot.
///
/// The composer is always enabled while a turn is running: a bot on a shared
/// desktop can be handed the next instruction while the previous one finishes,
/// and the desktop client behaves the same way. Messages are driven by
/// [botConversationProvider], so the poll survives this widget being rebuilt.
class BotChatScreen extends ConsumerStatefulWidget {
  final Bot bot;

  const BotChatScreen({super.key, required this.bot});

  @override
  ConsumerState<BotChatScreen> createState() => _BotChatScreenState();
}

class _BotChatScreenState extends ConsumerState<BotChatScreen> {
  final TextEditingController _controller = TextEditingController();
  final ScrollController _scrollController = ScrollController();
  bool _sending = false;

  @override
  void dispose() {
    _controller.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  Future<void> _send({BotPhase? phase, String? text}) async {
    final content = (text ?? _controller.text).trim();
    if (content.isEmpty) return;
    setState(() => _sending = true);
    if (text == null) _controller.clear();
    try {
      await ref
          .read(botConversationProvider(widget.bot.id).notifier)
          .send(content, phase: phase);
    } finally {
      if (mounted) setState(() => _sending = false);
      _scrollToBottom();
    }
  }

  void _scrollToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scrollController.hasClients) return;
      final target = _scrollController.position.maxScrollExtent;
      if (target <= 0) return;
      _scrollController.animateTo(
        target,
        duration: const Duration(milliseconds: 220),
        curve: Curves.easeOut,
      );
    });
  }

  @override
  Widget build(BuildContext context) {
    final messages = ref.watch(botConversationProvider(widget.bot.id));
    final activity = ref.watch(botActivityProvider);
    final scheme = Theme.of(context).colorScheme;
    final workState = activity.isRunning(widget.bot.id)
        ? BotWorkState.running
        : BotWorkState.idle;

    ref.listen(botConversationProvider(widget.bot.id), (previous, next) {
      final count = next.valueOrNull?.length ?? 0;
      if (count != previous?.valueOrNull?.length) _scrollToBottom();
    });

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _BotChatHeader(bot: widget.bot, workState: workState),
        Expanded(
          child: messages.when(
            loading: () => const Center(child: CircularProgressIndicator()),
            error: (error, stack) => Center(
              child: Padding(
                padding: const EdgeInsets.all(MaClawColors.spaceXl),
                child: EmptyStatePanel(
                  icon: Icons.cloud_off,
                  title: '无法加载对话',
                  message: botFailureMessage(error),
                  action: FilledButton(
                    onPressed: () => ref.invalidate(
                      botConversationProvider(widget.bot.id),
                    ),
                    child: const Text('重试'),
                  ),
                ),
              ),
            ),
            data: (list) => list.isEmpty
                ? _BotEmptyConversation(
                    bot: widget.bot,
                    enabled: !_sending,
                    onSend: (value) => _send(text: value),
                  )
                : ListView.builder(
                    controller: _scrollController,
                    padding: const EdgeInsets.fromLTRB(
                      MaClawColors.spaceLg,
                      MaClawColors.spaceMd,
                      MaClawColors.spaceLg,
                      MaClawColors.spaceLg,
                    ),
                    itemCount: list.length,
                    itemBuilder: (context, index) {
                      final message = list[index];
                      return _BotMessageRow(
                        message: message,
                        busy: _sending,
                        onConfirmPlan: (value) =>
                            _send(phase: BotPhase.execute, text: value),
                        onSend: (value) => _send(text: value),
                        onAnswer: (answer) async {
                          final notifier = ref.read(
                            botConversationProvider(widget.bot.id).notifier,
                          );
                          final question = message.question;
                          try {
                            await notifier.answerQuestion(question, answer);
                          } on Object catch (error) {
                            if (!context.mounted) return;
                            ScaffoldMessenger.of(context).showSnackBar(
                              SnackBar(content: Text(botFailureMessage(error))),
                            );
                          }
                        },
                      );
                    },
                  ),
          ),
        ),
        _BotComposer(
          controller: _controller,
          enabled: !_sending,
          onSend: _send,
        ),
        if (_sending)
          Padding(
            padding: const EdgeInsets.only(bottom: MaClawColors.spaceSm),
            child: Text(
              '正在发送…',
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
          ),
      ],
    );
  }
}

class _BotChatHeader extends StatelessWidget {
  final Bot bot;
  final BotWorkState workState;

  const _BotChatHeader({required this.bot, required this.workState});

  @override
  Widget build(BuildContext context) {
    final text = Theme.of(context).textTheme;
    return Padding(
      padding: const EdgeInsets.fromLTRB(
        MaClawColors.spaceXl,
        MaClawColors.spaceMd,
        MaClawColors.spaceMd,
        MaClawColors.spaceSm,
      ),
      child: Row(
        children: [
          BotAvatar(bot: bot, size: 34),
          const SizedBox(width: MaClawColors.spaceMd),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  bot.title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style:
                      text.titleMedium?.copyWith(fontWeight: FontWeight.w700),
                ),
                const SizedBox(height: 2),
                BotStatusBadge(state: workState),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _BotEmptyConversation extends StatelessWidget {
  final Bot bot;
  final bool enabled;
  final Future<void> Function(String text) onSend;

  const _BotEmptyConversation({
    required this.bot,
    required this.enabled,
    required this.onSend,
  });

  /// Seeds for a conversation that has not started. A bot with a description
  /// gets prompts that use it, since that is what the creator said it is for.
  static List<String> _suggestionsFor(Bot bot) {
    final description = bot.description.trim();
    if (description.isEmpty) {
      return const ['介绍一下你能做什么', '帮我打开一个网页', '现在桌面状态怎么样'];
    }
    return ['按你的定位做一次自我介绍', '开始工作'];
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final suggestions = _suggestionsFor(bot);
    return ListView(
      padding: const EdgeInsets.all(MaClawColors.spaceXl),
      children: [
        const SizedBox(height: MaClawColors.spaceXl),
        Center(child: BotAvatar(bot: bot, size: 60)),
        const SizedBox(height: MaClawColors.spaceLg),
        Center(
          child: Text(
            bot.title,
            style: Theme.of(context)
                .textTheme
                .titleLarge
                ?.copyWith(fontWeight: FontWeight.w700),
          ),
        ),
        if (bot.description.trim().isNotEmpty) ...[
          const SizedBox(height: MaClawColors.spaceSm),
          Center(
            child: Text(
              bot.description,
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: scheme.onSurfaceVariant,
                    height: 1.4,
                  ),
            ),
          ),
        ],
        const SizedBox(height: MaClawColors.spaceXl),
        Text(
          '试试这样开始',
          style: Theme.of(context).textTheme.labelLarge?.copyWith(
                color: scheme.onSurfaceVariant,
              ),
        ),
        const SizedBox(height: MaClawColors.spaceSm),
        for (final suggestion in suggestions)
          Padding(
            padding: const EdgeInsets.only(bottom: MaClawColors.spaceSm),
            child: OutlinedButton(
              onPressed: enabled ? () => onSend(suggestion) : null,
              child: Align(
                alignment: Alignment.centerLeft,
                child: Text(suggestion),
              ),
            ),
          ),
      ],
    );
  }
}

class _BotMessageRow extends StatelessWidget {
  final BotMessage message;
  final bool busy;
  final Future<void> Function(String) onConfirmPlan;
  final Future<void> Function(String) onSend;
  final Future<void> Function(String) onAnswer;

  const _BotMessageRow({
    required this.message,
    required this.busy,
    required this.onConfirmPlan,
    required this.onSend,
    required this.onAnswer,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;

    if (message.isUser) {
      return Padding(
        padding: const EdgeInsets.only(bottom: MaClawColors.spaceMd),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            ChatBubble(
              text: message.content,
              fromUser: true,
              failed: message.failed,
            ),
            if (message.failed && message.attentionReason.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: MaClawColors.spaceXs),
                child: Text(
                  message.attentionReason,
                  style: text.labelSmall?.copyWith(color: scheme.error),
                ),
              ),
          ],
        ),
      );
    }

    // The plan card and the question card replace the plain body, because both
    // are the reply: they say the same thing and add the control that finishes
    // the turn.
    final isPlan =
        message.phase == BotPhase.plan && message.content.trim().isNotEmpty;
    final hasQuestion = message.question.isAnswerable;
    final needsDesktop = message.needsDesktop;

    return Padding(
      padding: const EdgeInsets.only(bottom: MaClawColors.spaceMd),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (message.content.trim().isNotEmpty)
            isPlan
                ? BotPlanConfirmCard(
                    plan: message.content,
                    busy: busy,
                    onConfirm: onConfirmPlan,
                    onSend: onSend,
                  )
                : ChatBubble(
                    text: message.content,
                    body: MarkdownBody(
                      data: message.content,
                      selectable: true,
                      styleSheet:
                          MarkdownStyleSheet.fromTheme(Theme.of(context)),
                    ),
                  ),
          for (final image in message.images)
            BotImageAttachment(
              image: image,
              onTap: () => copyBotAttachmentToClipboard(image.data),
            ),
          for (final file in message.files)
            BotAttachmentTile(
              file: file,
              onTap: () => copyBotAttachmentToClipboard(file.data),
              onCopy: () => copyBotAttachmentToClipboard(file.data),
            ),
          if (needsDesktop)
            Padding(
              padding: const EdgeInsets.only(top: MaClawColors.spaceSm),
              child: BotDesktopHandoffCard(
                attentionReason: message.attentionReason,
                novncUrl: message.novncUrl,
              ),
            ),
          if (hasQuestion)
            Padding(
              padding: const EdgeInsets.only(top: MaClawColors.spaceSm),
              child: BotQuestionCard(
                question: message.question,
                busy: busy,
                onAnswer: onAnswer,
              ),
            ),
          if (message.pending)
            Padding(
              padding: const EdgeInsets.only(top: MaClawColors.spaceSm),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  SizedBox(
                    width: 12,
                    height: 12,
                    child: CircularProgressIndicator(
                      strokeWidth: 1.8,
                      color: scheme.primary,
                    ),
                  ),
                  const SizedBox(width: MaClawColors.spaceSm),
                  Text(
                    'Bot 正在处理…',
                    style: text.labelSmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _BotComposer extends StatelessWidget {
  final TextEditingController controller;
  final bool enabled;
  final Future<void> Function({BotPhase? phase, String? text}) onSend;

  const _BotComposer({
    required this.controller,
    required this.enabled,
    required this.onSend,
  });

  @override
  Widget build(BuildContext context) {
    return ChatComposerDock(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Expanded(
            child: TextField(
              controller: controller,
              enabled: enabled,
              minLines: 1,
              maxLines: 5,
              textInputAction: TextInputAction.newline,
              decoration: const InputDecoration(
                hintText: '给 Bot 派个任务…',
                isDense: true,
                border: OutlineInputBorder(),
              ),
            ),
          ),
          const SizedBox(width: MaClawColors.spaceSm),
          IconButton.filled(
            onPressed: enabled ? () => onSend() : null,
            icon: const Icon(Icons.send, size: 18),
            tooltip: '发送',
          ),
        ],
      ),
    );
  }
}
