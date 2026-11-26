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
  var spokenByKokoro = false;
  if (await engine.useKokoro) {
    final err = await engine.speakSegments(
      splitSpeechSegments(text),
      onEvent: (event, _) {
        if (event == 'start') spokenByKokoro = true;
      },
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
  try {
    final previous = tts.completionHandler;
    tts.setCompletionHandler(() {
      if (previous != null) previous();
      onDone();
    });
    tts.setErrorHandler((_) => onDone());
    await tts.stop();
    await tts.speak(text);
    return '';
  } on Object catch (e) {
    onDone();
    return 'system tts failed: $e';
  } finally {
    // Do not leave our handler attached after this turn.
    if (spokenByKokoro) tts.setCompletionHandler(() {});
  }
}
