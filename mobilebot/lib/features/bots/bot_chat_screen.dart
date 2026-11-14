import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';
import 'package:speech_to_text/speech_to_text.dart' as stt;
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'bot_chat_controller.dart';
import 'bot_models.dart';
import 'bots_controller.dart';
import 'widgets.dart';

/// One bot's workspace — the mobile version of the desktop GUI's
/// DesktopBotWorkspace: chat transcript + composer, with the desktop view,
/// work status and settings one tap away.
class BotChatScreen extends ConsumerStatefulWidget {
  final Bot bot;

  const BotChatScreen({super.key, required this.bot});

  @override
  ConsumerState<BotChatScreen> createState() => _BotChatScreenState();
}

class _BotChatScreenState extends ConsumerState<BotChatScreen> {
  final _inputCtrl = TextEditingController();
  final _scrollCtrl = ScrollController();
  final _speech = stt.SpeechToText();
  double? _uploadProgress;
  String _uploadingName = '';
  bool _listening = false;
  String _speechPrefix = '';

  @override
  void dispose() {
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    _speech.stop();
    super.dispose();
  }

  /// On-device speech-to-text: words land straight in the composer so the
  /// person can edit before sending.
  Future<void> _toggleListening() async {
    if (_listening) {
      await _speech.stop();
      if (mounted) setState(() => _listening = false);
      return;
    }
    final ok = await _speech.initialize(
      onError: (_) {
        if (mounted) setState(() => _listening = false);
      },
      onStatus: (status) {
        if (mounted && (status == 'done' || status == 'notListening')) {
          setState(() => _listening = false);
        }
      },
    );
    if (!ok || !mounted) return;
    _speechPrefix = _inputCtrl.text;
    setState(() => _listening = true);
    await _speech.listen(
      listenOptions: stt.SpeechListenOptions(localeId: 'zh_CN'),
      onResult: (result) {
        _inputCtrl.text = _speechPrefix.isEmpty
            ? result.recognizedWords
            : '$_speechPrefix ${result.recognizedWords}';
        _inputCtrl.selection = TextSelection.collapsed(
          offset: _inputCtrl.text.length,
        );
      },
    );
  }

  /// Add a supplementary instruction to the run still in flight
  /// (`POST /runs/{id}/supplement`) and toast the outcome Hub reports.
  Future<void> _supplement() async {
    final ctrl = TextEditingController();
    String? text;
    try {
      text = await showDialog<String>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('给当前任务补充说明'),
          content: TextField(
            controller: ctrl,
            autofocus: true,
            minLines: 1,
            maxLines: 4,
            decoration: const InputDecoration(
              hintText: '例如：只要前 5 条结果',
              border: OutlineInputBorder(),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, ctrl.text),
              child: const Text('追加'),
            ),
          ],
        ),
      );
    } finally {
      ctrl.dispose();
    }
    if (text == null || text.trim().isEmpty || !mounted) return;
    final result = await ref
        .read(botChatProvider.notifier)
        .supplement(text.trim());
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          result == null
              ? ref.read(botChatProvider).error.isNotEmpty
                    ? ref.read(botChatProvider).error
                    : '追加失败'
              : result.accepted
              ? '已补充到当前任务'
              : (result.detail.isNotEmpty ? result.detail : '任务已结束，补充未送达'),
        ),
      ),
    );
  }

  /// Pick files and push them to the bot's desktop as material
  /// (`POST /bots/{id}/desktop/files`, chunked). The landing path is left
  /// in the composer so the person can send it with context.
  Future<void> _attach() async {
    final api = ref.read(botApiProvider);
    if (api == null || _uploadProgress != null) return;
    final picked = await FilePicker.pickFiles();
    if (picked.isEmpty || !mounted) return;
    for (final file in picked) {
      // Path-backed picks stream from disk (no full-file buffer); content
      // URIs without a path still need one in-memory read.
      File? disk;
      final localPath = file.path;
      if (localPath != null) {
        final candidate = File(localPath);
        try {
          disk = await candidate.length() > 0 ? candidate : null;
        } on Object {
          disk = null;
        }
      }
      Uint8List? bytes;
      if (disk == null) {
        try {
          bytes = await file.readAsBytes();
        } on Object {
          bytes = null;
        }
        if (bytes == null || bytes.isEmpty) continue;
      }
      if (!mounted) return;
      setState(() {
        _uploadingName = file.name;
        _uploadProgress = 0;
      });
      try {
        void onProgress(int sent, int total) {
          if (mounted && total > 0) {
            setState(() => _uploadProgress = sent / total);
          }
        }

        final path = disk != null
            ? await api.uploadMaterialFile(
                widget.bot.id,
                file.name,
                disk,
                onProgress: onProgress,
              )
            : await api.uploadMaterial(
                widget.bot.id,
                file.name,
                bytes!,
                onProgress: onProgress,
              );
        if (!mounted) return;
        final line = path.isNotEmpty
            ? path
            : '~/素材/${widget.bot.name}/${file.name}';
        final current = _inputCtrl.text;
        _inputCtrl.text = current.isEmpty
            ? '素材已传送到 $line'
            : '$current\n素材已传送到 $line';
      } on Object catch (error) {
        if (mounted) {
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text('${file.name} 上传失败：$error')));
        }
      }
    }
    if (mounted) {
      setState(() {
        _uploadProgress = null;
        _uploadingName = '';
      });
    }
  }

  void _jumpToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scrollCtrl.hasClients) return;
      // Standard chat convention: only follow the transcript when the
      // person is already near the bottom — someone reading older history
      // must not be yanked down on every long-poll merge.
      final position = _scrollCtrl.position;
      if (position.maxScrollExtent - position.pixels > 400) return;
      _scrollCtrl.animateTo(
        position.maxScrollExtent,
        duration: const Duration(milliseconds: 200),
        curve: Curves.easeOut,
      );
    });
  }

  @override
  Widget build(BuildContext context) {
    // Keep the transcript pinned to the newest message as it grows.
    ref.listen<int>(chatScrollTickProvider, (_, _) => _jumpToBottom());
    final state = ref.watch(botChatProvider);
    return Scaffold(
      appBar: AppBar(
        titleSpacing: 0,
        title: Row(
          children: [
            BotAvatar(bot: widget.bot, radius: 17),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    widget.bot.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  Text(
                    (state.sending || state.activeRunId != null)
                        ? '正在处理…'
                        : '在线',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: (state.sending || state.activeRunId != null)
                          ? Theme.of(context).colorScheme.primary
                          : Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: '电脑画面',
            icon: const Icon(Icons.desktop_windows_outlined),
            onPressed: () => context.push(
              '/bot/${Uri.encodeComponent(widget.bot.id)}/desktop',
            ),
          ),
          IconButton(
            tooltip: '任务状态',
            icon: const Icon(Icons.fact_check_outlined),
            onPressed: () => context.push(
              '/bot/${Uri.encodeComponent(widget.bot.id)}/status',
            ),
          ),
          IconButton(
            tooltip: '设置',
            icon: const Icon(Icons.settings_outlined),
            onPressed: () => context.push(
              '/bot/${Uri.encodeComponent(widget.bot.id)}/settings',
            ),
          ),
        ],
      ),
      body: Column(
        children: [
          if (state.error.isNotEmpty)
            MaterialBanner(
              content: Text(state.error),
              actions: [
                TextButton(
                  onPressed: () =>
                      ref.read(botChatProvider.notifier).clearError(),
                  child: const Text('知道了'),
                ),
              ],
            ),
          Expanded(
            child: state.loadingHistory
                ? const Center(child: CircularProgressIndicator())
                : _MessageList(scrollCtrl: _scrollCtrl, bot: widget.bot),
          ),
          if (_uploadProgress != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 4, 16, 0),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '上传 $_uploadingName…',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                  LinearProgressIndicator(value: _uploadProgress),
                ],
              ),
            ),
          if (state.continueThread.isNotEmpty)
            Material(
              color: Theme.of(context).colorScheme.secondaryContainer,
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 14,
                  vertical: 4,
                ),
                child: Row(
                  children: [
                    const Icon(Icons.replay, size: 16),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        '下一条消息将继续该任务线程',
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Theme.of(
                            context,
                          ).colorScheme.onSecondaryContainer,
                        ),
                      ),
                    ),
                    TextButton(
                      onPressed: () =>
                          ref.read(botChatProvider.notifier).continueTask(''),
                      child: const Text('取消', style: TextStyle(fontSize: 12)),
                    ),
                  ],
                ),
              ),
            ),
          _Composer(
            inputCtrl: _inputCtrl,
            sending: state.sending,
            canSupplement: state.activeRunId != null,
            onAttach: _uploadProgress == null ? _attach : null,
            listening: _listening,
            onMic: _toggleListening,
            onSend: () {
              final text = _inputCtrl.text;
              if (text.trim().isEmpty) return;
              _inputCtrl.clear();
              ref.read(botChatProvider.notifier).send(text);
            },
            onSupplement: _supplement,
            onCancel: () =>
                ref.read(botChatProvider.notifier).cancelActiveRun(),
          ),
        ],
      ),
    );
  }
}

/// Bumped whenever the transcript should scroll — driven by message count.
// Declared dependencies keep this scoped into the bot route's ProviderScope
// together with botChatProvider instead of mounting at the root container.
final chatScrollTickProvider = Provider<int>((ref) {
  final state = ref.watch(botChatProvider);
  return state.messages.length * 100000 +
      state.messages.fold<int>(0, (acc, m) => acc + m.content.length);
}, dependencies: [botChatProvider]);

class _MessageList extends ConsumerWidget {
  final ScrollController scrollCtrl;
  final Bot bot;

  const _MessageList({required this.scrollCtrl, required this.bot});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final messages = ref.watch(botChatProvider).messages;
    if (messages.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Text(
            '和 ${bot.title} 说点什么开始吧',
            style: TextStyle(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ),
      );
    }
    return ListView.builder(
      controller: scrollCtrl,
      padding: const EdgeInsets.fromLTRB(12, 12, 12, 8),
      itemCount: messages.length,
      itemBuilder: (context, index) => _MessageBubble(
        message: messages[index],
        bot: bot,
        // foldPlanConfirmation: a later confirm sentence folds the plan's
        // run-it chip — the arrangement was already approved once.
        planConfirmed: messages
            .skip(index + 1)
            .any((m) => m.isUser && m.content == botConfirmTaskText),
      ),
    );
  }
}

class _MessageBubble extends ConsumerWidget {
  final ChatMessage message;
  final Bot bot;
  final bool planConfirmed;

  const _MessageBubble({
    required this.message,
    required this.bot,
    this.planConfirmed = false,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final isUser = message.isUser;
    final bubbleColor = message.failed
        ? theme.colorScheme.errorContainer
        : isUser
        ? theme.colorScheme.primary
        : theme.colorScheme.surfaceContainerHigh;
    final textColor = isUser
        ? theme.colorScheme.onPrimary
        : theme.colorScheme.onSurface;
    final bubble = Container(
        constraints: BoxConstraints(
          maxWidth: MediaQuery.of(context).size.width * 0.78,
        ),
        margin: const EdgeInsets.only(bottom: 8),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
        decoration: BoxDecoration(
          color: bubbleColor,
          borderRadius: BorderRadius.only(
            topLeft: const Radius.circular(16),
            topRight: const Radius.circular(16),
            bottomLeft: Radius.circular(isUser ? 16 : 4),
            bottomRight: Radius.circular(isUser ? 4 : 16),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (message.content.isNotEmpty)
              isUser
                  ? LinkifiedText(
                      message.content,
                      style: TextStyle(color: textColor, height: 1.4),
                      linkStyle: TextStyle(
                        color: textColor,
                        height: 1.4,
                        fontWeight: FontWeight.w600,
                        decoration: TextDecoration.underline,
                        decorationColor: textColor,
                      ),
                    )
                  : MarkdownBody(
                      data: linkifyMarkdownUrls(message.content),
                      selectable: true,
                      onTapLink: (text, href, title) {
                        if (href != null) openExternalUrl(href);
                      },
                      styleSheet: MarkdownStyleSheet.fromTheme(theme).copyWith(
                        p: theme.textTheme.bodyMedium?.copyWith(
                          color: textColor,
                          height: 1.45,
                        ),
                        a: theme.textTheme.bodyMedium?.copyWith(
                          color: theme.colorScheme.primary,
                          fontWeight: FontWeight.w600,
                          decoration: TextDecoration.underline,
                        ),
                      ),
                    ),
            if (message.pending)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    SizedBox(
                      width: 12,
                      height: 12,
                      child: CircularProgressIndicator(
                        strokeWidth: 1.5,
                        color: textColor,
                      ),
                    ),
                    const SizedBox(width: 6),
                    Text(
                      '发送中…',
                      style: TextStyle(fontSize: 11, color: textColor),
                    ),
                  ],
                ),
              ),
            if (message.queued && !message.failed)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  _queueTag(message),
                  style: TextStyle(fontSize: 11, color: textColor),
                ),
              ),
            if (message.failed)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      '发送失败',
                      style: TextStyle(
                        fontSize: 11,
                        color: theme.colorScheme.error,
                      ),
                    ),
                    TextButton(
                      onPressed: () =>
                          ref.read(botChatProvider.notifier).retry(message),
                      style: TextButton.styleFrom(
                        padding: const EdgeInsets.symmetric(horizontal: 8),
                        minimumSize: Size.zero,
                        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      ),
                      child: const Text('重发', style: TextStyle(fontSize: 11)),
                    ),
                  ],
                ),
              ),
            if (!isUser &&
                !message.failed &&
                !planConfirmed &&
                message.phase == 'plan' &&
                message.content.isNotEmpty)
              Align(
                alignment: Alignment.centerLeft,
                child: ActionChip(
                  avatar: const Icon(Icons.play_arrow, size: 16),
                  label: const Text('按这个安排执行', style: TextStyle(fontSize: 12)),
                  onPressed: () => ref
                      .read(botChatProvider.notifier)
                      .send(botConfirmTaskText, phase: BotPhase.execute),
                ),
              ),
            if (!isUser && message.conversation.startsWith('${bot.id}#'))
              Align(
                alignment: Alignment.centerLeft,
                child: ActionChip(
                  avatar: const Icon(Icons.replay, size: 16),
                  label: const Text('继续这个任务', style: TextStyle(fontSize: 12)),
                  onPressed: () => ref
                      .read(botChatProvider.notifier)
                      .continueTask(message.conversation),
                ),
              ),
            for (final image in message.images) _ReplyImage(image: image),
            for (final file in message.files) _ReplyFileChip(file: file),
            if (message.question.isAnswerable && !message.askResolved)
              _AskCard(message: message),
            if (message.options.isNotEmpty && !message.optionsUsed)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Wrap(
                  spacing: 8,
                  runSpacing: 6,
                  children: [
                    for (final option in message.options)
                      ActionChip(
                        label: Text(option),
                        onPressed: () => ref
                            .read(botChatProvider.notifier)
                            .pickOption(message, option),
                      ),
                  ],
                ),
              ),
            if (message.handoffUrl.isNotEmpty ||
                message.attentionReason.isNotEmpty)
              _HandoffBanner(message: message, bot: bot),
          ],
        ),
      );
    if (isUser) {
      return Align(alignment: Alignment.centerRight, child: bubble);
    }
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(top: 2),
          child: BotAvatar(bot: bot, radius: 14),
        ),
        const SizedBox(width: 8),
        Flexible(child: bubble),
      ],
    );
  }
}

class _ReplyImage extends StatelessWidget {
  final BotReplyImage image;

  const _ReplyImage({required this.image});

  @override
  Widget build(BuildContext context) {
    final bytes = _decodeImage(image.data);
    if (bytes == null) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(8),
        child: GestureDetector(
          onTap: () => showDialog(
            context: context,
            builder: (_) =>
                Dialog(child: InteractiveViewer(child: Image.memory(bytes))),
          ),
          child: Image.memory(bytes, fit: BoxFit.cover),
        ),
      ),
    );
  }

  Uint8List? _decodeImage(String data) {
    try {
      return base64Decode(data);
    } on Object {
      return null;
    }
  }
}

class _ReplyFileChip extends StatelessWidget {
  final BotReplyFile file;

  const _ReplyFileChip({required this.file});

  Future<void> _open(BuildContext context) async {
    if (file.data.isEmpty) return;
    try {
      final bytes = base64Decode(file.data);
      final dir = await getTemporaryDirectory();
      final safe = file.displayName.replaceAll(RegExp(r'[\\/:*?"<>|]'), '_');
      final path = '${dir.path}${Platform.pathSeparator}$safe';
      await File(path).writeAsBytes(bytes, flush: true);
      if (!context.mounted) return;
      await SharePlus.instance.share(
        ShareParams(files: [
          XFile(path, mimeType: file.mime.isNotEmpty ? file.mime : null),
        ]),
      );
    } on Object {
      if (context.mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('文件解析失败')));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: ActionChip(
        avatar: const Icon(Icons.attach_file, size: 18),
        label: Text(
          file.displayName,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        onPressed: file.data.isEmpty ? null : () => _open(context),
      ),
    );
  }
}

/// The bot's question — a text field for free answers/secrets, or option
/// chips for a choice. Secrets are sent via secret-fill, not the transcript.
class _AskCard extends ConsumerStatefulWidget {
  final ChatMessage message;

  const _AskCard({required this.message});

  @override
  ConsumerState<_AskCard> createState() => _AskCardState();
}

class _AskCardState extends ConsumerState<_AskCard> {
  final _answerCtrl = TextEditingController();

  @override
  void dispose() {
    _answerCtrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final question = widget.message.question;
    return Container(
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(
                question.isSecret ? Icons.key : Icons.help_outline,
                size: 16,
                color: theme.colorScheme.primary,
              ),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  question.question.isNotEmpty
                      ? question.question
                      : question.isSecret
                      ? '需要输入 ${question.secretName}'
                      : '需要你的回答',
                  style: theme.textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
            ],
          ),
          if (question.isChoice) ...[
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 6,
              children: [
                for (final option in question.options)
                  ActionChip(
                    label: Text(option),
                    onPressed: () {
                      ref
                          .read(botChatProvider.notifier)
                          .answerQuestion(widget.message, option);
                    },
                  ),
              ],
            ),
          ] else ...[
            const SizedBox(height: 8),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _answerCtrl,
                    obscureText: question.isSecret,
                    decoration: InputDecoration(
                      isDense: true,
                      hintText: question.isSecret ? '输入密钥（不会显示在聊天中）' : '输入回答',
                      border: const OutlineInputBorder(),
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                FilledButton(
                  onPressed: () {
                    final value = _answerCtrl.text.trim();
                    if (value.isEmpty) return;
                    _answerCtrl.clear();
                    ref
                        .read(botChatProvider.notifier)
                        .answerQuestion(widget.message, value);
                  },
                  child: const Text('提交'),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

/// The bot handed its desktop to the person — jump straight to the screen.
class _HandoffBanner extends StatelessWidget {
  final ChatMessage message;
  final Bot bot;

  const _HandoffBanner({required this.message, required this.bot});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: theme.colorScheme.tertiaryContainer,
        borderRadius: BorderRadius.circular(10),
      ),
      child: Row(
        children: [
          Icon(
            Icons.desktop_windows,
            size: 18,
            color: theme.colorScheme.onTertiaryContainer,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message.attentionReason.isNotEmpty
                  ? message.attentionReason
                  : 'Bot 把电脑交给你了',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onTertiaryContainer,
              ),
            ),
          ),
          TextButton(
            onPressed: () =>
                context.push('/bot/${Uri.encodeComponent(bot.id)}/desktop'),
            child: const Text('去查看'),
          ),
        ],
      ),
    );
  }
}

class _Composer extends StatelessWidget {
  final TextEditingController inputCtrl;
  final bool sending;
  final bool canSupplement;
  final VoidCallback? onAttach;
  final bool listening;
  final VoidCallback onMic;
  final VoidCallback onSend;
  final VoidCallback onSupplement;
  final VoidCallback onCancel;

  const _Composer({
    required this.inputCtrl,
    required this.sending,
    required this.canSupplement,
    required this.onAttach,
    required this.listening,
    required this.onMic,
    required this.onSend,
    required this.onSupplement,
    required this.onCancel,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(10, 6, 10, 8),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            Expanded(
              child: Container(
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerHigh,
                  borderRadius: BorderRadius.circular(26),
                ),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: [
                    IconButton(
                      onPressed: onAttach,
                      visualDensity: VisualDensity.compact,
                      icon: const Icon(Icons.attach_file),
                      tooltip: '传送文件到 Bot 的电脑',
                    ),
                    Expanded(
                      child: TextField(
                        controller: inputCtrl,
                        minLines: 1,
                        maxLines: 5,
                        textInputAction: TextInputAction.newline,
                        decoration: const InputDecoration(
                          hintText: '给 Bot 下达任务…',
                          border: InputBorder.none,
                          contentPadding: EdgeInsets.symmetric(
                            horizontal: 2,
                            vertical: 12,
                          ),
                        ),
                      ),
                    ),
                    IconButton(
                      onPressed: onMic,
                      visualDensity: VisualDensity.compact,
                      icon: Icon(
                        listening ? Icons.mic : Icons.mic_none,
                        color: listening ? scheme.error : null,
                      ),
                      tooltip: listening ? '停止语音输入' : '语音输入',
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(width: 6),
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (canSupplement)
                  IconButton(
                    onPressed: onSupplement,
                    icon: const Icon(Icons.playlist_add),
                    tooltip: '给当前任务补充说明',
                  ),
                // The send button stays usable while a run executes — the
                // Hub queues the message (`queued` reply) behind the turn.
                IconButton.filled(
                  onPressed: sending ? null : onSend,
                  icon: sending
                      ? SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(
                            strokeWidth: 2,
                            color: scheme.onPrimary,
                          ),
                        )
                      : const Icon(Icons.send),
                  tooltip: '发送',
                ),
                if (canSupplement)
                  IconButton(
                    onPressed: onCancel,
                    icon: const Icon(Icons.stop),
                    tooltip: '停止当前任务',
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

String _queueTag(ChatMessage message) {
  // held = the person still holds the desktop keyboard (user_control).
  if (message.queueHeld) return '已暂存（电脑正被你接管中）';
  if (message.queuePosition > 0) return '排队中 · 第 ${message.queuePosition} 位';
  return '排队中，Bot 忙完后处理';
}
