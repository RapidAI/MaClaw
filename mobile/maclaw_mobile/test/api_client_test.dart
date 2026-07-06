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
    const qrPayload =
        '{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_test","hub_url":"https://tenant-a.maclaw.top"}';
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
      ' $qrPayload ',
    );

    expect(adapter.requests, hasLength(1));
    expect(adapter.requests.single.baseUrl, 'https://tenant-a.maclaw.top');
    expect(
      adapter.requests.single.path,
      '/api/mobile/llm/desktop-qr-authorizations',
    );
    expect(adapter.requests.single.data, {
      'qr_payload': qrPayload,
    });
    expect(
      bootstrap.connection.selectedHubCenterUrl,
      'https://hubs.maclaw.top',
    );
    expect(bootstrap.connection.hubUrl, 'https://tenant-a.maclaw.top');
    expect(bootstrap.llmAccess.desktopQrDelegated, isTrue);
    expect(bootstrap.llmAccess.authorizationId, 'qr-auth-1');
  });

  test('desktop GUI QR LLM authorization rejects non MaClaw GUI payloads',
      () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) => _jsonResponse({'status': 'should-not-be-called'}),
    );
    final client = ApiClient(
      vault: const SecureVault(),
      dio: Dio()..httpClientAdapter = adapter,
      hubUrl: 'https://tenant-a.maclaw.top',
    );
    final invalidPayloads = [
      'https://llm.example.com/v1',
      'sk-test-secret',
      '{"v":1,"type":"maclaw_llm","url":"https://llm.example.com/v1","key":"sk-test"}',
      '{"v":2,"type":"maclaw_mobile_llm_authorization","hub_url":"https://tenant-a.maclaw.top"}',
      '{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_test"}',
      '{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_test","hub_url":"http://tenant-a.maclaw.top"}',
    ];

    for (final payload in invalidPayloads) {
      await expectLater(
        client.authorizeThirdPartyLlmWithDesktopQr(payload),
        throwsA(isA<FormatException>()),
      );
    }
    expect(adapter.requests, isEmpty);
  });

  test('desktop GUI QR LLM authorization rejects mobile auth payloads',
      () async {
    FlutterSecureStorage.setMockInitialValues({});
    const qrPayload =
        '{"v":2,"type":"maclaw_mobile_desktop_authorization","session_id":"maqr_test","hub_url":"https://tenant-a.maclaw.top"}';
    final adapter = _RecordingApiAdapter(
      (request) => _jsonResponse({'status': 'should-not-be-called'}),
    );
    final client = ApiClient(
      vault: const SecureVault(),
      dio: Dio()..httpClientAdapter = adapter,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    await expectLater(
      client.authorizeThirdPartyLlmWithDesktopQr(qrPayload),
      throwsA(isA<FormatException>()),
    );
    expect(adapter.requests, isEmpty);
  });

  test('desktop GUI QR LLM authorization rejects a different Hub URL',
      () async {
    FlutterSecureStorage.setMockInitialValues({});
    const qrPayload =
        '{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_test","hub_url":"https://other-tenant.maclaw.top"}';
    final adapter = _RecordingApiAdapter(
      (request) => _jsonResponse({'status': 'should-not-be-called'}),
    );
    final client = ApiClient(
      vault: const SecureVault(),
      dio: Dio()..httpClientAdapter = adapter,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    await expectLater(
      client.authorizeThirdPartyLlmWithDesktopQr(qrPayload),
      throwsUnsupportedError,
    );
    expect(adapter.requests, isEmpty);
  });

  test('SSH analysis request can include backend session id', () async {
    final adapter = _RecordingApiAdapter(
      (request) => _jsonResponse({
        'summary': 'summary',
        'recommendation': 'recommendation',
        'command_draft': 'systemctl status app',
        'backend_session_id': 'mobile-ssh:sess-1 token=server-secret',
      }),
    );
    final client = ApiClient(
      dio: Dio()..httpClientAdapter = adapter,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final analysis = await client.analyzeSSHOutput(
      'backend session output',
      backendSessionId: 'mobile-ssh:sess-1',
    );

    expect(adapter.requests, hasLength(1));
    expect(adapter.requests.single.path, '/api/mobile/ssh/analyze');
    expect(adapter.requests.single.data, {
      'output': 'backend session output',
      'backend_session_id': 'mobile-ssh:sess-1',
    });
    expect(analysis.commandDraft, 'systemctl status app');
    expect(
      analysis.backendSessionId,
      'mobile-ssh:sess-1 token=[REDACTED_SECRET]',
    );
  });

  test('LLM service status parses credits from configured path', () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) => _jsonResponse({
        'service_status': {
          'active': true,
          'auth_mode': 'grant_required',
          'default_model': 'maclaw-chat',
          'available_models': ['maclaw-chat', 'maclaw-fast'],
          'service_group_names': ['Official'],
          'credits_total': 100,
          'credits_used': 12.5,
          'credits_remaining': 87.5,
          'credits_available': 80,
          'tokens_per_credit': 1000,
        },
      }),
    );
    final dio = Dio()..httpClientAdapter = adapter;
    final client = ApiClient(
      vault: const SecureVault(),
      dio: dio,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final status = await client.llmServiceStatus('/api/llm/service/status');

    expect(adapter.requests.single.path, '/api/llm/service/status');
    expect(status.active, isTrue);
    expect(status.defaultModel, 'maclaw-chat');
    expect(status.availableModels, ['maclaw-chat', 'maclaw-fast']);
    expect(status.serviceGroupNames, ['Official']);
    expect(status.creditsTotal, 100);
    expect(status.creditsUsed, 12.5);
    expect(status.creditsRemaining, 87.5);
    expect(status.creditsAvailable, 80);
    expect(status.tokensPerCredit, 1000);
  });

  test('LLM service status accepts same discovered Hub absolute path',
      () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) => _jsonResponse({
        'service_status': {
          'active': true,
          'credits_available': 8,
        },
      }),
    );
    final client = ApiClient(
      vault: const SecureVault(),
      dio: Dio()..httpClientAdapter = adapter,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final status = await client.llmServiceStatus(
      'https://tenant-a.maclaw.top/api/llm/service/status',
    );

    expect(status.active, isTrue);
    expect(status.creditsAvailable, 8);
    expect(
      adapter.requests.single.path,
      'https://tenant-a.maclaw.top/api/llm/service/status',
    );
  });

  test('LLM service status rejects external absolute path before request',
      () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) => _jsonResponse({'status': 'should-not-be-called'}),
    );
    final client = ApiClient(
      vault: const SecureVault(),
      dio: Dio()..httpClientAdapter = adapter,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    await expectLater(
      client.llmServiceStatus('https://example.invalid/api/llm/service/status'),
      throwsUnsupportedError,
    );
    expect(adapter.requests, isEmpty);
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

  test('backend SSH sessions use tenant Hub session APIs', () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) {
        if (request.path == '/api/mobile/ssh/sessions') {
          return _jsonResponse({
            'session': {
              'session_id': 'ssh-session-1',
              'server_profile_id': 'srv-prod',
              'backend_session_id': 'mobile-ssh:ssh-session-1',
              'status': 'connected',
              'state': 'running',
              'claimed_by': 'desktop-agent-1',
              'pending_input_count': 2,
              'recent_output': 'ready\n',
              'output_chunk': 'ready\n',
              'output_seq': 1,
              'created_at': '2026-07-06T09:00:00Z',
              'updated_at': '2026-07-06T09:05:00Z',
              'last_activity_at': '2026-07-06T09:06:00Z',
            },
          });
        }
        if (request.path == '/api/mobile/ssh/sessions/ssh-session-1/input') {
          return _jsonResponse({
            'session_id': 'ssh-session-1',
            'output': 'ran uptime\n',
            'status': 'accepted',
          });
        }
        if (request.path ==
            '/api/mobile/ssh/sessions/ssh-session-1/reconnect') {
          return _jsonResponse({
            'session': {
              'session_id': 'ssh-session-1',
              'server_profile_id': 'srv-prod',
              'status': 'connected',
            },
          });
        }
        if (request.path ==
            '/api/mobile/ssh/sessions/ssh-session-1/interrupt') {
          return _jsonResponse({
            'session': {
              'session_id': 'ssh-session-1',
              'server_profile_id': 'srv-prod',
              'status': 'interrupt_requested',
              'state': 'interrupting',
            },
          });
        }
        return _jsonResponse({'ok': true});
      },
    );
    final client = ApiClient(
      vault: const SecureVault(),
      dio: Dio()..httpClientAdapter = adapter,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final session = await client.createBackendSSHSession(
      serverProfileId: 'srv-prod',
    );
    final input = await client.sendBackendSSHSessionInput(
      sessionId: session.sessionId,
      input: 'uptime\r',
    );
    final reconnected = await client.reconnectBackendSSHSession(
      session.sessionId,
    );
    final interrupted = await client.interruptBackendSSHSession(
      session.sessionId,
    );
    await client.closeBackendSSHSession(session.sessionId);

    expect(session.sessionId, 'ssh-session-1');
    expect(session.serverProfileId, 'srv-prod');
    expect(session.backendSessionId, 'mobile-ssh:ssh-session-1');
    expect(session.state, 'running');
    expect(session.claimedBy, 'desktop-agent-1');
    expect(session.pendingInputCount, 2);
    expect(session.connected, isTrue);
    expect(session.recentOutput, 'ready\n');
    expect(session.outputChunk, 'ready\n');
    expect(session.outputSeq, 1);
    expect(session.createdAt, DateTime.utc(2026, 7, 6, 9));
    expect(session.updatedAt, DateTime.utc(2026, 7, 6, 9, 5));
    expect(session.lastActivityAt, DateTime.utc(2026, 7, 6, 9, 6));
    expect(input.output, 'ran uptime\n');
    expect(reconnected.connected, isTrue);
    expect(interrupted.status, 'interrupt_requested');
    expect(interrupted.state, 'interrupting');
    expect(adapter.requests.map((request) => request.path), [
      '/api/mobile/ssh/sessions',
      '/api/mobile/ssh/sessions/ssh-session-1/input',
      '/api/mobile/ssh/sessions/ssh-session-1/reconnect',
      '/api/mobile/ssh/sessions/ssh-session-1/interrupt',
      '/api/mobile/ssh/sessions/ssh-session-1',
    ]);
    expect(adapter.requests.first.data, {'server_profile_id': 'srv-prod'});
    expect(adapter.requests[1].data, {'input': 'uptime\r'});
  });

  test('backend SSH tasks and file operations use Hub control records',
      () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) {
        if (request.path == '/api/mobile/ssh/sessions/ssh-session-1/tasks' &&
            request.method == 'POST') {
          return _jsonResponse({
            'task': {
              'task_id': 'task-1',
              'session_id': 'ssh-session-1',
              'backend_session_id': 'mobile-ssh:ssh-session-1',
              'command': 'journalctl -u app -n 200',
              'status': 'running',
              'log_tail': 'collecting logs\n',
              'claimed_by': 'desktop-agent-1',
              'created_at': '2026-07-06T10:00:00Z',
              'updated_at': '2026-07-06T10:00:05Z',
            },
          });
        }
        if (request.path == '/api/mobile/ssh/sessions/ssh-session-1/tasks' &&
            request.method == 'GET') {
          return _jsonResponse({
            'tasks': [
              {
                'task_id': 'task-1',
                'session_id': 'ssh-session-1',
                'status': 'running',
              },
            ],
          });
        }
        if (request.path ==
            '/api/mobile/ssh/sessions/ssh-session-1/tasks/task-1') {
          return _jsonResponse({
            'task_id': 'task-1',
            'session_id': 'ssh-session-1',
            'status': 'running',
            'tail': 'still running\n',
          });
        }
        if (request.path ==
            '/api/mobile/ssh/sessions/ssh-session-1/tasks/task-1/wait') {
          return _jsonResponse({
            'task': {
              'task_id': 'task-1',
              'session_id': 'ssh-session-1',
              'status': 'completed',
              'exit_code': 0,
              'output': 'done\n',
            },
          });
        }
        if (request.path ==
            '/api/mobile/ssh/sessions/ssh-session-1/tasks/task-1/kill') {
          return _jsonResponse({
            'task': {
              'task_id': 'task-1',
              'session_id': 'ssh-session-1',
              'status': 'killed',
              'message': 'terminated by GUI/agent',
            },
          });
        }
        if (request.path == '/api/mobile/ssh/sessions/ssh-session-1/files') {
          return _jsonResponse({
            'operation': {
              'operation_id': 'file-op-1',
              'session_id': 'ssh-session-1',
              'backend_session_id': 'mobile-ssh:ssh-session-1',
              'action': 'download',
              'remote_path': '/var/log/app.log',
              'local_path': 'mobile-downloads/app.log',
              'status': 'completed',
              'bytes_transferred': 42,
              'download_url': '/api/mobile/ssh/files/file-op-1/download',
              'claimed_by': 'desktop-agent-1',
            },
          });
        }
        return _jsonResponse({'ok': true});
      },
    );
    final client = ApiClient(
      vault: const SecureVault(),
      dio: Dio()..httpClientAdapter = adapter,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final started = await client.startBackendSSHBackgroundTask(
      sessionId: 'ssh-session-1',
      command: 'journalctl -u app -n 200',
      tailLines: 80,
    );
    final listed = await client.listBackendSSHBackgroundTasks('ssh-session-1');
    final checked = await client.getBackendSSHBackgroundTask(
      sessionId: 'ssh-session-1',
      taskId: started.taskId,
    );
    final waited = await client.waitBackendSSHBackgroundTask(
      sessionId: 'ssh-session-1',
      taskId: started.taskId,
      timeoutSeconds: 30,
      tailLines: 120,
    );
    final killed = await client.killBackendSSHBackgroundTask(
      sessionId: 'ssh-session-1',
      taskId: started.taskId,
    );
    final fileOperation = await client.requestBackendSSHFileOperation(
      sessionId: 'ssh-session-1',
      action: 'download',
      remotePath: '/var/log/app.log',
      localPath: 'mobile-downloads/app.log',
    );

    expect(started.taskId, 'task-1');
    expect(started.backendSessionId, 'mobile-ssh:ssh-session-1');
    expect(started.running, isTrue);
    expect(started.logTail, 'collecting logs\n');
    expect(started.createdAt, DateTime.utc(2026, 7, 6, 10));
    expect(started.updatedAt, DateTime.utc(2026, 7, 6, 10, 0, 5));
    expect(listed.single.taskId, 'task-1');
    expect(checked.logTail, 'still running\n');
    expect(waited.status, 'completed');
    expect(waited.exitCode, 0);
    expect(waited.logTail, 'done\n');
    expect(killed.status, 'killed');
    expect(fileOperation.operationId, 'file-op-1');
    expect(fileOperation.backendSessionId, 'mobile-ssh:ssh-session-1');
    expect(fileOperation.action, 'download');
    expect(fileOperation.bytesTransferred, 42);
    expect(
      fileOperation.downloadUrl,
      '/api/mobile/ssh/files/file-op-1/download',
    );
    expect(adapter.requests.map((request) => request.path), [
      '/api/mobile/ssh/sessions/ssh-session-1/tasks',
      '/api/mobile/ssh/sessions/ssh-session-1/tasks',
      '/api/mobile/ssh/sessions/ssh-session-1/tasks/task-1',
      '/api/mobile/ssh/sessions/ssh-session-1/tasks/task-1/wait',
      '/api/mobile/ssh/sessions/ssh-session-1/tasks/task-1/kill',
      '/api/mobile/ssh/sessions/ssh-session-1/files',
    ]);
    expect(adapter.requests.first.data, {
      'action': 'exec_background',
      'command': 'journalctl -u app -n 200',
      'tail_lines': 80,
    });
    expect(adapter.requests[3].data, {
      'timeout': 30,
      'tail_lines': 120,
    });
    expect(adapter.requests.last.data, {
      'action': 'download',
      'local_path': 'mobile-downloads/app.log',
      'remote_path': '/var/log/app.log',
    });
  });

  test('server profiles parse sanitized tenant Hub profiles', () async {
    FlutterSecureStorage.setMockInitialValues({});
    final adapter = _RecordingApiAdapter(
      (request) => _jsonResponse({
        'profiles': [
          {
            'id': 'prod',
            'name': 'Prod',
            'host': '10.0.0.10',
            'port': 2222,
            'username': 'deploy',
            'auth_mode': 'private_key',
            'tag': 'desktop',
            'note': 'Published from desktop',
          },
          {
            'id': 'bad',
            'host': '',
            'port': 22,
            'username': 'root',
          },
        ],
      }),
    );
    final client = ApiClient(
      vault: const SecureVault(),
      dio: Dio()..httpClientAdapter = adapter,
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final profiles = await client.listServerProfiles();

    expect(adapter.requests.single.path, '/api/mobile/server-profiles');
    expect(profiles, hasLength(1));
    expect(profiles.single.id, 'prod');
    expect(profiles.single.authMode, 'private_key');
    expect(profiles.single.tag, 'desktop');
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
  final String method;
  final Object? data;

  const _RecordedApiRequest({
    required this.baseUrl,
    required this.path,
    required this.method,
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
      method: options.method,
      data: options.data,
    );
    requests.add(request);
    return handler(request);
  }

  @override
  void close({bool force = false}) {}
}
