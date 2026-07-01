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
  @override
  MobileSharedIntent? build() => null;

  void accept(MobileSharedIntent intent) {
    state = intent;
  }

  void clear(String id) {
    if (state?.id == id) {
      state = null;
    }
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
    _mediaSubscription =
        ReceiveSharingIntent.instance.getMediaStream().listen(_handleMedia);
    unawaited(_loadInitialMedia());
  }

  @override
  void dispose() {
    _mediaSubscription?.cancel();
    super.dispose();
  }

  Future<void> _loadInitialMedia() async {
    final media = await ReceiveSharingIntent.instance.getInitialMedia();
    _handleMedia(media);
    ReceiveSharingIntent.instance.reset();
  }

  void _handleMedia(List<SharedMediaFile> media) {
    if (media.isEmpty) return;
    final first = media.first;
    final value = first.path.trim();
    if (value.isEmpty && (first.message ?? '').trim().isEmpty) return;
    ref.read(mobileSharedIntentProvider.notifier).accept(
          MobileSharedIntent.fromMedia(
            value: value,
            typeName: first.type.name,
            mimeType: first.mimeType,
            message: first.message,
          ),
        );
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
