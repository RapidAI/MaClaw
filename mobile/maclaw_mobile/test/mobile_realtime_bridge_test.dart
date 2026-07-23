import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_bridge.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_client.dart';
import 'package:maclaw_mobile/core/api/official_service.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/assistant/assistant_controller.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employees_controller.dart';
import 'package:maclaw_mobile/features/documents/documents_controller.dart';
import 'package:maclaw_mobile/features/servers/servers_controller.dart';
import 'package:stream_channel/stream_channel.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

class _SignedInSessionController extends SessionController {
  static int refreshCalls = 0;

  @override
  Future<SessionState> build() async => const SessionState.signedIn(
        hubUrl: maclawOfficialServiceUrl,
        bootstrap: MobileBootstrap(
          user: MobileUser(
            userId: 'user-1',
            email: 'mobile@example.com',
            tenantId: 'tenant-1',
          ),
          services: MobileServices(
            hubStatus: 'online',
            llmStatus: 'available',
            searchStatus: 'available',
            documentsStatus: 'available',
            digitalEmployeesStatus: 'available',
            llmStatusPath: '/api/llm/service/status',
            modelsPath: '/api/llm/v1/models',
            searchPath: '/api/mobile/search',
            documentsPath: '/api/mobile/documents',
            digitalEmployeesPath: '/api/mobile/digital-employees',
            realtimePath: maclawOfficialRealtimePath,
          ),
          features: MobileFeatures(
            search: true,
            documents: true,
            backendSshSessions: true,
            digitalEmployees: true,
            pushNotifications: true,
          ),
          limits: MobileLimits(
            maxUploadBytes: 25 * 1024 * 1024,
            maxExportJobs: 3,
          ),
        ),
      );

  @override
  Future<void> refreshBootstrap() async {
    refreshCalls++;
  }
}

class _FakeWsSink implements WebSocketSink {
  final void Function(Object? data) onAdd;
  final void Function() onClose;

  _FakeWsSink({required this.onAdd, required this.onClose});

  @override
  void add(Object? data) => onAdd(data);

  @override
  void addError(Object error, [StackTrace? stackTrace]) {}

  @override
  Future addStream(Stream stream) async {
    await for (final item in stream) {
      add(item);
    }
  }

  @override
  Future close([int? closeCode, String? closeReason]) async {
    onClose();
  }

  @override
  Future get done => Future.value();
}

class _FakeWebSocketChannel extends StreamChannelMixin
    implements WebSocketChannel {
  final StreamController<Object?> inbound;
  final List<Object?> sent;
  final Future<void> _ready;
  late final WebSocketSink _sink;

  _FakeWebSocketChannel({
    required this.inbound,
    required this.sent,
    Future<void>? ready,
  }) : _ready = ready ?? Future<void>.value() {
    _sink = _FakeWsSink(
      onAdd: sent.add,
      onClose: () {
        if (!inbound.isClosed) {
          unawaited(inbound.close());
        }
      },
    );
  }

  @override
  Stream get stream => inbound.stream;

  @override
  WebSocketSink get sink => _sink;

  @override
  Future<void> get ready => _ready;

  @override
  String? get protocol => null;

  @override
  int? get closeCode => null;

  @override
  String? get closeReason => null;
}

/// Fake WS harness: inbound JSON/binary, outbound hello/PTY recorded.
class _FakeWsHarness {
  final StreamController<Object?> inbound =
      StreamController<Object?>.broadcast();
  final List<Object?> sent = [];
  int connectCalls = 0;

  MobileRealtimeClient client() {
    return MobileRealtimeClient(
      readToken: () async => 'token',
      readHubUrl: () async => maclawOfficialServiceUrl,
      connect: (uri) {
        connectCalls++;
        return _FakeWebSocketChannel(inbound: inbound, sent: sent);
      },
    );
  }

  void pushJson(Map<String, dynamic> json) {
    inbound.add(jsonEncode(json));
  }

  Future<void> close() async {
    if (!inbound.isClosed) {
      await inbound.close();
    }
  }
}

class _RecordingDocumentsController extends DocumentsController {
  static final events = <MobileRealtimeEvent>[];

  @override
  Future<DocumentsState> build() async => const DocumentsState();

  @override
  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    events.add(event);
  }
}

class _EmptyDigitalEmployeesController extends DigitalEmployeesController {
  @override
  Future<List<DigitalEmployee>> build() async => const [];
}

class _EmptyDigitalEmployeeTaskHistoryController
    extends DigitalEmployeeTaskHistoryController {
  @override
  Future<List<MobileDigitalEmployeeTask>> build() async => const [];
}

class _RecordingDigitalEmployeeTaskController
    extends DigitalEmployeeTaskController {
  static final events = <MobileRealtimeEvent>[];

  @override
  Future<MobileDigitalEmployeeTask?> build() async => null;

  @override
  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    events.add(event);
  }
}

class _RecordingBackendSshSessionsController
    extends BackendSshSessionsController {
  static final events = <MobileRealtimeEvent>[];

  @override
  Future<Map<String, MobileBackendSSHSession>> build() async => const {};

  @override
  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    events.add(event);
  }
}

class _RecordingBackendSshTasksController extends BackendSshTasksController {
  static final events = <MobileRealtimeEvent>[];

  @override
  Future<Map<String, List<MobileBackendSSHTask>>> build() async => const {};

  @override
  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    events.add(event);
  }
}

class _RecordingBackendSshFileOperationsController
    extends BackendSshFileOperationsController {
  static final events = <MobileRealtimeEvent>[];

  @override
  Future<Map<String, List<MobileBackendSSHFileOperation>>> build() async =>
      const {};

  @override
  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    events.add(event);
  }
}

ProviderContainer _containerWith(
  MobileRealtimeClient client, {
  Duration? reconnectDelay,
}) {
  return ProviderContainer(
    overrides: [
      sessionControllerProvider.overrideWith(_SignedInSessionController.new),
      mobileRealtimeClientProvider.overrideWithValue(client),
      documentsControllerProvider.overrideWith(
        _RecordingDocumentsController.new,
      ),
      digitalEmployeesProvider
          .overrideWith(_EmptyDigitalEmployeesController.new),
      digitalEmployeeTaskHistoryProvider
          .overrideWith(_EmptyDigitalEmployeeTaskHistoryController.new),
      digitalEmployeeTaskProvider.overrideWith(
        _RecordingDigitalEmployeeTaskController.new,
      ),
      backendSshSessionsProvider.overrideWith(
        _RecordingBackendSshSessionsController.new,
      ),
      backendSshTasksProvider.overrideWith(
        _RecordingBackendSshTasksController.new,
      ),
      backendSshFileOperationsProvider.overrideWith(
        _RecordingBackendSshFileOperationsController.new,
      ),
      if (reconnectDelay != null)
        mobileRealtimeReconnectDelayProvider.overrideWithValue(reconnectDelay),
    ],
  );
}

void main() {
  test('realtime bridge dispatches document, digital employee, and ssh events',
      () async {
    _SignedInSessionController.refreshCalls = 0;
    _RecordingDocumentsController.events.clear();
    _RecordingDigitalEmployeeTaskController.events.clear();
    _RecordingBackendSshSessionsController.events.clear();
    _RecordingBackendSshTasksController.events.clear();
    _RecordingBackendSshFileOperationsController.events.clear();

    final harness = _FakeWsHarness();
    final container = _containerWith(harness.client());
    addTearDown(() async {
      // Close WS first so bridge onDispose does not race container disposal.
      await harness.close();
      await Future<void>.delayed(const Duration(milliseconds: 10));
      container.dispose();
    });

    await container.read(sessionControllerProvider.future);
    container.read(mobileRealtimeBridgeProvider);
    await Future<void>.delayed(const Duration(milliseconds: 30));
    final tabId = container.read(assistantTabsProvider).activeTabId;
    container.read(assistantTabsProvider.notifier).appendMessage(
          tabId,
          AssistantConversationMessage.assistant(
            query: 'record',
            text: 'processing',
            meetingRecording: const MeetingRecordingCardData(
              recordingId: 'meeting-1',
              title: 'Team sync',
              status: 'processing',
            ),
          ),
        );

    harness
      ..pushJson({'type': 'ready'})
      ..pushJson({
        'type': 'hello_ack',
        'ok': true,
        'binary_pty': true,
      })
      ..pushJson({
        'type': 'document_task',
        'job_id': 'export-1',
        'status': 'ready',
        'task': {'job_id': 'export-1', 'status': 'ready'},
      })
      ..pushJson({
        'type': 'digital_employee_task',
        'task_id': 'task-1',
        'status': 'done',
        'task': {'task_id': 'task-1', 'status': 'done'},
      })
      ..pushJson({
        'type': 'ssh_session',
        'session_id': 'mobssh_1',
        'status': 'connected',
        'session': {'session_id': 'mobssh_1', 'status': 'connected'},
      })
      ..pushJson({
        'type': 'ssh_task',
        'task_id': 'task-ssh-1',
        'session_id': 'mobssh_1',
        'status': 'completed',
        'task': {
          'session_id': 'mobssh_1',
          'task_id': 'task-ssh-1',
          'status': 'completed',
        },
      })
      ..pushJson({
        'type': 'ssh_file_operation',
        'operation_id': 'op-1',
        'session_id': 'mobssh_1',
        'status': 'completed',
        'operation': {
          'operation_id': 'op-1',
          'session_id': 'mobssh_1',
          'status': 'completed',
        },
      })
      ..pushJson({
        'type': 'meeting_recording',
        'recording_id': 'meeting-1',
        'recording': {
          'recording_id': 'meeting-1',
          'status': 'ready',
          'message': 'minutes ready',
          'progress': 1,
          'mode': 'minutes',
          'minutes_draft_id': 'minutes-1',
          'audio_available': true,
        },
      })
      ..pushJson({'type': 'pong'});

    await Future<void>.delayed(const Duration(milliseconds: 80));

    expect(_SignedInSessionController.refreshCalls, 1);
    expect(_RecordingDocumentsController.events, hasLength(1));
    expect(
      _RecordingDocumentsController.events.single.payload['job_id'],
      'export-1',
    );
    expect(_RecordingDigitalEmployeeTaskController.events, hasLength(1));
    expect(
      _RecordingDigitalEmployeeTaskController.events.single.payload['task_id'],
      'task-1',
    );
    expect(_RecordingBackendSshSessionsController.events, hasLength(1));
    expect(
      _RecordingBackendSshSessionsController
          .events.single.payload['session_id'],
      'mobssh_1',
    );
    expect(_RecordingBackendSshTasksController.events, hasLength(1));
    expect(
      _RecordingBackendSshTasksController.events.single.payload['task_id'],
      'task-ssh-1',
    );
    expect(_RecordingBackendSshFileOperationsController.events, hasLength(1));
    expect(
      _RecordingBackendSshFileOperationsController
          .events.single.payload['operation_id'],
      'op-1',
    );
    expect(container.read(mobileRealtimeBinaryPtyProvider), isTrue);
    expect(
      harness.sent.any(
        (m) => m is String && m.contains('"type":"hello"'),
      ),
      isTrue,
    );
    final card = container
        .read(assistantTabsProvider)
        .activeTab
        .messages
        .last
        .meetingRecording;
    expect(card?.status, 'ready');
    expect(card?.minutesDraftId, 'minutes-1');
  });

  test('MCP1 binary pty_out is parsed as ssh_session output_chunk', () {
    final sid = utf8.encode('mobssh_bin');
    final payload = utf8.encode('line\n');
    final rebuilt = ByteData(8 + sid.length + payload.length);
    final bytes = rebuilt.buffer.asUint8List();
    bytes[0] = 0x4d;
    bytes[1] = 0x43;
    bytes[2] = 0x50;
    bytes[3] = 0x31;
    bytes[4] = 2; // pty_out
    bytes[5] = 0;
    rebuilt.setUint16(6, sid.length, Endian.big);
    bytes.setRange(8, 8 + sid.length, sid);
    bytes.setRange(8 + sid.length, 8 + sid.length + payload.length, payload);

    final event = MobileRealtimeEvent.tryParse(bytes);
    expect(event, isNotNull);
    expect(event!.sshSession, isTrue);
    expect(event.payload['output_chunk'], 'line\n');
    expect(event.payload['session_id'], 'mobssh_bin');
    expect(event.binaryFrame, isTrue);
  });

  test('realtime bridge retries a rejected WebSocket upgrade safely', () async {
    final channels = <_FakeWebSocketChannel>[];
    var connectCalls = 0;
    final client = MobileRealtimeClient(
      readToken: () async => 'token',
      readHubUrl: () async => maclawOfficialServiceUrl,
      connect: (_) {
        connectCalls++;
        final channel = _FakeWebSocketChannel(
          inbound: StreamController<Object?>.broadcast(),
          sent: [],
          ready: connectCalls == 1
              ? Future<void>.error(StateError('HTTP upgrade rejected'))
              : Future<void>.value(),
        );
        channels.add(channel);
        return channel;
      },
    );
    final container = _containerWith(
      client,
      reconnectDelay: const Duration(milliseconds: 5),
    );
    addTearDown(() async {
      for (final channel in channels) {
        if (!channel.inbound.isClosed) await channel.inbound.close();
      }
      container.dispose();
    });

    await container.read(sessionControllerProvider.future);
    container.read(mobileRealtimeBridgeProvider);
    await Future<void>.delayed(const Duration(milliseconds: 40));

    expect(connectCalls, greaterThanOrEqualTo(2));
    expect(channels.first.sent, isEmpty);
    expect(
      channels[1].sent.any(
            (message) =>
                message is String && message.contains('"type":"hello"'),
          ),
      isTrue,
    );
  });
}
