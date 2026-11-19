/// Shared defensive JSON readers — Hub payloads occasionally carry fields of
/// the wrong type, and one malformed value must never poison a sync merge or
/// a periodic refresh. Strings pass through trimmed; anything else reads as
/// empty rather than throwing.
library;

/// A JSON field that should be text.
String syncText(Object? value) => value is String ? value.trim() : '';

/// Same tolerance for list fields.
List<dynamic> syncList(Object? value) => value is List ? value : const [];

/// First non-empty text among candidates (multi-key fallbacks).
String syncTextAny(Object? a, [Object? b, Object? c]) {
  for (final v in [a, b, c]) {
    final text = syncText(v);
    if (text.isNotEmpty) return text;
  }
  return '';
}

/// Text with a non-empty-string fallback.
String syncTextOr(Object? value, String fallback) {
  final text = syncText(value);
  return text.isEmpty ? fallback : text;
}

/// And for ints that may arrive as a JSON number or a numeric string.
int syncInt(Object? value) {
  if (value is num) return value.toInt();
  if (value is String) return int.tryParse(value.trim()) ?? 0;
  return 0;
}

/// Int with a non-zero fallback (e.g. day_of_week's "weekly" sentinel).
int syncIntOr(Object? value, int fallback) {
  if (value is num) return value.toInt();
  if (value is String) return int.tryParse(value.trim()) ?? fallback;
  return fallback;
}

/// Double with the same tolerance.
double syncDouble(Object? value) {
  if (value is num) return value.toDouble();
  if (value is String) return double.tryParse(value.trim()) ?? 0;
  return 0;
}
