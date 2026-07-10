import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/network/mobile_network_recovery.dart';
import 'package:maclaw_mobile/core/network/mobile_network_status.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_bridge.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employees_controller.dart';
import 'package:maclaw_mobile/features/servers/server_profile.dart';
import 'package:maclaw_mobile/features/servers/servers_controller.dart';

class _EmptyServerProfilesController extends ServerProfilesController {
  @override
  Future<List<ServerProfile>> build() async => const [];
}

class _EmptyDigitalEmployeesController extends DigitalEmployeesController {
  @override
  Future<List<DigitalEmployee>> build() async => const [];
}

class _EmptySshSessionsController extends BackendSshSessionsController {
  @override
  Future<Map<String, MobileBackendSSHSession>> build() async => const {};
}

class _EmptySshTasksController extends BackendSshTasksController {
  @override
  Future<Map<String, List<MobileBackendSSHTask>>> build() async => const {};
}

class _EmptyFileOperationsController
    extends BackendSshFileOperationsController {
  @override
  Future<Map<String, List<MobileBackendSSHFileOperation>>> build() async =>
      const {};
}

class _EmptyTaskHistoryController extends DigitalEmployeeTaskHistoryController {
  @override
  Future<List<MobileDigitalEmployeeTask>> build() async => const [];
}

class _RecordingSessionController extends SessionController {
  static int refreshCalls = 0;

  @override
  Future<SessionState> build() async => SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: MobileBootstrap.fromJson({
          'user': {'user_id': 'u1', 'tenant_id': 'tenant-a'},
          'services': {},
          'features': {},
          'limits': {},
        }),
      );

  @override
  Future<void> refreshBootstrap() async {
    refreshCalls++;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  List<Override> cacheOverrides() => [
        mobileRealtimeBridgeProvider.overrideWith((ref) {}),
        serverProfilesProvider.overrideWith(_EmptyServerProfilesController.new),
        digitalEmployeesProvider
            .overrideWith(_EmptyDigitalEmployeesController.new),
        backendSshSessionsProvider
            .overrideWith(_EmptySshSessionsController.new),
        backendSshTasksProvider.overrideWith(_EmptySshTasksController.new),
        backendSshFileOperationsProvider
            .overrideWith(_EmptyFileOperationsController.new),
        digitalEmployeeTaskHistoryProvider
            .overrideWith(_EmptyTaskHistoryController.new),
      ];

  test('network recovery refreshes the signed-in Hub session once', () async {
    _RecordingSessionController.refreshCalls = 0;
    final statuses = StreamController<MobileNetworkSnapshot>();
    final container = ProviderContainer(
      overrides: [
        sessionControllerProvider.overrideWith(_RecordingSessionController.new),
        mobileNetworkStatusProvider.overrideWith(
          (ref) => statuses.stream,
        ),
        ...cacheOverrides(),
      ],
    );
    addTearDown(() async {
      await statuses.close();
      container.dispose();
    });

    await container.read(sessionControllerProvider.future);
    container.read(mobileNetworkRecoveryProvider);
    statuses
      ..add(
        MobileNetworkSnapshot(
          quality: MobileNetworkQuality.offline,
          message: 'offline',
          checkedAt: DateTime.utc(2026, 7, 10),
        ),
      )
      ..add(
        MobileNetworkSnapshot(
          quality: MobileNetworkQuality.restored,
          message: 'restored',
          checkedAt: DateTime.utc(2026, 7, 10),
        ),
      );
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(_RecordingSessionController.refreshCalls, 1);
  });

  testWidgets('app resume refreshes the signed-in Hub session', (tester) async {
    _RecordingSessionController.refreshCalls = 0;
    final container = ProviderContainer(
      overrides: [
        sessionControllerProvider.overrideWith(_RecordingSessionController.new),
        ...cacheOverrides(),
      ],
    );
    addTearDown(container.dispose);

    await container.read(sessionControllerProvider.future);
    container.read(mobileAppLifecycleRecoveryProvider);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await tester.pumpAndSettle();

    expect(_RecordingSessionController.refreshCalls, 1);
  });
}
