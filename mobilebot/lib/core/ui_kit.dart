import 'dart:async' show unawaited;

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_animate/flutter_animate.dart';
import 'package:url_launcher/url_launcher.dart';

import 'theme.dart';

/// Small state chip used on work status rows — a tinted 11px pill whose
/// [color] carries the semantics (see runStatusColor).
class StateChip extends StatelessWidget {
  final String label;
  final Color color;

  const StateChip({super.key, required this.label, required this.color});

  @override
  Widget build(BuildContext context) {
    return AppBadge(label: label, color: color);
  }
}

/// Tinted pill badge for statuses and meta tags (主 Bot / 电脑 / 排队中…).
/// Rendered as a filled alpha wash — never raw saturated color — so it reads
/// as a label, not a button.
class AppBadge extends StatelessWidget {
  final String label;
  final Color color;
  final IconData? icon;
  final bool solid; // true → filled with [color], text on it

  const AppBadge({
    super.key,
    required this.label,
    required this.color,
    this.icon,
    this.solid = false,
  });

  @override
  Widget build(BuildContext context) {
    final background = solid ? color : color.withValues(alpha: 0.12);
    final foreground = solid ? Colors.white : color;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(AppRadius.sm),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 12, color: foreground),
            const SizedBox(width: 4),
          ],
          Text(
            label,
            style: TextStyle(
              fontSize: 11,
              color: foreground,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }
}

/// A tiny glowing status dot — the compact liveness marker used on cards
/// and pickers (online / running / offline).
class StatusDot extends StatelessWidget {
  final Color color;
  final double size;

  const StatusDot({super.key, required this.color, this.size = 8});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: color,
        shape: BoxShape.circle,
        boxShadow: [
          BoxShadow(color: color.withValues(alpha: 0.35), blurRadius: 6),
        ],
      ),
    );
  }
}

/// Section title inside scroll views — pairs with Card lists and keeps the
/// 16px page padding aligned (cards inset their own content by 16 too).
class SectionHeader extends StatelessWidget {
  final String title;
  final Widget? trailing;

  /// dense 去掉顶部留白 — 给页面自己已经留过间距的场景用。
  final bool dense;

  const SectionHeader({
    super.key,
    required this.title,
    this.trailing,
    this.dense = false,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(
        AppSpacing.xs,
        dense ? 2 : AppSpacing.lg + 4,
        AppSpacing.xs,
        AppSpacing.sm + 2,
      ),
      child: Row(
        children: [
          Text(title, style: theme.textTheme.titleSmall),
          const Spacer(),
          ?trailing,
        ],
      ),
    );
  }
}

/// Loading placeholder block with a soft shimmer sweep — used instead of a
/// bare spinner wherever a list or card skeleton fits the layout.
class SkeletonBox extends StatelessWidget {
  final double? width;
  final double height;
  final BorderRadius radius;

  const SkeletonBox({
    super.key,
    this.width,
    this.height = 16,
    BorderRadius? radius,
  }) : radius = radius ?? const BorderRadius.all(Radius.circular(AppRadius.sm));

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
          width: width,
          height: height,
          decoration: BoxDecoration(
            color: scheme.surfaceContainerHighest.withValues(alpha: 0.55),
            borderRadius: radius,
          ),
        )
        .animate(onPlay: (controller) => controller.repeat())
        .shimmer(
          duration: const Duration(milliseconds: 1500),
          colors: [
            scheme.surfaceContainerHighest.withValues(alpha: 0.55),
            scheme.onSurface.withValues(alpha: 0.08),
            scheme.surfaceContainerHighest.withValues(alpha: 0.55),
          ],
        );
  }
}

/// A full-page skeleton of card-shaped blocks — drop-in replacement for the
/// `Center(CircularProgressIndicator)` loading states of list screens.
class SkeletonList extends StatelessWidget {
  final int itemCount;
  final double itemHeight;
  final EdgeInsetsGeometry padding;

  const SkeletonList({
    super.key,
    this.itemCount = 4,
    this.itemHeight = 92,
    this.padding = const EdgeInsets.all(AppSpacing.lg),
  });

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      padding: padding,
      itemCount: itemCount,
      itemBuilder: (context, index) {
        return Padding(
          padding: const EdgeInsets.only(bottom: AppSpacing.md),
          child: SkeletonBox(
            width: double.infinity,
            height: itemHeight,
            radius: BorderRadius.circular(AppRadius.lg),
          ),
        );
      },
    );
  }
}

/// The app brand mark: a rounded gradient tile with the bot glyph. Used on
/// the login hero, splash and anywhere the product needs a face.
class BrandMark extends StatelessWidget {
  final double size;

  /// tile=true 用在顶栏这种已有圆角容器里的位置：只留渐变块，去掉向外
  /// 溢的阴影，以免在 44px 高的导航栏里发灰。
  final bool tile;

  const BrandMark({super.key, this.size = 76, this.tile = false});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final deep = Color.lerp(scheme.primary, Colors.black, 0.18)!;
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(size * 0.3),
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [scheme.primary, deep],
        ),
        boxShadow: tile
            ? null
            : [
                BoxShadow(
                  color: scheme.primary.withValues(alpha: 0.35),
                  blurRadius: size * 0.35,
                  offset: Offset(0, size * 0.1),
                ),
              ],
      ),
      child: Icon(
        Icons.smart_toy_outlined,
        size: size * 0.52,
        // On small tiles the tinted onPrimary washes out over the gradient.
        color: tile ? Colors.white : scheme.onPrimary,
      ),
    );
  }
}

/// Shared empty/error-state block: tinted icon + title + optional hint and
/// action, used by every list screen so placeholders look uniform.
class EmptyState extends StatelessWidget {
  final IconData icon;
  final String title;
  final String? hint;
  final Widget? action;

  const EmptyState({
    super.key,
    required this.icon,
    required this.title,
    this.hint,
    this.action,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 72,
              height: 72,
              decoration: BoxDecoration(
                color: theme.colorScheme.primaryContainer.withValues(
                  alpha: 0.5,
                ),
                shape: BoxShape.circle,
              ),
              child: Icon(icon, size: 34, color: theme.colorScheme.primary),
            ),
            const SizedBox(height: 16),
            Text(
              title,
              textAlign: TextAlign.center,
              style: theme.textTheme.titleSmall,
            ),
            if (hint != null) ...[
              const SizedBox(height: 6),
              Text(
                hint!,
                textAlign: TextAlign.center,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
            if (action != null) ...[const SizedBox(height: 16), action!],
          ],
        ),
      ),
    ).animate().fadeIn(duration: const Duration(milliseconds: 250));
  }
}

/// The gradient hero used at the top of list screens: eyebrow line, a big
/// line of copy, optional hint and a trailing action. Reads as the product
/// speaking first, which is what makes the home tab feel like a workspace
/// rather than a settings index.
class GradientHero extends StatelessWidget {
  final IconData icon;
  final String eyebrow;
  final String title;
  final String? hint;
  final Widget? trailing;

  const GradientHero({
    super.key,
    required this.icon,
    required this.eyebrow,
    required this.title,
    this.hint,
    this.trailing,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final deep = Color.lerp(scheme.primary, Colors.black, 0.32)!;
    final mid = Color.lerp(scheme.primary, Colors.black, 0.08)!;
    return Container(
      margin: const EdgeInsets.fromLTRB(
        AppSpacing.lg,
        AppSpacing.sm,
        AppSpacing.lg,
        AppSpacing.xs,
      ),
      padding: const EdgeInsets.fromLTRB(
        AppSpacing.xl,
        AppSpacing.lg + 2,
        AppSpacing.lg,
        AppSpacing.lg + 2,
      ),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(AppRadius.xl + 4),
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [mid, deep],
        ),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Icon(icon, size: 15, color: Colors.white70),
                    const SizedBox(width: 6),
                    Flexible(
                      child: Text(
                        eyebrow,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 12.5,
                          fontWeight: FontWeight.w600,
                          letterSpacing: 0.2,
                          color: Colors.white70,
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: AppSpacing.sm + 2),
                Text(
                  title,
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.headlineSmall?.copyWith(
                    fontSize: 22,
                    height: 1.25,
                    color: Colors.white,
                  ),
                ),
                if (hint != null && hint!.isNotEmpty) ...[
                  const SizedBox(height: AppSpacing.sm),
                  Text(
                    hint!,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 13,
                      height: 1.4,
                      color: Colors.white.withValues(alpha: 0.82),
                    ),
                  ),
                ],
              ],
            ),
          ),
          if (trailing != null) ...[const SizedBox(width: AppSpacing.md), trailing!],
        ],
      ),
    );
  }
}

// ── Linkify helpers (moved from features/bots/widgets.dart) ────────────────

/// Bare http(s) URLs in free text. The char class excludes whitespace,
/// brackets and CJK/ASCII punctuation that almost always belongs to the
/// surrounding prose rather than the link itself.
final bareUrlPattern = RegExp("https?://[^\\s<>\"'“”‘’（）【】《》\\[\\]`^|]+");

/// Sentence punctuation stripped from the END of a matched URL — it is prose,
/// not part of the link (`,` `;` `:` etc. remain legal mid-URL).
const _trailingUrlPunct = '.,;:!?\'"”’…）)】]。，；：、！？';

/// Opens an http(s) link in the external browser; other schemes are ignored.
/// A missing browser on the device fails silently rather than crashing.
void openExternalUrl(String url) {
  final uri = Uri.tryParse(url);
  if (uri == null) return;
  if (uri.scheme != 'http' && uri.scheme != 'https') return;
  unawaited(
    launchUrl(
      uri,
      mode: LaunchMode.externalApplication,
    ).catchError((_) => false),
  );
}

/// Splits [url] into (link, trailing-punctuation) so wrappers can keep the
/// prose tail outside the anchor.
(String, String) _splitUrlTail(String url) {
  var tail = '';
  while (url.isNotEmpty && _trailingUrlPunct.contains(url[url.length - 1])) {
    tail = url[url.length - 1] + tail;
    url = url.substring(0, url.length - 1);
  }
  return (url, tail);
}

/// Wraps bare URLs in [text] as markdown links so a Markdown renderer makes
/// them tappable. Fenced code blocks, inline `code`, existing `[t](url)`
/// links and `<url>` autolinks are left alone.
String linkifyMarkdownUrls(String text) {
  if (!text.contains('http')) return text;
  final out = StringBuffer();
  var inFence = false;
  final lines = text.split('\n');
  for (var i = 0; i < lines.length; i++) {
    if (i > 0) out.write('\n');
    final line = lines[i];
    if (line.trimLeft().startsWith('```')) {
      inFence = !inFence;
      out.write(line);
      continue;
    }
    if (inFence) {
      out.write(line);
      continue;
    }
    // Odd indices of a '`' split are inline code spans — untouched.
    final parts = line.split('`');
    for (var p = 0; p < parts.length; p += 2) {
      parts[p] = parts[p].replaceAllMapped(bareUrlPattern, (m) {
        final raw = m.group(0)!;
        if (m.start >= 2 && parts[p].substring(m.start - 2, m.start) == '](') {
          return raw;
        }
        if (m.start >= 1 && parts[p][m.start - 1] == '<') return raw;
        // A URL followed by '](' is link display text ([https://a](https://b))
        // — wrapping it would nest anchors.
        if (parts[p].substring(m.end).startsWith('](')) return raw;
        final (url, tail) = _splitUrlTail(raw);
        return url.isEmpty ? raw : '[$url]($url)$tail';
      });
    }
    out.write(parts.join('`'));
  }
  return out.toString();
}

/// Plain-text rendering that still makes bare URLs tappable — for bubbles and
/// cards that do not run through the Markdown pipeline.
class LinkifiedText extends StatefulWidget {
  final String text;
  final TextStyle? style;
  final TextStyle? linkStyle;
  final int? maxLines;
  final TextOverflow? overflow;

  const LinkifiedText(
    this.text, {
    super.key,
    this.style,
    this.linkStyle,
    this.maxLines,
    this.overflow,
  });

  @override
  State<LinkifiedText> createState() => _LinkifiedTextState();
}

class _LinkifiedTextState extends State<LinkifiedText> {
  final List<TapGestureRecognizer> _recognizers = [];

  void _resetRecognizers() {
    for (final r in _recognizers) {
      r.dispose();
    }
    _recognizers.clear();
  }

  @override
  void dispose() {
    _resetRecognizers();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final linkStyle =
        widget.linkStyle ??
        (widget.style ?? const TextStyle()).copyWith(
          color: theme.colorScheme.primary,
          decoration: TextDecoration.underline,
        );
    _resetRecognizers();
    final spans = <InlineSpan>[];
    var cursor = 0;
    for (final m in bareUrlPattern.allMatches(widget.text)) {
      final (url, tail) = _splitUrlTail(m.group(0)!);
      if (m.start > cursor) {
        spans.add(TextSpan(text: widget.text.substring(cursor, m.start)));
      }
      if (url.isEmpty) {
        spans.add(TextSpan(text: m.group(0)!));
      } else {
        final recognizer = TapGestureRecognizer()
          ..onTap = () => openExternalUrl(url);
        _recognizers.add(recognizer);
        spans
          ..add(TextSpan(text: url, style: linkStyle, recognizer: recognizer))
          ..add(TextSpan(text: tail));
      }
      cursor = m.end;
    }
    if (cursor < widget.text.length) {
      spans.add(TextSpan(text: widget.text.substring(cursor)));
    }
    return Text.rich(
      TextSpan(style: widget.style, children: spans),
      maxLines: widget.maxLines,
      overflow: widget.overflow ?? TextOverflow.clip,
    );
  }
}
