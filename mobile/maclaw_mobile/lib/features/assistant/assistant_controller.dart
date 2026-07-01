import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import 'search_history.dart';

final assistantQueryProvider = StateProvider<String>((ref) => '');

final assistantSearchProvider =
    AsyncNotifierProvider<AssistantSearchController, SearchAnswer?>(
  AssistantSearchController.new,
);

final searchHistoryProvider =
    AsyncNotifierProvider<SearchHistoryController, List<SearchHistoryEntry>>(
  SearchHistoryController.new,
);

class AssistantSearchController extends AsyncNotifier<SearchAnswer?> {
  @override
  Future<SearchAnswer?> build() async => null;

  Future<void> search(String query) async {
    final text = query.trim();
    if (text.isEmpty) return;
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(
        StateError('请先登录官方服务。'),
        StackTrace.current,
      );
      return;
    }
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
      final answer = await client.search(text);
      await ref.read(searchHistoryProvider.notifier).record(text, answer.answer);
      return answer;
    });
  }
}

class SearchHistoryController extends AsyncNotifier<List<SearchHistoryEntry>> {
  @override
  Future<List<SearchHistoryEntry>> build() {
    return ref.watch(mobileLocalStoreProvider).loadSearchHistory();
  }

  Future<void> record(String query, String answer) async {
    final current = state.valueOrNull ?? await future;
    final preview = answer.length > 140 ? '${answer.substring(0, 140)}...' : answer;
    SearchHistoryEntry? existing;
    for (final item in current) {
      if (item.query == query) {
        existing = item;
        break;
      }
    }
    final entry = SearchHistoryEntry(
      id: existing?.id ?? DateTime.now().microsecondsSinceEpoch.toString(),
      query: query,
      answerPreview: preview,
      createdAt: DateTime.now().toUtc(),
      favorite: existing?.favorite ?? false,
    );
    final next = [
      entry,
      ...current.where((item) => item.id != entry.id),
    ].take(50).toList();
    await _save(next);
  }

  Future<void> toggleFavorite(String id) async {
    final current = state.valueOrNull ?? await future;
    final next = [
      for (final item in current)
        if (item.id == id) item.copyWith(favorite: !item.favorite) else item,
    ];
    await _save(next);
  }

  Future<void> remove(String id) async {
    final current = state.valueOrNull ?? await future;
    final next = current.where((item) => item.id != id).toList();
    await _save(next);
  }

  Future<void> clearNonFavorites() async {
    final current = state.valueOrNull ?? await future;
    final next = current.where((item) => item.favorite).toList();
    await _save(next);
  }

  Future<void> _save(List<SearchHistoryEntry> next) async {
    await ref.read(mobileLocalStoreProvider).saveSearchHistory(next);
    state = AsyncData(next);
  }
}
