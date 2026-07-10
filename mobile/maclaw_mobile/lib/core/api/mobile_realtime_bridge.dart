import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../features/auth/session_controller.dart';
import '../../features/digital_employees/digital_employees_controller.dart';
import '../../features/documents/documents_controller.dart';
import '../../features/servers/servers_controller.dart';
import 'mobile_realtime_client.dart';

final mobileRealtimeReconnectDelayProvider = Provider<Duration>(
  (ref) => const Duration(seconds: 5),
);

final mobileRealtimeBridgeProvider = Provider<void>((ref) {
  final session = ref.watch(sessionControllerProvider).valueOrNull;
  if (session == null || !session.authenticated) {
    return;
  }

  var disposed = false;
  StreamSubscription<MobileRealtimeEvent>? subscription;
  Timer? retryTimer;
  var refreshOnReady = true;
  late void Function() startListening;

  void scheduleReconnect() {
    if (disposed || (retryTimer?.isActive ?? false)) return;
    retryTimer = Timer(ref.read(mobileRealtimeReconnectDelayProvider), () {
      if (!disposed) {
        startListening();
      }
    });
  }

  Future<void> applyEventSafely(Future<void> Function() apply) async {
    try {
      await apply();
    } on Object {
      // A malformed or stale task update must not terminate the realtime
      // subscription; the next event or reconnect can refresh the state.
    }
  }

  void handleEvent(MobileRealtimeEvent event) {
    if (event.ready && refreshOnReady) {
      refreshOnReady = false;
      unawaited(_refreshHubStateAfterRealtimeReconnect(ref));
    }
    if (event.documentTask) {
      unawaited(
        applyEventSafely(
          () => ref
              .read(documentsControllerProvider.notifier)
              .applyRealtimeEvent(event),
        ),
      );
      return;
    }
    if (event.digitalEmployeeTask) {
      unawaited(
        applyEventSafely(
          () => ref
              .read(digitalEmployeeTaskProvider.notifier)
              .applyRealtimeEvent(event),
        ),
      );
      return;
    }
    if (event.sshSession) {
      unawaited(
        applyEventSafely(
          () => ref
              .read(backendSshSessionsProvider.notifier)
              .applyRealtimeEvent(event),
        ),
      );
      return;
    }
    if (event.sshTask) {
      unawaited(
        applyEventSafely(
          () => ref
              .read(backendSshTasksProvider.notifier)
              .applyRealtimeEvent(event),
        ),
      );
      return;
    }
    if (event.sshFileOperation) {
      unawaited(
        applyEventSafely(
          () => ref
              .read(backendSshFileOperationsProvider.notifier)
              .applyRealtimeEvent(event),
        ),
      );
    }
  }

  void handleDisconnect() {
    unawaited(subscription?.cancel());
    subscription = null;
    scheduleReconnect();
  }

  startListening = () {
    retryTimer?.cancel();
    retryTimer = null;
    refreshOnReady = true;
    unawaited(subscription?.cancel());
    subscription = ref.read(mobileRealtimeClientProvider).events().listen(
          handleEvent,
          onError: (_) => handleDisconnect(),
          onDone: handleDisconnect,
          cancelOnError: false,
        );
  };

  startListening();

  ref.onDispose(() {
    disposed = true;
    retryTimer?.cancel();
    unawaited(subscription?.cancel());
  });
});

Future<void> _refreshHubStateAfterRealtimeReconnect(Ref ref) async {
  try {
    await ref.read(sessionControllerProvider.notifier).refreshBootstrap();
    ref.invalidate(documentsControllerProvider);
    ref.invalidate(digitalEmployeesProvider);
    ref.invalidate(backendSshSessionsProvider);
    ref.invalidate(backendSshTasksProvider);
    ref.invalidate(backendSshFileOperationsProvider);
    ref.invalidate(digitalEmployeeTaskHistoryProvider);
  } catch (_) {
    // A reconnect can race with Hub recovery; the next reconnect or lifecycle
    // recovery will retry the snapshot refresh.
  }
}
