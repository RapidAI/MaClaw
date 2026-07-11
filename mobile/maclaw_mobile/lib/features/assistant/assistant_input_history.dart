// Pure helpers for mobile AI assistant input history recall.
// Desktop can use ArrowUp/ArrowDown; phones need explicit touch controls
// (sheet + prev/next). Logic is shared and unit-tested.

/// Build newest-first, de-duplicated prompts for recall UI.
///
/// [conversationUserMessages] is chronological (oldest first).
/// [historyQueries] is typically newest-first local search history.
List<String> buildAssistantInputRecallPrompts({
  List<String> conversationUserMessages = const [],
  List<String> historyQueries = const [],
  int maxItems = 40,
}) {
  if (maxItems <= 0) return const [];
  final out = <String>[];
  final seen = <String>{};

  void add(String raw) {
    final text = raw.trim();
    if (text.isEmpty) return;
    // Case-insensitive de-dupe, keep first (newest) casing.
    final key = text.toLowerCase();
    if (!seen.add(key)) return;
    out.add(text);
  }

  for (var i = conversationUserMessages.length - 1; i >= 0; i--) {
    add(conversationUserMessages[i]);
    if (out.length >= maxItems) return out;
  }
  for (final query in historyQueries) {
    add(query);
    if (out.length >= maxItems) return out;
  }
  return out;
}

/// Filter recall prompts by a free-text needle (case-insensitive substring).
List<String> filterAssistantInputRecallPrompts(
  List<String> prompts,
  String needle, {
  int maxItems = 40,
}) {
  final q = needle.trim().toLowerCase();
  if (q.isEmpty) {
    if (prompts.length <= maxItems) return List<String>.from(prompts);
    return prompts.take(maxItems).toList(growable: false);
  }
  final out = <String>[];
  for (final p in prompts) {
    if (p.toLowerCase().contains(q)) {
      out.add(p);
      if (out.length >= maxItems) break;
    }
  }
  return out;
}

/// Sequential browser for touch prev/next (mirrors desktop ArrowUp/Down).
///
/// [index] is an index into a newest-first [prompts] list:
/// - `-1` means not browsing (composer shows live draft)
/// - `0` is newest submitted prompt
/// - last index is oldest among the recall list
class AssistantInputHistoryBrowser {
  int index;
  String? draftBeforeHistory;

  AssistantInputHistoryBrowser({
    this.index = -1,
    this.draftBeforeHistory,
  });

  bool get isBrowsing => index >= 0;

  void reset() {
    index = -1;
    draftBeforeHistory = null;
  }

  /// Step toward older prompts (like ArrowUp). Returns text to show, or null.
  String? stepOlder(List<String> prompts, {required String currentInput}) {
    if (prompts.isEmpty) return null;
    if (index < 0) {
      draftBeforeHistory = currentInput;
      index = 0;
      return prompts[0];
    }
    if (index >= prompts.length - 1) {
      // Stay on oldest.
      index = prompts.length - 1;
      return prompts[index];
    }
    index += 1;
    return prompts[index];
  }

  /// Step toward newer prompts (like ArrowDown). Returns text to show, or null
  /// when leaving browse mode (caller should restore draft).
  String? stepNewer(List<String> prompts) {
    if (!isBrowsing || prompts.isEmpty) return null;
    if (index <= 0) {
      final draft = draftBeforeHistory ?? '';
      reset();
      return draft;
    }
    index -= 1;
    if (index >= prompts.length) {
      index = prompts.length - 1;
    }
    return prompts[index];
  }

  /// 1-based position for UI, or 0 when not browsing.
  int positionDisplay(List<String> prompts) {
    if (!isBrowsing || prompts.isEmpty) return 0;
    final clamped = index.clamp(0, prompts.length - 1);
    // Show newest as 1: index 0 → 1, oldest last.
    return clamped + 1;
  }
}
