import 'package:drift/native.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/network/mobile_network_status.dart';
import 'package:maclaw_mobile/core/settings/app_preferences.dart';
import 'package:maclaw_mobile/core/shared_intents/mobile_shared_intent.dart';
import 'package:maclaw_mobile/core/shared_intents/shared_intent_bootstrap.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/assistant/assistant_controller.dart';
import 'package:maclaw_mobile/features/assistant/assistant_screen.dart';
import 'package:maclaw_mobile/features/assistant/assistant_voice_input.dart';
import 'package:maclaw_mobile/features/assistant/search_history.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';
import 'package:maclaw_mobile/features/documents/documents_controller.dart';

class _TestAppPreferencesController extends AppPreferencesController {
  @override
  Future<AppPreferences> build() async => const AppPreferences();
}

class _FakeAssistantVoiceInput implements AssistantVoiceInput {
  final String text;
  final bool ready;
  String? localeId;
  bool stopped = false;

  _FakeAssistantVoiceInput({
    required this.text,
    this.ready = true,
  });

  @override
  Future<bool> start({
    required String localeId,
    required ValueChanged<String> onText,
  }) async {
    this.localeId = localeId;
    if (!ready) return false;
    onText(text);
    return true;
  }

  @override
  Future<void> stop() async {
    stopped = true;
  }
}

class _ResultAssistantSearchController extends AssistantSearchController {
  @override
  Future<SearchAnswer?> build() async => const SearchAnswer(
        answer: '结论：官方服务运行正常。',
        citations: [
          SearchCitation(
            title: 'MaClaw 状态页',
            url: 'https://hubs.mypapers.top/status',
            snippet: '所有移动服务均可用。',
          ),
        ],
      );
}

class _RecordingAssistantSearchController extends AssistantSearchController {
  static final queries = <String>[];

  @override
  Future<SearchAnswer?> build() async => null;

  @override
  Future<void> search(String query) async {
    queries.add(query);
    state = AsyncData(
      SearchAnswer(
        answer: '已整理分享内容：$query',
        citations: const [],
      ),
    );
  }
}

class _FakeSearchApiClient extends ApiClient {
  static final queries = <String>[];

  _FakeSearchApiClient() : super();

  @override
  Future<SearchAnswer> search(String query) async {
    queries.add(query);
    return SearchAnswer(
      answer: '移动助手回答：$query',
      citations: const [
        SearchCitation(
          title: '官方服务来源',
          url: 'https://hubs.mypapers.top/status',
          snippet: '来自 MaClaw 官方服务的助手联网结果。',
        ),
      ],
    );
  }
}

class _AssistantDisabledSessionController extends SessionController {
  @override
  Future<SessionState> build() async => const SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: MobileBootstrap(
          user: MobileUser(
            userId: 'u1',
            email: '',
            tenantId: 'tenant-a',
          ),
          services: MobileServices(
            hubStatus: 'online',
            llmStatus: 'available',
            searchStatus: 'disabled',
            documentsStatus: 'available',
            digitalEmployeesStatus: 'available',
            llmStatusPath: '/api/llm/status',
            modelsPath: '/api/llm/models',
            searchPath: '',
            documentsPath: '/api/mobile/documents',
            digitalEmployeesPath: '/api/mobile/digital-employees',
            realtimePath: '/api/mobile/realtime',
          ),
          connection: MobileConnection(
            hubCenterCandidates: [
              'https://hubs.mypapers.top',
              'https://hubs.maclaw.top',
              'https://hubs2.maclaw.top',
            ],
            selectedHubCenterUrl: 'https://hubs.mypapers.top',
            hubUrl: 'https://tenant-a.maclaw.top',
            hubId: 'hub-a',
            tenantId: 'tenant-a',
          ),
          features: MobileFeatures(
            search: false,
            documents: true,
            localSsh: true,
            digitalEmployees: true,
            pushNotifications: false,
          ),
          limits: MobileLimits(maxUploadBytes: 1024, maxExportJobs: 2),
        ),
      );
}

class _AssistantEnabledSessionController extends SessionController {
  @override
  Future<SessionState> build() async => SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: _assistantTestBootstrap(searchEnabled: true),
      );
}

MobileBootstrap _assistantTestBootstrap({required bool searchEnabled}) {
  return MobileBootstrap(
    user: const MobileUser(
      userId: 'u1',
      email: '',
      tenantId: 'tenant-a',
    ),
    services: MobileServices(
      hubStatus: 'online',
      llmStatus: 'available',
      searchStatus: searchEnabled ? 'available' : 'disabled',
      documentsStatus: 'available',
      digitalEmployeesStatus: 'available',
      llmStatusPath: '/api/llm/status',
      modelsPath: '/api/llm/models',
      searchPath: searchEnabled ? '/api/mobile/search' : '',
      documentsPath: '/api/mobile/documents',
      digitalEmployeesPath: '/api/mobile/digital-employees',
      realtimePath: '/api/mobile/realtime',
    ),
    connection: const MobileConnection(
      hubCenterCandidates: [
        'https://hubs.mypapers.top',
        'https://hubs.maclaw.top',
        'https://hubs2.maclaw.top',
      ],
      selectedHubCenterUrl: 'https://hubs.mypapers.top',
      hubUrl: 'https://tenant-a.maclaw.top',
      hubId: 'hub-a',
      tenantId: 'tenant-a',
    ),
    features: MobileFeatures(
      search: searchEnabled,
      documents: true,
      localSsh: true,
      digitalEmployees: true,
      pushNotifications: false,
    ),
    limits: const MobileLimits(maxUploadBytes: 1024, maxExportJobs: 2),
  );
}

class _RecordingDocumentsController extends DocumentsController {
  static final created = <({
    String title,
    DocumentTemplate template,
    String content,
  })>[];
  static final uploaded = <String>[];

  @override
  Future<DocumentsState> build() async => const DocumentsState();

  @override
  Future<void> createDraft({
    required String title,
    required DocumentTemplate template,
    String content = '',
  }) async {
    created.add((title: title, template: template, content: content));
    state = AsyncData(
      DocumentsState(
        draft: DocumentDraft(
          id: 'draft-${created.length}',
          title: title,
          template: template,
          markdown: content,
          updatedAt: DateTime.utc(2026, 7, 2),
        ),
      ),
    );
  }

  @override
  Future<void> uploadSharedDocument(String path) async {
    uploaded.add(path);
    state = AsyncData(
      DocumentsState(
        uploadTask: MobileDocumentUploadTask(
          taskId: 'upload-${uploaded.length}',
          filename: path.split('/').last,
          status: 'queued',
        ),
        lastUploadPath: path,
      ),
    );
  }
}

class _InitialAssistantSharedIntentController
    extends MobileSharedIntentController {
  @override
  MobileSharedIntent? build() => MobileSharedIntent(
        id: 'shared-link-1',
        kind: MobileSharedIntentKind.link,
        value: 'https://example.com/incident',
        receivedAt: DateTime.utc(2026, 7, 2),
      );
}

class _MixedTextLinkSharedIntentController
    extends MobileSharedIntentController {
  @override
  MobileSharedIntent? build() => MobileSharedIntent(
        id: 'shared-link-mixed',
        kind: MobileSharedIntentKind.link,
        value: '请看这个事故复盘：https://example.com/incident?from=im',
        receivedAt: DateTime.utc(2026, 7, 2),
      );
}

class _FakeHistoryStore extends MobileLocalStore {
  List<SearchHistoryEntry> entries;

  _FakeHistoryStore(this.entries);

  @override
  Future<List<SearchHistoryEntry>> loadSearchHistory() async => entries;

  @override
  Future<void> saveSearchHistory(List<SearchHistoryEntry> entries) async {
    this.entries = entries;
  }
}

void main() {
  test('assistant citation merge preserves shared URL fallback', () {
    final merged = mergeAssistantCitations(
      const [],
      const SearchCitation(
        title: '分享来源',
        url: 'https://example.com/incident',
        snippet: '从系统分享进入 MaClaw Mobile 的链接。',
      ),
    );

    expect(merged, hasLength(1));
    expect(merged.single.title, '分享来源');
    expect(merged.single.url, 'https://example.com/incident');
  });

  test('assistant citation merge deduplicates fallback URLs', () {
    final merged = mergeAssistantCitations(
      const [
        SearchCitation(
          title: '后端来源',
          url: 'https://example.com/incident',
          snippet: '已有来源。',
        ),
      ],
      const SearchCitation(
        title: '分享来源',
        url: 'https://example.com/incident',
        snippet: '从系统分享进入 MaClaw Mobile 的链接。',
      ),
    );

    expect(merged, hasLength(1));
    expect(merged.single.title, '后端来源');
  });

  test('assistant export markdown redacts common secrets', () {
    final markdown = assistantAnswerMarkdown(
      query: 'check outage token: query-secret',
      answer: 'service ok\nAuthorization: Bearer answer-secret',
      citations: const [
        SearchCitation(
          title: 'source api_key=source-key',
          url: 'https://user:pass@example.com/status',
          snippet: 'snippet password=snippet-password',
        ),
      ],
    );

    expect(markdown, contains('token=[REDACTED_SECRET]'));
    expect(markdown, contains('Authorization: Bearer [REDACTED_TOKEN]'));
    expect(markdown, contains('api_key=[REDACTED_SECRET]'));
    expect(markdown, contains('https://[REDACTED_CREDENTIALS]@example.com'));
    expect(markdown, contains('password=[REDACTED_SECRET]'));
    expect(markdown, isNot(contains('query-secret')));
    expect(markdown, isNot(contains('answer-secret')));
    expect(markdown, isNot(contains('source-key')));
    expect(markdown, isNot(contains('user:pass')));
    expect(markdown, isNot(contains('snippet-password')));
  });

  test('assistant citation markdown redacts copied source secrets', () {
    final citation = assistantCitationMarkdown(
      const SearchCitation(
        title: 'status token: citation-token',
        url: 'https://ops:secret@example.com/logs',
        snippet: 'Authorization: Bearer citation-secret',
      ),
    );

    expect(citation, contains('token=[REDACTED_SECRET]'));
    expect(citation, contains('https://[REDACTED_CREDENTIALS]@example.com'));
    expect(citation, contains('Authorization: Bearer [REDACTED_TOKEN]'));
    expect(citation, isNot(contains('citation-token')));
    expect(citation, isNot(contains('ops:secret')));
    expect(citation, isNot(contains('citation-secret')));
  });

  test('assistant history redacts query and answer secrets', () async {
    final store = _FakeHistoryStore([]);
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
      ],
    );
    addTearDown(container.dispose);

    await container.read(searchHistoryProvider.future);
    await container.read(searchHistoryProvider.notifier).record(
          'check production incident token: query-secret '
              'password="query password"',
          'service ok\nAuthorization: Bearer history-token\n'
              'password=history-password',
        );

    expect(store.entries, hasLength(1));
    expect(
      store.entries.single.query,
      contains('token=[REDACTED_SECRET]'),
    );
    expect(
      store.entries.single.query,
      contains('password=[REDACTED_SECRET]'),
    );
    expect(store.entries.single.query, isNot(contains('query-secret')));
    expect(store.entries.single.query, isNot(contains('query password')));
    expect(
      store.entries.single.answerPreview,
      contains('Authorization: Bearer [REDACTED_TOKEN]'),
    );
    expect(
      store.entries.single.answerPreview,
      contains('password=[REDACTED_SECRET]'),
    );
    expect(
      store.entries.single.answerPreview,
      isNot(contains('history-token')),
    );
    expect(
      store.entries.single.answerPreview,
      isNot(contains('history-password')),
    );
  });

  testWidgets('assistant screen handles shared links automatically',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingAssistantSearchController.queries.clear();
    tester.view.physicalSize = const Size(1200, 2000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          mobileSharedIntentProvider.overrideWith(
            _InitialAssistantSharedIntentController.new,
          ),
          sessionControllerProvider.overrideWith(
            _AssistantEnabledSessionController.new,
          ),
          assistantSearchProvider.overrideWith(
            _RecordingAssistantSearchController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pumpAndSettle();
    await tester.pump(const Duration(milliseconds: 300));

    expect(_RecordingAssistantSearchController.queries, hasLength(1));
    expect(
      _RecordingAssistantSearchController.queries.single,
      '请交给 MaClaw AI 助手处理这个链接，保留来源引用：https://example.com/incident',
    );
    final container = ProviderScope.containerOf(
      tester.element(find.byType(AssistantScreen)),
      listen: false,
    );
    final sharedCitation = container.read(assistantSharedCitationProvider);
    expect(sharedCitation?.title, '分享来源');
    expect(sharedCitation?.url, 'https://example.com/incident');
    expect(sharedCitation?.snippet, '从系统分享进入 MaClaw Mobile 的链接。');
    expect(find.textContaining('https://example.com/incident'), findsWidgets);
  });

  testWidgets('assistant shared text extracts URL for citation fallback',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingAssistantSearchController.queries.clear();
    tester.view.physicalSize = const Size(1200, 2000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          mobileSharedIntentProvider.overrideWith(
            _MixedTextLinkSharedIntentController.new,
          ),
          sessionControllerProvider.overrideWith(
            _AssistantEnabledSessionController.new,
          ),
          assistantSearchProvider.overrideWith(
            _RecordingAssistantSearchController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pumpAndSettle();
    await tester.pump(const Duration(milliseconds: 300));

    expect(_RecordingAssistantSearchController.queries, hasLength(1));
    expect(
      _RecordingAssistantSearchController.queries.single,
      contains('https://example.com/incident?from=im'),
    );
    expect(
      _RecordingAssistantSearchController.queries.single,
      contains('分享附带说明'),
    );
    final container = ProviderScope.containerOf(
      tester.element(find.byType(AssistantScreen)),
      listen: false,
    );
    final sharedCitation = container.read(assistantSharedCitationProvider);
    expect(sharedCitation?.title, '分享来源');
    expect(sharedCitation?.url, 'https://example.com/incident?from=im');
    expect(
      find.textContaining('https://example.com/incident?from=im'),
      findsWidgets,
    );
  });

  testWidgets('assistant screen exposes GUI-like AI assistant actions',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    expect(find.text('AI助手'), findsOneWidget);
    expect(
      find.text(
        '类似 MaClaw GUI 的多对话 AI 助手，可文字或语音输入，也可接入截图、文件、服务器日志和数字员工能力。',
      ),
      findsOneWidget,
    );
    expect(find.byTooltip('开始语音输入'), findsOneWidget);
    expect(find.byTooltip('语音输入'), findsOneWidget);
    expect(find.text('问 MaClaw AI 助手'), findsOneWidget);
    expect(
      find.text('支持普通对话、助手联网、文档处理、日志排障和应急操作草案。'),
      findsOneWidget,
    );
    expect(find.text('MaClaw AI 助手'), findsOneWidget);
    expect(
      find.text('像电脑端一样用自然语言发起多轮对话；手机端重点支持语音、截图、文件、服务器日志和应急排障。'),
      findsOneWidget,
    );
    expect(find.text('语音输入'), findsOneWidget);
    expect(find.text('自由对话'), findsOneWidget);
    expect(find.text('助手联网'), findsWidgets);
    expect(find.text('截图提问'), findsOneWidget);
    expect(find.text('远程排障'), findsOneWidget);
    expect(find.text('文档草稿'), findsWidgets);
    expect(find.text('日志排障'), findsOneWidget);
    expect(find.text('发送给 AI 助手'), findsOneWidget);
    expect(find.byTooltip('拍照提问'), findsOneWidget);
    expect(find.byTooltip('导入截图或文件'), findsOneWidget);
  });

  testWidgets('assistant quick prompts fill the mobile query field',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    await tester.tap(find.text('日志排障'));
    await tester.pump();

    final queryField = tester.widget<TextField>(
      find.widgetWithText(TextField, '问 MaClaw AI 助手'),
    );
    expect(queryField.controller?.text, contains('服务器日志'));
    expect(queryField.controller?.text, contains('人工确认'));
  });

  testWidgets(
      'assistant disables sending when Hub assistant access is unavailable',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _FakeSearchApiClient.queries.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(_FakeSearchApiClient()),
          sessionControllerProvider.overrideWith(
            _AssistantDisabledSessionController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();
    await tester.enterText(
      find.widgetWithText(TextField, '问 MaClaw AI 助手'),
      '帮我查服务状态',
    );
    await tester.pump();

    expect(find.textContaining('当前 Hub 未启用助手联网能力'), findsOneWidget);
    final sendButton = tester.widget<FilledButton>(
      find.widgetWithText(FilledButton, '发送给 AI 助手'),
    );
    expect(sendButton.onPressed, isNull);
    expect(_FakeSearchApiClient.queries, isEmpty);
  });

  testWidgets('shared assistant links respect disabled Hub assistant access',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _FakeSearchApiClient.queries.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(_FakeSearchApiClient()),
          sessionControllerProvider.overrideWith(
            _AssistantDisabledSessionController.new,
          ),
          mobileSharedIntentProvider.overrideWith(
            _InitialAssistantSharedIntentController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pumpAndSettle();

    expect(_FakeSearchApiClient.queries, isEmpty);
    expect(find.textContaining('当前 Hub 未启用助手联网能力'), findsWidgets);
    expect(find.textContaining('https://example.com/incident'), findsWidgets);
  });

  testWidgets('assistant supports a primary tab and secondary tabs',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    expect(find.text('主对话'), findsOneWidget);
    await tester.tap(find.byTooltip('新建副对话'));
    await tester.pump();
    expect(find.text('副对话 1'), findsOneWidget);

    await tester.enterText(
      find.widgetWithText(TextField, '问 MaClaw AI 助手'),
      '副对话里的应急问题',
    );
    await tester.pump();
    expect(find.text('副对话里的应急问题'), findsWidgets);

    await tester.ensureVisible(find.text('主对话'));
    await tester.tap(find.text('主对话'));
    await tester.pump();
    var queryField = tester.widget<TextField>(
      find.widgetWithText(TextField, '问 MaClaw AI 助手'),
    );
    expect(queryField.controller?.text, isEmpty);

    await tester.tap(find.textContaining('副对话里的应'));
    await tester.pump();
    queryField = tester.widget<TextField>(
      find.widgetWithText(TextField, '问 MaClaw AI 助手'),
    );
    expect(queryField.controller?.text, '副对话里的应急问题');
  });

  testWidgets('assistant tabs keep independent assistant results',
      (tester) async {
    final store = _FakeHistoryStore([]);
    _FakeSearchApiClient.queries.clear();
    tester.view.physicalSize = const Size(1200, 2200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(_FakeSearchApiClient()),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    await tester.enterText(
      find.widgetWithText(TextField, '问 MaClaw AI 助手'),
      'main incident',
    );
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('发送给 AI 助手'));
    await tester.tap(find.text('发送给 AI 助手'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.textContaining('main incident'), findsWidgets);

    await tester.tap(find.byIcon(Icons.add));
    await tester.pump();
    await tester.enterText(
      find.widgetWithText(TextField, '问 MaClaw AI 助手'),
      'secondary incident',
    );
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('发送给 AI 助手'));
    await tester.tap(find.text('发送给 AI 助手'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.textContaining('secondary incident'), findsWidgets);
    expect(find.text('移动助手回答：secondary incident'), findsNWidgets(2));

    await tester.ensureVisible(find.text('主对话'));
    await tester.tap(find.text('主对话'));
    await tester.pumpAndSettle();
    expect(find.textContaining('main incident'), findsWidgets);
    expect(find.text('移动助手回答：secondary incident'), findsOneWidget);

    await tester.tap(find.textContaining('secondary in').first);
    await tester.pumpAndSettle();
    expect(find.textContaining('secondary incident'), findsWidgets);
    expect(find.text('移动助手回答：main incident'), findsOneWidget);
    expect(
      _FakeSearchApiClient.queries,
      ['main incident', 'secondary incident'],
    );
  });

  testWidgets('assistant send uses official API and records history',
      (tester) async {
    final store = _FakeHistoryStore([]);
    _FakeSearchApiClient.queries.clear();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(_FakeSearchApiClient()),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    await tester.enterText(
      find.widgetWithText(TextField, '问 MaClaw AI 助手'),
      '查一下 MaClaw 官方移动服务状态',
    );
    await tester.pump();
    final searchButton = find.text('发送给 AI 助手');
    await tester.ensureVisible(searchButton);
    await tester.tap(searchButton);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(_FakeSearchApiClient.queries, ['查一下 MaClaw 官方移动服务状态']);
    expect(find.text('移动助手回答：查一下 MaClaw 官方移动服务状态'), findsOneWidget);
    expect(find.text('助手回答'), findsOneWidget);
    expect(find.text('官方服务来源'), findsOneWidget);
    expect(find.text('https://hubs.mypapers.top/status'), findsOneWidget);
    expect(store.entries, hasLength(1));
    expect(store.entries.single.query, '查一下 MaClaw 官方移动服务状态');
    expect(store.entries.single.answerPreview, contains('移动助手回答'));
  });

  testWidgets('assistant voice input fills the assistant query',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final voice = _FakeAssistantVoiceInput(text: '请 AI 助手分析今天的服务器告警');
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          assistantVoiceInputProvider.overrideWithValue(voice),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    await tester.tap(find.byTooltip('开始语音输入'));
    await tester.pump();

    expect(voice.localeId, 'zh_CN');
    expect(find.text('请 AI 助手分析今天的服务器告警'), findsOneWidget);
    expect(find.text('正在听写，识别结果会填入 AI 助手输入框'), findsOneWidget);
  });

  testWidgets('assistant voice input explains unavailable microphone',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final voice = _FakeAssistantVoiceInput(text: '', ready: false);
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          assistantVoiceInputProvider.overrideWithValue(voice),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    await tester.tap(find.byTooltip('开始语音输入'));
    await tester.pump();

    expect(find.text('语音输入不可用，请检查麦克风权限'), findsWidgets);
  });

  testWidgets('assistant file import enters document parsing flow',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingDocumentsController.uploaded.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          documentsControllerProvider.overrideWith(
            _RecordingDocumentsController.new,
          ),
          assistantFilePathPickerProvider.overrideWithValue(
            () async => '/tmp/mobile-incident.pdf',
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    await tester.tap(find.byTooltip('导入截图或文件'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(
      _RecordingDocumentsController.uploaded,
      ['/tmp/mobile-incident.pdf'],
    );
    expect(find.text('请总结刚导入文件或截图的关键信息。'), findsOneWidget);
    expect(
      find.text('文件已提交文档解析，完成后可在“文档”页继续处理。'),
      findsOneWidget,
    );
  });

  testWidgets('assistant camera capture enters document parsing flow',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingDocumentsController.uploaded.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          documentsControllerProvider.overrideWith(
            _RecordingDocumentsController.new,
          ),
          assistantCameraImagePathPickerProvider.overrideWithValue(
            () async => '/tmp/mobile-photo.jpg',
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    await tester.tap(find.byTooltip('拍照提问'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(_RecordingDocumentsController.uploaded, ['/tmp/mobile-photo.jpg']);
    expect(find.text('请分析刚拍摄的图片，并给出可执行结论。'), findsOneWidget);
    expect(
      find.text('图片已提交文档解析，完成后可在“文档”页继续处理。'),
      findsOneWidget,
    );
  });

  testWidgets('assistant gallery screenshot enters document parsing flow',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingDocumentsController.uploaded.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          documentsControllerProvider.overrideWith(
            _RecordingDocumentsController.new,
          ),
          assistantGalleryImagePathPickerProvider.overrideWithValue(
            () async => '/tmp/mobile-screenshot.png',
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    await tester.tap(find.byTooltip('从相册选择截图'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(
      _RecordingDocumentsController.uploaded,
      ['/tmp/mobile-screenshot.png'],
    );
    expect(
      find.text('请分析这张截图或相册图片，提取关键信息并给出下一步。'),
      findsOneWidget,
    );
    expect(
      find.text('截图/图片已提交文档解析，完成后可在“文档”页继续处理。'),
      findsOneWidget,
    );
  });

  testWidgets('assistant result exposes citation copy and share actions',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final copiedTexts = <String>[];
    final sharedTexts = <String>[];
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          assistantSearchProvider.overrideWith(
            _ResultAssistantSearchController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          assistantClipboardWriterProvider.overrideWithValue(
            (text) async => copiedTexts.add(text),
          ),
          assistantShareProvider.overrideWithValue(
            (text) async => sharedTexts.add(text),
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();
    await tester.drag(find.byType(ListView).first, const Offset(0, -650));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('来源'), findsOneWidget);
    expect(find.text('MaClaw 状态页'), findsOneWidget);
    expect(find.text('https://hubs.mypapers.top/status'), findsOneWidget);
    expect(find.text('复制链接'), findsOneWidget);
    expect(find.text('复制引用'), findsOneWidget);
    expect(find.text('分享来源'), findsOneWidget);

    await tester.tap(find.text('分享结果'));
    await tester.pump(const Duration(milliseconds: 300));
    expect(sharedTexts, hasLength(1));
    expect(sharedTexts.single, contains('结论：官方服务运行正常。'));
    expect(sharedTexts.single, contains('https://hubs.mypapers.top/status'));

    await tester.tap(find.text('复制结果'));
    await tester.pump(const Duration(milliseconds: 300));
    expect(copiedTexts, hasLength(1));
    expect(copiedTexts.single, contains('结论：官方服务运行正常。'));
    expect(copiedTexts.single, contains('MaClaw 状态页'));

    await tester.tap(find.text('复制链接'));
    await tester.pump(const Duration(milliseconds: 300));
    expect(copiedTexts.last, 'https://hubs.mypapers.top/status');

    await tester.tap(find.text('复制引用'));
    await tester.pump(const Duration(milliseconds: 300));
    expect(copiedTexts.last, contains('MaClaw 状态页'));
    expect(copiedTexts.last, contains('https://hubs.mypapers.top/status'));

    await tester.tap(find.text('分享来源'));
    await tester.pump(const Duration(milliseconds: 300));
    expect(sharedTexts, hasLength(2));
    expect(sharedTexts.last, contains('MaClaw 状态页'));
    expect(sharedTexts.last, contains('https://hubs.mypapers.top/status'));
  });

  testWidgets(
      'assistant result can become every document template with citations',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingDocumentsController.created.clear();
    tester.view.physicalSize = const Size(1200, 2200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          assistantSearchProvider.overrideWith(
            _ResultAssistantSearchController.new,
          ),
          assistantQueryProvider.overrideWith(
            (ref) => '排查 MaClaw Mobile 官方服务状态',
          ),
          documentsControllerProvider.overrideWith(
            _RecordingDocumentsController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    for (final template in DocumentTemplate.values) {
      final draftButton = find.text('整理为草稿');
      await tester.ensureVisible(draftButton);
      await tester.pump(const Duration(milliseconds: 300));
      await tester.tap(draftButton);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      for (final label in ['通知', '报告', '邮件', '方案', '会议纪要', '说明书']) {
        expect(find.text(label), findsOneWidget);
      }

      final label = documentTemplateLabel(template);
      final templateTile = find.text(label).last;
      await tester.ensureVisible(templateTile);
      await tester.tap(templateTile);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      expect(
        _RecordingDocumentsController.created.last.title,
        'AI助手：排查 MaClaw Mobile 官方服务状态',
      );
      expect(_RecordingDocumentsController.created.last.template, template);
      expect(
        _RecordingDocumentsController.created.last.content,
        contains('## 问题'),
      );
      expect(
        _RecordingDocumentsController.created.last.content,
        contains('排查 MaClaw Mobile 官方服务状态'),
      );
      expect(
        _RecordingDocumentsController.created.last.content,
        contains('## 结论'),
      );
      expect(
        _RecordingDocumentsController.created.last.content,
        contains('结论：官方服务运行正常。'),
      );
      expect(
        _RecordingDocumentsController.created.last.content,
        contains('## 来源'),
      );
      expect(
        _RecordingDocumentsController.created.last.content,
        contains('MaClaw 状态页'),
      );
      expect(
        _RecordingDocumentsController.created.last.content,
        contains('https://hubs.mypapers.top/status'),
      );
      expect(
        _RecordingDocumentsController.created.last.content,
        contains('所有移动服务均可用。'),
      );
    }

    expect(
      _RecordingDocumentsController.created.map((item) => item.template),
      DocumentTemplate.values,
    );
  });

  testWidgets('assistant history confirms before keeping only favorites',
      (tester) async {
    final now = DateTime.utc(2026, 7, 2);
    final store = _FakeHistoryStore([
      SearchHistoryEntry(
        id: 'favorite',
        query: '常用：排查 502',
        answerPreview: '检查网关和 upstream',
        createdAt: now,
        favorite: true,
      ),
      SearchHistoryEntry(
        id: 'temporary',
        query: '临时：天气',
        answerPreview: '今天有雨',
        createdAt: now,
      ),
    ]);
    tester.view.physicalSize = const Size(1200, 1800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: now,
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: AssistantScreen())),
      ),
    );
    await tester.pump();

    expect(find.text('常用问题'), findsOneWidget);
    expect(find.text('最近对话'), findsOneWidget);
    expect(find.text('常用：排查 502'), findsOneWidget);
    expect(find.text('临时：天气'), findsOneWidget);

    await tester.ensureVisible(find.byIcon(Icons.cleaning_services_outlined));
    await tester.tap(find.byIcon(Icons.cleaning_services_outlined));
    await tester.pump();

    expect(find.text('只保留收藏问题？'), findsOneWidget);
    expect(store.entries.length, 2);

    await tester.tap(find.text('只保留收藏'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(store.entries.map((entry) => entry.id), ['favorite']);
    expect(find.text('已清理未收藏历史，常用问题已保留'), findsOneWidget);
  });
}
