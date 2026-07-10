import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:receive_sharing_intent/receive_sharing_intent.dart';

import 'mobile_shared_intent.dart';

final mobileSharedIntentProvider =
    NotifierProvider<MobileSharedIntentController, MobileSharedIntent?>(
  MobileSharedIntentController.new,
);

class MobileSharedIntentController extends Notifier<MobileSharedIntent?> {
  static const duplicateWindow = Duration(seconds: 3);

  String? _lastAcceptedSignature;
  DateTime? _lastAcceptedAt;

  @override
  MobileSharedIntent? build() => null;

  bool accept(MobileSharedIntent intent) {
    final signature = _signatureFor(intent);
    final lastAt = _lastAcceptedAt;
    if (_lastAcceptedSignature == signature &&
        lastAt != null &&
        intent.receivedAt.difference(lastAt).abs() <= duplicateWindow) {
      return false;
    }
    _lastAcceptedSignature = signature;
    _lastAcceptedAt = intent.receivedAt;
    state = intent;
    return true;
  }

  void clear(String id) {
    if (state?.id == id) {
      state = null;
    }
  }

  String _signatureFor(MobileSharedIntent intent) {
    return [
      intent.kind.name,
      intent.value.trim(),
      intent.mimeType?.trim() ?? '',
      intent.message?.trim() ?? '',
    ].join('\n');
  }
}

class SharedIntentBootstrap extends ConsumerStatefulWidget {
  final Widget child;

  const SharedIntentBootstrap({super.key, required this.child});

  @override
  ConsumerState<SharedIntentBootstrap> createState() =>
      _SharedIntentBootstrapState();
}

class _SharedIntentBootstrapState extends ConsumerState<SharedIntentBootstrap> {
  StreamSubscription<List<SharedMediaFile>>? _mediaSubscription;

  @override
  void initState() {
    super.initState();
    _mediaSubscription = ReceiveSharingIntent.instance
        .getMediaStream()
        .listen(_handleMedia, onError: _handleMediaError);
    unawaited(_loadInitialMedia());
  }

  @override
  void dispose() {
    _mediaSubscription?.cancel();
    super.dispose();
  }

  Future<void> _loadInitialMedia() async {
    try {
      final media = await ReceiveSharingIntent.instance.getInitialMedia();
      _handleMedia(media);
    } on Object {
      // A broken share provider must not prevent the phone login or assistant
      // shell from starting.
    } finally {
      try {
        ReceiveSharingIntent.instance.reset();
      } on Object {
        // Reset is best effort when the platform share provider is unavailable.
      }
    }
  }

  void _handleMediaError(Object error, StackTrace stackTrace) {
    // The share surface is optional; keep the rest of the app usable when the
    // platform stream fails or permission is denied.
  }

  void _handleMedia(List<SharedMediaFile> media) {
    final intent = MobileSharedIntent.fromPayloads(
      media.map(
        (item) => MobileSharedIntentPayload(
          value: item.path,
          typeName: item.type.name,
          mimeType: item.mimeType,
          message: item.message,
        ),
      ),
    );
    if (intent == null) return;
    ref.read(mobileSharedIntentProvider.notifier).accept(intent);
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
