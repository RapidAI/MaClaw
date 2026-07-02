import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import 'search_history.dart';

final assistantQueryProvider = StateProvider<String>((ref) => '');

final assistantSharedCitationProvider =
    StateProvider<SearchCitation?>((ref) => null);

final assistantTabsProvider =
    NotifierProvider<AssistantTabsController, AssistantTabsState>(
  AssistantTabsController.new,
);

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
    final tabId = ref.read(assistantTabsProvider).activeTabId;
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(
        StateError('请先登录官方服务。'),
        StackTrace.current,
      );
      ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, state);
      return;
    }
    state = const AsyncLoading();
    ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, state);
    state = await AsyncValue.guard(() async {
      final answer = await client.search(text);
      await ref
          .read(searchHistoryProvider.notifier)
          .record(text, answer.answer);
      return answer;
    });
    ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, state);
  }
}

class AssistantConversationTab {
  final String id;
  final String title;
  final String query;
  final bool primary;
  final AsyncValue<SearchAnswer?> result;
  final bool resultTouched;
  final SearchCitation? sharedCitation;

  const AssistantConversationTab({
    required this.id,
    required this.title,
    this.query = '',
    this.primary = false,
    this.result = const AsyncData(null),
    this.resultTouched = false,
    this.sharedCitation,
  });

  AssistantConversationTab copyWith({
    String? title,
    String? query,
    AsyncValue<SearchAnswer?>? result,
    bool? resultTouched,
    SearchCitation? sharedCitation,
    bool clearSharedCitation = false,
  }) {
    return AssistantConversationTab(
      id: id,
      title: title ?? this.title,
      query: query ?? this.query,
      primary: primary,
      result: result ?? this.result,
      resultTouched: resultTouched ?? this.resultTouched,
      sharedCitation:
          clearSharedCitation ? null : sharedCitation ?? this.sharedCitation,
    );
  }
}

class AssistantTabsState {
  final List<AssistantConversationTab> tabs;
  final String activeTabId;

  const AssistantTabsState({
    required this.tabs,
    required this.activeTabId,
  });

  AssistantConversationTab get activeTab {
    return tabs.firstWhere(
      (tab) => tab.id == activeTabId,
      orElse: () => tabs.first,
    );
  }
}

class AssistantTabsController extends Notifier<AssistantTabsState> {
  @override
  AssistantTabsState build() {
    return const AssistantTabsState(
      activeTabId: 'main',
      tabs: [
        AssistantConversationTab(
          id: 'main',
          title: '主对话',
          primary: true,
        ),
      ],
    );
  }

  void activate(String id) {
    if (state.activeTabId == id || !state.tabs.any((tab) => tab.id == id)) {
      return;
    }
    state = AssistantTabsState(tabs: state.tabs, activeTabId: id);
  }

  AssistantConversationTab addTab() {
    final index = state.tabs.length;
    final tab = AssistantConversationTab(
      id: 'assistant-tab-${DateTime.now().microsecondsSinceEpoch}',
      title: '副对话 $index',
    );
    state = AssistantTabsState(
      tabs: [...state.tabs, tab],
      activeTabId: tab.id,
    );
    return tab;
  }

  void close(String id) {
    AssistantConversationTab? tab;
    for (final item in state.tabs) {
      if (item.id == id) {
        tab = item;
        break;
      }
    }
    if (tab == null || tab.primary || state.tabs.length == 1) return;
    final index = state.tabs.indexWhere((item) => item.id == id);
    final next = state.tabs.where((item) => item.id != id).toList();
    final activeId = state.activeTabId == id
        ? next[(index - 1).clamp(0, next.length - 1).toInt()].id
        : state.activeTabId;
    state = AssistantTabsState(tabs: next, activeTabId: activeId);
  }

  void updateActiveQuery(String query) {
    final activeId = state.activeTabId;
    state = AssistantTabsState(
      activeTabId: activeId,
      tabs: [
        for (final tab in state.tabs)
          if (tab.id == activeId)
            tab.copyWith(
              query: query,
              title: tab.primary ? '主对话' : _titleForQuery(query, tab.title),
            )
          else
            tab,
      ],
    );
  }

  void setActiveResult(AsyncValue<SearchAnswer?> result) {
    setResultForTab(state.activeTabId, result);
  }

  void setResultForTab(String tabId, AsyncValue<SearchAnswer?> result) {
    final activeId = state.activeTabId;
    state = AssistantTabsState(
      activeTabId: activeId,
      tabs: [
        for (final tab in state.tabs)
          if (tab.id == tabId)
            tab.copyWith(result: result, resultTouched: true)
          else
            tab,
      ],
    );
  }

  void setActiveSharedCitation(SearchCitation? citation) {
    final activeId = state.activeTabId;
    state = AssistantTabsState(
      activeTabId: activeId,
      tabs: [
        for (final tab in state.tabs)
          if (tab.id == activeId)
            tab.copyWith(
              sharedCitation: citation,
              clearSharedCitation: citation == null,
            )
          else
            tab,
      ],
    );
  }

  String _titleForQuery(String query, String fallback) {
    final text = query.trim();
    if (text.isEmpty) return fallback.startsWith('副对话') ? fallback : '副对话';
    return text.length > 12 ? '${text.substring(0, 12)}...' : text;
  }
}

class SearchHistoryController extends AsyncNotifier<List<SearchHistoryEntry>> {
  @override
  Future<List<SearchHistoryEntry>> build() {
    return ref.watch(mobileLocalStoreProvider).loadSearchHistory();
  }

  Future<void> record(String query, String answer) async {
    final current = state.valueOrNull ?? await future;
    final preview =
        answer.length > 140 ? '${answer.substring(0, 140)}...' : answer;
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
