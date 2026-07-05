import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_bridge.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_client.dart';
import 'package:maclaw_mobile/core/api/official_service.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employees_controller.dart';
import 'package:maclaw_mobile/features/documents/documents_controller.dart';

class _SignedInSessionController extends SessionController {
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

class _RecordingDocumentsController extends DocumentsController {
  static final events = <MobileRealtimeEvent>[];

  @override
  Future<DocumentsState> build() async => const DocumentsState();

  @override
  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    events.add(event);
  }
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

void main() {
  test('realtime bridge dispatches document and digital employee events',
      () async {
    _RecordingDocumentsController.events.clear();
    _RecordingDigitalEmployeeTaskController.events.clear();
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
        digitalEmployeeTaskProvider.overrideWith(
          _RecordingDigitalEmployeeTaskController.new,
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
      ..add(const MobileRealtimeEvent(type: 'pong'));
    await Future<void>.delayed(Duration.zero);

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
  });
}
