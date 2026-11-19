import 'package:flutter/material.dart';

import 'locale.dart';
import 'theme.dart';

/// Which side of the transcript a [MessageBubble] belongs to.
enum ChatRole { user, assistant }

/// Unified transcript bubble: user messages sit right-aligned on solid
/// `primary`, assistant messages sit left with an optional leading avatar on
/// `surfaceContainerHigh`. The [child] owns the content (plain text,
/// markdown, chips…); the bubble only decides geometry and default colors.
class MessageBubble extends StatelessWidget {
  final ChatRole role;
  final Widget child;
  final Widget? leading;
  final Color? color;
  final double maxWidth;
  final EdgeInsetsGeometry? padding;
  final EdgeInsetsGeometry margin;

  const MessageBubble({
    super.key,
    required this.role,
    required this.child,
    this.leading,
    this.color,
    required this.maxWidth,
    this.padding,
    this.margin = const EdgeInsets.only(bottom: AppSpacing.md),
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final isUser = role == ChatRole.user;
    final background =
        color ?? (isUser ? scheme.primary : scheme.surfaceContainerHigh);
    final foreground = isUser ? scheme.onPrimary : scheme.onSurface;
    final bubble = Container(
      constraints: BoxConstraints(maxWidth: maxWidth),
      margin: margin,
      padding:
          padding ?? const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.only(
          topLeft: const Radius.circular(20),
          topRight: const Radius.circular(20),
          bottomLeft: Radius.circular(isUser ? 20 : 6),
          bottomRight: Radius.circular(isUser ? 6 : 20),
        ),
      ),
      child: DefaultTextStyle.merge(
        style: TextStyle(color: foreground, height: 1.4),
        child: child,
      ),
    );
    if (isUser) {
      return Align(alignment: Alignment.centerRight, child: bubble);
    }
    return Align(
      alignment: Alignment.centerLeft,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (leading != null) ...[
            leading!,
            const SizedBox(width: AppSpacing.sm),
          ],
          Flexible(child: bubble),
        ],
      ),
    );
  }
}

/// Unified chat composer: pill input with optional attach/mic inside and
/// supplement/stop around the filled send button. Every chat surface
/// (bot chat, assistant, remote task, room chat) renders this so the typing
/// experience is identical app-wide.
class ChatComposer extends StatelessWidget {
  final TextEditingController controller;
  final String hint;
  final bool sending;
  final VoidCallback onSend;
  final ValueChanged<String>? onSubmitted;
  final TextInputAction textInputAction;
  final VoidCallback? onAttach;
  final String? attachTooltip;
  final VoidCallback? onMic;
  final bool listening;
  final VoidCallback? onSupplement;
  final VoidCallback? onStop;

  const ChatComposer({
    super.key,
    required this.controller,
    required this.hint,
    required this.sending,
    required this.onSend,
    this.onSubmitted,
    this.textInputAction = TextInputAction.send,
    this.onAttach,
    this.attachTooltip,
    this.onMic,
    this.listening = false,
    this.onSupplement,
    this.onStop,
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
                    if (onAttach != null)
                      IconButton(
                        onPressed: onAttach,
                        visualDensity: VisualDensity.compact,
                        icon: const Icon(Icons.attach_file),
                        tooltip: attachTooltip ?? context.S.composerAttach,
                      ),
                    Expanded(
                      child: TextField(
                        controller: controller,
                        minLines: 1,
                        maxLines: 5,
                        textInputAction: textInputAction,
                        onSubmitted: onSubmitted,
                        decoration: InputDecoration(
                          hintText: hint,
                          filled: false,
                          border: InputBorder.none,
                          enabledBorder: InputBorder.none,
                          contentPadding: const EdgeInsets.symmetric(
                            horizontal: 2,
                            vertical: 12,
                          ),
                        ),
                      ),
                    ),
                    if (onMic != null)
                      IconButton(
                        onPressed: onMic,
                        visualDensity: VisualDensity.compact,
                        icon: Icon(
                          listening ? Icons.mic : Icons.mic_none,
                          color: listening ? scheme.error : null,
                        ),
                        tooltip: listening
                            ? context.S.composerMicStop
                            : context.S.composerMicStart,
                      ),
                  ],
                ),
              ),
            ),
            const SizedBox(width: 6),
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (onSupplement != null)
                  IconButton(
                    onPressed: onSupplement,
                    icon: const Icon(Icons.playlist_add),
                    tooltip: context.S.composerSupplement,
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
                      : const Icon(Icons.arrow_upward_rounded),
                  tooltip: context.S.composerSend,
                ),
                if (onStop != null)
                  IconButton(
                    onPressed: onStop,
                    icon: const Icon(Icons.stop),
                    tooltip: context.S.composerStop,
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
