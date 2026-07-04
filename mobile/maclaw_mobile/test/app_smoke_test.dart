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
import 'package:maclaw_mobile/features/auth/session_controller.dart';

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

class _TestAppPreferencesController extends AppPreferencesController {
  @override
  Future<AppPreferences> build() async => const AppPreferences();
}

const _connectingOfficialService =
    '\u6b63\u5728\u8fde\u63a5 MaClaw \u5b98\u65b9\u670d\u52a1';
const _llmSetupTitle = '\u914d\u7f6e MaClaw LLM \u670d\u52a1';
const _connectOfficialService = '\u63a5\u5165\u5b98\u65b9\u670d\u52a1';
const _connectQrProvider = '\u63a5\u5165\u4e8c\u7ef4\u7801\u670d\u52a1\u5546';
const _lookupTab = '\u67e5\u4fe1\u606f';
const _mainConversation = '\u4e3b\u5bf9\u8bdd';
const _webLookup = '\u8054\u7f51\u67e5\u8be2';
const _emergencyDocuments = '\u5e94\u6025\u6587\u6863';
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

  testWidgets('renders LLM setup before mobile service is configured',
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

    expect(find.text(_llmSetupTitle), findsOneWidget);
    expect(find.text(_connectOfficialService), findsOneWidget);
    expect(find.text(_connectQrProvider), findsOneWidget);
    expect(find.textContaining('https://hubs.mypapers.top'), findsOneWidget);
    expect(find.textContaining('https://hubs.maclaw.top'), findsOneWidget);
    expect(find.textContaining('https://hubs2.maclaw.top'), findsOneWidget);
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

    expect(find.text(_lookupTab), findsWidgets);
    expect(find.text(_mainConversation), findsOneWidget);
    expect(find.text(_webLookup), findsOneWidget);
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
    await tester.pumpAndSettle();

    expect(find.textContaining('document-export:job-1'), findsOneWidget);
    expect(find.text(_emergencyDocuments), findsOneWidget);
    expect(notifications.latestOpenedNotification, isNull);
    expect(notifications.consumeLastOpenedPayload(), isNull);
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
    await tester.pumpAndSettle();

    expect(find.textContaining(_unknownTaskNotification), findsNothing);
    expect(find.text(_mainConversation), findsOneWidget);
    expect(find.text(_emergencyDocuments), findsNothing);
    expect(notifications.latestOpenedNotification, isNull);
    expect(notifications.consumeLastOpenedPayload(), isNull);
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
    await tester.pumpAndSettle();

    expect(find.text(_emergencyDocuments), findsOneWidget);
    expect(find.textContaining('[REDACTED_CREDENTIALS]'), findsOneWidget);
    expect(find.textContaining('[REDACTED_SECRET]'), findsOneWidget);
    expect(find.textContaining('admin:pass'), findsNothing);
    expect(find.textContaining('secret-token'), findsNothing);
    expect(notifications.latestOpenedNotification, isNull);
  });

  testWidgets('signed-in missing LLM still requires setup', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
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

    expect(find.text(_llmSetupTitle), findsOneWidget);
  });

  testWidgets('signed-in official LLM without phone credits requires setup',
      (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
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

    expect(find.text(_llmSetupTitle), findsOneWidget);
    expect(find.text(_mainConversation), findsNothing);
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
      localSsh: true,
      digitalEmployees: true,
      pushNotifications: true,
    );

    expect(
      mobileNotificationTargetPath('document-export:job-1', features),
      '/documents',
    );
    expect(
      mobileNotificationTargetPath('digital-employee-task:task-1', features),
      '/employees',
    );
    expect(
      mobileNotificationTargetPath('server-profile:srv-prod', features),
      '/servers',
    );
    expect(mobileNotificationTargetPath('raw-id', features), isNull);
    expect(
      mobileNotificationTargetPath(
        'server-profile:srv-prod',
        const MobileFeatures(
          search: true,
          documents: true,
          localSsh: false,
          digitalEmployees: true,
          pushNotifications: true,
        ),
      ),
      '/assistant',
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
      localSsh: true,
      digitalEmployees: true,
      pushNotifications: true,
    ),
    limits: const MobileLimits(
      maxUploadBytes: 1024,
      maxExportJobs: 2,
    ),
  );
}
