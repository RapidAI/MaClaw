import 'dart:async';

import 'package:drift/native.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/app.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/network/mobile_network_status.dart';
import 'package:maclaw_mobile/core/notifications/mobile_notification_service.dart';
import 'package:maclaw_mobile/core/settings/app_preferences.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/core/shared_intents/mobile_shared_intent.dart';
import 'package:maclaw_mobile/core/shared_intents/shared_intent_bootstrap.dart';
import 'package:maclaw_mobile/features/assistant/assistant_controller.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/documents/documents_controller.dart';
import 'support/widget_pumps.dart';

class _SignedOutSessionController extends SessionController {
  @override
  Future<SessionState> build() async => const SessionState.signedOut();
}

class _SignedInSessionController extends SessionController {
  final MobileLlmAccess llmAccess;

  _SignedInSessionController(this.llmAccess);

  @override
  Future<SessionState> build() async => SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: _bootstrap(llmAccess: llmAccess),
      );
}

class _LoadingSessionController extends SessionController {
  @override
  Future<SessionState> build() => Completer<SessionState>().future;
}

class _TransitioningSessionController extends SessionController {
  @override
  Future<SessionState> build() async => const SessionState.signedOut();

  void completeLogin() {
    state = AsyncData(
      SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: _bootstrap(
          llmAccess: const MobileLlmAccess(
            mode: 'maclaw_official',
            status: 'available',
            authorizationId: '',
            authorizedBy: '',
            creditsAccount: 'phone:19900001111',
            authorizedAt: null,
          ),
        ),
      ),
    );
  }
}

class _PendingSharedLinkController extends MobileSharedIntentController {
  @override
  MobileSharedIntent? build() => MobileSharedIntent(
        id: 'pending-link-after-login',
        kind: MobileSharedIntentKind.link,
        value: 'https://status.example.com/incident/42',
        message: '请在登录后继续分析这个链接',
        receivedAt: DateTime.utc(2026, 7, 10),
      );
}

class _PendingSharedDocumentController extends MobileSharedIntentController {
  @override
  MobileSharedIntent? build() => MobileSharedIntent(
        id: 'pending-document-after-login',
        kind: MobileSharedIntentKind.file,
        value: '/tmp/incident-report.pdf',
        mimeType: 'application/pdf',
        receivedAt: DateTime.utc(2026, 7, 10),
      );
}

class _PendingDocumentFlowController extends DocumentsController {
  static final uploadedPaths = <String>[];

  @override
  Future<DocumentsState> build() async => const DocumentsState();

  @override
  Future<void> uploadSharedDocument(String path) async {
    uploadedPaths.add(path);
  }
}

class _TestAppPreferencesController extends AppPreferencesController {
  @override
  Future<AppPreferences> build() async =>
      const AppPreferences(language: appLanguageChinese);
}

const _connectingOfficialService =
    '\u6b63\u5728\u8fde\u63a5 MaClaw \u5b98\u65b9\u670d\u52a1';
const _llmSetupTitle = '\u914d\u7f6e MaClaw LLM \u670d\u52a1';
const _connectOfficialService = '\u63a5\u5165\u5b98\u65b9\u670d\u52a1';
const _connectQrProvider = '\u63a5\u5165\u4e8c\u7ef4\u7801\u670d\u52a1\u5546';
const _phoneRegistrationLogin = '\u624b\u673a\u53f7\u6ce8\u518c/\u767b\u5f55';
const _sendVerificationCode = '\u53d1\u9001\u9a8c\u8bc1\u7801';
const _assistantTab = 'AI助手';
const _mainConversation = '\u4e3b\u5bf9\u8bdd';
const _documentsScreenSubtitle =
    '\u4e0e\u7535\u8111\u7aef MaClaw GUI \u5171\u4eab\u540c\u4e00 Hub \u6587\u7a3f\u5e93\u3002\u624b\u673a\u4fa7\u91cd\u67e5\u770b\u3001\u5bfc\u5165\u3001AI \u5904\u7406\u4e0e\u5206\u4eab\uff0c\u6b63\u6587\u8bf7\u7528\u7535\u8111 GUI \u6216 AI \u52a9\u624b\u6539\u5199\u3002';
const _tasksScreenSubtitle =
    '\u957f\u4efb\u52a1\u7edf\u4e00\u67e5\u770b\uff1a\u6587\u6863\u89e3\u6790/\u5bfc\u51fa\u3001\u5458\u5de5\u4efb\u52a1\u7b49\u3002\u77ed\u64cd\u4f5c\u8bf7\u56de AI \u52a9\u624b\u6216\u6570\u5b57\u5458\u5de5\u9875\u3002';
const _emergencyServers = '\u5e94\u6025\u670d\u52a1\u5668';
const _openedTaskNotification = '\u5df2\u6253\u5f00\u4efb\u52a1\u63d0\u9192';
const _unknownTaskNotification =
    '\u65e0\u6cd5\u8bc6\u522b\u4efb\u52a1\u63d0\u9192';

void main() {
  testWidgets('startup shows MaClaw logo while session is loading',
      (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionControllerProvider.overrideWith(
            _LoadingSessionController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );

    expect(find.text('MaClaw Mobile'), findsOneWidget);
    expect(find.text(_connectingOfficialService), findsOneWidget);
  });

  testWidgets('renders phone registration before mobile service is configured',
      (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionControllerProvider.overrideWith(
            _SignedOutSessionController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();

    expect(find.text(_phoneRegistrationLogin), findsOneWidget);
    expect(find.text(_sendVerificationCode), findsOneWidget);
    expect(find.text(_llmSetupTitle), findsNothing);
    expect(find.text(_connectOfficialService), findsNothing);
    expect(find.text(_connectQrProvider), findsNothing);
    expect(find.textContaining('https://hubs.mypapers.top'), findsNothing);
    expect(find.textContaining('https://hubs.maclaw.top'), findsNothing);
    expect(find.textContaining('https://hubs2.maclaw.top'), findsNothing);
  });

  testWidgets('keeps a shared link pending until phone login completes',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(null),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 10),
              ),
            ),
          ),
          sessionControllerProvider.overrideWith(
            _TransitioningSessionController.new,
          ),
          mobileSharedIntentProvider.overrideWith(
            _PendingSharedLinkController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();

    expect(find.text(_phoneRegistrationLogin), findsOneWidget);
    final container = ProviderScope.containerOf(
      tester.element(find.byType(MaClawMobileApp)),
      listen: false,
    );
    expect(
      container.read(mobileSharedIntentProvider)?.id,
      'pending-link-after-login',
    );

    (container.read(sessionControllerProvider.notifier)
            as _TransitioningSessionController)
        .completeLogin();
    await tester.pump();
    await pumpQuietly(tester);

    expect(find.text(_assistantTab), findsWidgets);
    expect(
      container.read(assistantQueryProvider),
      contains('https://status.example.com/incident/42'),
    );
    expect(container.read(mobileSharedIntentProvider), isNull);
    await disposePumpedTree(tester);
  });

  testWidgets('keeps a shared document pending until phone login completes',
      (tester) async {
    _PendingDocumentFlowController.uploadedPaths.clear();
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(null),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 10),
              ),
            ),
          ),
          sessionControllerProvider.overrideWith(
            _TransitioningSessionController.new,
          ),
          mobileSharedIntentProvider.overrideWith(
            _PendingSharedDocumentController.new,
          ),
          documentsControllerProvider.overrideWith(
            _PendingDocumentFlowController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();

    expect(find.text(_phoneRegistrationLogin), findsOneWidget);
    final container = ProviderScope.containerOf(
      tester.element(find.byType(MaClawMobileApp)),
      listen: false,
    );
    expect(
      container.read(mobileSharedIntentProvider)?.id,
      'pending-document-after-login',
    );

    (container.read(sessionControllerProvider.notifier)
            as _TransitioningSessionController)
        .completeLogin();
    await tester.pump();
    await pumpQuietly(tester);

    expect(find.textContaining(_documentsScreenSubtitle), findsOneWidget);
    expect(
      _PendingDocumentFlowController.uploadedPaths,
      ['/tmp/incident-report.pdf'],
    );
    expect(container.read(mobileSharedIntentProvider), isNull);
    await disposePumpedTree(tester);
  });

  testWidgets('keeps an opened task notification until phone login completes',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final notifications = MobileNotificationService();
    notifications.handleNotificationResponse(
      const NotificationResponse(
        notificationResponseType: NotificationResponseType.selectedNotification,
        payload: 'document-export:job-before-login',
      ),
    );
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(null),
          mobileNotificationServiceProvider.overrideWithValue(notifications),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 10),
              ),
            ),
          ),
          sessionControllerProvider.overrideWith(
            _TransitioningSessionController.new,
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();

    expect(find.text(_phoneRegistrationLogin), findsOneWidget);
    expect(
      notifications.latestOpenedNotification?.payload,
      'document-export:job-before-login',
    );
    final container = ProviderScope.containerOf(
      tester.element(find.byType(MaClawMobileApp)),
      listen: false,
    );

    (container.read(sessionControllerProvider.notifier)
            as _TransitioningSessionController)
        .completeLogin();
    await pumpQuietly(tester);

    expect(find.textContaining(_tasksScreenSubtitle), findsOneWidget);
    expect(find.textContaining(_openedTaskNotification), findsOneWidget);
    expect(
      find.textContaining('document-export:job-before-login'),
      findsOneWidget,
    );
    expect(notifications.consumeLastOpenedPayload(), isNull);
    await disposePumpedTree(tester);
  });

  testWidgets('signed-in official LLM opens the assistant tab', (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
          sessionControllerProvider.overrideWith(
            () => _SignedInSessionController(
              const MobileLlmAccess(
                mode: 'maclaw_official',
                status: 'available',
                authorizationId: '',
                authorizedBy: '',
                creditsAccount: 'phone:19900001111',
                authorizedAt: null,
              ),
            ),
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();

    expect(find.text(_assistantTab), findsWidgets);
    expect(find.text(_mainConversation), findsOneWidget);
    expect(find.byTooltip('开始语音输入'), findsOneWidget);
    expect(find.text('查信息'), findsNothing);
    await disposePumpedTree(tester);
  });

  testWidgets('opened notification payload is consumed by app shell',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final notifications = MobileNotificationService();
    notifications.handleNotificationResponse(
      const NotificationResponse(
        notificationResponseType: NotificationResponseType.selectedNotification,
        payload: 'document-export:job-1',
      ),
    );
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(null),
          mobileNotificationServiceProvider.overrideWithValue(notifications),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
          sessionControllerProvider.overrideWith(
            () => _SignedInSessionController(
              const MobileLlmAccess(
                mode: 'maclaw_official',
                status: 'available',
                authorizationId: '',
                authorizedBy: '',
                creditsAccount: 'phone:19900001111',
                authorizedAt: null,
              ),
            ),
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();
    await pumpQuietly(tester);

    expect(find.textContaining(_openedTaskNotification), findsOneWidget);
    expect(find.textContaining('document-export:job-1'), findsOneWidget);
    expect(find.textContaining(_tasksScreenSubtitle), findsOneWidget);
    expect(notifications.latestOpenedNotification, isNull);
    expect(notifications.consumeLastOpenedPayload(), isNull);
    await disposePumpedTree(tester);
  });

  testWidgets('unknown notification payload does not fake task recovery',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final notifications = MobileNotificationService();
    notifications.handleNotificationResponse(
      const NotificationResponse(
        notificationResponseType: NotificationResponseType.selectedNotification,
        payload: 'document-export:',
      ),
    );
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(null),
          mobileNotificationServiceProvider.overrideWithValue(notifications),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
          sessionControllerProvider.overrideWith(
            () => _SignedInSessionController(
              const MobileLlmAccess(
                mode: 'maclaw_official',
                status: 'available',
                authorizationId: '',
                authorizedBy: '',
                creditsAccount: 'phone:19900001111',
                authorizedAt: null,
              ),
            ),
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();
    await pumpQuietly(tester);

    expect(find.textContaining(_unknownTaskNotification), findsNothing);
    expect(find.textContaining(_openedTaskNotification), findsNothing);
    expect(find.text(_mainConversation), findsOneWidget);
    expect(find.textContaining(_documentsScreenSubtitle), findsNothing);
    expect(find.textContaining(_tasksScreenSubtitle), findsNothing);
    expect(notifications.latestOpenedNotification, isNull);
    expect(notifications.consumeLastOpenedPayload(), isNull);
    await disposePumpedTree(tester);
  });

  testWidgets('opened notification payload message redacts URL secrets',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final notifications = MobileNotificationService();
    notifications.handleNotificationResponse(
      const NotificationResponse(
        notificationResponseType: NotificationResponseType.selectedNotification,
        payload: 'https://admin:pass@example.com/export.pdf?token=secret-token',
      ),
    );
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(null),
          mobileNotificationServiceProvider.overrideWithValue(notifications),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
          sessionControllerProvider.overrideWith(
            () => _SignedInSessionController(
              const MobileLlmAccess(
                mode: 'maclaw_official',
                status: 'available',
                authorizationId: '',
                authorizedBy: '',
                creditsAccount: 'phone:19900001111',
                authorizedAt: null,
              ),
            ),
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();
    await pumpQuietly(tester);

    expect(find.textContaining(_documentsScreenSubtitle), findsOneWidget);
    expect(find.textContaining(_openedTaskNotification), findsOneWidget);
    expect(find.textContaining('[REDACTED_CREDENTIALS]'), findsOneWidget);
    expect(find.textContaining('[REDACTED_SECRET]'), findsOneWidget);
    expect(find.textContaining('admin:pass'), findsNothing);
    expect(find.textContaining('secret-token'), findsNothing);
    expect(notifications.latestOpenedNotification, isNull);
    await disposePumpedTree(tester);
  });

  testWidgets('opened server notification payload recovers to servers tab',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final notifications = MobileNotificationService();
    notifications.handleNotificationResponse(
      const NotificationResponse(
        notificationResponseType: NotificationResponseType.selectedNotification,
        payload: 'server-profile:srv-prod',
      ),
    );
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(null),
          mobileNotificationServiceProvider.overrideWithValue(notifications),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
          sessionControllerProvider.overrideWith(
            () => _SignedInSessionController(
              const MobileLlmAccess(
                mode: 'maclaw_official',
                status: 'available',
                authorizationId: '',
                authorizedBy: '',
                creditsAccount: 'phone:19900001111',
                authorizedAt: null,
              ),
            ),
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));

    expect(find.text(_emergencyServers), findsOneWidget);
    expect(find.textContaining(_openedTaskNotification), findsOneWidget);
    expect(find.textContaining('server-profile:srv-prod'), findsOneWidget);
    expect(notifications.latestOpenedNotification, isNull);
    await disposePumpedTree(tester);
  });

  testWidgets('signed-in missing LLM opens the configuration screen',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
          sessionControllerProvider.overrideWith(
            () => _SignedInSessionController(
              const MobileLlmAccess(
                mode: 'maclaw_official',
                status: 'missing',
                authorizationId: '',
                authorizedBy: '',
                authorizedAt: null,
              ),
            ),
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();
    await pumpQuietly(tester);

    expect(find.text('数字分身'), findsWidgets);
    expect(find.text(_mainConversation), findsNothing);
    await disposePumpedTree(tester);
  });

  testWidgets(
      'signed-in official LLM without phone credits keeps settings optional',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
          sessionControllerProvider.overrideWith(
            () => _SignedInSessionController(
              const MobileLlmAccess(
                mode: 'maclaw_official',
                status: 'available',
                authorizationId: '',
                authorizedBy: '',
                authorizedAt: null,
              ),
            ),
          ),
          appPreferencesProvider.overrideWith(
            _TestAppPreferencesController.new,
          ),
        ],
        child: const MaClawMobileApp(),
      ),
    );
    await tester.pump();
    await pumpQuietly(tester);

    expect(find.text('数字分身'), findsWidgets);
    expect(find.text(_mainConversation), findsNothing);
    await disposePumpedTree(tester);
  });

  test('mobileLlmConfigured accepts official and desktop QR delegated access',
      () {
    expect(
      mobileLlmConfigured(
        _bootstrap(
          llmAccess: const MobileLlmAccess(
            mode: 'maclaw_official',
            status: 'available',
            authorizationId: '',
            authorizedBy: '',
            creditsAccount: 'phone:19900001111',
            authorizedAt: null,
          ),
        ),
      ),
      isTrue,
    );
    expect(
      mobileLlmConfigured(
        MobileBootstrap.fromJson({
          'user': {
            'user_id': 'user-1',
            'phone_number': '199 0000-1111',
            'tenant_id': 'tenant-1',
          },
          'llm_access': {
            'mode': 'maclaw_official',
            'status': 'available',
            'credits_account': 'phone:199 0000-1111',
          },
        }),
      ),
      isTrue,
    );
    expect(
      mobileLlmConfigured(
        MobileBootstrap.fromJson({
          'user': {
            'user_id': 'user-1',
            'phone_number': '19900001111',
            'tenant_id': 'tenant-1',
          },
          'llm_access': {
            'mode': 'maclaw_official',
            'status': 'available',
            'credits_account': 'phone:user19900001111',
          },
        }),
      ),
      isFalse,
    );
    expect(
      mobileLlmConfigured(
        _bootstrap(
          llmAccess: const MobileLlmAccess(
            mode: 'desktop_qr_third_party',
            status: 'available',
            authorizationId: 'auth-1',
            authorizedBy: 'desktop',
            authorizedAt: null,
          ),
        ),
      ),
      isTrue,
    );
    expect(
      mobileLlmConfigured(
        _bootstrap(
          llmAccess: const MobileLlmAccess(
            mode: 'maclaw_official',
            status: 'not_configured',
            authorizationId: '',
            authorizedBy: '',
            authorizedAt: null,
          ),
        ),
      ),
      isFalse,
    );
    expect(
      mobileLlmConfigured(
        _bootstrap(
          llmAccess: const MobileLlmAccess(
            mode: 'maclaw_official',
            status: 'available',
            authorizationId: '',
            authorizedBy: '',
            creditsAccount: 'phone:user@example.com',
            authorizedAt: null,
          ),
        ),
      ),
      isFalse,
    );
    expect(
      mobileLlmConfigured(
        _bootstrap(
          llmAccess: const MobileLlmAccess(
            mode: 'maclaw_official',
            status: 'available',
            authorizationId: '',
            authorizedBy: '',
            creditsAccount: 'phone:199 0000 1111',
            authorizedAt: null,
          ),
        ),
      ),
      isFalse,
    );
    expect(
      mobileLlmConfigured(
        _bootstrap(
          llmAccess: const MobileLlmAccess(
            mode: 'maclaw_official',
            status: 'available',
            authorizationId: '',
            authorizedBy: '',
            authorizedAt: null,
          ),
        ),
      ),
      isFalse,
    );
    expect(
      mobileLlmConfigured(
        _bootstrap(
          llmAccess: const MobileLlmAccess(
            mode: 'desktop_qr_third_party',
            status: 'available',
            authorizationId: '',
            authorizedBy: 'desktop',
            authorizedAt: null,
          ),
        ),
      ),
      isFalse,
    );
  });

  test('mobile notification targets recover to feature-enabled tabs', () {
    const features = MobileFeatures(
      search: true,
      documents: true,
      backendSshSessions: true,
      digitalEmployees: true,
      pushNotifications: true,
    );

    expect(
      mobileNotificationTargetPath('document-export:job-1', features),
      '/tasks',
    );
    expect(
      mobileNotificationTargetPath('digital-employee-task:task-1', features),
      '/employees',
    );
    expect(
      mobileNotificationTargetPath('server-profile:srv-prod', features),
      '/servers',
    );
    expect(
      mobileNotificationTargetPath('assistant-task:llm-1', features),
      '/assistant',
    );
    expect(mobileNotificationTargetPath('raw-id', features), isNull);
    expect(
      mobileNotificationTargetPath(
        'server-profile:srv-prod',
        const MobileFeatures(
          search: true,
          documents: true,
          backendSshSessions: false,
          digitalEmployees: true,
          pushNotifications: true,
        ),
      ),
      '/assistant',
    );
  });

  test('mobile notification recovery messages name the target workflow', () {
    expect(
      mobileNotificationRecoveryMessage(
        'document-export:job-1',
        '/tasks',
      ),
      '已打开任务提醒：请在后台或文档页查看导出任务状态',
    );
    expect(
      mobileNotificationRecoveryMessage(
        'digital-employee-task:task-1',
        '/employees',
      ),
      '已打开任务提醒：请在数字员工页查看远程任务状态',
    );
    expect(
      mobileNotificationRecoveryMessage(
        'server-profile:srv-prod',
        '/servers',
      ),
      '已打开任务提醒：请在远程页查看后台 SSH 会话或服务器档案',
    );
    expect(
      mobileNotificationRecoveryMessage(
        'server-profile:srv-prod',
        '/assistant',
      ),
      '已打开任务提醒',
    );
    expect(
      mobileNotificationRecoveryMessage('assistant-task:llm-1', '/assistant'),
      '已打开任务提醒：请在 AI 助手页查看长任务结果',
    );
    expect(
      mobileNotificationRecoveryMessage('raw-id', null),
      _unknownTaskNotification,
    );
  });
}

MobileBootstrap _bootstrap({required MobileLlmAccess llmAccess}) {
  return MobileBootstrap(
    user: const MobileUser(
      userId: 'u1',
      email: 'user@example.com',
      tenantId: 'tenant-a',
    ),
    services: const MobileServices(
      hubStatus: 'online',
      llmStatus: 'available',
      searchStatus: 'available',
      documentsStatus: 'available',
      digitalEmployeesStatus: 'available',
      llmStatusPath: '/api/llm/status',
      modelsPath: '/api/llm/models',
      searchPath: '/api/mobile/search',
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
    llmAccess: llmAccess,
    features: const MobileFeatures(
      search: true,
      documents: true,
      backendSshSessions: true,
      digitalEmployees: true,
      pushNotifications: true,
    ),
    limits: const MobileLimits(
      maxUploadBytes: 1024,
      maxExportJobs: 2,
    ),
  );
}
