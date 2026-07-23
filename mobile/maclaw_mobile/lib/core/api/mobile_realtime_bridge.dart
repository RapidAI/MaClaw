import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../../features/assistant/assistant_controller.dart';
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
  var connectionGeneration = 0;
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

  Future<void> closeChannelSafely(WebSocketChannel? target) async {
    if (target == null) return;
    try {
      await target.sink.close();
    } on Object {
      // A failed handshake or an already-closed socket must not surface as an
      // unhandled asynchronous error while the bridge is recovering.
    }
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
      final ok =
          event.payload['binary_pty'] == true || event.payload['ok'] == true;
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
    if (event.meetingRecording) {
      final recording = event.payload['recording'];
      final details = recording is Map
          ? Map<String, dynamic>.from(recording)
          : event.payload;
      final recordingId =
          (details['recording_id'] ?? event.taskId).toString().trim();
      if (recordingId.isEmpty) return;
      final audioAvailable = details['audio_available'];
      ref.read(assistantTabsProvider.notifier).updateMeetingRecordingById(
            recordingId,
            (current) => current.copyWith(
              status: details['status']?.toString() ?? current.status,
              message: details['message']?.toString() ?? current.message,
              failureCode:
                  details['failure_code']?.toString() ?? current.failureCode,
              progress:
                  (details['progress'] as num?)?.toDouble() ?? current.progress,
              transcriptDraftId: details['transcript_draft_id']?.toString() ??
                  current.transcriptDraftId,
              minutesDraftId: details['minutes_draft_id']?.toString() ??
                  current.minutesDraftId,
              processMode: details['mode']?.toString() ?? current.processMode,
              audioAvailable: audioAvailable is bool
                  ? audioAvailable
                  : current.audioAvailable,
            ),
          );
      ref.invalidate(mobileJobsProvider);
      return;
    }
    if (event.assistantJob) {
      // Unified 后台 jobs list + assistant long-task handoff refresh.
      ref.invalidate(mobileJobsProvider);
    }
  }

  void handleDisconnect(int generation, WebSocketChannel? disconnectedChannel) {
    // A cancelled/slow handshake can complete after a newer connection is
    // already active. It must never clear the new sender or schedule a second
    // reconnect loop.
    if (disposed || generation != connectionGeneration) return;
    if (channel != null &&
        disconnectedChannel != null &&
        !identical(channel, disconnectedChannel)) {
      return;
    }
    clearSender();
    final activeSubscription = subscription;
    subscription = null;
    unawaited(activeSubscription?.cancel() ?? Future<void>.value());
    final activeChannel = channel ?? disconnectedChannel;
    channel = null;
    unawaited(closeChannelSafely(activeChannel));
    scheduleReconnect();
  }

  startListening = () {
    if (disposed) return;
    retryTimer?.cancel();
    retryTimer = null;
    refreshOnReady = true;
    final generation = ++connectionGeneration;
    final previousSubscription = subscription;
    clearSender();
    subscription = null;
    unawaited(previousSubscription?.cancel() ?? Future<void>.value());
    final previousChannel = channel;
    channel = null;
    unawaited(closeChannelSafely(previousChannel));
    unawaited(() async {
      WebSocketChannel? pendingChannel;
      var disconnected = false;

      void disconnectThisAttempt() {
        if (disconnected) return;
        disconnected = true;
        handleDisconnect(generation, pendingChannel);
      }

      try {
        final client = ref.read(mobileRealtimeClientProvider);
        final ch = await client.connect();
        pendingChannel = ch;
        // WebSocketChannel.connect returns before the HTTP upgrade completes.
        // Awaiting `ready` here consumes a rejected upgrade (for example a Hub
        // that does not expose the realtime route) and prevents it becoming an
        // uncaught asynchronous exception in release builds.
        await ch.ready;
        if (disposed || generation != connectionGeneration) {
          await closeChannelSafely(ch);
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
          onError: (_, __) => disconnectThisAttempt(),
          onDone: disconnectThisAttempt,
          cancelOnError: false,
        );
      } on Object {
        disconnectThisAttempt();
      }
    }());
  };

  startListening();

  ref.onDispose(() {
    disposed = true;
    connectionGeneration++;
    retryTimer?.cancel();
    clearSender();
    unawaited(subscription?.cancel());
    unawaited(closeChannelSafely(channel));
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
