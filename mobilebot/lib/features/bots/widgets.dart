import 'dart:async' show unawaited;

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import 'bot_models.dart';

/// 与桌面 GUI botAppearance.ts 相同的调色板与形状集。
const botColors = [
  '#16a34a', '#2563eb', '#7c3aed', '#db2777', '#ea580c',
  '#ca8a04', '#0891b2', '#dc2626', '#4f46e5', '#475569',
];
const botShapes = ['blob', 'cloud', 'leaf', 'square', 'pill', 'circle'];

Color hexColor(String hex) {
  final value = int.tryParse(hex.replaceFirst('#', ''), radix: 16);
  return value == null ? Colors.grey : Color(0xFF000000 | value);
}

String botShapeLabel(String shape) => switch (shape) {
      'blob' => '团子',
      'cloud' => '云朵',
      'leaf' => '叶子',
      'square' => '方块',
      'pill' => '胶囊',
      _ => '圆形',
    };

/// Avatar honouring the bot's configured color/shape, falling back to a
/// deterministic palette color by id.
class BotAvatar extends StatelessWidget {
  final Bot bot;
  final double radius;

  const BotAvatar({super.key, required this.bot, this.radius = 20});

  static const _palette = [
    Color(0xFF5B8DEF),
    Color(0xFF7C5CD6),
    Color(0xFF3FA7A0),
    Color(0xFFD97B42),
    Color(0xFFC15B8F),
    Color(0xFF6AA84F),
    Color(0xFF4E79A7),
    Color(0xFFB85C38),
  ];

  Color _color() {
    if (bot.color.isNotEmpty) {
      final parsed = int.tryParse(
        bot.color.replaceFirst('#', ''),
        radix: 16,
      );
      if (parsed != null) return Color(0xFF000000 | parsed);
    }
    return _palette[bot.id.hashCode.abs() % _palette.length];
  }

  @override
  Widget build(BuildContext context) {
    final label = Text(
      bot.initials,
      style: TextStyle(
        color: Colors.white,
        fontWeight: FontWeight.w600,
        fontSize: radius * 0.7,
      ),
    );
    final color = _color();
    if (bot.shape == 'square') {
      return Container(
        width: radius * 2,
        height: radius * 2,
        alignment: Alignment.center,
        decoration: BoxDecoration(
          color: color,
          borderRadius: BorderRadius.circular(radius * 0.45),
        ),
        child: label,
      );
    }
    if (bot.shape == 'pill') {
      return Container(
        width: radius * 2.6,
        height: radius * 2,
        alignment: Alignment.center,
        decoration: BoxDecoration(
          color: color,
          borderRadius: BorderRadius.circular(radius),
        ),
        child: label,
      );
    }
    return CircleAvatar(radius: radius, backgroundColor: color, child: label);
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
                color: theme.colorScheme.surfaceContainerHighest,
                shape: BoxShape.circle,
              ),
              child: Icon(
                icon,
                size: 34,
                color: theme.colorScheme.onSurfaceVariant,
              ),
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
            if (action != null) ...[
              const SizedBox(height: 16),
              action!,
            ],
          ],
        ),
      ),
    );
  }
}

/// Small state chip used on work status rows.
class StateChip extends StatelessWidget {
  final String label;
  final Color color;

  const StateChip({super.key, required this.label, required this.color});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 11,
          color: color,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}

/// Bare http(s) URLs in free text. The char class excludes whitespace,
/// brackets and CJK/ASCII punctuation that almost always belongs to the
/// surrounding prose rather than the link itself.
final _bareUrlPattern = RegExp(
  "https?://[^\\s<>\"'“”‘’（）【】《》\\[\\]`^|]+",
);

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
    launchUrl(uri, mode: LaunchMode.externalApplication)
        .catchError((_) => false),
  );
}

/// Splits [url] into (link, trailing-punctuation) so wrappers can keep the
/// prose tail outside the anchor.
(String, String) _splitUrlTail(String url) {
  var tail = '';
  while (url.isNotEmpty &&
      _trailingUrlPunct.contains(url[url.length - 1])) {
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
      parts[p] = parts[p].replaceAllMapped(_bareUrlPattern, (m) {
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
    final linkStyle = widget.linkStyle ??
        (widget.style ?? const TextStyle()).copyWith(
          color: theme.colorScheme.primary,
          decoration: TextDecoration.underline,
        );
    _resetRecognizers();
    final spans = <InlineSpan>[];
    var cursor = 0;
    for (final m in _bareUrlPattern.allMatches(widget.text)) {
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
          ..add(
            TextSpan(text: url, style: linkStyle, recognizer: recognizer),
          )
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

Color runStatusColor(String status) => switch (status.toLowerCase()) {
      'running' || 'accepted' || 'pending' => const Color(0xFF3FA7A0),
      'paused' => const Color(0xFFD97B42),
      'waiting_for_user' || 'waiting' => const Color(0xFFC15B8F),
      'failed' || 'error' => const Color(0xFFD64545),
      'done' || 'finished' || 'completed' => const Color(0xFF6AA84F),
      _ => Colors.grey,
    };
