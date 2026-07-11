// On-device personal memory. Only active notes enter the assistant context,
// under a strict item/rune budget (similar to MaClaw GUI capacity discipline).

const kLocalMemoryMaxStored = 100;
const kLocalMemoryContextMaxItems = 8;
const kLocalMemoryContextMaxRunes = 1600;
const kLocalMemoryLineMaxChars = 220;

const localMemoryCategories = <String, String>{
  'user_fact': '用户事实',
  'preference': '偏好',
  'project_knowledge': '项目知识',
  'instruction': '指令/约定',
  'conversation_summary': '对话摘要',
  'other': '其它',
};

String localMemoryCategoryLabel(String category) {
  final key = category.trim().isEmpty ? 'other' : category.trim();
  return localMemoryCategories[key] ?? key;
}

class LocalMemoryNote {
  final String id;
  final String title;
  final String content;
  final String category;
  final DateTime createdAt;
  final DateTime updatedAt;
  final bool pinned;
  /// When false, kept on device but not injected into assistant context.
  final bool active;
  final bool syncedToHub;

  const LocalMemoryNote({
    required this.id,
    required this.title,
    required this.content,
    this.category = 'user_fact',
    required this.createdAt,
    DateTime? updatedAt,
    this.pinned = false,
    this.active = true,
    this.syncedToHub = false,
  }) : updatedAt = updatedAt ?? createdAt;

  LocalMemoryNote copyWith({
    String? id,
    String? title,
    String? content,
    String? category,
    DateTime? createdAt,
    DateTime? updatedAt,
    bool? pinned,
    bool? active,
    bool? syncedToHub,
  }) {
    return LocalMemoryNote(
      id: id ?? this.id,
      title: title ?? this.title,
      content: content ?? this.content,
      category: category ?? this.category,
      createdAt: createdAt ?? this.createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
      pinned: pinned ?? this.pinned,
      active: active ?? this.active,
      syncedToHub: syncedToHub ?? this.syncedToHub,
    );
  }

  factory LocalMemoryNote.fromJson(Map<String, dynamic> json) {
    final created = DateTime.tryParse(json['created_at'] as String? ?? '') ??
        DateTime.fromMillisecondsSinceEpoch(0);
    return LocalMemoryNote(
      id: json['id'] as String? ?? '',
      title: json['title'] as String? ?? '',
      content: json['content'] as String? ?? '',
      category: (json['category'] as String? ?? 'user_fact').trim().isEmpty
          ? 'user_fact'
          : (json['category'] as String? ?? 'user_fact').trim(),
      createdAt: created,
      updatedAt: DateTime.tryParse(json['updated_at'] as String? ?? '') ?? created,
      pinned: json['pinned'] as bool? ?? false,
      active: json['active'] as bool? ?? true,
      syncedToHub: json['synced_to_hub'] as bool? ?? false,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'title': title,
      'content': content,
      'category': category,
      'created_at': createdAt.toUtc().toIso8601String(),
      'updated_at': updatedAt.toUtc().toIso8601String(),
      'pinned': pinned,
      'active': active,
      'synced_to_hub': syncedToHub,
    };
  }

  int get contentRunes => content.trim().runes.length;
}

class LocalMemoryStatus {
  final int total;
  final int active;
  final int pinned;
  final int inactive;
  final int synced;
  final int contextItems;
  final int contextRunes;
  final int contextBudgetItems;
  final int contextBudgetRunes;
  final int storedBudget;

  const LocalMemoryStatus({
    required this.total,
    required this.active,
    required this.pinned,
    required this.inactive,
    required this.synced,
    required this.contextItems,
    required this.contextRunes,
    this.contextBudgetItems = kLocalMemoryContextMaxItems,
    this.contextBudgetRunes = kLocalMemoryContextMaxRunes,
    this.storedBudget = kLocalMemoryMaxStored,
  });

  double get contextFillRatio {
    if (contextBudgetRunes <= 0) return 0;
    return (contextRunes / contextBudgetRunes).clamp(0.0, 1.0);
  }
}

LocalMemoryStatus computeLocalMemoryStatus(List<LocalMemoryNote> notes) {
  final activeNotes = notes.where((n) => n.active).toList();
  final block = buildLocalMemoryContextBlock(notes);
  final contextRunes = block.isEmpty ? 0 : block.runes.length;
  // Approximate items used: count lines after header.
  final contextItems = block.isEmpty
      ? 0
      : block.split('\n').where((l) => l.trim().startsWith('-')).length;
  return LocalMemoryStatus(
    total: notes.length,
    active: activeNotes.length,
    pinned: notes.where((n) => n.pinned).length,
    inactive: notes.where((n) => !n.active).length,
    synced: notes.where((n) => n.syncedToHub).length,
    contextItems: contextItems,
    contextRunes: contextRunes,
  );
}

/// Build a compact memory block for the assistant request context.
/// Only [active] notes are considered; pinned first, then newest.
String buildLocalMemoryContextBlock(
  List<LocalMemoryNote> notes, {
  int maxItems = kLocalMemoryContextMaxItems,
  int maxRunes = kLocalMemoryContextMaxRunes,
  int lineMaxChars = kLocalMemoryLineMaxChars,
}) {
  if (notes.isEmpty || maxItems <= 0 || maxRunes <= 0) return '';
  final ordered = [
    for (final n in notes)
      if (n.active) n,
  ]..sort((a, b) {
      if (a.pinned != b.pinned) return a.pinned ? -1 : 1;
      return b.updatedAt.compareTo(a.updatedAt);
    });
  if (ordered.isEmpty) return '';

  final buf = StringBuffer();
  buf.writeln('【用户本机记忆】仅列出已启用且在预算内的要点，回答时优先参考：');
  var used = buf.toString().runes.length;
  var count = 0;
  for (final note in ordered) {
    if (count >= maxItems) break;
    final title = note.title.trim();
    var body = note.content.trim().replaceAll(RegExp(r'\s+'), ' ');
    if (body.isEmpty && title.isEmpty) continue;
    if (body.length > lineMaxChars) {
      body = '${body.substring(0, lineMaxChars)}…';
    }
    final cat = localMemoryCategoryLabel(note.category);
    final line = title.isEmpty
        ? '- [$cat] $body'
        : '- [$cat] $title：$body';
    final nextLen = used + line.runes.length + 1;
    if (nextLen > maxRunes) break;
    buf.writeln(line);
    used = nextLen;
    count++;
  }
  if (count == 0) return '';
  return buf.toString().trimRight();
}

class LocalMemoryCompressResult {
  final int removedDuplicates;
  final int prunedOverflow;
  final int remaining;
  final int inactiveKept;

  const LocalMemoryCompressResult({
    required this.removedDuplicates,
    required this.prunedOverflow,
    required this.remaining,
    required this.inactiveKept,
  });
}

/// Deduplicate exact content and prune oldest unpinned when over [maxKeep].
/// Pinned notes are never pruned. Inactive notes count toward storage but not context.
List<LocalMemoryNote> compressLocalMemories(
  List<LocalMemoryNote> notes, {
  int maxKeep = kLocalMemoryMaxStored,
}) {
  if (notes.isEmpty) return const [];
  // Prefer pinned, then newer when deduping same content key.
  final sorted = [...notes]..sort((a, b) {
      if (a.pinned != b.pinned) return a.pinned ? -1 : 1;
      return b.updatedAt.compareTo(a.updatedAt);
    });
  final seen = <String>{};
  final deduped = <LocalMemoryNote>[];
  for (final n in sorted) {
    final key = n.content.trim().toLowerCase();
    if (key.isEmpty) continue;
    if (!seen.add(key)) continue;
    deduped.add(n);
  }

  if (deduped.length <= maxKeep) {
    return deduped;
  }

  // Drop oldest unpinned first.
  final pinned = deduped.where((n) => n.pinned).toList();
  final unpinned = deduped.where((n) => !n.pinned).toList()
    ..sort((a, b) => a.updatedAt.compareTo(b.updatedAt));
  final room = maxKeep - pinned.length;
  final keepUnpinned = room <= 0
      ? <LocalMemoryNote>[]
      : unpinned.skip(unpinned.length > room ? unpinned.length - room : 0).toList();
  final out = [...pinned, ...keepUnpinned]
    ..sort((a, b) {
      if (a.pinned != b.pinned) return a.pinned ? -1 : 1;
      return b.updatedAt.compareTo(a.updatedAt);
    });
  return out;
}

LocalMemoryCompressResult describeCompress(
  List<LocalMemoryNote> before,
  List<LocalMemoryNote> after,
) {
  final beforeIds = before.map((n) => n.id).toSet();
  final afterIds = after.map((n) => n.id).toSet();
  final removed = beforeIds.difference(afterIds).length;
  // Approximate split: dups vs overflow is internal; report totals.
  return LocalMemoryCompressResult(
    removedDuplicates: removed, // total removed (dup+prune)
    prunedOverflow: 0,
    remaining: after.length,
    inactiveKept: after.where((n) => !n.active).length,
  );
}
