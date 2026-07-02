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

  test('requestEmailLogin posts trimmed email to selected HubCenter', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'status': 'sent',
        'message': 'check inbox',
        'poll_id': 'poll-1',
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final service = AuthService(dio: dio);

    final result = await service.requestEmailLogin(' user@example.com ');

    expect(result.status, 'sent');
    expect(result.message, 'check inbox');
    expect(result.pollId, 'poll-1');
    expect(result.hubCenterUrl, maclawDefaultHubCenterUrl);
    expect(adapter.requests, hasLength(1));
    expect(adapter.requests.single.method, 'POST');
    expect(adapter.requests.single.baseUrl, maclawDefaultHubCenterUrl);
    expect(adapter.requests.single.path, '/api/auth/email-request');
    expect(adapter.requests.single.data, {'email': 'user@example.com'});
    expect(
      adapter.requests.single.headers['X-MaClaw-HubCenter-URL'],
      maclawDefaultHubCenterUrl,
    );
  });

  test('confirmed email poll stores discovered Hub session token', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'status': 'confirmed',
        'access_token': 'mobile-access-token',
        'user': {
          'email': 'user@example.com',
          'tenant_id': 'tenant-a',
        },
        'hub': {
          'id': 'hub-a',
          'base_url': 'https://tenant-a.maclaw.top',
        },
        'hubcenter_url': 'https://hubs.maclaw.top',
        'llm': {
          'mode': 'desktop_qr_third_party',
          'authorization_id': 'llm-auth-1',
        },
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    const vault = SecureVault();
    final service = AuthService(vault: vault, dio: dio);

    final result = await service.pollEmailLogin('poll-confirmed');

    expect(result.confirmed, isTrue);
    expect(result.accessToken, 'mobile-access-token');
    expect(result.email, 'user@example.com');
    expect(result.tenantId, 'tenant-a');
    expect(result.hubUrl, 'https://tenant-a.maclaw.top');
    expect(result.hubId, 'hub-a');
    expect(result.hubCenterUrl, maclawDefaultHubCenterUrl);
    expect(result.llmMode, 'desktop_qr_third_party');
    expect(result.llmAuthorizationId, 'llm-auth-1');
    expect(await vault.readHubUrl(), 'https://tenant-a.maclaw.top');
    expect(await vault.readToken(), 'mobile-access-token');
    expect(adapter.requests.single.path, '/api/auth/email-poll');
    expect(adapter.requests.single.data, {'poll_id': 'poll-confirmed'});
    expect(
      adapter.requests.single.headers['X-MaClaw-HubCenter-URL'],
      maclawDefaultHubCenterUrl,
    );
  });

  test('email login falls back to the next official HubCenter on 5xx',
      () async {
    final adapter = _RecordingAuthAdapter(
      (request) {
        if (request.baseUrl == maclawDefaultHubCenterUrl) {
          return _jsonResponse({'error': 'temporarily unavailable'}, 503);
        }
        return _jsonResponse({
          'status': 'sent',
          'message': 'check inbox',
          'poll_id': 'poll-2',
        });
      },
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final service = AuthService(dio: dio);

    final result = await service.requestEmailLogin('user@example.com');

    expect(result.status, 'sent');
    expect(result.pollId, 'poll-2');
    expect(result.hubCenterUrl, 'https://hubs.maclaw.top');
    expect(
      adapter.requests.map((request) => request.baseUrl),
      [maclawDefaultHubCenterUrl, 'https://hubs.maclaw.top'],
    );
  });

  test('email poll uses the HubCenter selected during login request', () async {
    final adapter = _RecordingAuthAdapter(
      (request) {
        if (request.path == '/api/auth/email-request' &&
            request.baseUrl == maclawDefaultHubCenterUrl) {
          return _jsonResponse({'error': 'temporarily unavailable'}, 503);
        }
        if (request.path == '/api/auth/email-request') {
          return _jsonResponse({
            'status': 'sent',
            'message': 'check inbox',
            'poll_id': 'poll-3',
          });
        }
        return _jsonResponse({
          'status': 'confirmed',
          'access_token': 'mobile-access-token',
          'hub': {
            'id': 'hub-a',
            'base_url': 'https://tenant-a.maclaw.top',
          },
        });
      },
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final service = AuthService(dio: dio);

    await service.requestEmailLogin('user@example.com');
    final result = await service.pollEmailLogin('poll-3');

    expect(result.confirmed, isTrue);
    expect(result.hubCenterUrl, 'https://hubs.maclaw.top');
    expect(adapter.requests.last.path, '/api/auth/email-poll');
    expect(adapter.requests.last.baseUrl, 'https://hubs.maclaw.top');
    expect(
      adapter.requests.last.headers['X-MaClaw-HubCenter-URL'],
      'https://hubs.maclaw.top',
    );
  });

  test('email login does not fallback on client validation errors', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({'error': 'invalid email'}, 400),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final service = AuthService(dio: dio);

    await expectLater(
      service.requestEmailLogin('not-an-email'),
      throwsA(isA<DioException>()),
    );
    expect(adapter.requests, hasLength(1));
    expect(adapter.requests.single.baseUrl, maclawDefaultHubCenterUrl);
  });

  test('pending email poll does not store a session token', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'status': 'pending',
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    const vault = SecureVault();
    final service = AuthService(vault: vault, dio: dio);

    final result = await service.pollEmailLogin('poll-pending');

    expect(result.confirmed, isFalse);
    expect(await vault.readHubUrl(), isNull);
    expect(await vault.readToken(), isNull);
  });

  test('targeted Hub email request posts to discovered Hub', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'status': 'sent',
        'message': 'check inbox',
        'poll_id': 'hub-poll-1',
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final service = AuthService(dio: dio);

    final result = await service.requestEmailLoginOnHub(
      hubUrl: 'https://tenant-a.maclaw.top/path',
      hubCenterUrl: 'https://hubs.maclaw.top',
      email: ' user@example.com ',
    );

    expect(result.status, 'sent');
    expect(result.pollId, 'hub-poll-1');
    expect(result.hubCenterUrl, 'https://hubs.maclaw.top');
    expect(adapter.requests, hasLength(1));
    expect(adapter.requests.single.baseUrl, 'https://tenant-a.maclaw.top');
    expect(adapter.requests.single.path, '/api/auth/email-request');
    expect(adapter.requests.single.data, {'email': 'user@example.com'});
    expect(
      adapter.requests.single.headers['X-MaClaw-HubCenter-URL'],
      'https://hubs.maclaw.top',
    );
  });

  test('targeted Hub email poll stores token issued by Hub', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'status': 'confirmed',
        'access_token': 'hub-issued-token',
        'user': {
          'email': 'user@example.com',
          'tenant_id': 'tenant-a',
        },
        'hub': {
          'id': 'hub-a',
          'base_url': 'https://tenant-a.maclaw.top',
        },
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    const vault = SecureVault();
    final service = AuthService(vault: vault, dio: dio);

    final result = await service.pollEmailLoginOnHub(
      hubUrl: 'https://tenant-a.maclaw.top',
      hubCenterUrl: 'https://hubs.maclaw.top',
      pollId: 'hub-poll-1',
    );

    expect(result.confirmed, isTrue);
    expect(result.accessToken, 'hub-issued-token');
    expect(result.hubUrl, 'https://tenant-a.maclaw.top');
    expect(result.hubCenterUrl, 'https://hubs.maclaw.top');
    expect(await vault.readHubUrl(), 'https://tenant-a.maclaw.top');
    expect(await vault.readToken(), 'hub-issued-token');
    expect(adapter.requests.single.baseUrl, 'https://tenant-a.maclaw.top');
    expect(adapter.requests.single.path, '/api/auth/email-poll');
    expect(adapter.requests.single.data, {'poll_id': 'hub-poll-1'});
  });

  test('official redemption stores discovered Hub session token', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'access_token': 'redeemed-token',
        'hub': {
          'id': 'hub-a',
          'base_url': 'https://tenant-a.maclaw.top',
        },
        'user': {
          'tenant_id': 'tenant-a',
        },
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    const vault = SecureVault();
    final service = AuthService(vault: vault, dio: dio);

    final result = await service.redeemOfficialServiceCode(' CODE-123 ');

    expect(result.accessToken, 'redeemed-token');
    expect(result.hubUrl, 'https://tenant-a.maclaw.top');
    expect(result.hubId, 'hub-a');
    expect(result.tenantId, 'tenant-a');
    expect(result.hubCenterUrl, maclawDefaultHubCenterUrl);
    expect(await vault.readHubUrl(), 'https://tenant-a.maclaw.top');
    expect(await vault.readToken(), 'redeemed-token');
    expect(adapter.requests.single.path, '/api/mobile/service-redemptions');
    expect(adapter.requests.single.data, {'code': 'CODE-123'});
    expect(
      adapter.requests.single.headers['X-MaClaw-HubCenter-URL'],
      maclawDefaultHubCenterUrl,
    );
  });

  test('official redemption pending email login does not store session',
      () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse(
        {
          'status': 'requires_email_login',
          'next_action': 'email_login',
          'message': 'continue with email login',
          'hub': {
            'id': 'hub-a',
            'base_url': 'https://tenant-a.maclaw.top',
          },
          'tenant_id': 'tenant-a',
        },
        202,
      ),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    const vault = SecureVault();
    final service = AuthService(vault: vault, dio: dio);

    await expectLater(
      service.redeemOfficialServiceCode('CODE-123'),
      throwsA(isA<MobileServiceConnectionPendingException>()),
    );
    expect(await vault.readHubUrl(), isNull);
    expect(await vault.readToken(), isNull);
    expect(adapter.requests.single.path, '/api/mobile/service-redemptions');
  });

  test('desktop GUI QR session stores discovered Hub session token', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'access_token': 'desktop-qr-token',
        'hub_url': 'https://tenant-b.maclaw.top',
        'hub_id': 'hub-b',
        'tenant_id': 'tenant-b',
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    const vault = SecureVault();
    final service = AuthService(vault: vault, dio: dio);

    final result = await service.connectWithDesktopLlmQr(' qr-payload ');

    expect(result.accessToken, 'desktop-qr-token');
    expect(result.hubUrl, 'https://tenant-b.maclaw.top');
    expect(result.hubId, 'hub-b');
    expect(result.tenantId, 'tenant-b');
    expect(await vault.readHubUrl(), 'https://tenant-b.maclaw.top');
    expect(await vault.readToken(), 'desktop-qr-token');
    expect(adapter.requests.single.path, '/api/mobile/llm/desktop-qr-sessions');
    expect(adapter.requests.single.data, {'qr_payload': 'qr-payload'});
  });

  test('desktop GUI mobile authorization QR consumes session on discovered Hub',
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
        'bootstrap': {
          'llm_access': {
            'mode': 'desktop_qr_third_party',
            'authorization_id': 'mllm_1',
          },
        },
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    const vault = SecureVault();
    final service = AuthService(vault: vault, dio: dio);

    final result = await service.connectWithDesktopLlmQr(qrPayload);

    expect(result.accessToken, 'desktop-qr-mobile-token');
    expect(result.hubUrl, 'https://tenant-a.maclaw.top');
    expect(result.hubId, 'hub-a');
    expect(result.tenantId, 'tenant-a');
    expect(await vault.readHubUrl(), 'https://tenant-a.maclaw.top');
    expect(await vault.readToken(), 'desktop-qr-mobile-token');
    expect(adapter.requests, hasLength(1));
    expect(adapter.requests.single.baseUrl, 'https://tenant-a.maclaw.top');
    expect(
      adapter.requests.single.path,
      '/api/mobile/llm/desktop-qr-sessions/consume',
    );
    expect(adapter.requests.single.data, {'qr_payload': qrPayload});
  });

  test('official service connection requires a Hub URL and token', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'access_token': 'missing-hub',
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    const vault = SecureVault();
    final service = AuthService(vault: vault, dio: dio);

    await expectLater(
      service.redeemOfficialServiceCode('CODE-123'),
      throwsA(isA<StateError>()),
    );
    expect(await vault.readHubUrl(), isNull);
    expect(await vault.readToken(), isNull);
  });

  test('confirmed email poll requires Hub URL and token', () async {
    final adapter = _RecordingAuthAdapter(
      (request) => _jsonResponse({
        'status': 'confirmed',
        'access_token': 'mobile-access-token',
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    const vault = SecureVault();
    final service = AuthService(vault: vault, dio: dio);

    await expectLater(
      service.pollEmailLogin('poll-confirmed'),
      throwsA(isA<StateError>()),
    );
    expect(await vault.readHubUrl(), isNull);
    expect(await vault.readToken(), isNull);
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
