import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../core/security/mobile_redaction.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import '../tasks/mobile_jobs_provider.dart';
import 'search_history.dart';

final assistantQueryProvider = StateProvider<String>((ref) => '');

final assistantSharedCitationProvider =
    StateProvider<SearchCitation?>((ref) => null);

/// Bound Hub document_id for the official assistant working object (sync path).
final assistantBoundDocumentIdProvider = StateProvider<String?>((ref) => null);

final assistantTabsProvider =
    NotifierProvider<AssistantTabsController, AssistantTabsState>(
  AssistantTabsController.new,
);

final assistantSearchProvider =
    AsyncNotifierProvider<AssistantSearchController, SearchAnswer?>(
  AssistantSearchController.new,
);

const assistantLongTaskNotificationThreshold = Duration(seconds: 10);

/// If interactive SSE has not finished by this budget, upgrade to a Hub
/// background assistant job so the user can leave the screen.
const assistantSyncUpgradeTimeout = Duration(seconds: 35);

bool shouldNotifyAssistantLongTask(Duration elapsed, String requestId) {
  return elapsed >= assistantLongTaskNotificationThreshold &&
      requestId.trim().isNotEmpty;
}

/// Whether elapsed interactive wait should hand off to async job.
bool shouldUpgradeAssistantToBackground(Duration elapsed) {
  return elapsed >= assistantSyncUpgradeTimeout;
}

/// Extract proposed draft rewrite from assistant answer fence
/// (```maclaw-document-edit ... ```).
String? extractMaclawDocumentEditFence(String answer) {
  const open = '```maclaw-document-edit';
  final idx = answer.indexOf(open);
  if (idx < 0) return null;
  var rest = answer.substring(idx + open.length);
  if (rest.startsWith('\r\n')) {
    rest = rest.substring(2);
  } else if (rest.startsWith('\n')) {
    rest = rest.substring(1);
  }
  final end = rest.indexOf('```');
  if (end < 0) return null;
  final body = rest.substring(0, end).trim();
  return body.isEmpty ? null : body;
}

final searchHistoryProvider =
    AsyncNotifierProvider<SearchHistoryController, List<SearchHistoryEntry>>(
  SearchHistoryController.new,
);

class AssistantSearchController extends AsyncNotifier<SearchAnswer?> {
  /// Per-tab in-flight guards so one tab's stream does not block another tab.
  final Set<String> _searchingTabIds = <String>{};

  /// Active interactive searches that can be cancelled / upgraded mid-flight.
  final Map<String, _InteractiveSearchSession> _interactiveSessions = {};

  @override
  Future<SearchAnswer?> build() async => null;

  /// True when the active tab has an interactive stream that can hand off.
  bool get canUpgradeActiveToBackground {
    final tabId = ref.read(assistantTabsProvider).activeTabId;
    final session = _interactiveSessions[tabId];
    return session != null && !session.finished;
  }

  /// User-initiated handoff while SSE is still streaming.
  Future<MobileAgentJob?> upgradeActiveToBackground() async {
    final tabId = ref.read(assistantTabsProvider).activeTabId;
    final session = _interactiveSessions[tabId];
    if (session == null || session.finished) return null;
    return session.requestUpgrade(reason: 'manual');
  }

  static String _clipHistory(String text, int maxRunes) {
    final trimmed = text.trim();
    if (trimmed.isEmpty) return trimmed;
    final runes = trimmed.runes.toList();
    if (runes.length <= maxRunes) return trimmed;
    return '${String.fromCharCodes(runes.take(maxRunes))}…';
  }

  Future<void> search(String query) async {
    final text = query.trim();
    if (text.isEmpty) return;
    // Pin the tab at start — user may switch tabs while the request runs.
    final tabId = ref.read(assistantTabsProvider).activeTabId;
    if (_searchingTabIds.contains(tabId)) return;
    _searchingTabIds.add(tabId);
    SearchAnswer? answer;
    var startedAt = DateTime.now();
    try {
      final tab = ref.read(assistantTabsProvider).tabs.firstWhere(
            (item) => item.id == tabId,
            orElse: () => ref.read(assistantTabsProvider).activeTab,
          );
      final client = ref.read(apiClientProvider);
      if (client == null) {
        final err = AsyncError<SearchAnswer?>(
          StateError('请先登录官方服务。'),
          StackTrace.current,
        );
        // Keep global provider in sync for callers that still watch it.
        state = err;
        ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, err);
        return;
      }
      final authSession = ref.read(sessionControllerProvider).valueOrNull;
      if (authSession?.bootstrap?.features.assistant == false) {
        final err = AsyncError<SearchAnswer?>(
          StateError('当前 Hub 未启用 AI 助手服务能力，仍可使用语音输入、图片/文件导入和文档草稿。'),
          StackTrace.current,
        );
        state = err;
        ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, err);
        return;
      }
      final previousMessages = tab.messages.length > 8
          ? tab.messages.sublist(tab.messages.length - 8)
          : tab.messages;
      // Multi-turn: structured messages[] for current Hub + legacy context strings
      // for older hubs that ignore the messages field. Clip to Hub-side limits.
      final historyMessages = <Map<String, String>>[
        for (final message in previousMessages)
          {
            'role': message.role,
            'content': _clipHistory(message.text, 4000),
          },
      ];
      final context = [
        for (final message in previousMessages)
          '${message.role}: ${_clipHistory(message.text, 4000)}',
      ];
      ref.read(assistantTabsProvider.notifier).appendMessage(
            tabId,
            AssistantConversationMessage.user(text),
          );
      const loading = AsyncLoading<SearchAnswer?>();
      // Only mirror loading onto the global provider when this tab is active,
      // so a background tab search does not flash loading on the visible tab.
      if (ref.read(assistantTabsProvider).activeTabId == tabId) {
        state = loading;
      }
      ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, loading);
      startedAt = DateTime.now();
      // Throttle mid-stream UI updates (~25 fps) to avoid rebuild storms on
      // high-frequency token deltas; always emit the final non-streaming frame.
      var lastUiEmit = DateTime.fromMillisecondsSinceEpoch(0);
      const uiMinInterval = Duration(milliseconds: 40);
      // Bound-document working object removed from product UI.
      const boundDocumentId = '';

      final interactive = _InteractiveSearchSession(
        tabId: tabId,
        query: text,
        historyMessages: historyMessages,
        context: context,
        boundDocumentId: boundDocumentId,
        enqueue: ({
          required List<Map<String, String>> historyMessages,
          required List<String> context,
          required String boundDocumentId,
          required String tabId,
        }) {
          return _enqueueBackgroundInternal(
            text,
            historyMessages: historyMessages,
            context: context,
            boundDocumentId: boundDocumentId,
            tabId: tabId,
            appendUserMessage: false,
          );
        },
        applyHandoff: (job, reason) {
          final reasonText = reason == 'timeout'
              ? '对话超过 ${assistantSyncUpgradeTimeout.inSeconds}s，已自动转为后台任务'
              : '已手动转为后台任务';
          final handoff = AsyncData<SearchAnswer?>(
            SearchAnswer(
              answer: '$reasonText ${job.jobId}。\n'
                  '可在「后台」Tab 查看进度；完成后结果会写回本对话。',
              citations: const [],
              llmMode: 'async_job_upgrade',
              llmRequestId: job.jobId,
              streaming: false,
            ),
          );
          if (ref.read(assistantTabsProvider).activeTabId == tabId) {
            state = handoff;
          }
          ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, handoff);
          ref.read(assistantTabsProvider.notifier).appendMessage(
                tabId,
                AssistantConversationMessage.assistant(
                  query: text,
                  text: handoff.value!.answer,
                  citations: const [],
                  llmMode: 'async_job_upgrade',
                  llmRequestId: job.jobId,
                  llmUsageRecordId: '',
                ),
              );
        },
      );
      _interactiveSessions[tabId] = interactive;

      final streamCompleter = Completer<void>();
      Object? streamError;
      StackTrace? streamStack;
      interactive.subscription = client
          .searchWithContextStream(
            text,
            messages: historyMessages,
            context: context,
            documentId: boundDocumentId,
          )
          .listen(
        (snapshot) {
          if (interactive.finished) return;
          answer = snapshot;
          final now = DateTime.now();
          final shouldEmit = !snapshot.streaming ||
              now.difference(lastUiEmit) >= uiMinInterval;
          if (shouldEmit) {
            lastUiEmit = now;
            final data = AsyncData<SearchAnswer?>(snapshot);
            if (ref.read(assistantTabsProvider).activeTabId == tabId) {
              state = data;
            }
            ref
                .read(assistantTabsProvider.notifier)
                .setResultForTab(tabId, data);
          }
          if (!snapshot.streaming && !streamCompleter.isCompleted) {
            streamCompleter.complete();
          }
        },
        onError: (Object error, StackTrace stack) {
          streamError = error;
          streamStack = stack;
          if (!streamCompleter.isCompleted) {
            streamCompleter.complete();
          }
        },
        onDone: () {
          if (!streamCompleter.isCompleted) {
            streamCompleter.complete();
          }
        },
        cancelOnError: true,
      );

      // Auto-upgrade timer (same budget as stream.timeout previously).
      final upgradeTimer = Timer(assistantSyncUpgradeTimeout, () {
        if (!interactive.finished) {
          unawaited(interactive.requestUpgrade(reason: 'timeout'));
        }
      });

      try {
        await Future.any<void>([
          streamCompleter.future,
          interactive.upgradeDone.future,
        ]);
      } finally {
        upgradeTimer.cancel();
        await interactive.subscription?.cancel();
        _interactiveSessions.remove(tabId);
      }

      if (interactive.upgraded) {
        // Handoff UI already applied; do not treat as interactive success/error.
        return;
      }
      if (streamError != null) {
        Error.throwWithStackTrace(
          streamError!,
          streamStack ?? StackTrace.current,
        );
      }
      final currentAnswer = answer;
      if (currentAnswer == null) {
        throw StateError('助手未返回结果，请重试。');
      }
      final finalized = currentAnswer.streaming
          ? currentAnswer.copyWith(streaming: false)
          : currentAnswer;
      answer = finalized;
      interactive.markFinished();
      final data = AsyncData<SearchAnswer?>(finalized);
      if (ref.read(assistantTabsProvider).activeTabId == tabId) {
        state = data;
      }
      ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, data);
      await ref
          .read(searchHistoryProvider.notifier)
          .record(text, finalized.answer);
      ref.read(assistantTabsProvider.notifier).appendMessage(
            tabId,
            AssistantConversationMessage.assistant(
              query: text,
              text: finalized.answer,
              citations: finalized.citations,
              llmMode: finalized.llmMode,
              llmRequestId: finalized.llmRequestId,
              llmUsageRecordId: finalized.llmUsageRecordId,
            ),
          );
    } catch (error, stack) {
      // Always attribute the error to the tab that started the search.
      final err = AsyncError<SearchAnswer?>(error, stack);
      if (ref.read(assistantTabsProvider).activeTabId == tabId) {
        state = err;
      }
      ref.read(assistantTabsProvider.notifier).setResultForTab(tabId, err);
      return;
    } finally {
      _searchingTabIds.remove(tabId);
      _interactiveSessions.remove(tabId);
    }
    final doneAnswer = answer;
    if (doneAnswer != null &&
        shouldNotifyAssistantLongTask(
          DateTime.now().difference(startedAt),
          doneAnswer.llmRequestId,
        )) {
      unawaited(_notifyLongTask(doneAnswer.llmRequestId));
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

  /// Enqueue the current query as a Hub background assistant job (长任务).
  Future<MobileAgentJob?> enqueueBackground(String query) async {
    final text = query.trim();
    if (text.isEmpty) return null;
    final client = ref.read(apiClientProvider);
    if (client == null) {
      throw StateError('请先登录官方服务。');
    }
    final session = ref.read(sessionControllerProvider).valueOrNull;
    if (session?.bootstrap?.features.assistant == false) {
      throw StateError('当前 Hub 未启用 AI 助手服务能力。');
    }
    final tabId = ref.read(assistantTabsProvider).activeTabId;
    final tab = ref.read(assistantTabsProvider).tabs.firstWhere(
          (item) => item.id == tabId,
          orElse: () => ref.read(assistantTabsProvider).activeTab,
        );
    final previousMessages = tab.messages.length > 8
        ? tab.messages.sublist(tab.messages.length - 8)
        : tab.messages;
    final historyMessages = <Map<String, String>>[
      for (final message in previousMessages)
        {
          'role': message.role,
          'content': _clipHistory(message.text, 4000),
        },
    ];
    final context = [
      for (final message in previousMessages)
        '${message.role}: ${_clipHistory(message.text, 4000)}',
    ];
    const boundDocumentId = '';
    final job = await _enqueueBackgroundInternal(
      text,
      historyMessages: historyMessages,
      context: context,
      boundDocumentId: boundDocumentId,
      tabId: tabId,
      appendUserMessage: true,
    );
    ref.read(assistantTabsProvider.notifier).appendMessage(
          tabId,
          AssistantConversationMessage.assistant(
            query: text,
            text: '已提交后台任务 ${job.jobId}（${job.status}）。\n'
                '可在「后台」Tab 查看进度；完成后会显示在统一任务列表。',
            citations: const [],
            llmMode: 'async_job',
            llmRequestId: job.jobId,
            llmUsageRecordId: '',
          ),
        );
    return job;
  }

  Future<MobileAgentJob> _enqueueBackgroundInternal(
    String text, {
    required List<Map<String, String>> historyMessages,
    required List<String> context,
    required String boundDocumentId,
    required String tabId,
    required bool appendUserMessage,
  }) async {
    final client = ref.read(apiClientProvider);
    if (client == null) {
      throw StateError('请先登录官方服务。');
    }
    final job = await client.createAgentJob(
      query: text,
      messages: historyMessages,
      context: context,
      documentId: boundDocumentId,
    );
    if (appendUserMessage) {
      ref.read(assistantTabsProvider.notifier).appendMessage(
            tabId,
            AssistantConversationMessage.user(text),
          );
    }
    ref.invalidate(mobileJobsProvider);
    unawaited(_pollAgentJobAndNotify(job.jobId, text, tabId));
    return job;
  }

  Future<void> _pollAgentJobAndNotify(
    String jobId,
    String query,
    String tabId,
  ) async {
    final client = ref.read(apiClientProvider);
    if (client == null || jobId.isEmpty) return;
    // Poll up to ~6 minutes (matches Hub job timeout).
    for (var i = 0; i < 72; i++) {
      await Future<void>.delayed(const Duration(seconds: 5));
      try {
        final job = await client.getAgentJob(jobId);
        if (job.isReady) {
          final answer =
              job.answer.trim().isEmpty ? '（后台任务已完成，无正文）' : job.answer;
          ref.read(assistantTabsProvider.notifier).appendMessage(
                tabId,
                AssistantConversationMessage.assistant(
                  query: query,
                  text: answer,
                  citations: const [],
                  llmMode: 'async_job',
                  llmRequestId:
                      job.llmRequestId.isEmpty ? jobId : job.llmRequestId,
                  llmUsageRecordId: '',
                ),
              );
          await ref.read(searchHistoryProvider.notifier).record(query, answer);
          ref.invalidate(mobileJobsProvider);
          try {
            await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
                  title: 'AI 助手后台任务完成',
                  body: '点击回到 AI 助手查看完整结果。',
                  payload: mobileAssistantTaskNotificationPayload(jobId),
                );
          } on Object {
            // Notification failure must not affect the answer.
          }
          return;
        }
        if (job.isFailed) {
          ref.read(assistantTabsProvider.notifier).appendMessage(
                tabId,
                AssistantConversationMessage.assistant(
                  query: query,
                  text:
                      '后台任务失败：${job.message.isEmpty ? job.status : job.message}',
                  citations: const [],
                  llmMode: 'async_job',
                  llmRequestId: jobId,
                  llmUsageRecordId: '',
                ),
              );
          ref.invalidate(mobileJobsProvider);
          return;
        }
      } on Object {
        // Transient poll errors — keep trying until budget exhausted.
      }
    }
  }
}

/// In-flight interactive SSE search that can be upgraded to a Hub job.
class _InteractiveSearchSession {
  final String tabId;
  final String query;
  final List<Map<String, String>> historyMessages;
  final List<String> context;
  final String boundDocumentId;
  final Future<MobileAgentJob> Function({
    required List<Map<String, String>> historyMessages,
    required List<String> context,
    required String boundDocumentId,
    required String tabId,
  }) enqueue;
  final void Function(MobileAgentJob job, String reason) applyHandoff;

  StreamSubscription<SearchAnswer>? subscription;
  bool finished = false;
  bool upgraded = false;
  final Completer<void> upgradeDone = Completer<void>();
  Future<MobileAgentJob?>? _upgradeFuture;

  _InteractiveSearchSession({
    required this.tabId,
    required this.query,
    required this.historyMessages,
    required this.context,
    required this.boundDocumentId,
    required this.enqueue,
    required this.applyHandoff,
  });

  void markFinished() {
    finished = true;
  }

  Future<MobileAgentJob?> requestUpgrade({required String reason}) {
    if (finished || upgraded) {
      return _upgradeFuture ?? Future<MobileAgentJob?>.value(null);
    }
    _upgradeFuture ??= _doUpgrade(reason);
    return _upgradeFuture!;
  }

  Future<MobileAgentJob?> _doUpgrade(String reason) async {
    upgraded = true;
    finished = true;
    try {
      await subscription?.cancel();
    } on Object {
      // Ignore cancel races.
    }
    try {
      final job = await enqueue(
        historyMessages: historyMessages,
        context: context,
        boundDocumentId: boundDocumentId,
        tabId: tabId,
      );
      applyHandoff(job, reason);
      if (!upgradeDone.isCompleted) {
        upgradeDone.complete();
      }
      return job;
    } on Object {
      if (!upgradeDone.isCompleted) {
        upgradeDone.complete();
      }
      rethrow;
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
    // Keep the last meaningful tab title when the composer is cleared after send.
    if (text.isEmpty) {
      final previous = fallback.trim();
      if (previous.isEmpty) return '副对话';
      return previous;
    }
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
