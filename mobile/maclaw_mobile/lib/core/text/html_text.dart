/// Lightweight HTML entity / markup cleanup for search snippets and assistant text.
///
/// Mobile must not pull in a full HTML parser for this; search engines often
/// emit named and numeric entities in titles/snippets.
String unescapeHtmlEntities(String input) {
  if (input.isEmpty) return input;
  var text = input;

  // Numeric decimal entities: &#183; &#0183;
  text = text.replaceAllMapped(RegExp(r'&#0*(\d{1,7});'), (match) {
    final code = int.tryParse(match.group(1)!);
    if (code == null || code < 0 || code > 0x10FFFF) return match.group(0)!;
    return String.fromCharCode(code);
  });

  // Numeric hex entities: &#xB7; &#x00b7;
  text = text.replaceAllMapped(RegExp(r'&#x([0-9a-fA-F]{1,6});'), (match) {
    final code = int.tryParse(match.group(1)!, radix: 16);
    if (code == null || code < 0 || code > 0x10FFFF) return match.group(0)!;
    return String.fromCharCode(code);
  });

  const named = <String, String>{
    '&nbsp;': ' ',
    '&ensp;': ' ',
    '&emsp;': ' ',
    '&thinsp;': ' ',
    '&amp;': '&',
    '&lt;': '<',
    '&gt;': '>',
    '&quot;': '"',
    '&apos;': "'",
    '&middot;': '·',
    '&bull;': '•',
    '&hellip;': '…',
    '&mdash;': '—',
    '&ndash;': '–',
    '&laquo;': '«',
    '&raquo;': '»',
  };
  for (final entry in named.entries) {
    text = text.replaceAll(entry.key, entry.value);
  }
  // Case-insensitive common names without semicolon (rare but seen in feeds).
  text = text
      .replaceAll(RegExp(r'&nbsp', caseSensitive: false), ' ')
      .replaceAll(RegExp(r'&ensp', caseSensitive: false), ' ')
      .replaceAll(RegExp(r'&emsp', caseSensitive: false), ' ');

  return text;
}

/// Strip simple tags and collapse whitespace after unescaping entities.
///
/// Set [preserveNewlines] when cleaning multi-line Markdown so tables and
/// lists keep their structure.
String cleanSearchSnippet(
  String input, {
  int maxLength = 240,
  bool preserveNewlines = false,
}) {
  var text = unescapeHtmlEntities(input);
  text = text.replaceAll(RegExp(r'<[^>]*>'), ' ');
  if (preserveNewlines) {
    text = text
        .replaceAll(RegExp(r'[ \t\f\v]+'), ' ')
        .replaceAll(RegExp(r' *\n *'), '\n')
        .replaceAll(RegExp(r'\n{3,}'), '\n\n')
        .trim();
  } else {
    text = text.replaceAll(RegExp(r'\s+'), ' ').trim();
  }
  if (maxLength > 0 && text.length > maxLength) {
    text = '${text.substring(0, maxLength).trimRight()}…';
  }
  return text;
}

/// Host-only label for compact citation rows.
String citationHostLabel(String url) {
  final trimmed = url.trim();
  if (trimmed.isEmpty) return '';
  final uri = Uri.tryParse(trimmed);
  final host = uri?.host.trim() ?? '';
  if (host.isEmpty) return trimmed;
  return host.startsWith('www.') ? host.substring(4) : host;
}
