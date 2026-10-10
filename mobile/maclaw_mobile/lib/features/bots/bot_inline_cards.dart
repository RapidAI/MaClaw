import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../shared/theme.dart';
import 'bot.dart';

/// Confirmation card for a plan reply.
///
/// A `plan` turn proposes an arrangement that the bot will not carry out on its
/// own. The person has to approve it, and the approval is sent back as the same
/// content with the `execute` phase, so the bot continues from its own plan
/// rather than from a re-typed instruction.
class BotPlanConfirmCard extends StatefulWidget {
  final String plan;
  final bool busy;
  final Future<void> Function(String message) onConfirm;
  final Future<void> Function(String message) onSend;

  const BotPlanConfirmCard({
    super.key,
    required this.plan,
    required this.busy,
    required this.onConfirm,
    required this.onSend,
  });

  @override
  State<BotPlanConfirmCard> createState() => _BotPlanConfirmCardState();
}

class _BotPlanConfirmCardState extends State<BotPlanConfirmCard> {
  final TextEditingController _controller = TextEditingController();
  bool _expanded = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    return Card(
      margin: EdgeInsets.zero,
      color: scheme.secondaryContainer.withValues(alpha: 0.5),
      child: Padding(
        padding: const EdgeInsets.all(MaClawColors.spaceMd),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.assignment_turned_in_outlined,
                  size: 18,
                  color: scheme.primary,
                ),
                const SizedBox(width: MaClawColors.spaceSm),
                Text(
                  'Bot 给出了执行方案',
                  style: text.titleSmall?.copyWith(fontWeight: FontWeight.w600),
                ),
              ],
            ),
            const SizedBox(height: MaClawColors.spaceSm),
            if (_expanded)
              SelectableText(
                widget.plan,
                style: text.bodyMedium?.copyWith(height: 1.4),
              )
            else
              Text(
                widget.plan,
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
                style: text.bodyMedium?.copyWith(height: 1.4),
              ),
            Align(
              alignment: Alignment.centerRight,
              child: TextButton(
                onPressed: () => setState(() => _expanded = !_expanded),
                child: Text(_expanded ? '收起' : '展开'),
              ),
            ),
            const SizedBox(height: MaClawColors.spaceXs),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton.icon(
                    onPressed: widget.busy
                        ? null
                        : () async {
                            final value = _controller.text.trim();
                            if (value.isNotEmpty) {
                              await widget.onSend(value);
                              _controller.clear();
                              return;
                            }
                            await widget.onConfirm(widget.plan);
                          },
                    icon: const Icon(Icons.check, size: 18),
                    label: const Text('确认执行'),
                  ),
                ),
                const SizedBox(width: MaClawColors.spaceSm),
                Expanded(
                  child: TextButton(
                    onPressed: widget.busy ? null : () => _controller.clear(),
                    child: const Text('重新安排'),
                  ),
                ),
              ],
            ),
            TextField(
              controller: _controller,
              enabled: !widget.busy,
              decoration: const InputDecoration(
                hintText: '想改一改？在这里补充说明',
                isDense: true,
                border: OutlineInputBorder(),
              ),
              onSubmitted: widget.busy
                  ? null
                  : (value) {
                      final trimmed = value.trim();
                      if (trimmed.isEmpty) return;
                      widget.onSend(trimmed);
                      _controller.clear();
                    },
            ),
          ],
        ),
      ),
    );
  }
}

/// Question card: a free-text question, a choice list, or a secret prompt.
class BotQuestionCard extends StatefulWidget {
  final BotQuestion question;
  final bool busy;
  final Future<void> Function(String answer) onAnswer;

  const BotQuestionCard({
    super.key,
    required this.question,
    required this.busy,
    required this.onAnswer,
  });

  @override
  State<BotQuestionCard> createState() => _BotQuestionCardState();
}

class _BotQuestionCardState extends State<BotQuestionCard> {
  late final TextEditingController _controller =
      TextEditingController(text: widget.question.question);
  bool _revealSecret = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final question = widget.question;
    // A choice is answered by tapping; a secret needs a masked field; anything
    // else is a plain question.
    if (question.isChoice && !question.isSecret) {
      return Card(
        margin: EdgeInsets.zero,
        color: scheme.tertiaryContainer.withValues(alpha: 0.45),
        child: Padding(
          padding: const EdgeInsets.all(MaClawColors.spaceMd),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (question.question.isNotEmpty) ...[
                Text(
                  question.question,
                  style: text.bodyMedium?.copyWith(height: 1.4),
                ),
                const SizedBox(height: MaClawColors.spaceSm),
              ],
              for (final option in question.options)
                Padding(
                  padding: const EdgeInsets.only(bottom: MaClawColors.spaceXs),
                  child: OutlinedButton(
                    onPressed:
                        widget.busy ? null : () => widget.onAnswer(option),
                    child: Align(
                      alignment: Alignment.centerLeft,
                      child: Text(option),
                    ),
                  ),
                ),
            ],
          ),
        ),
      );
    }
    return Card(
      margin: EdgeInsets.zero,
      color: scheme.tertiaryContainer.withValues(alpha: 0.45),
      child: Padding(
        padding: const EdgeInsets.all(MaClawColors.spaceMd),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  question.isSecret ? Icons.key_outlined : Icons.help_outline,
                  size: 18,
                  color: scheme.tertiary,
                ),
                const SizedBox(width: MaClawColors.spaceSm),
                Expanded(
                  child: Text(
                    question.isSecret
                        ? 'Bot 需要你填写：${question.secretName}'
                        : (question.question.isEmpty
                            ? 'Bot 需要你的确认'
                            : question.question),
                    style:
                        text.titleSmall?.copyWith(fontWeight: FontWeight.w600),
                  ),
                ),
              ],
            ),
            const SizedBox(height: MaClawColors.spaceSm),
            TextField(
              controller: _controller,
              enabled: !widget.busy,
              obscureText: question.isSecret && !_revealSecret,
              // A login or captcha is not a free-text answer; the field must
              // offer paste and autocorrect-off so a paste is not mangled.
              autocorrect: !question.isSecret,
              enableSuggestions: !question.isSecret,
              decoration: InputDecoration(
                isDense: true,
                border: const OutlineInputBorder(),
                labelText: question.isSecret ? question.secretName : '回复',
                suffixIcon: question.isSecret
                    ? IconButton(
                        tooltip: _revealSecret ? '隐藏' : '显示',
                        icon: Icon(
                          _revealSecret
                              ? Icons.visibility_off_outlined
                              : Icons.visibility_outlined,
                          size: 18,
                        ),
                        onPressed: () =>
                            setState(() => _revealSecret = !_revealSecret),
                      )
                    : null,
              ),
            ),
            const SizedBox(height: MaClawColors.spaceSm),
            Align(
              alignment: Alignment.centerRight,
              child: FilledButton.icon(
                onPressed: widget.busy
                    ? null
                    : () {
                        final value = _controller.text.trim();
                        if (value.isEmpty) return;
                        widget.onAnswer(value);
                      },
                icon: const Icon(Icons.send, size: 16),
                label: Text(question.isSecret ? '填入' : '发送'),
              ),
            ),
            if (question.isSecret) ...[
              const SizedBox(height: MaClawColors.spaceXs),
              Text(
                '该内容只发送给 Bot 的桌面，不会写入聊天记录。',
                style:
                    text.labelSmall?.copyWith(color: scheme.onSurfaceVariant),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Handoff card: the bot stopped and needs the person at its desktop.
class BotDesktopHandoffCard extends StatelessWidget {
  final String attentionReason;
  final String novncUrl;
  final VoidCallback? onOpenDesktop;

  const BotDesktopHandoffCard({
    super.key,
    required this.attentionReason,
    required this.novncUrl,
    this.onOpenDesktop,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final hasUrl = novncUrl.trim().isNotEmpty;
    return Card(
      margin: EdgeInsets.zero,
      color: MaClawColors.success.withValues(alpha: 0.12),
      child: Padding(
        padding: const EdgeInsets.all(MaClawColors.spaceMd),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(
                  Icons.pan_tool_alt_outlined,
                  size: 18,
                  color: MaClawColors.success,
                ),
                const SizedBox(width: MaClawColors.spaceSm),
                Text(
                  'Bot 把桌面交给你了',
                  style: Theme.of(context).textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w600,
                      ),
                ),
              ],
            ),
            if (attentionReason.trim().isNotEmpty) ...[
              const SizedBox(height: MaClawColors.spaceSm),
              Text(
                attentionReason.trim(),
                style: Theme.of(context)
                    .textTheme
                    .bodyMedium
                    ?.copyWith(height: 1.4),
              ),
            ],
            if (hasUrl) ...[
              const SizedBox(height: MaClawColors.spaceSm),
              Align(
                alignment: Alignment.centerLeft,
                child: OutlinedButton.icon(
                  onPressed: onOpenDesktop,
                  icon: const Icon(Icons.desktop_windows_outlined, size: 18),
                  label: const Text('打开远程桌面'),
                ),
              ),
            ] else ...[
              const SizedBox(height: MaClawColors.spaceXs),
              Text(
                '请在桌面端 MaClaw 中完成这一步，然后回到这里继续。',
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// One attachment the bot produced. Images inline, documents as a tile.
class BotAttachmentTile extends StatelessWidget {
  final BotReplyFile file;
  final VoidCallback onTap;
  final VoidCallback onCopy;

  const BotAttachmentTile({
    super.key,
    required this.file,
    required this.onTap,
    required this.onCopy,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      margin: EdgeInsets.zero,
      child: ListTile(
        dense: true,
        leading: Icon(Icons.insert_drive_file_outlined, color: scheme.primary),
        title: Text(
          file.displayName,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        subtitle: file.mime.isEmpty ? null : Text(file.mime),
        trailing: IconButton(
          tooltip: '复制到剪贴板',
          icon: const Icon(Icons.copy, size: 18),
          onPressed: onCopy,
        ),
        onTap: onTap,
      ),
    );
  }
}

/// Inline image the bot produced. Tapping copies it to the clipboard so a
/// screenshot of the bot's desktop can leave the app without a file picker.
class BotImageAttachment extends StatelessWidget {
  final BotReplyImage image;
  final VoidCallback onTap;

  const BotImageAttachment({
    super.key,
    required this.image,
    required this.onTap,
  });

  /// Base64 bytes above this are not worth decoding into an image widget on a
  /// phone; the tile still offers a copy instead.
  static const int maxDecodableBytes = 4 * 1024 * 1024;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final approxBytes = image.data.length * 3 ~/ 4;
    if (approxBytes > maxDecodableBytes) {
      return Card(
        margin: EdgeInsets.zero,
        child: ListTile(
          dense: true,
          leading: Icon(Icons.image_outlined, color: scheme.primary),
          title: const Text('图片过大，点按复制'),
          subtitle:
              Text('${(approxBytes / 1024 / 1024).toStringAsFixed(1)} MB'),
          onTap: onTap,
        ),
      );
    }
    return Padding(
      padding: const EdgeInsets.only(top: MaClawColors.spaceSm),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(MaClawColors.radiusMd),
        child: InkWell(
          onTap: onTap,
          child: Image.memory(
            _decodeBotImage(image),
            fit: BoxFit.contain,
            errorBuilder: (context, error, stack) => Padding(
              padding: const EdgeInsets.all(MaClawColors.spaceMd),
              child: Text(
                '图片无法显示，点按复制原图',
                style: Theme.of(context).textTheme.labelMedium?.copyWith(
                      color: scheme.error,
                    ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Decode base64 image bytes, tolerating a payload that is not valid image data.
Uint8List _decodeBotImage(BotReplyImage image) {
  try {
    return base64Decode(image.data);
  } on FormatException {
    return Uint8List(0);
  }
}

/// Copy helper shared by the image and file tiles.
Future<void> copyBotAttachmentToClipboard(String data) async {
  await Clipboard.setData(ClipboardData(text: data));
}
