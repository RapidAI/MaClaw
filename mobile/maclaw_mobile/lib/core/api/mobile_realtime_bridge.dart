import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../features/auth/session_controller.dart';
import '../../features/digital_employees/digital_employees_controller.dart';
import '../../features/documents/documents_controller.dart';
import 'mobile_realtime_client.dart';

final mobileRealtimeBridgeProvider = Provider<void>((ref) {
  final session = ref.watch(sessionControllerProvider).valueOrNull;
  if (session == null || !session.authenticated) {
    return;
  }

  var disposed = false;
  StreamSubscription<MobileRealtimeEvent>? subscription;
  Timer? retryTimer;
  late void Function() startListening;

  void scheduleReconnect() {
    if (disposed || (retryTimer?.isActive ?? false)) return;
    retryTimer = Timer(const Duration(seconds: 5), () {
      if (!disposed) {
        startListening();
      }
    });
  }

  void handleEvent(MobileRealtimeEvent event) {
    if (event.documentTask) {
      unawaited(
        ref
            .read(documentsControllerProvider.notifier)
            .applyRealtimeEvent(event),
      );
      return;
    }
    if (event.digitalEmployeeTask) {
      unawaited(
        ref
            .read(digitalEmployeeTaskProvider.notifier)
            .applyRealtimeEvent(event),
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
