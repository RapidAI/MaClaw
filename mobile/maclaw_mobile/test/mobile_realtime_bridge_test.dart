import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_bridge.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_client.dart';
import 'package:maclaw_mobile/core/api/official_service.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employees_controller.dart';
import 'package:maclaw_mobile/features/documents/documents_controller.dart';
import 'package:maclaw_mobile/features/servers/servers_controller.dart';

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

class _FakeRealtimeClient extends MobileRealtimeClient {
  final Stream<MobileRealtimeEvent> stream;

  _FakeRealtimeClient(this.stream);

  @override
  Stream<MobileRealtimeEvent> events({
    String path = maclawOfficialRealtimePath,
  }) {
    return stream;
  }
}

class _ReconnectableRealtimeClient extends MobileRealtimeClient {
  final List<Stream<MobileRealtimeEvent>> streams;
  int calls = 0;

  _ReconnectableRealtimeClient(this.streams);

  @override
  Stream<MobileRealtimeEvent> events({
    String path = maclawOfficialRealtimePath,
  }) {
    final index = calls++;
    return streams[index < streams.length ? index : streams.length - 1];
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

void main() {
  test('realtime bridge dispatches document, digital employee, and ssh events',
      () async {
    _SignedInSessionController.refreshCalls = 0;
    _RecordingDocumentsController.events.clear();
    _RecordingDigitalEmployeeTaskController.events.clear();
    _RecordingBackendSshSessionsController.events.clear();
    _RecordingBackendSshTasksController.events.clear();
    _RecordingBackendSshFileOperationsController.events.clear();
    final stream = StreamController<MobileRealtimeEvent>();
    final container = ProviderContainer(
      overrides: [
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        mobileRealtimeClientProvider.overrideWithValue(
          _FakeRealtimeClient(stream.stream),
        ),
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
      ],
    );
    addTearDown(() async {
      await stream.close();
      container.dispose();
    });

    await container.read(sessionControllerProvider.future);
    container.read(mobileRealtimeBridgeProvider);

    stream
      ..add(
        const MobileRealtimeEvent(type: 'ready'),
      )
      ..add(
        const MobileRealtimeEvent(
          type: 'document_task',
          payload: {'job_id': 'export-1', 'status': 'ready'},
        ),
      )
      ..add(
        const MobileRealtimeEvent(
          type: 'digital_employee_task',
          payload: {'task_id': 'task-1', 'status': 'done'},
        ),
      )
      ..add(
        const MobileRealtimeEvent(
          type: 'ssh_session',
          payload: {'session_id': 'mobssh_1', 'status': 'connected'},
        ),
      )
      ..add(
        const MobileRealtimeEvent(
          type: 'ssh_task',
          payload: {
            'session_id': 'mobssh_1',
            'task_id': 'task-ssh-1',
            'status': 'completed',
          },
        ),
      )
      ..add(
        const MobileRealtimeEvent(
          type: 'ssh_file_operation',
          payload: {
            'operation_id': 'op-1',
            'session_id': 'mobssh_1',
            'status': 'completed',
          },
        ),
      )
      ..add(const MobileRealtimeEvent(type: 'pong'));
    await Future<void>.delayed(Duration.zero);

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
  });

  test('realtime bridge reconnects after stream completion and refreshes state',
      () async {
    _SignedInSessionController.refreshCalls = 0;
    final first = StreamController<MobileRealtimeEvent>();
    final second = StreamController<MobileRealtimeEvent>();
    final client = _ReconnectableRealtimeClient([
      first.stream,
      second.stream,
    ]);
    final container = ProviderContainer(
      overrides: [
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        mobileRealtimeClientProvider.overrideWithValue(client),
        mobileRealtimeReconnectDelayProvider.overrideWithValue(Duration.zero),
        documentsControllerProvider.overrideWith(
          _RecordingDocumentsController.new,
        ),
        digitalEmployeesProvider
            .overrideWith(_EmptyDigitalEmployeesController.new),
        digitalEmployeeTaskHistoryProvider
            .overrideWith(_EmptyDigitalEmployeeTaskHistoryController.new),
      ],
    );
    addTearDown(() async {
      await first.close();
      await second.close();
      container.dispose();
    });

    await container.read(sessionControllerProvider.future);
    container.read(mobileRealtimeBridgeProvider);
    first.add(const MobileRealtimeEvent(type: 'ready'));
    await Future<void>.delayed(Duration.zero);
    await first.close();
    await Future<void>.delayed(const Duration(milliseconds: 10));
    second.add(const MobileRealtimeEvent(type: 'ready'));
    await Future<void>.delayed(Duration.zero);

    expect(client.calls, 2);
    expect(_SignedInSessionController.refreshCalls, 2);
  });
}
