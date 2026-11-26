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
  // The plugin has one completion slot. Restoring a previous wrapper, or
  // clearing it in finally, drops the turn that is still waiting and leaves
  // the mic shut. Only the turn that still owns the slot may put the caller's
  // handler back, and only after its own callback.
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

/// One system-TTS utterance. A later utterance shares the plugin slot, so an
/// earlier turn must not write its wrapper back over the later one.
class _SystemTtsTurn {
  _SystemTtsTurn._(this.tts);

  static final Expando<_SystemTtsTurn> _active = Expando<_SystemTtsTurn>();

  final FlutterTts tts;
  void Function()? callerComplete;
  ErrorHandler? callerError;
  bool _settled = false;
  bool _accepting = false;

  static _SystemTtsTurn claim(FlutterTts tts, void Function() onDone) {
    final previous = _active[tts];
    final turn = _SystemTtsTurn._(tts);
    if (previous != null) {
      turn.callerComplete = previous.callerComplete;
      turn.callerError = previous.callerError;
      previous._settled = true;
    } else {
      turn.callerComplete = tts.completionHandler;
      turn.callerError = tts.errorHandler;
    }
    _active[tts] = turn;
    tts.setCompletionHandler(() {
      if (!_owns(tts, turn)) return;
      turn._complete(onDone);
    });
    tts.setErrorHandler((message) {
      if (!_owns(tts, turn)) return;
      turn.callerError?.call(message);
      turn._complete(onDone);
    });
    return turn;
  }

  static bool _owns(FlutterTts tts, _SystemTtsTurn turn) {
    return identical(_active[tts], turn);
  }

  void accept() {
    _accepting = true;
  }

  /// End this turn after speak() throws. A later turn already owns the slot,
  /// so this one must not restore or fire onDone again.
  bool fail() {
    if (_settled || !_owns(tts, this)) return false;
    _settled = true;
    _release();
    return true;
  }

  void _complete(void Function() onDone) {
    if (_settled || !_accepting || !_owns(tts, this)) return;
    _settled = true;
    final caller = callerComplete;
    _release();
    if (caller != null) caller();
    onDone();
  }

  void _release() {
    if (!identical(_active[tts], this)) return;
    _active[tts] = null;
    tts.setCompletionHandler(callerComplete ?? () {});
    tts.setErrorHandler(callerError ?? (_) {});
  }
}
