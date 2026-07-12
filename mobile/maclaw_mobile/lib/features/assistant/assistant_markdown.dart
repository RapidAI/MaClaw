import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:markdown/markdown.dart' as md;

import '../../core/text/html_text.dart';
import '../../shared/theme.dart';

/// Whether [text] looks like Markdown worth structured rendering.
bool looksLikeAssistantMarkdown(String text) {
  final value = text.trim();
  if (value.isEmpty) return false;
  return RegExp(
    r'(^|\n)\s{0,3}(#{1,6}\s|[-*+]\s|\d+\.\s|\|.+\|)',
    multiLine: true,
  ).hasMatch(value) ||
      value.contains('**') ||
      value.contains('```') ||
      value.contains('> ');
}

// Regional indicators sit inside 0x1F300–0x1FAFF (aligned with corelib/textutil).
bool _isPictographBase(int r) {
  return (r >= 0x1F300 && r <= 0x1FAFF) ||
      (r >= 0x2600 && r <= 0x27BF) ||
      (r >= 0x2300 && r <= 0x23FF);
}

/// Strip leading decorative pictograph clusters (ZWJ sequences + trailing spaces).
String stripLeadingEmojiCluster(String text) {
  if (text.isEmpty) return text;
  final runes = text.runes.toList();
  var i = 0;
  while (i < runes.length && _isPictographBase(runes[i])) {
    i++;
    if (i < runes.length && runes[i] == 0xFE0F) i++;
    while (i + 1 < runes.length &&
        runes[i] == 0x200D &&
        _isPictographBase(runes[i + 1])) {
      i++; // ZWJ
      i++; // next base
      if (i < runes.length && runes[i] == 0xFE0F) i++;
    }
    while (i < runes.length && (runes[i] == 0x20 || runes[i] == 0x09)) {
      i++;
    }
  }
  if (i == 0) return text;
  return String.fromCharCodes(runes.sublist(i));
}

final _mdLinePrefix = RegExp(r'^(#{1,6}[ \t]+|[-*+][ \t]+|\d+\.[ \t]+|>[ \t]+)');
final _fenceLine = RegExp(r'^[ \t]*(`{3,}|~{3,})');

String stripLineLeadingEmoji(String line) {
  var wsEnd = 0;
  while (wsEnd < line.length) {
    final ch = line.codeUnitAt(wsEnd);
    if (ch == 0x20 || ch == 0x09) {
      wsEnd++;
    } else {
      break;
    }
  }
  final afterWs = line.substring(wsEnd);
  final mdMatch = _mdLinePrefix.matchAsPrefix(afterWs);
  final mdPrefix = mdMatch?.group(0) ?? '';
  final rest =
      mdPrefix.isNotEmpty ? afterWs.substring(mdPrefix.length) : afterWs;
  final stripped = stripLeadingEmojiCluster(rest);
  // No pictograph removed — keep original line identity.
  if (identical(stripped, rest) || stripped == rest) return line;
  return '${line.substring(0, wsEnd)}$mdPrefix$stripped';
}

bool _mayContainPictograph(String text) {
  for (final r in text.runes) {
    if (_isPictographBase(r)) return true;
  }
  return false;
}

/// Display policy: strip line-leading decorative pictographs outside fences.
String prepareChatBodyForDisplay(String text) {
  if (text.isEmpty || !_mayContainPictograph(text)) return text;
  final lines = text.split('\n');
  var inFence = false;
  var changed = false;
  final out = <String>[];
  for (final line in lines) {
    if (_fenceLine.hasMatch(line)) {
      inFence = !inFence;
      out.add(line);
      continue;
    }
    if (inFence) {
      out.add(line);
      continue;
    }
    final cleaned = stripLineLeadingEmoji(line);
    if (cleaned != line) changed = true;
    out.add(cleaned);
  }
  if (!changed) return text;
  return out.join('\n');
}

/// Normalize assistant answer text before Markdown rendering.
String prepareAssistantMarkdown(String raw) {
  final cleaned = cleanSearchSnippet(
    raw,
    maxLength: 0,
    preserveNewlines: true,
  ).trim();
  if (cleaned.isEmpty) return raw.trim();
  return prepareChatBodyForDisplay(cleaned);
}

/// Themed, soft-wrap Markdown body for companion chat bubbles.
class AssistantMarkdownBody extends StatelessWidget {
  final String data;
  final Color? textColor;

  const AssistantMarkdownBody({
    super.key,
    required this.data,
    this.textColor,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final textTheme = Theme.of(context).textTheme;
    final ink = textColor ?? scheme.onSurface;
    final muted = scheme.onSurfaceVariant;
    final dark = Theme.of(context).brightness == Brightness.dark;
    final codeBg =
        dark ? MaClawColors.darkElevated : scheme.surfaceContainerHighest;

    final styleSheet = MarkdownStyleSheet.fromTheme(Theme.of(context)).copyWith(
      p: textTheme.bodyMedium?.copyWith(color: ink, height: 1.45),
      pPadding: const EdgeInsets.only(bottom: 6),
      h1: textTheme.titleLarge?.copyWith(
        color: ink,
        fontWeight: FontWeight.w700,
        height: 1.25,
      ),
      h1Padding: const EdgeInsets.only(top: 4, bottom: 8),
      h2: textTheme.titleMedium?.copyWith(
        color: ink,
        fontWeight: FontWeight.w700,
        height: 1.3,
      ),
      h2Padding: const EdgeInsets.only(top: 4, bottom: 6),
      h3: textTheme.titleSmall?.copyWith(
        color: ink,
        fontWeight: FontWeight.w600,
        height: 1.3,
      ),
      h3Padding: const EdgeInsets.only(top: 2, bottom: 4),
      strong: textTheme.bodyMedium?.copyWith(
        color: ink,
        fontWeight: FontWeight.w700,
      ),
      em: textTheme.bodyMedium?.copyWith(
        color: ink,
        fontStyle: FontStyle.italic,
      ),
      listBullet: textTheme.bodyMedium?.copyWith(color: ink, height: 1.4),
      listIndent: 20,
      blockquote: textTheme.bodyMedium?.copyWith(color: muted, height: 1.4),
      blockquoteDecoration: BoxDecoration(
        border: Border(
          left: BorderSide(color: scheme.primary.withValues(alpha: 0.45), width: 3),
        ),
        color: scheme.primaryContainer.withValues(alpha: 0.25),
      ),
      blockquotePadding: const EdgeInsets.fromLTRB(10, 6, 8, 6),
      code: textTheme.bodySmall?.copyWith(
        color: ink,
        fontFamily: 'monospace',
        backgroundColor: codeBg,
      ),
      codeblockDecoration: BoxDecoration(
        color: codeBg,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: scheme.outlineVariant),
      ),
      codeblockPadding: const EdgeInsets.all(10),
      tableHead: textTheme.labelLarge?.copyWith(
        color: ink,
        fontWeight: FontWeight.w700,
      ),
      tableBody: textTheme.bodySmall?.copyWith(color: ink, height: 1.35),
      tableBorder: TableBorder.all(
        color: scheme.outlineVariant,
        width: 1,
        borderRadius: BorderRadius.circular(6),
      ),
      tableHeadAlign: TextAlign.left,
      tableCellsPadding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      tableColumnWidth: const IntrinsicColumnWidth(),
      horizontalRuleDecoration: BoxDecoration(
        border: Border(
          top: BorderSide(color: scheme.outlineVariant),
        ),
      ),
      a: textTheme.bodyMedium?.copyWith(
        color: scheme.primary,
        decoration: TextDecoration.underline,
        decorationColor: scheme.primary.withValues(alpha: 0.5),
      ),
    );

    final markdown = prepareAssistantMarkdown(data);
    // Soft-wrap long lines; tables scroll horizontally when needed.
    return MarkdownBody(
      data: markdown,
      selectable: true,
      softLineBreak: true,
      styleSheet: styleSheet,
      shrinkWrap: true,
      fitContent: true,
      // GFM tables/lists used by Hub companion answers.
      extensionSet: md.ExtensionSet.gitHubFlavored,
      styleSheetTheme: MarkdownStyleSheetBaseTheme.material,
    );
  }
}
