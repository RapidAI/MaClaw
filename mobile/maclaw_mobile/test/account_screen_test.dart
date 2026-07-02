import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
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

class _SignedInSessionController extends SessionController {
  @override
  Future<SessionState> build() async => const SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: MobileBootstrap(
          user: MobileUser(
            userId: 'user-1',
            email: 'mobile@example.com',
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
            authorizedAt: null,
          ),
          features: MobileFeatures(
            search: true,
            documents: true,
            localSsh: true,
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
  Future<AppPreferences> build() async => const AppPreferences();
}

final _qrAuthorizationPayloads = <String>[];

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
  }) : appPreferences = const AppPreferences();

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
    appPreferences = const AppPreferences();
  }

  @override
  Future<void> clearServerProfiles() async {
    clearedServerProfiles = true;
    serverProfiles = [];
  }
}

class _FakeNotificationService extends MobileNotificationService {
  var requested = 0;

  @override
  Future<MobileNotificationPermissionResult> requestPermissions() async {
    requested += 1;
    return const MobileNotificationPermissionResult(androidGranted: true);
  }
}

class _FakeSecureVault extends SecureVault {
  final deletedPasswords = <String>[];
  final deletedPrivateKeys = <String>[];

  @override
  Future<void> deleteServerPassword(String serverId) async {
    deletedPasswords.add(serverId);
  }

  @override
  Future<void> deleteServerPrivateKey(String serverId) async {
    deletedPrivateKeys.add(serverId);
  }
}

void main() {
  testWidgets('account screen shows Hub discovery and LLM access',
      (tester) async {
    await _pumpAccount(tester);

    expect(find.text('mobile@example.com'), findsOneWidget);
    expect(find.text('Hub 接入'), findsOneWidget);
    expect(find.text('https://hubs.maclaw.top'), findsOneWidget);
    expect(find.text('https://tenant-a.maclaw.top'), findsWidgets);
    expect(find.text('hub-a'), findsOneWidget);
    expect(find.text('桌面 GUI 二维码授权的第三方 LLM（llm-auth-1）'), findsOneWidget);
    expect(find.text('可用'), findsWidgets);
    expect(find.text('llm-auth-1 · 来自 maclaw-gui'), findsOneWidget);

    await tester.scrollUntilVisible(
      find.text('25 MB'),
      320,
      scrollable: find.byType(Scrollable),
    );
    expect(find.text('25 MB'), findsOneWidget);
    expect(find.text('3'), findsOneWidget);
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
    await _tapActionTileButton(tester, Icons.cleaning_services_outlined);
    await tester.pumpAndSettle();
    await tester.tap(find.byType(FilledButton).last);
    await tester.pumpAndSettle();

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
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(Icons.dark_mode_outlined));
    await tester.pump();

    expect(store.appPreferences.themeMode, ThemeMode.dark);

    await tester.tap(find.byType(DropdownButtonFormField<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('English').last);
    await tester.pumpAndSettle();

    expect(store.appPreferences.language, appLanguageEnglish);
  });

  testWidgets('requests notification permission from account screen',
      (tester) async {
    final notifications = _FakeNotificationService();
    await _pumpAccount(tester, notifications: notifications);

    await tester.scrollUntilVisible(
      find.byIcon(Icons.notifications_active_outlined),
      320,
      scrollable: find.byType(Scrollable),
    );
    await _tapActionTileButton(tester, Icons.notifications_active_outlined);
    await tester.pump();

    expect(notifications.requested, 1);
  });

  testWidgets('submits desktop GUI QR payload from authorization screen',
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
      ' maclaw-gui-llm-qr-payload ',
    );
    await tester.tap(find.text('确认授权'));
    await tester.pumpAndSettle();

    expect(_qrAuthorizationPayloads, ['maclaw-gui-llm-qr-payload']);
  });

  testWidgets('clears server profiles and SSH credentials separately',
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
    await _tapActionTileButton(tester, Icons.key_off_outlined);
    await tester.pumpAndSettle();
    await tester.tap(find.byType(FilledButton).last);
    await tester.pumpAndSettle();

    expect(store.clearedServerProfiles, isTrue);
    expect(await store.loadServerProfiles(), isEmpty);
    expect(vault.deletedPasswords, ['srv-password', 'srv-key']);
    expect(vault.deletedPrivateKeys, ['srv-password', 'srv-key']);
  });
}

Future<void> _pumpLlmQrAuthorization(WidgetTester tester) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        sessionControllerProvider.overrideWith(
          _QrAuthorizingSessionController.new,
        ),
      ],
      child: const MaterialApp(
        home: LlmQrAuthorizationScreen(
          scanner: SizedBox.expand(),
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
}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        sessionControllerProvider.overrideWith(
          _SignedInSessionController.new,
        ),
        appPreferencesProvider.overrideWith(
          _TestAppPreferencesController.new,
        ),
        if (store != null) mobileLocalStoreProvider.overrideWithValue(store),
        if (notifications != null)
          mobileNotificationServiceProvider.overrideWithValue(notifications),
        if (vault != null) secureVaultProvider.overrideWithValue(vault),
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
  final card = find.ancestor(
    of: find.byIcon(icon),
    matching: find.byType(Card),
  );
  final button = find.descendant(
    of: card,
    matching: find.byType(FilledButton),
  );
  await tester.ensureVisible(button);
  await tester.pumpAndSettle();
  await tester.tap(button);
}
