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

const _thirdPartyQrPayload =
    '{"v":2,"type":"$thirdPartyAuthQrType","session_id":"maqr_tp456",'
    '"hub_url":"https://hub.example.com","name":"RapidEditor AI"}';

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
    return 'RapidEditor AI';
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
}
