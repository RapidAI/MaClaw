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
      if (request.path == '/api/entry/resolve') {
        return _jsonResponse({
          'mode': 'matched',
          'default_hub_id': 'hub-a',
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
        'resend_cooldown_seconds': 42,
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
    expect(result.resendCooldownSeconds, 42);
    expect(adapter.requests, hasLength(2));
    expect(adapter.requests.first.baseUrl, maclawDefaultHubCenterUrl);
    expect(adapter.requests.first.path, '/api/entry/resolve');
    expect(adapter.requests.first.data, {'phone_number': '19900001111'});
    expect(adapter.requests.last.baseUrl, 'https://tenant-a.maclaw.top');
    expect(adapter.requests.last.path, '/api/mobile/auth/phone/send-code');
    expect(adapter.requests.last.data, {
      'phone_number': '19900001111',
      'tenant_id': 'tenant-a',
    });
  });

  test(
      'requestPhoneLogin accepts the live multiple-Hub discovery response shape',
      () async {
    final adapter = _RecordingAuthAdapter((request) {
      if (request.path == '/api/entry/resolve') {
        return _jsonResponse({
          'email': 'phone:19900000000',
          'mode': 'multiple',
          'default_hub_id': 'hub-live',
          'default_pwa_url': 'https://hub.mypapers.top/app',
          'hubs': [
            {
              'hub_id': 'hub-live',
              'base_url': 'https://hub.mypapers.top',
              'status': 'online',
            },
            {
              'hub_id': 'hub-live',
              'tenant_id': 'vantagics',
              'tenant_name': 'Vantagics',
              'name': 'MaClaw Official',
              'base_url': 'https://hub.mypapers.top',
              'status': 'online',
            },
          ],
        });
      }
      return _jsonResponse({
        'status': 'sent',
        'expires_min': 5,
        'code_length': 6,
      });
    });
    final service = AuthService(dio: Dio()..httpClientAdapter = adapter);

    final result = await service.requestPhoneLogin('19900000000');

    expect(result.hubUrl, 'https://hub.mypapers.top');
    expect(result.hubId, 'hub-live');
    expect(result.tenantId, 'vantagics');
    expect(result.tenantName, 'Vantagics');
    expect(adapter.requests.last.data, {
      'phone_number': '19900000000',
      'tenant_id': 'vantagics',
    });
  });

  test('phone login honors HubCenter default hub before list ordering', () {
    final route = PhoneLoginRouteResult.fromJson({
      'mode': 'multiple',
      'default_hub_id': 'hub-b',
      'hubs': [
        {
          'hub_id': 'hub-a',
          'base_url': 'https://a.maclaw.top',
          'status': 'online',
        },
        {
          'hub_id': 'hub-b',
          'tenant_id': 'tenant-b',
          'base_url': 'https://b.maclaw.top',
          'status': 'online',
        },
      ],
    });

    expect(route.selectedHub?.hubId, 'hub-b');
    expect(route.selectedHub?.tenantId, 'tenant-b');
  });

  test('phone login falls back to the next official HubCenter on 5xx',
      () async {
    final adapter = _RecordingAuthAdapter((request) {
      if (request.baseUrl == maclawDefaultHubCenterUrl) {
        return _jsonResponse({'error': 'temporarily unavailable'}, 503);
      }
      if (request.path == '/api/entry/resolve') {
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

  test('phone login falls back when a preset HubCenter has no mobile route',
      () async {
    final adapter = _RecordingAuthAdapter((request) {
      if (request.baseUrl == maclawDefaultHubCenterUrl) {
        return _jsonResponse({'error': 'route unavailable'}, 404);
      }
      if (request.path == '/api/entry/resolve') {
        return _jsonResponse({
          'default_hub_id': 'hub-a',
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
      return _jsonResponse({'status': 'sent', 'expires_minutes': 5}, 200);
    });
    final service = AuthService(dio: Dio()..httpClientAdapter = adapter);

    final result = await service.requestPhoneLogin('19900001111');

    expect(result.hubCenterUrl, 'https://hubs.maclaw.top');
    expect(
      adapter.requests.map((request) => request.baseUrl),
      contains(maclawDefaultHubCenterUrl),
    );
  });

  test('phone login rejects invalid phone numbers before HubCenter probe',
      () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({'status': 'should-not-be-called'}),
    );
    final service = AuthService(dio: Dio()..httpClientAdapter = adapter);

    for (final value in const [
      '',
      'abc',
      '1234567',
      '1234567890123456',
      'user19900001111',
      '199-TEST-1111',
      'phone:19900001111',
    ]) {
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
    expect(adapter.requests.single.path,
        '/api/mobile/auth/phone/verify-and-start');
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
