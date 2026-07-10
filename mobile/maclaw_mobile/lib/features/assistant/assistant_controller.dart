import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../core/security/mobile_redaction.dart';
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

const assistantLongTaskNotificationThreshold = Duration(seconds: 10);

bool shouldNotifyAssistantLongTask(Duration elapsed, String requestId) {
  return elapsed >= assistantLongTaskNotificationThreshold &&
      requestId.trim().isNotEmpty;
}

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
    final tab = ref.read(assistantTabsProvider).activeTab;
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(
        StateError('请先登录官方服务。'),
        StackTrace.current,
      );
      ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, state);
      return;
    }
    final session = ref.read(sessionControllerProvider).valueOrNull;
    if (session?.bootstrap?.features.assistant == false) {
      state = AsyncError(
        StateError('当前 Hub 未启用 AI 助手服务能力，仍可使用语音输入、图片/文件导入和文档草稿。'),
        StackTrace.current,
      );
      ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, state);
      return;
    }
    final previousMessages = tab.messages.length > 8
        ? tab.messages.sublist(tab.messages.length - 8)
        : tab.messages;
    final context = [
      for (final message in previousMessages)
        '${message.role}: ${message.text}',
    ];
    ref.read(assistantTabsProvider.notifier).appendMessage(
          tabId,
          AssistantConversationMessage.user(text),
        );
    state = const AsyncLoading();
    ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, state);
    final startedAt = DateTime.now();
    state = await AsyncValue.guard(() async {
      final answer = context.isEmpty
          ? await client.search(text)
          : await client.searchWithContext(text, context: context);
      await ref
          .read(searchHistoryProvider.notifier)
          .record(text, answer.answer);
      return answer;
    });
    ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, state);
    final answer = state.valueOrNull;
    if (answer != null) {
      ref.read(assistantTabsProvider.notifier).appendMessage(
            tabId,
            AssistantConversationMessage.assistant(
              query: text,
              text: answer.answer,
              citations: answer.citations,
              llmMode: answer.llmMode,
              llmRequestId: answer.llmRequestId,
              llmUsageRecordId: answer.llmUsageRecordId,
            ),
          );
    }
    if (answer != null &&
        shouldNotifyAssistantLongTask(
          DateTime.now().difference(startedAt),
          answer.llmRequestId,
        )) {
      unawaited(_notifyLongTask(answer.llmRequestId));
    }
  }

  Future<void> _notifyLongTask(String requestId) async {
    try {
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: 'AI 助手任务完成',
            body: '长任务已完成，点击回到 AI 助手查看完整结果。',
            payload: mobileAssistantTaskNotificationPayload(requestId),
          );
    } on Object {
      // A notification plugin failure must not change the completed answer.
    }
  }
}

class AssistantConversationMessage {
  final String id;
  final String role;
  final String text;
  final String query;
  final List<SearchCitation> citations;
  final String llmMode;
  final String llmRequestId;
  final String llmUsageRecordId;

  const AssistantConversationMessage({
    required this.id,
    required this.role,
    required this.text,
    this.query = '',
    this.citations = const [],
    this.llmMode = '',
    this.llmRequestId = '',
    this.llmUsageRecordId = '',
  });

  factory AssistantConversationMessage.user(String text) {
    return AssistantConversationMessage(
      id: 'user-${DateTime.now().microsecondsSinceEpoch}',
      role: 'user',
      text: text,
    );
  }

  factory AssistantConversationMessage.assistant({
    required String query,
    required String text,
    List<SearchCitation> citations = const [],
    String llmMode = '',
    String llmRequestId = '',
    String llmUsageRecordId = '',
  }) {
    return AssistantConversationMessage(
      id: 'assistant-${DateTime.now().microsecondsSinceEpoch}',
      role: 'assistant',
      text: text,
      query: query,
      citations: citations,
      llmMode: llmMode,
      llmRequestId: llmRequestId,
      llmUsageRecordId: llmUsageRecordId,
    );
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
  final List<AssistantConversationMessage> messages;

  const AssistantConversationTab({
    required this.id,
    required this.title,
    this.query = '',
    this.primary = false,
    this.result = const AsyncData(null),
    this.resultTouched = false,
    this.sharedCitation,
    this.messages = const [],
  });

  AssistantConversationTab copyWith({
    String? title,
    String? query,
    AsyncValue<SearchAnswer?>? result,
    bool? resultTouched,
    SearchCitation? sharedCitation,
    List<AssistantConversationMessage>? messages,
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
      messages: messages ?? this.messages,
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

  void appendMessage(String tabId, AssistantConversationMessage message) {
    state = AssistantTabsState(
      activeTabId: state.activeTabId,
      tabs: [
        for (final tab in state.tabs)
          if (tab.id == tabId)
            tab.copyWith(messages: [...tab.messages, message])
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
    final historyQuery = redactMobileSensitiveText(query);
    final redactedAnswer = redactMobileSensitiveText(answer);
    final preview = redactedAnswer.length > 140
        ? '${redactedAnswer.substring(0, 140)}...'
        : redactedAnswer;
    SearchHistoryEntry? existing;
    for (final item in current) {
      if (item.query == historyQuery) {
        existing = item;
        break;
      }
    }
    final entry = SearchHistoryEntry(
      id: existing?.id ?? DateTime.now().microsecondsSinceEpoch.toString(),
      query: historyQuery,
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
