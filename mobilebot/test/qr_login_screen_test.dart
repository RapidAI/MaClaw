import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'l10n_test_helper.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/providers.dart';
import 'package:mobilebot/features/auth/auth_api.dart';
import 'package:mobilebot/features/auth/desktop_auth_qr.dart';
import 'package:mobilebot/features/auth/qr_login_screen.dart';

const _qrPayload =
    '{"v":2,"type":"$desktopAuthQrType","session_id":"maqr_abc123",'
    '"hub_url":"https://hub.example.com"}';

/// Legacy (Hub-issued) third-party QR: carries hub_url, authorizes directly.
const _thirdPartyQrPayload =
    '{"v":2,"type":"$thirdPartyAuthQrType","session_id":"maqr_tp456",'
    '"hub_url":"https://hub.example.com","name":"RapidEditor AI"}';

/// Relay (HubCenter-issued) third-party QR: carries center_url only — the
/// phone authorizes on its own account's Hub and delivers the token back.
const _relayQrPayload =
    '{"v":2,"type":"$thirdPartyAuthQrType","session_id":"tpr_relay1",'
    '"center_url":"https://hubs.mypapers.top","name":"RapidEditor AI"}';

/// What the account's own Hub returns to the phone at relay step 1.
const _hubSessionQrPayload =
    '{"v":2,"type":"$thirdPartyAuthQrType","session_id":"maqr_hub789",'
    '"hub_url":"https://hub.example.com","name":"第三方客户端"}';

Account _activeAccount() => Account(
      key: 'phone:19900007777',
      label: '19900007777',
      hubUrl: 'https://hub.example.com',
      token: 'viewer-token',
      addedAt: DateTime.now(),
    );

class _FakeAuthApi extends AuthApi {
  String? lastPayload;
  String? authorizedPayload;
  Object? failure;
  VerifyResult result = const VerifyResult(status: 'pending', message: 'wait');

  // Relay flow bookkeeping.
  String? createdHubUrl;
  String? authorizedHubUrl;
  String? fetchedSessionId;
  String? deliveredCenterUrl;
  String? deliveredSessionId;
  String? deliveredHubUrl;
  String? deliveredToken;

  @override
  Future<VerifyResult> redeemDesktopQr({
    required DesktopAuthQrPayload payload,
    required String machineName,
    required String clientId,
    String language = '',
  }) async {
    if (failure != null) throw failure!;
    lastPayload = payload.raw;
    return result;
  }

  @override
  Future<String> authorizeThirdParty({
    required ThirdPartyAuthQrPayload payload,
    required String accountHubUrl,
    required String accessToken,
  }) async {
    if (failure != null) throw failure!;
    authorizedPayload = payload.raw;
    authorizedHubUrl = accountHubUrl;
    return 'RapidEditor AI';
  }

  @override
  Future<({ThirdPartyAuthQrPayload payload, String pollToken})>
      createThirdPartyHubSession({
    required String hubUrl,
    String clientId = 'maclawbot-relay',
    String clientName = '第三方客户端',
  }) async {
    if (failure != null) throw failure!;
    createdHubUrl = hubUrl;
    return (
      payload: parseThirdPartyAuthQrPayload(_hubSessionQrPayload),
      pollToken: 'poll-token-1',
    );
  }

  @override
  Future<({String accessToken, String email})> fetchThirdPartyToken({
    required String hubUrl,
    required String sessionId,
    required String pollToken,
  }) async {
    if (failure != null) throw failure!;
    fetchedSessionId = sessionId;
    return (accessToken: 'minted-viewer-token', email: 'user@example.com');
  }

  @override
  Future<void> deliverThirdPartyToken({
    required String centerUrl,
    required String sessionId,
    required String hubUrl,
    required String accessToken,
    String email = '',
  }) async {
    if (failure != null) throw failure!;
    deliveredCenterUrl = centerUrl;
    deliveredSessionId = sessionId;
    deliveredHubUrl = hubUrl;
    deliveredToken = accessToken;
  }
}

class _FakeSession extends SessionController {
  Account? added;
  Account? activeAccount;

  @override
  SessionState build() => SessionState(loaded: true, active: activeAccount);

  @override
  Future<void> addAccount(Account account) async {
    added = account;
  }
}

Widget _app(
  _FakeAuthApi api,
  _FakeSession session, {
  QrScanMode mode = QrScanMode.desktopLogin,
  String qr = _qrPayload,
}) =>
    ProviderScope(
      overrides: [
        authApiProvider.overrideWithValue(api),
        sessionProvider.overrideWith(() => session),
        clientIdProvider.overrideWith((_) async => 'cid-1'),
      ],
      child: l10nApp(
        child: QrLoginScreen(
          mode: mode,
          scannerBuilder: (onPayload) => TextButton(
            onPressed: () => onPayload(qr),
            child: const Text('scan'),
          ),
        ),
      ),
    );

void main() {
  testWidgets('invalid QR shows an error without calling the api', (t) async {
    final api = _FakeAuthApi();
    final session = _FakeSession();
    await t.pumpWidget(
      ProviderScope(
        overrides: [
          authApiProvider.overrideWithValue(api),
          sessionProvider.overrideWith(() => session),
          clientIdProvider.overrideWith((_) async => 'cid-1'),
        ],
        child: l10nApp(
          child: QrLoginScreen(
            scannerBuilder: (onPayload) => TextButton(
              onPressed: () => onPayload('junk'),
              child: const Text('scan'),
            ),
          ),
        ),
      ),
    );
    await t.tap(find.text('scan'));
    await t.pumpAndSettle();
    expect(api.lastPayload, isNull);
    expect(find.textContaining('二维码'), findsWidgets);
  });

  testWidgets('scanned payload reaches the redeem call', (t) async {
    final api = _FakeAuthApi();
    final session = _FakeSession();
    await t.pumpWidget(_app(api, session));
    await t.tap(find.text('scan'));
    await t.pump();
    expect(api.lastPayload, _qrPayload);
  });

  testWidgets('authorize mode calls authorize, never addAccount', (t) async {
    final api = _FakeAuthApi();
    final session = _FakeSession()..activeAccount = _activeAccount();
    await t.pumpWidget(_app(
      api,
      session,
      mode: QrScanMode.thirdPartyAuthorize,
      qr: _thirdPartyQrPayload,
    ));
    await t.tap(find.text('scan'));
    for (var i = 0; i < 10; i++) {
      await t.pump(const Duration(milliseconds: 50));
    }
    expect(api.authorizedPayload, _thirdPartyQrPayload);
    expect(api.lastPayload, isNull);
    expect(session.added, isNull);
  });

  testWidgets('authorize mode requires a signed-in account', (t) async {
    final api = _FakeAuthApi();
    final session = _FakeSession();
    await t.pumpWidget(_app(
      api,
      session,
      mode: QrScanMode.thirdPartyAuthorize,
      qr: _thirdPartyQrPayload,
    ));
    await t.tap(find.text('scan'));
    for (var i = 0; i < 10; i++) {
      await t.pump(const Duration(milliseconds: 50));
    }
    expect(api.authorizedPayload, isNull);
    expect(session.added, isNull);
  });

  testWidgets('relay QR authorizes on the account hub and delivers back',
      (t) async {
    final api = _FakeAuthApi();
    final session = _FakeSession()..activeAccount = _activeAccount();
    await t.pumpWidget(_app(
      api,
      session,
      mode: QrScanMode.thirdPartyAuthorize,
      qr: _relayQrPayload,
    ));
    await t.tap(find.text('scan'));
    for (var i = 0; i < 10; i++) {
      await t.pump(const Duration(milliseconds: 50));
    }
    // Step 1: session created on the account's own Hub.
    expect(api.createdHubUrl, 'https://hub.example.com');
    // Step 2: the Hub-issued session payload got authorized there.
    expect(api.authorizedPayload, _hubSessionQrPayload);
    expect(api.authorizedHubUrl, 'https://hub.example.com');
    // Step 3: token fetched for the Hub session.
    expect(api.fetchedSessionId, 'maqr_hub789');
    // Step 4: credential delivered to the center for the relay session.
    expect(api.deliveredCenterUrl, 'https://hubs.mypapers.top');
    expect(api.deliveredSessionId, 'tpr_relay1');
    expect(api.deliveredHubUrl, 'https://hub.example.com');
    expect(api.deliveredToken, 'minted-viewer-token');
    // No account switch happened.
    expect(session.added, isNull);
  });
}
