/// Markdown → plain text for TTS readback. The transcript renders markdown,
/// but speaking markup literally ("asterisk asterisk") is unusable, so the
/// voice assistant strips structure and keeps only the prose.
library;

import 'locale.dart';

/// Maximum characters spoken in one turn — very long replies get truncated
/// with a spoken pointer back to the screen instead of a minutes-long readout.
const int _speechTextLimit = 1200;

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
  if (s.length > _speechTextLimit) {
    s = '${s.substring(0, _speechTextLimit)}${l.speechReplyTooLong}';
  }
  return s;
}
