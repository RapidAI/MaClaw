import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../../features/auth/session_controller.dart';
import '../../features/digital_employees/digital_employees_controller.dart';
import '../../features/documents/documents_controller.dart';
import '../../features/servers/servers_controller.dart';
import '../../features/tasks/mobile_jobs_provider.dart';
import 'mobile_realtime_client.dart';

final mobileRealtimeReconnectDelayProvider = Provider<Duration>(
  (ref) => const Duration(seconds: 5),
);

final mobileRealtimeBridgeProvider = Provider<void>((ref) {
  final session = ref.watch(sessionControllerProvider).valueOrNull;
  if (session == null || !session.authenticated) {
    ref.read(mobileRealtimeSenderProvider.notifier).state = null;
    return;
  }

  var disposed = false;
  StreamSubscription<dynamic>? subscription;
  Timer? retryTimer;
  var refreshOnReady = true;
  late void Function() startListening;
  WebSocketChannel? channel;

  void clearSender() {
    try {
      ref.read(mobileRealtimeSenderProvider.notifier).state = null;
      ref.read(mobileRealtimeBinaryPtyProvider.notifier).state = false;
    } on Object {
      // ProviderContainer may already be disposing (tests / logout race).
    }
  }

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
    if (event.helloAck) {
      final ok = event.payload['binary_pty'] == true ||
          event.payload['ok'] == true;
      if (ok) {
        ref.read(mobileRealtimeBinaryPtyProvider.notifier).state = true;
      }
      return;
    }
    if (event.ptyAck) {
      // Output already streamed via ssh_session chunks; ack is for diagnostics.
      if (event.payload['binary'] == true || event.binaryFrame) {
        ref.read(mobileRealtimeBinaryPtyProvider.notifier).state = true;
      }
      return;
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
      ref.invalidate(mobileJobsProvider);
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
      // Jobs list refreshed inside tasks controller as well.
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
      return;
    }
    if (event.assistantJob) {
      // Unified 后台 jobs list + assistant long-task handoff refresh.
      ref.invalidate(mobileJobsProvider);
    }
  }

  void handleDisconnect() {
    clearSender();
    unawaited(subscription?.cancel());
    subscription = null;
    try {
      channel?.sink.close();
    } on Object {
      // best effort
    }
    channel = null;
    scheduleReconnect();
  }

  startListening = () {
    retryTimer?.cancel();
    retryTimer = null;
    refreshOnReady = true;
    unawaited(subscription?.cancel());
    clearSender();
    unawaited(() async {
      try {
        final client = ref.read(mobileRealtimeClientProvider);
        final ch = await client.connect();
        if (disposed) {
          await ch.sink.close();
          return;
        }
        channel = ch;
        ref.read(mobileRealtimeSenderProvider.notifier).state = (encoded) {
          try {
            ch.sink.add(encoded);
          } on Object {
            // Drop send failures; next reconnect restores the sender.
          }
        };
        // Advertise MCP1 binary PTY capability.
        try {
          ch.sink.add(client.encodeHello());
        } on Object {
          // Hello is best-effort; binary frames still auto-enable server-side.
        }
        subscription = ch.stream.listen(
          (raw) {
            final event = MobileRealtimeEvent.tryParse(raw);
            if (event != null) {
              handleEvent(event);
            }
          },
          onError: (_) => handleDisconnect(),
          onDone: handleDisconnect,
          cancelOnError: false,
        );
      } on Object {
        handleDisconnect();
      }
    }());
  };

  startListening();

  ref.onDispose(() {
    disposed = true;
    retryTimer?.cancel();
    clearSender();
    unawaited(subscription?.cancel());
    try {
      channel?.sink.close();
    } on Object {
      // ignore
    }
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
    ref.invalidate(mobileJobsProvider);
  } catch (_) {
    // A reconnect can race with Hub recovery; the next reconnect or lifecycle
    // recovery will retry the snapshot refresh.
  }
}
