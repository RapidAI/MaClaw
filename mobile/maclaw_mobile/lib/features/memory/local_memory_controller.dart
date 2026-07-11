import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/security/mobile_redaction.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import 'local_memory_note.dart';

final localMemoryProvider =
    AsyncNotifierProvider<LocalMemoryController, List<LocalMemoryNote>>(
  LocalMemoryController.new,
);

class LocalMemoryController extends AsyncNotifier<List<LocalMemoryNote>> {
  @override
  Future<List<LocalMemoryNote>> build() {
    return ref.watch(mobileLocalStoreProvider).loadLocalMemories();
  }

  LocalMemoryStatus get status {
    final notes = state.valueOrNull ?? const <LocalMemoryNote>[];
    return computeLocalMemoryStatus(notes);
  }

  Future<LocalMemoryNote> remember({
    required String content,
    String title = '',
    String category = 'user_fact',
    bool pinned = false,
    bool active = true,
    bool syncToHub = true,
  }) async {
    final body = redactMobileSensitiveText(content).trim();
    if (body.isEmpty) {
      throw StateError('记忆内容不能为空');
    }
    final headline = redactMobileSensitiveText(title).trim();
    final cat = category.trim().isEmpty ? 'user_fact' : category.trim();
    final current = state.valueOrNull ?? await future;
    final now = DateTime.now().toUtc();
    final note = LocalMemoryNote(
      id: now.microsecondsSinceEpoch.toString(),
      title: headline,
      content: body,
      category: cat,
      createdAt: now,
      updatedAt: now,
      pinned: pinned,
      active: active,
      syncedToHub: false,
    );
    var next = compressLocalMemories(
      [note, ...current],
      maxKeep: kLocalMemoryMaxStored,
    );
    await _save(next);

    if (syncToHub) {
      final synced = await _trySyncToHub(note);
      if (synced) {
        next = [
          for (final item in next)
            if (item.id == note.id) item.copyWith(syncedToHub: true) else item,
        ];
        await _save(next);
        return note.copyWith(syncedToHub: true);
      }
    }
    return note;
  }

  Future<void> updateNote(
    String id, {
    String? title,
    String? content,
    String? category,
    bool? pinned,
    bool? active,
  }) async {
    final current = state.valueOrNull ?? await future;
    final next = [
      for (final item in current)
        if (item.id == id)
          item.copyWith(
            title: title != null ? redactMobileSensitiveText(title).trim() : null,
            content: content != null
                ? redactMobileSensitiveText(content).trim()
                : null,
            category: category,
            pinned: pinned,
            active: active,
            updatedAt: DateTime.now().toUtc(),
            syncedToHub: false,
          )
        else
          item,
    ];
    await _save(next);
  }

  Future<void> togglePin(String id) async {
    final current = state.valueOrNull ?? await future;
    final next = [
      for (final item in current)
        if (item.id == id)
          item.copyWith(
            pinned: !item.pinned,
            updatedAt: DateTime.now().toUtc(),
          )
        else
          item,
    ];
    await _save(next);
  }

  Future<void> toggleActive(String id) async {
    final current = state.valueOrNull ?? await future;
    final next = [
      for (final item in current)
        if (item.id == id)
          item.copyWith(
            active: !item.active,
            updatedAt: DateTime.now().toUtc(),
          )
        else
          item,
    ];
    await _save(next);
  }

  Future<void> remove(String id) async {
    final current = state.valueOrNull ?? await future;
    await _save(current.where((item) => item.id != id).toList());
  }

  Future<void> clearInactive() async {
    final current = state.valueOrNull ?? await future;
    await _save(current.where((item) => item.active).toList());
  }

  Future<void> clearUnpinned() async {
    final current = state.valueOrNull ?? await future;
    await _save(current.where((item) => item.pinned).toList());
  }

  /// Dedup + prune storage. Does not delete pinned notes.
  Future<LocalMemoryCompressResult> compress() async {
    final before = state.valueOrNull ?? await future;
    final after = compressLocalMemories(before, maxKeep: kLocalMemoryMaxStored);
    await _save(after);
    final removed = before.length - after.length;
    return LocalMemoryCompressResult(
      removedDuplicates: removed,
      prunedOverflow: 0,
      remaining: after.length,
      inactiveKept: after.where((n) => !n.active).length,
    );
  }

  Future<void> syncPendingToHub() async {
    final current = state.valueOrNull ?? await future;
    final pending = current.where((n) => !n.syncedToHub).toList();
    if (pending.isEmpty) return;
    var next = [...current];
    for (final note in pending) {
      final ok = await _trySyncToHub(note);
      if (!ok) continue;
      next = [
        for (final item in next)
          if (item.id == note.id) item.copyWith(syncedToHub: true) else item,
      ];
    }
    await _save(next);
  }

  Future<bool> _trySyncToHub(LocalMemoryNote note) async {
    final client = ref.read(apiClientProvider);
    if (client == null) return false;
    try {
      final result = await client.ingestAgentKnowledge(
        text: note.content,
        title: note.title.isEmpty ? '手机记忆' : note.title,
      );
      return result.ok;
    } on Object {
      return false;
    }
  }

  Future<void> _save(List<LocalMemoryNote> next) async {
    await ref.read(mobileLocalStoreProvider).saveLocalMemories(next);
    state = AsyncData(next);
  }
}
