import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/official_service.dart';
import 'package:maclaw_mobile/core/storage/secure_vault.dart';
import 'package:maclaw_mobile/features/auth/auth_service.dart';

void main() {
  setUp(() {
    FlutterSecureStorage.setMockInitialValues({});
  });

  test('requestPhoneLogin resolves Hub through HubCenter and sends SMS on Hub',
      () async {
    final adapter = _RecordingAuthAdapter((request) {
      if (request.path == '/api/entry/probe') {
        return _jsonResponse({
          'mode': 'matched',
          'hubs': [
            {
              'hub_id': 'hub-a',
              'tenant_id': 'tenant-a',
              'tenant_name': 'Tenant A',
              'base_url': 'https://tenant-a.maclaw.top',
              'status': 'online',
            }
          ],
        });
      }
      return _jsonResponse({
        'ok': true,
        'tenant_id': 'tenant-a',
        'expires_min': 5,
        'code_length': 6,
      });
    });
    final service = AuthService(dio: Dio()..httpClientAdapter = adapter);

    final result = await service.requestPhoneLogin(' 199 0000 1111 ');

    expect(result.status, 'sent');
    expect(result.phoneNumber, '19900001111');
    expect(result.hubUrl, 'https://tenant-a.maclaw.top');
    expect(result.hubId, 'hub-a');
    expect(result.tenantId, 'tenant-a');
    expect(result.hubCenterUrl, maclawDefaultHubCenterUrl);
    expect(result.expiresMinutes, 5);
    expect(result.codeLength, 6);
    expect(adapter.requests, hasLength(2));
    expect(adapter.requests.first.baseUrl, maclawDefaultHubCenterUrl);
    expect(adapter.requests.first.path, '/api/entry/probe');
    expect(adapter.requests.first.data, {'phone_number': '19900001111'});
    expect(adapter.requests.last.baseUrl, 'https://tenant-a.maclaw.top');
    expect(adapter.requests.last.path, '/api/enroll/sms/send-code');
    expect(adapter.requests.last.data, {
      'phone_number': '19900001111',
      'tenant_id': 'tenant-a',
    });
  });

  test('phone login falls back to the next official HubCenter on 5xx',
      () async {
    final adapter = _RecordingAuthAdapter((request) {
      if (request.baseUrl == maclawDefaultHubCenterUrl) {
        return _jsonResponse({'error': 'temporarily unavailable'}, 503);
      }
      if (request.path == '/api/entry/probe') {
        return _jsonResponse({
          'hubs': [
            {
              'hub_id': 'hub-a',
              'tenant_id': 'tenant-a',
              'base_url': 'https://tenant-a.maclaw.top',
              'status': 'online',
            }
          ],
        });
      }
      return _jsonResponse({'ok': true, 'tenant_id': 'tenant-a'});
    });
    final service = AuthService(dio: Dio()..httpClientAdapter = adapter);

    final result = await service.requestPhoneLogin('19900001111');

    expect(result.hubCenterUrl, 'https://hubs.maclaw.top');
    expect(
      adapter.requests.map((request) => request.baseUrl),
      [
        maclawDefaultHubCenterUrl,
        'https://hubs.maclaw.top',
        'https://tenant-a.maclaw.top',
      ],
    );
  });

  test('phone login rejects invalid phone numbers before HubCenter probe',
      () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({'status': 'should-not-be-called'}),
    );
    final service = AuthService(dio: Dio()..httpClientAdapter = adapter);

    for (final value in const ['', 'abc', '1234567', '1234567890123456']) {
      await expectLater(
        service.requestPhoneLogin(value),
        throwsA(isA<ArgumentError>()),
      );
    }

    expect(adapter.requests, isEmpty);
  });

  test('verifyPhoneLoginOnHub stores viewer token issued by Hub', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'status': 'approved',
        'viewer_token': 'hub-issued-token',
        'email': 'phone:19900001111',
        'phone_number': '19900001111',
        'tenant_id': 'tenant-a',
        'machine_id': 'machine-a',
      }),
    );
    const vault = SecureVault();
    final service =
        AuthService(vault: vault, dio: Dio()..httpClientAdapter = adapter);

    final result = await service.verifyPhoneLoginOnHub(
      hubUrl: 'https://tenant-a.maclaw.top/path',
      hubCenterUrl: 'https://hubs.maclaw.top',
      tenantId: 'tenant-a',
      phoneNumber: '199 0000 1111',
      verifyCode: '303246',
    );

    expect(result.confirmed, isTrue);
    expect(result.accessToken, 'hub-issued-token');
    expect(result.account, 'phone:19900001111');
    expect(result.creditsAccount, 'phone:19900001111');
    expect(result.phoneNumber, '19900001111');
    expect(result.hubUrl, 'https://tenant-a.maclaw.top');
    expect(result.tenantId, 'tenant-a');
    expect(await vault.readHubUrl(), 'https://tenant-a.maclaw.top');
    expect(await vault.readToken(), 'hub-issued-token');
    expect(adapter.requests.single.path, '/api/enroll/sms/verify-and-start');
    expect(adapter.requests.single.data, {
      'phone_number': '19900001111',
      'verify_code': '303246',
      'machine_name': 'MaClaw Mobile',
      'platform': 'mobile',
      'client_id': 'maclaw-mobile',
      'tenant_id': 'tenant-a',
    });
  });

  test('phone login verify rejects invalid phone before Hub request', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({'status': 'should-not-be-called'}),
    );
    final service = AuthService(dio: Dio()..httpClientAdapter = adapter);

    await expectLater(
      service.verifyPhoneLoginOnHub(
        hubUrl: 'https://tenant-a.maclaw.top',
        phoneNumber: '123',
        verifyCode: '303246',
      ),
      throwsA(isA<ArgumentError>()),
    );

    expect(adapter.requests, isEmpty);
  });

  test('phone login verify defaults account to nested user phone credits',
      () async {
    final result = PhoneLoginVerifyResult.fromJson({
      'status': 'approved',
      'viewer_token': 'hub-issued-token',
      'user': {
        'phone_number': '19900001111',
        'tenant_id': 'tenant-a',
      },
      'hub': {
        'base_url': 'https://tenant-a.maclaw.top',
        'id': 'hub-a',
      },
      'llm': {'mode': 'maclaw_official'},
    });

    expect(result.confirmed, isTrue);
    expect(result.phoneNumber, '19900001111');
    expect(result.account, 'phone:19900001111');
    expect(result.creditsAccount, 'phone:19900001111');
    expect(result.llmMode, 'maclaw_official');
    expect(result.hubUrl, 'https://tenant-a.maclaw.top');
  });

  test('phone login verify keeps official credits bound to phone account',
      () async {
    final result = PhoneLoginVerifyResult.fromJson({
      'status': 'approved',
      'viewer_token': 'hub-issued-token',
      'email': 'user@example.com',
      'user': {
        'email': 'user@example.com',
        'phone_number': '19900001111',
        'credits_account': 'phone:19900001111',
      },
      'llm': {
        'mode': 'maclaw_official',
        'credits_account': 'legacy-llm-account',
      },
    });

    expect(result.account, 'phone:19900001111');
    expect(result.creditsAccount, 'phone:19900001111');
    expect(result.llmMode, 'maclaw_official');
  });

  test('phone login verify normalizes formatted phone credits', () async {
    final result = PhoneLoginVerifyResult.fromJson({
      'status': 'approved',
      'viewer_token': 'hub-issued-token',
      'phone_number': '199 0000-1111',
      'account': 'phone:199 0000-1111',
      'credits_account': ' phone:199 0000-1111 ',
      'llm': {'mode': 'maclaw_official'},
    });

    expect(result.phoneNumber, '19900001111');
    expect(result.account, 'phone:19900001111');
    expect(result.creditsAccount, 'phone:19900001111');
    expect(
      result.copyWith(creditsAccount: 'phone:199 0000-1111').creditsAccount,
      'phone:19900001111',
    );
  });

  test('phone login verify keeps malformed phone credits unnormalized', () {
    final result = PhoneLoginVerifyResult.fromJson({
      'status': 'approved',
      'viewer_token': 'hub-issued-token',
      'phone_number': '19900001111',
      'credits_account': 'phone:user19900001111',
    });

    expect(result.phoneNumber, '19900001111');
    expect(result.creditsAccount, 'phone:user19900001111');
    expect(
      result.copyWith(creditsAccount: 'phone:user19900001111').creditsAccount,
      'phone:user19900001111',
    );
  });

  test('official redemption pending phone login does not store session',
      () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse(
        {
          'status': 'requires_phone_login',
          'next_action': 'phone_login',
          'message': 'continue with phone login',
          'hub': {
            'id': 'hub-a',
            'base_url': 'https://tenant-a.maclaw.top',
          },
          'tenant_id': 'tenant-a',
        },
        202,
      ),
    );
    const vault = SecureVault();
    final service =
        AuthService(vault: vault, dio: Dio()..httpClientAdapter = adapter);

    await expectLater(
      service.redeemOfficialServiceCode('CODE-123'),
      throwsA(isA<MobileServiceConnectionPendingException>()),
    );
    expect(await vault.readHubUrl(), isNull);
    expect(await vault.readToken(), isNull);
    expect(adapter.requests.single.path, '/api/mobile/service-redemptions');
  });

  test('desktop GUI mobile authorization QR resolves session through HubCenter',
      () async {
    const qrPayload =
        '{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_test","hub_url":"https://tenant-a.maclaw.top"}';
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'status': 'authorized',
        'access_token': 'desktop-qr-mobile-token',
        'hub_url': 'https://tenant-a.maclaw.top',
        'hub_id': 'hub-a',
        'tenant_id': 'tenant-a',
      }),
    );
    const vault = SecureVault();
    final service =
        AuthService(vault: vault, dio: Dio()..httpClientAdapter = adapter);

    final result = await service.connectWithDesktopLlmQr(qrPayload);

    expect(result.accessToken, 'desktop-qr-mobile-token');
    expect(result.hubUrl, 'https://tenant-a.maclaw.top');
    expect(await vault.readHubUrl(), 'https://tenant-a.maclaw.top');
    expect(await vault.readToken(), 'desktop-qr-mobile-token');
    expect(adapter.requests.single.baseUrl, maclawDefaultHubCenterUrl);
    expect(
      adapter.requests.single.path,
      '/api/mobile/llm/desktop-qr-sessions',
    );
    expect(adapter.requests.single.data, {'qr_payload': qrPayload});
    expect(
      adapter.requests.single.headers['X-MaClaw-HubCenter-URL'],
      maclawDefaultHubCenterUrl,
    );
  });

  test('desktop GUI QR hub URL is not used as a direct mobile endpoint',
      () async {
    const qrPayload =
        '{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_test","hub_url":"https://evil.example"}';
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'status': 'authorized',
        'access_token': 'desktop-qr-mobile-token',
        'hub_url': 'https://tenant-a.maclaw.top',
        'hub_id': 'hub-a',
        'tenant_id': 'tenant-a',
      }),
    );
    const vault = SecureVault();
    final service =
        AuthService(vault: vault, dio: Dio()..httpClientAdapter = adapter);

    final result = await service.connectWithDesktopLlmQr(qrPayload);

    expect(result.accessToken, 'desktop-qr-mobile-token');
    expect(result.hubUrl, 'https://tenant-a.maclaw.top');
    expect(adapter.requests.single.baseUrl, maclawDefaultHubCenterUrl);
    expect(
      adapter.requests.single.path,
      '/api/mobile/llm/desktop-qr-sessions',
    );
  });

  test('desktop GUI QR rejects arbitrary third-party payloads before request',
      () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({'status': 'should-not-be-called'}),
    );
    final service = AuthService(dio: Dio()..httpClientAdapter = adapter);
    final invalidPayloads = [
      'https://llm.example.com/v1',
      'sk-test-secret',
      '{"v":1,"type":"maclaw_llm","url":"https://llm.example.com/v1","key":"sk-test"}',
      '{"v":2,"type":"maclaw_mobile_llm_authorization","hub_url":"https://tenant-a.maclaw.top"}',
    ];

    for (final payload in invalidPayloads) {
      await expectLater(
        service.connectWithDesktopLlmQr(payload),
        throwsA(isA<FormatException>()),
      );
    }
    expect(adapter.requests, isEmpty);
  });
}

ResponseBody _jsonResponse(Map<String, Object?> body, [int statusCode = 200]) {
  return ResponseBody.fromString(
    jsonEncode(body),
    statusCode,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

class _RecordedAuthRequest {
  final String method;
  final String baseUrl;
  final String path;
  final Object? data;
  final Map<String, Object?> headers;

  const _RecordedAuthRequest({
    required this.method,
    required this.baseUrl,
    required this.path,
    required this.data,
    required this.headers,
  });
}

class _RecordingAuthAdapter implements HttpClientAdapter {
  final ResponseBody Function(_RecordedAuthRequest request) handler;
  final requests = <_RecordedAuthRequest>[];

  _RecordingAuthAdapter(this.handler);

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<List<int>>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    final request = _RecordedAuthRequest(
      method: options.method,
      baseUrl: options.baseUrl,
      path: options.path,
      data: options.data,
      headers: Map<String, Object?>.from(options.headers),
    );
    requests.add(request);
    return handler(request);
  }

  @override
  void close({bool force = false}) {}
}
