/// Markdown → plain text for TTS readback. The transcript renders markdown,
/// but speaking markup literally ("asterisk asterisk") is unusable, so the
/// voice assistant strips structure and keeps only the prose.
library;

import 'locale.dart';

/// Maximum characters spoken in one turn — a reply that still exceeds this
/// after summarization gets a spoken pointer back to the screen instead of a
/// minutes-long readout.
const int _speechTextLimit = 1200;

/// Hard cap for one readback. The assistant's replies are agent answers, so a
/// 50k-character result is possible; speaking all of it is not.
const int maxSpeechSummaryChars = 480;

/// Preferred cap when the answer's own summary lines are short enough.
const int preferredSpeechSummaryChars = 300;

/// Strips markdown so TTS reads prose, then summarizes the result into what a
/// voice assistant should read out (see [speechSummary]).
String plainTextForSpeech(String markdown) {
  final l = sharedL10n;
  var s = markdown;
  // Fenced code reads terribly aloud — name it and move on.
  s = s.replaceAll(RegExp(r'```[\s\S]*?(```|$)'), l.speechCodeOnScreen);
  s = s.replaceAllMapped(RegExp(r'`([^`]*)`'), (m) => m[1] ?? '');
  s = s.replaceAll(RegExp(r'!\[[^\]]*\]\([^)]*\)'), '');
  s = s.replaceAllMapped(RegExp(r'\[([^\]]*)\]\([^)]*\)'), (m) => m[1] ?? '');
  s = s.replaceAll(RegExp(r'<https?://[^>]+>'), '');
  s = s.replaceAll(RegExp(r'^#{1,6}\s*', multiLine: true), '');
  s = s.replaceAll(RegExp(r'^\s{0,3}>\s?', multiLine: true), '');
  s = s.replaceAll(RegExp(r'^\s*[-*+]\s+', multiLine: true), '');
  s = s.replaceAll(RegExp(r'^\s*\d+\.\s+', multiLine: true), '');
  s = s.replaceAll(
    RegExp(r'^\s*(-{3,}|\*{3,}|_{3,})\s*$', multiLine: true),
    '',
  );
  s = s.replaceAllMapped(RegExp(r'(\*\*|__)(.*?)\1'), (m) => m[2] ?? '');
  s = s.replaceAllMapped(RegExp(r'(\*|_)(.*?)\1'), (m) => m[2] ?? '');
  s = s.replaceAllMapped(RegExp(r'~~(.*?)~~'), (m) => m[1] ?? '');
  s = s.replaceAll(RegExp(r'[|#]'), ' ');
  s = s.replaceAll(RegExp(r'[ \t]+'), ' ');
  s = s.replaceAll(RegExp(r'\n{3,}'), '\n\n');
  s = s.trim();
  // Summarize before the hard clip, so a long reply is cut at a sentence and
  // ends with "that is the summary" rather than a mid-sentence truncation.
  if (s.length > maxSpeechSummaryChars) s = speechSummary(s);
  if (s.length > _speechTextLimit) {
    s = '${s.substring(0, _speechTextLimit)}${l.speechReplyTooLong}';
  }
  return s;
}

/// One sentence ends at CJK or latin punctuation. The negated class
/// excludes newlines transitively, so a heading without punctuation still
/// closes at the line break.
final RegExp _sentenceEnd =
    RegExp('[^\u0000-\u001f。！？!?]*[。！？!?]?');

/// Picks what to speak. No LLM call on the readback path: a summary costs a
/// round trip and arrives after the answer is already on screen. Instead the
/// text is treated as an answer that states its conclusion first (results,
/// lists and summaries do), and only its leading sentences are spoken; the
/// rest stays on screen with a spoken pointer.
String speechSummary(String text) {
  final s = text.trim();
  if (s.length <= maxSpeechSummaryChars) return s;

  final picked = <String>[];
  var used = 0;
  for (final m in _sentenceEnd.allMatches(s)) {
    final piece = (m.group(0) ?? '').trim();
    if (piece.isEmpty) continue;
    final next = used + piece.length + 1;
    if (next > maxSpeechSummaryChars) break;
    picked.add(piece);
    used = next;
    if (used >= preferredSpeechSummaryChars) break;
  }
  if (picked.isEmpty) return s;
  final end = sharedL10n.speechSummaryEnd;
  final body = picked.join(' ');
  return body + String.fromCharCode(10) + end;
}

/// Splits a readback into the segments the engine queues: one sentence (or
/// short list item) each. Segmenting keeps every Kokoro call well under the
/// model's token window, so the first audio needs only a short inference and
/// the rest of the reply is synthesized while it plays.
///
/// A segment is closed at CJK/latin punctuation or a line break; anything
/// longer than [speechSegmentMaxChars] is hard-split.
List<String> splitSpeechSegments(
  String text, {
  int maxChars = speechSegmentMaxChars,
}) {
  final out = <String>[];
  final buffer = StringBuffer();
  void flush() {
    final piece = buffer.toString().trim();
    buffer.clear();
    if (piece.isEmpty) return;
    if (piece.length <= maxChars) {
      out.add(piece);
      return;
    }
    for (var i = 0; i < piece.length; i += maxChars) {
      out.add(piece.substring(i, (i + maxChars).clamp(0, piece.length)));
    }
  }

  for (final ch in text.split('')) {
    buffer.write(ch);
    final closes = ch.codeUnitAt(0) == 10 || ch == '。' || ch == '！' || ch == '？' || ch == '!' || ch == '?' || ch == ';' || ch == '；';
    if (closes && buffer.length >= speechSegmentMinChars) flush();
    if (buffer.length >= maxChars) flush();
  }
  flush();
  return out;
}

/// Character budget of one spoken segment (under Kokoro's phoneme window).
const int speechSegmentMaxChars = 60;

/// Segments shorter than this are merged into the next one, so a stray "好的。"
/// does not cost a separate inference.
const int speechSegmentMinChars = 12;
