import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/providers.dart';
import 'package:mobilebot/features/auth/auth_api.dart';
import 'package:mobilebot/features/auth/desktop_auth_qr.dart';
import 'package:mobilebot/features/auth/qr_login_screen.dart';

const _qrPayload =
    '{"v":2,"type":"$desktopAuthQrType","session_id":"maqr_abc123",'
    '"hub_url":"https://hub.example.com"}';

class _FakeAuthApi extends AuthApi {
  String? lastPayload;
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
}

class _FakeSession extends SessionController {
  Account? added;

  @override
  SessionState build() => const SessionState(loaded: true);

  @override
  Future<void> addAccount(Account account) async {
    added = account;
  }
}

Widget _app(_FakeAuthApi api, _FakeSession session) => ProviderScope(
      overrides: [
        authApiProvider.overrideWithValue(api),
        sessionProvider.overrideWith(() => session),
        clientIdProvider.overrideWith((_) async => 'cid-1'),
      ],
      child: MaterialApp(
        home: QrLoginScreen(
          scannerBuilder: (onPayload) =>
              TextButton(
            onPressed: () => onPayload(_qrPayload),
            child: const Text('scan'),
          ),
        ),
      ),
    );

void main() {
  testWidgets('invalid QR shows an error without calling the api', (t) async {
    final api = _FakeAuthApi();
    final session = _FakeSession();
    await t.pumpWidget(ProviderScope(
      overrides: [
        authApiProvider.overrideWithValue(api),
        sessionProvider.overrideWith(() => session),
        clientIdProvider.overrideWith((_) async => 'cid-1'),
      ],
      child: MaterialApp(
        home: QrLoginScreen(
          scannerBuilder: (onPayload) =>
              TextButton(onPressed: () => onPayload('junk'), child: const Text('scan')),
        ),
      ),
    ));
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
}
