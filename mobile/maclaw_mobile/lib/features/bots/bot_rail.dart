import 'package:flutter/material.dart';

import '../../shared/theme.dart';
import 'bot.dart';

/// Status dot plus label for a bot's work state.
///
/// A running bot gets a spinner instead of a dot, because "still working" is
/// the one state where the difference between ten seconds and ten minutes
/// matters.
class BotStatusBadge extends StatelessWidget {
  final BotWorkState state;
  final bool compact;

  const BotStatusBadge({
    super.key,
    required this.state,
    this.compact = false,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final (Color color, String label) = switch (state) {
      BotWorkState.idle => (scheme.outline, BotWorkState.idle.label()),
      BotWorkState.running => (scheme.primary, BotWorkState.running.label()),
      BotWorkState.waitingForUser => (
          MaClawColors.success,
          BotWorkState.waitingForUser.label()
        ),
      BotWorkState.awaitingAnswer => (
          MaClawColors.success,
          BotWorkState.awaitingAnswer.label()
        ),
      BotWorkState.failed => (scheme.error, BotWorkState.failed.label()),
    };
    final icon = switch (state) {
      BotWorkState.running => SizedBox(
          width: 10,
          height: 10,
          child: CircularProgressIndicator(strokeWidth: 1.8, color: color),
        ),
      BotWorkState.waitingForUser =>
        Icon(Icons.pan_tool_alt_outlined, size: 11, color: color),
      BotWorkState.awaitingAnswer =>
        Icon(Icons.help_outline, size: 11, color: color),
      BotWorkState.failed => Icon(Icons.error_outline, size: 11, color: color),
      BotWorkState.idle => Icon(Icons.circle, size: 7, color: color),
    };
    if (compact) {
      return Tooltip(message: label, child: icon);
    }
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        icon,
        const SizedBox(width: MaClawColors.spaceXs),
        Text(
          label,
          style: text.labelSmall
              ?.copyWith(color: color, fontWeight: FontWeight.w600),
        ),
      ],
    );
  }
}

/// Round avatar derived from the bot's name.
///
/// The server sends no avatar, so initials keep every client consistent. The
/// colour is picked from the name so the same bot keeps the same colour here and
/// in any other client that derives one the same way.
class BotAvatar extends StatelessWidget {
  final Bot bot;
  final double size;

  const BotAvatar({super.key, required this.bot, this.size = 36});

  static const _palette = [
    Color(0xFF2D6B9F),
    Color(0xFF317C6E),
    Color(0xFF8A5A2D),
    Color(0xFF5B4B9F),
    Color(0xFF9F2D4A),
    Color(0xFF2D7F9F),
  ];

  static Color colorForBot(String seed) {
    if (seed.isEmpty) return _palette.first;
    var hash = 0;
    for (final unit in seed.codeUnits) {
      hash = (hash * 31 + unit) & 0x7fffffff;
    }
    return _palette[hash % _palette.length];
  }

  @override
  Widget build(BuildContext context) {
    final background = colorForBot(bot.id.isEmpty ? bot.title : bot.id);
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(size * 0.32),
      ),
      child: Text(
        bot.initials,
        style: Theme.of(context).textTheme.labelLarge?.copyWith(
              color: Colors.white,
              fontWeight: FontWeight.w700,
              fontSize: size * 0.36,
            ),
      ),
    );
  }
}

/// One row in the bot rail.
class BotRailCard extends StatelessWidget {
  final Bot bot;
  final BotWorkState state;
  final bool selected;
  final String? preview;
  final VoidCallback? onTap;
  final VoidCallback? onLongPress;

  const BotRailCard({
    super.key,
    required this.bot,
    required this.state,
    required this.selected,
    this.preview,
    this.onTap,
    this.onLongPress,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final previewText = (preview ?? '').trim();
    return Material(
      color: selected
          ? scheme.primaryContainer.withValues(alpha: 0.42)
          : Colors.transparent,
      borderRadius: BorderRadius.circular(MaClawColors.radiusMd),
      child: InkWell(
        onTap: onTap,
        onLongPress: onLongPress,
        borderRadius: BorderRadius.circular(MaClawColors.radiusMd),
        child: Padding(
          padding: const EdgeInsets.symmetric(
            horizontal: MaClawColors.spaceMd,
            vertical: MaClawColors.spaceSm + 2,
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              BotAvatar(bot: bot, size: 34),
              const SizedBox(width: MaClawColors.spaceMd),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            bot.title,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: text.titleSmall?.copyWith(
                              fontWeight: FontWeight.w600,
                              color: scheme.onSurface,
                            ),
                          ),
                        ),
                        const SizedBox(width: MaClawColors.spaceSm),
                        BotStatusBadge(state: state, compact: true),
                      ],
                    ),
                    if (previewText.isNotEmpty) ...[
                      const SizedBox(height: 2),
                      Text(
                        previewText,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: text.bodySmall?.copyWith(
                          color: scheme.onSurfaceVariant,
                          height: 1.3,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
