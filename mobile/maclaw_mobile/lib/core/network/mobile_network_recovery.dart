import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../features/auth/session_controller.dart';
import '../../features/digital_employees/digital_employees_controller.dart';
import '../../features/servers/servers_controller.dart';
import '../../features/meeting_recording/meeting_recording_upload_queue.dart';
import '../api/mobile_realtime_bridge.dart';
import '../notifications/mobile_push_sync.dart';
import 'mobile_network_status.dart';

final mobileNetworkRecoveryProvider = Provider<void>((ref) {
  final session = ref.watch(sessionControllerProvider).valueOrNull;
  if (session?.authenticated != true) return;

  // First authenticated build and subsequent recovery share the same durable
  // queue. A recording completed while offline therefore resumes without a
  // user having to reopen its conversation.
  unawaited(MeetingRecordingUploadQueue().resumePending());
  var recovering = false;
  ref.listen<AsyncValue<MobileNetworkSnapshot>>(
    mobileNetworkStatusProvider,
    (previous, next) {
      if (next.valueOrNull?.restored != true || recovering) return;
      recovering = true;
      unawaited(
        _recoverMobileHubState(ref).whenComplete(() {
          recovering = false;
        }),
      );
    },
  );
});

final mobileAppLifecycleRecoveryProvider = Provider<void>((ref) {
  final listener = AppLifecycleListener(
    onStateChange: (state) {
      if (state == AppLifecycleState.resumed) {
        unawaited(_recoverMobileHubState(ref));
      }
    },
  );
  ref.onDispose(listener.dispose);
});

Future<void> _recoverMobileHubState(Ref ref) async {
  try {
    final session = ref.read(sessionControllerProvider).valueOrNull;
    if (session?.authenticated != true) return;
    await ref.read(sessionControllerProvider.notifier).refreshBootstrap();
    ref.invalidate(serverProfilesProvider);
    ref.invalidate(digitalEmployeesProvider);
    ref.invalidate(backendSshSessionsProvider);
    ref.invalidate(backendSshTasksProvider);
    ref.invalidate(backendSshFileOperationsProvider);
    ref.invalidate(digitalEmployeeTaskHistoryProvider);
    ref.invalidate(mobileRealtimeBridgeProvider);
    // Catch completions that finished while the app/process was offline.
    unawaited(registerMobilePushDeviceFromRef(ref));
    unawaited(syncMobilePushPendingFromRef(ref));
    // Meeting audio is persisted before upload; retry it after network/app
    // recovery so an interrupted transfer never loses a completed recording.
    unawaited(MeetingRecordingUploadQueue().resumePending());
  } catch (_) {
    // The normal screen-level retry paths remain available if the Hub comes
    // back between the lifecycle event and the refresh request.
  }
}
