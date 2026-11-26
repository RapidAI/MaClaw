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
  // Restore the previous handlers only after this turn ends. Clearing them in
  // finally runs before the platform completion callback, so a Kokoro start
  // followed by a failed segment used to drop onDone and leave the mic shut.
  final previousComplete = tts.completionHandler;
  final previousError = tts.errorHandler;
  var settled = false;
  void restore() {
    tts.setCompletionHandler(previousComplete ?? () {});
    tts.setErrorHandler(previousError ?? (_) {});
  }

  void finish() {
    if (settled) return;
    settled = true;
    restore();
    onDone();
  }

  try {
    tts.setCompletionHandler(() {
      previousComplete?.call();
      finish();
    });
    tts.setErrorHandler((message) {
      previousError?.call(message);
      finish();
    });
    await tts.stop();
    await tts.speak(text);
    return '';
  } on Object catch (e) {
    if (!settled) {
      settled = true;
      restore();
      onDone();
    }
    return 'system tts failed: $e';
  }
}
