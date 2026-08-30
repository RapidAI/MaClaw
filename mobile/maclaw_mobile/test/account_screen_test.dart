import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/network/mobile_network_status.dart';
import 'package:maclaw_mobile/core/notifications/mobile_notification_service.dart';
import 'package:maclaw_mobile/core/settings/app_preferences.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/core/storage/secure_vault.dart';
import 'package:maclaw_mobile/features/account/account_screen.dart';
import 'package:maclaw_mobile/features/account/llm_qr_authorization_screen.dart';
import 'package:maclaw_mobile/features/assistant/search_history.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';
import 'package:maclaw_mobile/features/servers/server_profile.dart';
import 'support/widget_pumps.dart';

class _SignedInSessionController extends SessionController {
  @override
  Future<SessionState> build() async => const SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: MobileBootstrap(
          user: MobileUser(
            userId: 'user-1',
            email: 'phone:19900001111',
            phoneNumber: '19900001111',
            accountId: 'phone:19900001111',
            creditsAccount: 'phone:19900001111',
            tenantId: 'tenant-1',
          ),
          services: MobileServices(
            hubStatus: 'online',
            llmStatus: 'available',
            searchStatus: 'unavailable',
            documentsStatus: 'available',
            digitalEmployeesStatus: 'available',
            llmStatusPath: '/api/llm/service/status',
            modelsPath: '/api/llm/v1/models',
            searchPath: '/api/mobile/search',
            documentsPath: '/api/mobile/documents',
            digitalEmployeesPath: '/api/mobile/digital-employees',
            realtimePath: '',
          ),
          connection: MobileConnection(
            hubCenterCandidates: [
              'https://hubs.mypapers.top',
              'https://hubs.maclaw.top',
              'https://hubs2.maclaw.top',
            ],
            selectedHubCenterUrl: 'https://hubs.maclaw.top',
            hubUrl: 'https://tenant-a.maclaw.top',
            hubId: 'hub-a',
            tenantId: 'tenant-1',
          ),
          llmAccess: MobileLlmAccess(
            mode: 'desktop_qr_third_party',
            status: 'available',
            authorizationId: 'llm-auth-1',
            authorizedBy: 'maclaw-gui',
            creditsAccount: 'phone:19900001111',
            authorizedAt: null,
          ),
          features: MobileFeatures(
            search: true,
            documents: true,
            backendSshSessions: true,
            digitalEmployees: true,
            pushNotifications: false,
          ),
          limits: MobileLimits(
            maxUploadBytes: 25 * 1024 * 1024,
            maxExportJobs: 3,
          ),
        ),
      );
}

class _MalformedCreditsSessionController extends SessionController {
  @override
  Future<SessionState> build() async => const SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: MobileBootstrap(
          user: MobileUser(
            userId: 'user-1',
            email: 'phone:19900001111',
            phoneNumber: '19900001111',
            accountId: 'phone:19900001111',
            creditsAccount: 'phone:user19900001111',
            tenantId: 'tenant-1',
          ),
          services: MobileServices(
            hubStatus: 'online',
            llmStatus: 'available',
            searchStatus: 'available',
            documentsStatus: 'available',
            digitalEmployeesStatus: 'available',
            llmStatusPath: '/api/llm/service/status',
            modelsPath: '/api/llm/v1/models',
            searchPath: '/api/mobile/search',
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
            selectedHubCenterUrl: 'https://hubs.maclaw.top',
            hubUrl: 'https://tenant-a.maclaw.top',
            hubId: 'hub-a',
            tenantId: 'tenant-1',
          ),
          llmAccess: MobileLlmAccess(
            mode: 'maclaw_official',
            status: 'available',
            authorizationId: '',
            authorizedBy: '',
            creditsAccount: 'phone:user19900001111',
            authorizedAt: null,
          ),
          features: MobileFeatures(
            search: true,
            documents: true,
            backendSshSessions: true,
            digitalEmployees: true,
            pushNotifications: false,
          ),
          limits: MobileLimits(
            maxUploadBytes: 25 * 1024 * 1024,
            maxExportJobs: 3,
          ),
        ),
      );
}

class _TestAppPreferencesController extends AppPreferencesController {
  @override
  Future<AppPreferences> build() async {
    final store = ref.read(mobileLocalStoreProvider);
    return store.loadAppPreferences();
  }
}

final _qrAuthorizationPayloads = <String>[];
const _desktopLlmQrPayload =
    '{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_test","hub_url":"https://tenant-a.maclaw.top"}';
const _scannedDesktopLlmQrPayload =
    '{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_scan","hub_url":"https://tenant-a.maclaw.top"}';

class _QrAuthorizingSessionController extends _SignedInSessionController {
  @override
  Future<MobileBootstrap> authorizeThirdPartyLlmWithDesktopQr(
    String qrPayload,
  ) async {
    _qrAuthorizationPayloads.add(qrPayload);
    final current = state.valueOrNull ?? await future;
    final bootstrap = current.bootstrap!;
    final next = MobileBootstrap(
      user: bootstrap.user,
      services: bootstrap.services,
      connection: bootstrap.connection,
      llmAccess: MobileLlmAccess(
        mode: 'desktop_qr_third_party',
        status: 'available',
        authorizationId: 'qr-auth-2',
        authorizedBy: 'maclaw-gui',
        authorizedAt: DateTime.utc(2026, 7, 2),
      ),
      features: bootstrap.features,
      limits: bootstrap.limits,
    );
    state = AsyncData(current.copyWith(bootstrap: next));
    return next;
  }
}

class _FakeMobileLocalStore extends MobileLocalStore {
  List<ServerProfile> serverProfiles;
  List<SearchHistoryEntry> searchHistory;
  DocumentDraft? lastDraft;
  AppPreferences appPreferences;
  var clearedServerProfiles = false;

  _FakeMobileLocalStore({
    required this.serverProfiles,
    required this.searchHistory,
    required this.lastDraft,
  }) : appPreferences = const AppPreferences(language: appLanguageChinese);

  @override
  Future<List<ServerProfile>> loadServerProfiles() async => serverProfiles;

  @override
  Future<List<SearchHistoryEntry>> loadSearchHistory() async => searchHistory;

  @override
  Future<DocumentDraft?> loadLastDocumentDraft() async => lastDraft;

  @override
  Future<AppPreferences> loadAppPreferences() async => appPreferences;

  @override
  Future<void> saveAppPreferences(AppPreferences preferences) async {
    appPreferences = preferences;
  }

  @override
  Future<void> clearLocalWorkCache() async {
    searchHistory = [];
    lastDraft = null;
    appPreferences = const AppPreferences(language: appLanguageChinese);
  }

  @override
  Future<void> clearServerProfiles() async {
    clearedServerProfiles = true;
    serverProfiles = [];
  }
}

class _FakeNotificationService extends MobileNotificationService {
  var requested = 0;
  final MobileNotificationPermissionResult result;
  final Object? error;

  _FakeNotificationService({
    this.result =
        const MobileNotificationPermissionResult(androidGranted: true),
    this.error,
  });

  @override
  Future<MobileNotificationPermissionResult> requestPermissions() async {
    requested += 1;
    final failure = error;
    if (failure != null) throw failure;
    return result;
  }
}

class _FakeSecureVault extends SecureVault {
  final clearedLegacyServerCredentials = <String>[];

  @override
  Future<void> clearLegacyServerCredentials(String serverId) async {
    clearedLegacyServerCredentials.add(serverId);
  }
}

class _FakeApiClient extends ApiClient {
  final LlmServiceStatus status;
  String requestedStatusPath = '';

  _FakeApiClient(this.status) : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<LlmServiceStatus> llmServiceStatus([String path = '']) async {
    requestedStatusPath = path;
    return status;
  }

  @override
  Future<MobileEntitlementsCaps> getEntitlementsCaps() async {
    return const MobileEntitlementsCaps();
  }

  @override
  Future<MobileDocumentQuota> getDocumentQuota() async {
    return const MobileDocumentQuota();
  }

  @override
  Future<void> registerPushDevice({
    required String platform,
    required String token,
    String deviceId = '',
    String path = '/api/mobile/push/devices',
  }) async {}

  @override
  Future<MobilePushPendingList> listPushPending({
    String path = '/api/mobile/push/pending',
  }) async {
    return const MobilePushPendingList();
  }
}

void main() {
  testWidgets('account screen shows Hub discovery and LLM access',
      (tester) async {
    tester.view.physicalSize = const Size(1200, 3000);
    tester.view.devicePixelRatio = 1;
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    });
    final apiClient = _FakeApiClient(
      const LlmServiceStatus(
        active: true,
        skipLlmConfig: true,
        authMode: 'grant_required',
        defaultModel: 'maclaw-chat',
        availableModels: ['maclaw-chat'],
        serviceGroupNames: ['Official'],
        inactiveReasons: [],
        nearestExpiresAt: '2026-08-01T00:00:00Z',
        creditsTotal: 100,
        creditsUsed: 12.5,
        creditsRemaining: 87.5,
        creditsAvailable: 80,
        tokensPerCredit: 1000,
      ),
    );
    await _pumpAccount(tester, apiClient: apiClient);
    await pumpQuietly(tester);

    expect(find.text('手机号'), findsOneWidget);
    expect(find.text('phone:199****1111'), findsWidgets);
    expect(find.text('MaClaw 官方 credits 使用 phone:199****1111'), findsOneWidget);
    expect(find.text('phone:19900001111'), findsNothing);
    expect(find.text('MaClaw 官方 credits 使用 phone:19900001111'), findsNothing);
    expect(find.text('Hub 接入'), findsOneWidget);
    expect(find.text('https://hubs.maclaw.top'), findsOneWidget);
    expect(find.text('https://tenant-a.maclaw.top'), findsWidgets);
    expect(find.text('hub-a'), findsOneWidget);
    expect(find.text('桌面 GUI 二维码授权的第三方 LLM（llm-auth-1）'), findsOneWidget);
    expect(find.text('可用'), findsWidgets);
    expect(find.text('llm-auth-1 · 来自 maclaw-gui'), findsOneWidget);
    expect(find.text('第三方 LLM 授权'), findsOneWidget);
    expect(
      find.text(
        '默认使用 MaClaw 官方 LLM；如需接入第三方 LLM，只能扫描或粘贴 MaClaw 桌面 GUI 生成的授权二维码。',
      ),
      findsOneWidget,
    );
    expect(find.text('扫码授权'), findsOneWidget);
    expect(find.text('恢复官方 LLM'), findsOneWidget);
    expect(find.text('撤销授权'), findsOneWidget);
    expect(apiClient.requestedStatusPath, '/api/llm/service/status');
    expect(find.text('官方 credits'), findsOneWidget);
    expect(find.text('80'), findsOneWidget);
    expect(find.text('87.50'), findsOneWidget);
    expect(find.text('12.50'), findsOneWidget);
    expect(find.text('maclaw-chat'), findsOneWidget);
    expect(find.textContaining('助手联网'), findsWidgets);
    expect(find.textContaining('本地通知'), findsOneWidget);
    expect(find.textContaining('远程 Push'), findsNothing);
    expect(find.text('联网搜索'), findsNothing);

    expect(find.textContaining('助手联网接口'), findsOneWidget);
    expect(find.text('联网搜索接口'), findsNothing);
    expect(find.text('25 MB'), findsOneWidget);
    expect(find.text('3'), findsOneWidget);

    await _tapActionTileButton(tester, Icons.security_outlined);
    await pumpQuietly(tester);
    expect(
      find.textContaining(
        '\u540e\u53f0\u4f1a\u8bdd\u8f93\u51fa\u6216\u65e5\u5fd7\u53d1\u9001\u7ed9 AI \u5206\u6790\u524d',
      ),
      findsOneWidget,
    );
    expect(
      find.textContaining(
        '\u7ec8\u7aef\u8f93\u51fa\u6216\u65e5\u5fd7\u53d1\u9001\u7ed9 AI',
      ),
      findsNothing,
    );
  });

  testWidgets('account screen does not show malformed phone credits',
      (tester) async {
    final apiClient = _FakeApiClient(
      const LlmServiceStatus(
        active: true,
        skipLlmConfig: true,
        authMode: 'grant_required',
        defaultModel: 'maclaw-chat',
        availableModels: ['maclaw-chat'],
        serviceGroupNames: ['Official'],
        inactiveReasons: [],
        nearestExpiresAt: '',
        creditsTotal: 0,
        creditsUsed: 0,
        creditsRemaining: 0,
        creditsAvailable: 0,
        tokensPerCredit: 1000,
      ),
    );

    await _pumpAccount(
      tester,
      apiClient: apiClient,
      sessionControllerBuilder: _MalformedCreditsSessionController.new,
    );
    await pumpQuietly(tester);

    expect(find.text('phone:user19900001111'), findsNothing);
    expect(find.text('MaClaw 官方 credits 使用 phone:199****1111'), findsNothing);
    expect(find.text('phone:199****1111'), findsOneWidget);
    expect(find.text('恢复官方 LLM'), findsNothing);
  });

  testWidgets('clears local work records without deleting server access data',
      (tester) async {
    final now = DateTime.utc(2026, 7, 2);
    final store = _FakeMobileLocalStore(
      serverProfiles: const [
        ServerProfile(
          id: 'srv-1',
          name: 'prod',
          host: '10.0.0.8',
          port: 22,
          username: 'ops',
          authMode: serverAuthModePassword,
        ),
      ],
      searchHistory: [
        SearchHistoryEntry(
          id: 'search-1',
          query: 'check 502',
          answerPreview: 'check gateway logs',
          createdAt: now,
        ),
      ],
      lastDraft: DocumentDraft(
        id: 'draft-1',
        title: 'incident report',
        template: DocumentTemplate.report,
        markdown: '# incident report',
        updatedAt: now,
      ),
    );

    await _pumpAccount(tester, store: store);

    await tester.scrollUntilVisible(
      find.byIcon(Icons.cleaning_services_outlined),
      320,
      scrollable: find.byType(Scrollable),
    );
    expect(
      find.textContaining(
        '\u4fdd\u7559\u767b\u5f55\u6001\u548c\u670d\u52a1\u5668\u6863\u6848\u7f13\u5b58',
      ),
      findsOneWidget,
    );
    await _tapActionTileButton(tester, Icons.cleaning_services_outlined);
    await pumpQuietly(tester);
    expect(
      find.textContaining('将删除助手历史、文档草稿'),
      findsOneWidget,
    );
    expect(find.textContaining('将删除搜索历史'), findsNothing);
    await tester.tap(find.byType(FilledButton).last);
    await pumpQuietly(tester);

    expect(await store.loadServerProfiles(), isNotEmpty);
    expect(await store.loadSearchHistory(), isEmpty);
    expect(await store.loadLastDocumentDraft(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
      'updates theme and speech language preferences from account screen',
      (tester) async {
    final store = _FakeMobileLocalStore(
      serverProfiles: const [],
      searchHistory: const [],
      lastDraft: null,
    );

    await _pumpAccount(tester, store: store);

    await tester.scrollUntilVisible(
      find.byIcon(Icons.dark_mode_outlined),
      420,
      scrollable: find.byType(Scrollable),
    );
    await tester.ensureVisible(find.byIcon(Icons.dark_mode_outlined));
    await tester.pump();
    await tester.tap(find.byIcon(Icons.dark_mode_outlined));
    await tester.pump();

    expect(store.appPreferences.themeMode, ThemeMode.dark);

    await tester.tap(find.byType(DropdownButtonFormField<String>));
    await tester.pump();
    await tester.tap(find.text('English').last);
    await tester.pump();

    expect(store.appPreferences.language, appLanguageEnglish);
  });

  testWidgets('requests notification permission from account screen',
      (tester) async {
    final notifications = _FakeNotificationService(
      result: const MobileNotificationPermissionResult(
        androidGranted: true,
        grantId: 'notification-account-test',
      ),
    );
    await _pumpAccount(tester, notifications: notifications);

    await tester.scrollUntilVisible(
      find.byIcon(Icons.notifications_active_outlined),
      320,
      scrollable: find.byType(Scrollable),
    );
    await _tapActionTileButton(tester, Icons.notifications_active_outlined);

    expect(notifications.requested, 1);
    expect(
      find.textContaining('通知权限已开启，长任务和 SSH 异常会提醒你'),
      findsOneWidget,
    );
    expect(
      find.textContaining('permission-grant:notification-account-test'),
      findsOneWidget,
    );
  });

  testWidgets('shows denied notification permission result from account screen',
      (tester) async {
    final notifications = _FakeNotificationService(
      result: const MobileNotificationPermissionResult(androidGranted: false),
    );
    await _pumpAccount(tester, notifications: notifications);

    await tester.scrollUntilVisible(
      find.byIcon(Icons.notifications_active_outlined),
      320,
      scrollable: find.byType(Scrollable),
    );
    await _tapActionTileButton(tester, Icons.notifications_active_outlined);

    expect(notifications.requested, 1);
    expect(
      find.text('系统未授予通知权限，请在系统设置中开启 MaClaw Mobile 通知'),
      findsOneWidget,
    );
  });

  testWidgets('shows notification permission request failures', (tester) async {
    final notifications = _FakeNotificationService(
      error: StateError('permission API unavailable'),
    );
    await _pumpAccount(tester, notifications: notifications);

    await tester.scrollUntilVisible(
      find.byIcon(Icons.notifications_active_outlined),
      320,
      scrollable: find.byType(Scrollable),
    );
    await _tapActionTileButton(tester, Icons.notifications_active_outlined);
    await tester.pump();

    expect(notifications.requested, 1);
    expect(
      find.textContaining('通知权限请求失败：Bad state: permission API unavailable'),
      findsOneWidget,
    );
  });

  testWidgets('submits desktop GUI QR payload from authorization screen',
      (tester) async {
    _qrAuthorizationPayloads.clear();
    await _pumpLlmQrAuthorization(tester);

    expect(find.text('桌面二维码授权'), findsOneWidget);
    expect(find.text('扫描 MaClaw 桌面 GUI 生成的二维码'), findsOneWidget);
    expect(
      find.text(
        '移动端默认使用 MaClaw 官方 LLM。只有扫描或粘贴桌面 GUI 生成的授权二维码后，才会通过你的 Hub 接入第三方 LLM。',
      ),
      findsOneWidget,
    );

    await tester.scrollUntilVisible(
      find.byType(TextField),
      420,
      scrollable: find.byType(Scrollable),
    );
    expect(find.text('粘贴二维码内容'), findsOneWidget);
    await tester.enterText(
      find.byType(TextField),
      ' $_desktopLlmQrPayload ',
    );
    await tester.tap(find.text('确认授权'));
    await pumpQuietly(tester);

    expect(_qrAuthorizationPayloads, [_desktopLlmQrPayload]);
  });

  testWidgets('rejects non desktop GUI QR payload before authorization',
      (tester) async {
    _qrAuthorizationPayloads.clear();
    await _pumpLlmQrAuthorization(tester);

    await tester.scrollUntilVisible(
      find.byType(TextField),
      420,
      scrollable: find.byType(Scrollable),
    );
    await tester.enterText(
      find.byType(TextField),
      'https://llm.example.com/v1\nsk-test-secret',
    );
    await tester.tap(find.text('确认授权'));
    await pumpQuietly(tester);

    expect(_qrAuthorizationPayloads, isEmpty);
    expect(
      find.text('授权失败：QR payload must be MaClaw desktop GUI JSON.'),
      findsOneWidget,
    );
  });

  testWidgets('submits detected desktop GUI QR payload from scanner',
      (tester) async {
    _qrAuthorizationPayloads.clear();
    await _pumpLlmQrAuthorization(
      tester,
      scannerBuilder: (onPayload) => Center(
        child: FilledButton(
          onPressed: () => onPayload(' $_scannedDesktopLlmQrPayload '),
          child: const Text('模拟扫码'),
        ),
      ),
    );

    await tester.tap(find.text('模拟扫码'));
    await pumpQuietly(tester);

    expect(_qrAuthorizationPayloads, [_scannedDesktopLlmQrPayload]);
  });

  testWidgets('clears phone-side server profile cache separately',
      (tester) async {
    final vault = _FakeSecureVault();
    final store = _FakeMobileLocalStore(
      serverProfiles: const [
        ServerProfile(
          id: 'srv-password',
          name: 'prod-password',
          host: '10.0.0.8',
          port: 22,
          username: 'ops',
          authMode: serverAuthModePassword,
        ),
        ServerProfile(
          id: 'srv-key',
          name: 'prod-key',
          host: '10.0.0.9',
          port: 22,
          username: 'root',
          authMode: serverAuthModePrivateKey,
        ),
      ],
      searchHistory: const [],
      lastDraft: null,
    );

    await _pumpAccount(tester, store: store, vault: vault);

    await tester.scrollUntilVisible(
      find.byIcon(Icons.key_off_outlined),
      320,
      scrollable: find.byType(Scrollable),
    );
    expect(
      find.text('\u670d\u52a1\u5668\u6863\u6848\u7f13\u5b58'),
      findsOneWidget,
    );
    expect(
      find.textContaining(
        '\u771f\u5b9e SSH \u51ed\u636e\u4ecd\u7531 MaClaw GUI/agent \u7ba1\u7406',
      ),
      findsOneWidget,
    );
    await _tapActionTileButton(tester, Icons.key_off_outlined);
    await pumpQuietly(tester);
    expect(
      find.text('\u6e05\u7406\u670d\u52a1\u5668\u6863\u6848\u7f13\u5b58\uff1f'),
      findsOneWidget,
    );
    await tester.tap(find.byType(FilledButton).last);
    await pumpQuietly(tester);

    expect(store.clearedServerProfiles, isTrue);
    expect(await store.loadServerProfiles(), isEmpty);
    expect(vault.clearedLegacyServerCredentials, ['srv-password', 'srv-key']);
  });
}

Future<void> _pumpLlmQrAuthorization(
  WidgetTester tester, {
  LlmQrPayloadScannerBuilder? scannerBuilder,
}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        sessionControllerProvider.overrideWith(
          _QrAuthorizingSessionController.new,
        ),
      ],
      child: MaterialApp(
        home: LlmQrAuthorizationScreen(
          scanner: scannerBuilder == null ? const SizedBox.expand() : null,
          scannerBuilder: scannerBuilder,
        ),
      ),
    ),
  );
  await tester.pump();
}

Future<void> _pumpAccount(
  WidgetTester tester, {
  _FakeMobileLocalStore? store,
  _FakeNotificationService? notifications,
  _FakeSecureVault? vault,
  ApiClient? apiClient,
  SessionController Function()? sessionControllerBuilder,
}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        sessionControllerProvider.overrideWith(
          sessionControllerBuilder ?? _SignedInSessionController.new,
        ),
        appPreferencesProvider.overrideWith(
          _TestAppPreferencesController.new,
        ),
        if (store != null) mobileLocalStoreProvider.overrideWithValue(store),
        if (notifications != null)
          mobileNotificationServiceProvider.overrideWithValue(notifications),
        if (vault != null) secureVaultProvider.overrideWithValue(vault),
        apiClientProvider.overrideWithValue(
          apiClient ??
              _FakeApiClient(
                const LlmServiceStatus(
                  active: true,
                  skipLlmConfig: true,
                  authMode: 'grant_required',
                  defaultModel: 'maclaw-chat',
                  availableModels: ['maclaw-chat'],
                  serviceGroupNames: [],
                  inactiveReasons: [],
                  nearestExpiresAt: '',
                  creditsTotal: 0,
                  creditsUsed: 0,
                  creditsRemaining: 0,
                  creditsAvailable: 0,
                  tokensPerCredit: 1000,
                ),
              ),
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
      child: const MaterialApp(home: Scaffold(body: AccountScreen())),
    ),
  );
  await tester.pump();
}

Future<void> _tapActionTileButton(
  WidgetTester tester,
  IconData icon,
) async {
  final iconFinder = find.byIcon(icon);
  await tester.scrollUntilVisible(
    iconFinder,
    320,
    scrollable: find.byType(Scrollable).first,
  );
  await tester.pump();
  final card = find.ancestor(
    of: iconFinder,
    matching: find.byType(Card),
  );
  final button = find.descendant(
    of: card,
    matching: find.byWidgetPredicate((widget) => widget is ButtonStyleButton),
  );
  await tester.ensureVisible(button);
  await pumpQuietly(tester);
  await tester.tap(button);
  await pumpQuietly(tester);
}
