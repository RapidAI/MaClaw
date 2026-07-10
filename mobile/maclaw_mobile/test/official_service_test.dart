import 'package:flutter_test/flutter_test.dart';
import 'package:dio/dio.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/official_service.dart';
import 'package:maclaw_mobile/features/auth/auth_service.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';

void main() {
  test('mobile app has exactly three preset official HubCenters', () {
    expect(maclawOfficialHubCenterUrls, [
      'https://hubs.mypapers.top',
      'https://hubs.maclaw.top',
      'https://hubs2.maclaw.top',
    ]);
    expect(
      maclawOfficialHubCenterUrls.every(
        (url) =>
            url.startsWith('https://') && isMaclawOfficialHubCenterUrl(url),
      ),
      isTrue,
    );
  });

  test('HubCenter unavailable errors do not expose candidate URLs', () {
    const error = OfficialHubCenterUnavailableException([
      OfficialHubCenterAttempt(
        url: 'https://hubs.mypapers.top',
        available: false,
        message: 'HTTP 502',
      ),
      OfficialHubCenterAttempt(
        url: 'https://hubs.maclaw.top',
        available: false,
        message: 'timeout',
      ),
    ]);

    expect(error.toString(), contains('No official MaClaw HubCenter'));
    expect(error.toString(), isNot(contains('https://')));
    expect(error.attempts, hasLength(2));
  });

  test('HubCenter discovery falls through a failed preset', () async {
    final attempts = <String>[];
    final first = maclawOfficialHubCenterUrls[0];
    final second = maclawOfficialHubCenterUrls[1];

    final resolution = await tryOfficialHubCenters<String>(
      hubCenterUrls: maclawOfficialHubCenterUrls,
      operation: (_, candidate) async {
        attempts.add(candidate);
        if (candidate == first) {
          throw DioException(
            requestOptions: RequestOptions(path: '/api/entry/resolve'),
            response: Response(
              statusCode: 502,
              requestOptions: RequestOptions(path: '/api/entry/resolve'),
            ),
          );
        }
        return candidate;
      },
    );

    expect(resolution.selectedHubCenterUrl, second);
    expect(resolution.value, second);
    expect(attempts, [first, second]);
    expect(resolution.attempts, hasLength(2));
    expect(resolution.attempts.first.available, isFalse);
    expect(resolution.attempts.last.available, isTrue);
  });

  test('official HubCenter clients use bounded mobile network timeouts', () {
    final client = officialHubCenterDio(
      null,
      hubCenterUrl: maclawDefaultHubCenterUrl,
    );

    expect(client.options.connectTimeout, maclawHubCenterConnectTimeout);
    expect(client.options.sendTimeout, maclawHubCenterSendTimeout);
    expect(client.options.receiveTimeout, maclawHubCenterReceiveTimeout);
  });

  test('official HubCenter timeout defaults do not overwrite custom values',
      () {
    final client = officialHubCenterDio(
      Dio(
        BaseOptions(
          baseUrl: maclawDefaultHubCenterUrl,
          connectTimeout: const Duration(seconds: 2),
        ),
      ),
      hubCenterUrl: maclawDefaultHubCenterUrl,
    );

    expect(client.options.connectTimeout, const Duration(seconds: 2));
    expect(client.options.sendTimeout, maclawHubCenterSendTimeout);
    expect(client.options.receiveTimeout, maclawHubCenterReceiveTimeout);
  });

  test('discovered Hub clients use bounded mobile network timeouts', () {
    final client = discoveredHubDio(
      null,
      hubUrl: 'https://tenant-a.maclaw.top/path',
    );

    expect(client.options.baseUrl, 'https://tenant-a.maclaw.top');
    expect(client.options.connectTimeout, maclawHubCenterConnectTimeout);
    expect(client.options.sendTimeout, maclawHubCenterSendTimeout);
    expect(client.options.receiveTimeout, maclawHubCenterReceiveTimeout);
  });

  test('discovered Hub timeout defaults do not overwrite custom values', () {
    final client = discoveredHubDio(
      Dio(
        BaseOptions(
          connectTimeout: const Duration(seconds: 2),
          receiveTimeout: const Duration(seconds: 3),
        ),
      ),
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    expect(client.options.connectTimeout, const Duration(seconds: 2));
    expect(client.options.sendTimeout, maclawHubCenterSendTimeout);
    expect(client.options.receiveTimeout, const Duration(seconds: 3));
  });

  test('api client resolves relative paths against discovered Hub', () {
    final client = ApiClient(hubUrl: 'https://tenant-a.maclaw.top');

    expect(
      client.absoluteUrl('/api/mobile/bootstrap'),
      'https://tenant-a.maclaw.top/api/mobile/bootstrap',
    );
  });

  test('api client accepts only same-Hub absolute URLs', () {
    final client = ApiClient(hubUrl: 'https://tenant-a.maclaw.top');

    expect(
      client.absoluteUrl('https://tenant-a.maclaw.top/api/mobile/bootstrap'),
      'https://tenant-a.maclaw.top/api/mobile/bootstrap',
    );
    expect(
      () =>
          client.absoluteUrl('http://tenant-a.maclaw.top/api/mobile/bootstrap'),
      throwsUnsupportedError,
    );
    expect(
      () => client.absoluteUrl('https://example.invalid/api/mobile/bootstrap'),
      throwsUnsupportedError,
    );
  });

  test('document export downloads reject non-Hub absolute URLs', () async {
    final client = ApiClient(hubUrl: 'https://tenant-a.maclaw.top');

    await expectLater(
      client.downloadDocumentExport(
        DocumentExportJob(
          jobId: 'export-1',
          draftId: 'draft-1',
          format: DocumentExportFormat.pdf,
          status: 'ready',
          downloadUrl: 'https://example.invalid/export.pdf',
          createdAt: DateTime.utc(2026, 7, 2),
        ),
      ),
      throwsUnsupportedError,
    );
  });

  test('api client rejects clients for a different discovered Hub', () {
    expect(
      () => ApiClient(
        hubUrl: 'https://tenant-a.maclaw.top',
        dio: Dio(BaseOptions(baseUrl: 'https://tenant-a.maclaw.top/')),
      ),
      returnsNormally,
    );
    expect(
      () => ApiClient(
        hubUrl: 'https://tenant-a.maclaw.top',
        dio: Dio(BaseOptions(baseUrl: 'https://tenant-a.maclaw.top:443')),
      ),
      returnsNormally,
    );
    expect(
      () => ApiClient(
        hubUrl: 'https://tenant-a.maclaw.top',
        dio: Dio(BaseOptions(baseUrl: 'https://example.invalid')),
      ),
      throwsUnsupportedError,
    );
    expect(
      () => ApiClient(
        hubUrl: 'https://tenant-a.maclaw.top',
        dio: Dio(BaseOptions(baseUrl: 'https://tenant-a.maclaw.top:8443')),
      ),
      throwsUnsupportedError,
    );
  });

  test('api client resolves only discovered Hub websocket URLs', () {
    expect(
      maclawHubWebSocketUrl(hubUrl: 'https://tenant-a.maclaw.top'),
      'wss://tenant-a.maclaw.top/api/mobile/realtime',
    );
    expect(
      maclawHubWebSocketUrl(
        hubUrl: 'https://tenant-a.maclaw.top',
        path: 'api/mobile/realtime',
      ),
      'wss://tenant-a.maclaw.top/api/mobile/realtime',
    );
    expect(
      maclawHubWebSocketUrl(
        hubUrl: 'https://tenant-a.maclaw.top',
        path: 'wss://tenant-a.maclaw.top/api/mobile/realtime',
      ),
      'wss://tenant-a.maclaw.top/api/mobile/realtime',
    );
    expect(
      () => maclawHubWebSocketUrl(
        hubUrl: 'https://tenant-a.maclaw.top',
        path: 'wss://example.invalid/api/mobile/realtime',
      ),
      throwsUnsupportedError,
    );
    expect(
      () => maclawHubWebSocketUrl(
        hubUrl: 'https://tenant-a.maclaw.top',
        path: 'https://example.invalid/api/mobile/realtime',
      ),
      throwsUnsupportedError,
    );
    expect(
      isMaclawHubWebSocketUrl(
        'wss://tenant-a.maclaw.top/api/mobile/realtime',
        'https://tenant-a.maclaw.top',
      ),
      isTrue,
    );
    expect(
      isMaclawHubWebSocketUrl(
        'ws://tenant-a.maclaw.top/api/mobile/realtime',
        'https://tenant-a.maclaw.top',
      ),
      isFalse,
    );
    expect(
      isMaclawHubWebSocketUrl(
        'wss://example.invalid/api/mobile/realtime',
        'https://tenant-a.maclaw.top',
      ),
      isFalse,
    );
  });

  test('auth service rejects non-preset HubCenter clients', () {
    expect(
      () => AuthService(
        dio: Dio(BaseOptions(baseUrl: 'https://example.invalid')),
      ),
      throwsUnsupportedError,
    );
  });
}
