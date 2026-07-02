import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/storage/secure_vault.dart';
import 'package:maclaw_mobile/features/documents/document_draft.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('desktop GUI QR LLM authorization posts to discovered Hub', () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) => _jsonResponse({
        'bootstrap': {
          'user': {
            'user_id': 'user-1',
            'email': 'mobile@example.com',
            'tenant_id': 'tenant-1',
          },
          'connection': {
            'selected_hubcenter_url': 'https://hubs.maclaw.top',
            'hub_url': 'https://tenant-a.maclaw.top',
            'hub_id': 'hub-a',
            'tenant_id': 'tenant-1',
          },
          'llm_access': {
            'mode': 'desktop_qr_third_party',
            'status': 'available',
            'authorization_id': 'qr-auth-1',
          },
          'services': {},
          'features': {},
          'limits': {},
        },
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final client = ApiClient(
      vault: const SecureVault(),
      dio: dio,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final bootstrap = await client.authorizeThirdPartyLlmWithDesktopQr(
      ' maclaw-gui-qr-payload ',
    );

    expect(adapter.requests, hasLength(1));
    expect(adapter.requests.single.baseUrl, 'https://tenant-a.maclaw.top');
    expect(
      adapter.requests.single.path,
      '/api/mobile/llm/desktop-qr-authorizations',
    );
    expect(adapter.requests.single.data, {
      'qr_payload': 'maclaw-gui-qr-payload',
    });
    expect(
      bootstrap.connection.selectedHubCenterUrl,
      'https://hubs.maclaw.top',
    );
    expect(bootstrap.connection.hubUrl, 'https://tenant-a.maclaw.top');
    expect(bootstrap.llmAccess.desktopQrDelegated, isTrue);
    expect(bootstrap.llmAccess.authorizationId, 'qr-auth-1');
  });

  test('digital employee task posts mobile task type and context', () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) => _jsonResponse({
        'task_id': 'mobve_1',
        'employee_id': 'ops',
        'prompt': 'check disk',
        'task_type': 'server_maintenance',
        'context': {'source': 'maclaw_mobile'},
        'status': 'queued',
        'result': '',
        'claimed_by': '',
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final client = ApiClient(
      vault: const SecureVault(),
      dio: dio,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final task = await client.createDigitalEmployeeTask(
      employeeId: 'ops',
      prompt: 'check disk',
      taskType: 'server_maintenance',
      context: {'source': 'maclaw_mobile'},
    );

    expect(
      adapter.requests.single.path,
      '/api/mobile/digital-employees/ops/tasks',
    );
    expect(adapter.requests.single.data, {
      'prompt': 'check disk',
      'task_type': 'server_maintenance',
      'context': {'source': 'maclaw_mobile'},
    });
    expect(task.taskType, 'server_maintenance');
    expect(task.context['source'], 'maclaw_mobile');
  });

  test('document export download requests relative paths from discovered Hub',
      () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) => _bytesResponse([1, 2, 3]),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final client = ApiClient(
      vault: const SecureVault(),
      dio: dio,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final bytes = await client.downloadDocumentExport(
      DocumentExportJob(
        jobId: 'export-1',
        draftId: 'draft-1',
        format: DocumentExportFormat.pdf,
        status: 'ready',
        downloadUrl: '/api/mobile/documents/exports/export-1/download',
        createdAt: DateTime.utc(2026, 7, 2),
      ),
    );

    expect(bytes, [1, 2, 3]);
    expect(adapter.requests, hasLength(1));
    expect(adapter.requests.single.baseUrl, 'https://tenant-a.maclaw.top');
    expect(
      adapter.requests.single.path,
      'https://tenant-a.maclaw.top/api/mobile/documents/exports/export-1/download',
    );
  });

  test('document export download accepts same discovered Hub absolute URL',
      () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) => _bytesResponse([4, 5, 6]),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final client = ApiClient(
      vault: const SecureVault(),
      dio: dio,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final bytes = await client.downloadDocumentExport(
      DocumentExportJob(
        jobId: 'export-2',
        draftId: 'draft-1',
        format: DocumentExportFormat.word,
        status: 'ready',
        downloadUrl:
            'https://tenant-a.maclaw.top/api/mobile/documents/exports/export-2/download',
        createdAt: DateTime.utc(2026, 7, 2),
      ),
    );

    expect(bytes, [4, 5, 6]);
    expect(adapter.requests, hasLength(1));
    expect(
      adapter.requests.single.path,
      'https://tenant-a.maclaw.top/api/mobile/documents/exports/export-2/download',
    );
  });

  test('document export download rejects external URL before request',
      () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) => _bytesResponse([9]),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final client = ApiClient(
      vault: const SecureVault(),
      dio: dio,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    await expectLater(
      client.downloadDocumentExport(
        DocumentExportJob(
          jobId: 'export-3',
          draftId: 'draft-1',
          format: DocumentExportFormat.markdown,
          status: 'ready',
          downloadUrl:
              'https://example.invalid/api/mobile/documents/exports/export-3/download',
          createdAt: DateTime.utc(2026, 7, 2),
        ),
      ),
      throwsUnsupportedError,
    );
    expect(adapter.requests, isEmpty);
  });
}

ResponseBody _jsonResponse(Map<String, Object?> body) {
  return ResponseBody.fromString(
    jsonEncode(body),
    200,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

ResponseBody _bytesResponse(List<int> body) {
  return ResponseBody.fromBytes(
    body,
    200,
    headers: {
      Headers.contentTypeHeader: ['application/octet-stream'],
    },
  );
}

class _RecordedApiRequest {
  final String baseUrl;
  final String path;
  final Object? data;

  const _RecordedApiRequest({
    required this.baseUrl,
    required this.path,
    required this.data,
  });
}

class _RecordingApiAdapter implements HttpClientAdapter {
  final ResponseBody Function(_RecordedApiRequest request) handler;
  final requests = <_RecordedApiRequest>[];

  _RecordingApiAdapter(this.handler);

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<List<int>>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    final request = _RecordedApiRequest(
      baseUrl: options.baseUrl,
      path: options.path,
      data: options.data,
    );
    requests.add(request);
    return handler(request);
  }

  @override
  void close({bool force = false}) {}
}
