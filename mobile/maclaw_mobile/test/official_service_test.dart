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
