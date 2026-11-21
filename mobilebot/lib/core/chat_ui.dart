import 'package:flutter/foundation.dart';
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
    // Assistant replies read as prose on the canvas (no card, no bubble), the
    // way a mobile AI app presents an answer; only the person's own turn gets
    // a filled bubble. A failed user bubble keeps its own tint and geometry.
    final flat = !isUser && color == null;
    final bubble = Container(
      constraints: BoxConstraints(maxWidth: maxWidth),
      margin: margin,
      padding: flat
          ? const EdgeInsets.fromLTRB(
              2,
              2,
              2,
              AppSpacing.md - 2,
            )
          : (padding ??
                const EdgeInsets.symmetric(horizontal: 14, vertical: 10)),
      decoration: BoxDecoration(
        color: flat ? null : background,
        borderRadius: BorderRadius.only(
          topLeft: Radius.circular(isUser ? 20 : 8),
          topRight: const Radius.circular(20),
          bottomLeft: Radius.circular(isUser ? 20 : 8),
          bottomRight: Radius.circular(isUser ? 6 : 8),
        ),
      ),
      child: DefaultTextStyle.merge(
        style: TextStyle(color: foreground, height: 1.45),
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

/// Unified chat composer: rounded input field with attach/mic inside and
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

  /// 输入栏上方的常驻聆听行。空则不占位。回退时由调用方换成
  /// 实际引擎文案，并保留原因。
  final String? listenStatus;

  /// Optional 0..1 PCM level. Null while [listening] means the path has no
  /// samples (system recognition) — pulse only, do not invent a meter.
  /// Widget tests inject this; production passes the capture notifier.
  final ValueListenable<double?>? level;

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
    this.listenStatus,
    this.level,
    this.onSupplement,
    this.onStop,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 6, 12, 10),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (listenStatus != null && listenStatus!.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(left: 16, right: 8, bottom: 4),
                child: Text(
                  listenStatus!,
                  key: const ValueKey('listen-status'),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.primary,
                  ),
                ),
              ),
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
            Expanded(
              child: Container(
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerHigh,
                  borderRadius: BorderRadius.circular(AppRadius.md + 6),
                  border: Border.all(
                    color: scheme.outlineVariant.withValues(alpha: 0.7),
                  ),
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
                        icon: _MicLevelIcon(
                          listening: listening,
                          level: level,
                          color: listening
                              ? scheme.error
                              : scheme.onSurfaceVariant,
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
          ],
        ),
      ),
    );
  }
}


/// Mic glyph that follows an injected level, or pulses when listening
/// without PCM. [level] is nullable so widget tests never open a mic.
class _MicLevelIcon extends StatelessWidget {
  final bool listening;
  final ValueListenable<double?>? level;
  final Color color;

  const _MicLevelIcon({
    required this.listening,
    required this.level,
    required this.color,
  });

  @override
  Widget build(BuildContext context) {
    final icon = Icon(
      listening ? Icons.mic : Icons.mic_none,
      color: color,
    );
    if (!listening) return icon;
    final source = level;
    if (source == null) return _ListenPulse(color: color, child: icon);
    return ValueListenableBuilder<double?>(
      valueListenable: source,
      builder: (context, value, _) {
        if (value == null) return _ListenPulse(color: color, child: icon);
        return _LevelMic(level: value, color: color, child: icon);
      },
    );
  }
}

class _ListenPulse extends StatefulWidget {
  final Color color;
  final Widget child;

  const _ListenPulse({required this.color, required this.child});

  @override
  State<_ListenPulse> createState() => _ListenPulseState();
}

class _ListenPulseState extends State<_ListenPulse>
    with SingleTickerProviderStateMixin {
  late final AnimationController _ctrl;

  @override
  void initState() {
    super.initState();
    _ctrl = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 900),
    )..repeat(reverse: true);
  }

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _ctrl,
      builder: (context, child) {
        final t = Curves.easeInOut.transform(_ctrl.value);
        return SizedBox(
          key: const ValueKey('listen-pulse'),
          width: 28,
          height: 28,
          child: Stack(
            alignment: Alignment.center,
            children: [
              Container(
                width: 16 + 10 * t,
                height: 16 + 10 * t,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: widget.color.withValues(alpha: 0.18 + 0.22 * (1 - t)),
                ),
              ),
              child!,
            ],
          ),
        );
      },
      child: widget.child,
    );
  }
}

class _LevelMic extends StatelessWidget {
  final double level;
  final Color color;
  final Widget child;

  const _LevelMic({
    required this.level,
    required this.color,
    required this.child,
  });

  @override
  Widget build(BuildContext context) {
    final clamped = level.clamp(0.0, 1.0);
    return SizedBox(
      key: const ValueKey('listen-level'),
      width: 36,
      height: 28,
      child: Row(
        mainAxisAlignment: MainAxisAlignment.center,
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          child,
          const SizedBox(width: 2),
          Container(
            key: const ValueKey('listen-level-bar'),
            width: 4,
            height: 6 + 16 * clamped,
            margin: const EdgeInsets.only(bottom: 4),
            decoration: BoxDecoration(
              color: color.withValues(alpha: 0.7),
              borderRadius: BorderRadius.circular(2),
            ),
          ),
        ],
      ),
    );
  }
}
