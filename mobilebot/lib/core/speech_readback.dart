/// One place that turns an assistant reply into speech.
///
/// Both the assistant page and the Bot voice chat used their own copy of
/// "Kokoro, else flutter_tts", which spoke the whole reply as one clip: the
/// first sound waited for the entire synthesis, and a long answer cost
/// minutes. This helper summarizes the reply (see `plainTextForSpeech`) and
/// feeds it to the engine's segmented queue, which synthesizes one segment
/// ahead while the previous one plays.
library;

import 'package:flutter_tts/flutter_tts.dart';

import 'speech_text.dart';
import 'voice_engine.dart';

/// Speaks [markdown] for a reply.
///
/// [onDone] fires once when the readback ends — spoken out, failed over to
/// system TTS, or stopped. [systemTts] provides the platform TTS used when
/// Kokoro is not the selected engine or fails to load.
///
/// Returns null when the reply was spoken by Kokoro, otherwise the reason
/// Kokoro did not (empty when system TTS was used as configured).
Future<String?> speakReplyReadback(
  VoiceEngine engine,
  String markdown, {
  required void Function() onDone,
  FlutterTts? Function()? systemTts,
}) async {
  final text = plainTextForSpeech(markdown);
  if (text.trim().isEmpty) {
    onDone();
    return '';
  }
  if (await engine.useKokoro) {
    final err = await engine.speakSegments(
      splitSpeechSegments(text),
      onEvent: (_, _) {},
    );
    if (err == null) {
      onDone();
      return null;
    }
  }
  final tts = systemTts?.call();
  if (tts == null) {
    onDone();
    return 'system tts unavailable';
  }
  // flutter_tts has one MethodChannel. A later FlutterTts() replaces the
  // handler, so ownership cannot be an Expando on one instance. Only the
  // turn that still receives channel events may restore the caller's
  // handler, and only after its own callback. A preempted turn still ends.
  noteSystemTtsReceiver(tts);
  final turn = _SystemTtsTurn.claim(tts, onDone);
  try {
    await tts.stop();
    // stop() can emit the previous utterance's completion. Accepting before
    // speak() would finish this turn before it has spoken.
    turn.accept();
    await tts.speak(text);
    return '';
  } on Object catch (e) {
    if (!turn.fail()) return '';
    onDone();
    return 'system tts failed: $e';
  }
}

/// Register [tts] as the process-wide flutter_tts receiver.
///
/// `FlutterTts()` replaces the plugin's only method-channel handler. Call
/// this immediately after construction, and again after replacing the
/// completion or error handler, so a turn that can no longer receive events
/// still ends.
void noteSystemTtsReceiver(FlutterTts tts) {
  _SystemTtsTurn.noteReceiver(tts);
}

/// One system-TTS utterance. Every [FlutterTts] shares one platform channel,
/// so a later instance must not leave the earlier turn waiting on a handler
/// that will never run.
class _OuterHandlers {
  _OuterHandlers(this.complete, this.error);

  final void Function()? complete;
  final ErrorHandler? error;
}

class _SystemTtsTurn {
  _SystemTtsTurn._(this.tts);

  static _SystemTtsTurn? _active;
  static FlutterTts? _receiver;
  static final Expando<void Function()> _wrappedComplete =
      Expando<void Function()>();
  static final Expando<ErrorHandler> _wrappedError = Expando<ErrorHandler>();
  static final Expando<_OuterHandlers> _outer = Expando<_OuterHandlers>();

  final FlutterTts tts;
  void Function()? callerComplete;
  ErrorHandler? callerError;
  void Function()? _onDone;
  bool _settled = false;
  bool _accepting = false;

  static _SystemTtsTurn claim(FlutterTts tts, void Function() onDone) {
    final previous = _active;
    final turn = _SystemTtsTurn._(tts).._onDone = onDone;
    if (previous != null && !identical(previous.tts, tts)) {
      // Different page, different instance. Its completion handler is not
      // this page's caller, and writing it onto the new receiver would drop
      // the handler the new page installed in its constructor.
      previous._preempt();
    } else if (previous != null) {
      turn.callerComplete = previous.callerComplete;
      turn.callerError = previous.callerError;
      previous._settled = true;
      previous._onDone = null;
    } else {
      turn.callerComplete = _unwrapComplete(tts);
      turn.callerError = _unwrapError(tts);
    }
    _active = turn;
    _bind(tts);
    return turn;
  }

  /// [FlutterTts()] replaces the process-wide channel handler. Call this
  /// immediately after construction, and again after any direct
  /// [FlutterTts.setCompletionHandler], so the turn that can no longer
  /// receive events is released.
  static void noteReceiver(FlutterTts tts) {
    final previous = _active;
    if (previous != null && !identical(previous.tts, tts)) {
      previous._preempt();
      if (identical(_active, previous)) _active = null;
    }
    _bind(tts);
  }

  /// Handlers live on the instance the constructor just installed. Re-wrap
  /// when a caller replaced that wrapper, and keep the wrapper on the
  /// receiver that will actually see channel events.
  static void _bind(FlutterTts tts) {
    _receiver = tts;
    final installedComplete = _wrappedComplete[tts];
    final installedError = _wrappedError[tts];
    // identical(null, null) is true. A fresh instance has no wrapper yet.
    if (installedComplete != null &&
        installedError != null &&
        identical(tts.completionHandler, installedComplete) &&
        identical(tts.errorHandler, installedError)) {
      return;
    }
    final outer = _OuterHandlers(tts.completionHandler, tts.errorHandler);
    _outer[tts] = outer;
    void wrappedComplete() {
      if (!_dispatchComplete(tts)) outer.complete?.call();
    }

    void wrappedError(dynamic message) {
      if (!_dispatchError(tts, message)) outer.error?.call(message);
    }

    _wrappedComplete[tts] = wrappedComplete;
    _wrappedError[tts] = wrappedError;
    tts.setCompletionHandler(wrappedComplete);
    tts.setErrorHandler(wrappedError);
  }

  static void Function()? _unwrapComplete(FlutterTts tts) {
    if (identical(tts.completionHandler, _wrappedComplete[tts])) {
      return _outer[tts]?.complete;
    }
    return tts.completionHandler;
  }

  static ErrorHandler? _unwrapError(FlutterTts tts) {
    if (identical(tts.errorHandler, _wrappedError[tts])) {
      return _outer[tts]?.error;
    }
    return tts.errorHandler;
  }

  static bool _dispatchComplete(FlutterTts tts) {
    final turn = _active;
    if (turn == null || !identical(_receiver, tts) || !identical(turn.tts, tts)) {
      return false;
    }
    turn._complete();
    return true;
  }

  static bool _dispatchError(FlutterTts tts, dynamic message) {
    final turn = _active;
    if (turn == null || !identical(_receiver, tts) || !identical(turn.tts, tts)) {
      return false;
    }
    turn.callerError?.call(message);
    turn._complete();
    return true;
  }

  void accept() {
    _accepting = true;
  }

  /// End this turn after speak() throws. A later turn already owns the slot,
  /// so this one must not restore or fire onDone again.
  bool fail() {
    if (_settled || !identical(_active, this)) return false;
    _settled = true;
    _release();
    return true;
  }

  void _preempt() {
    if (_settled) return;
    _settled = true;
    final done = _onDone;
    _onDone = null;
    if (done != null) done();
  }

  void _complete() {
    if (_settled || !_accepting || !identical(_active, this)) return;
    _settled = true;
    final caller = callerComplete;
    final done = _onDone;
    _release();
    if (caller != null) caller();
    if (done != null) done();
  }

  void _release() {
    if (!identical(_active, this)) return;
    _active = null;
    final receiver = _receiver;
    if (receiver == null || !identical(receiver, tts)) return;
    receiver.setCompletionHandler(callerComplete ?? () {});
    receiver.setErrorHandler(callerError ?? (_) {});
  }
}
