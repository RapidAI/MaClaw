/// Pure ASR engine choice shared by assistant and bot dictation.
///
/// System recognition is tried first unless the user already selected the
/// local engine and its model is ready. A failed system start falls back to
/// local only once, and only when the local model is ready. Otherwise the
/// caller should toast and retry later instead of opening a second mic.
library;

import 'voice_engine.dart';

/// Shared listen outcome so assistant and bot pages cannot drift into a
/// second mic. `retryLater` exists because a failed system start with no
/// ready local model must toast, not open another recognizer.
enum AsrFallbackChoice { system, local, retryLater }

/// `listen()` returned, but the platform never sent a result, status, error,
/// or sound-level callback. 2.5s is long enough for the first `listening`
/// status on a healthy recognizer, and short enough that a dead session
/// does not leave the composer stuck at 聆听中.
const Duration kListenSilentWatchdog = Duration(milliseconds: 2500);

/// Upper bound for one system-recognition turn. speech_to_text 7.4 treats
/// this as a maximum the plugin will enforce; some OEMs stop sooner anyway.
/// Leaving it null lets those OEMs apply a few-second platform default.
const Duration kSystemListenFor = Duration(seconds: 30);

/// Silence after speech before the recognizer should end the turn. Android's
/// plugin only writes EXTRA_SPEECH_INPUT_COMPLETE_SILENCE_LENGTH_MILLIS when
/// this is set; null falls back to the OEM window (often 1–3s, and the
/// plugin's own end-of-speech timer defaults to 1s). It cannot be lengthened
/// past what the recognizer itself allows.
const Duration kSystemPauseFor = Duration(milliseconds: 2500);

/// PCM16 RMS at or above this is treated as a full bar. Matches the
/// loudness gate in the capture loop (`rms > 400` counts as speech).
const double kPcmRmsFullScale = 4000;

/// Maps a PCM16 RMS (or any non-negative level) onto 0..1 for the composer
/// meter. Null stays null so a system-recognition path can animate without
/// pretending it has a real level.
double? normalizeListenLevel(
  double? rms, {
  double fullScale = kPcmRmsFullScale,
}) {
  if (rms == null) return null;
  if (rms.isNaN || rms <= 0 || fullScale <= 0) return 0;
  final n = rms / fullScale;
  if (n >= 1) return 1;
  return n;
}

/// Minimum gap between composer-meter writes. PCM arrives in ~32ms chunks;
/// writing every chunk rebuilds the meter at 30Hz. 70ms is inside the
/// 60–80ms window: the bar still tracks speech, but most chunks are skipped.
const Duration kListenLevelThrottle = Duration(milliseconds: 70);

/// How long a trailing level stays visible after the capture loop ends.
/// One frame is enough for the meter to paint; holding the full throttle
/// window would delay stop and the transcript.
const Duration kListenLevelFlushHold = Duration(milliseconds: 16);

/// Whether this chunk should publish a meter value.
///
/// [now] is the chunk time, [lastPublish] is the previous write (null on the
/// first chunk). A gap shorter than [minGap] is skipped so the notifier does
/// not rebuild the composer every 32ms.
///
/// A chunk that returns false was not written. The capture loop must publish
/// that pending value when the stream ends (`stop`, natural end, or error),
/// otherwise the last 32ms sample is dropped just because it landed inside
/// the window. Do not re-check the clock at exit: the gap may have elapsed
/// by then and the pending value would be skipped again.
bool shouldPublishListenLevel({
  required DateTime now,
  required DateTime? lastPublish,
  Duration minGap = kListenLevelThrottle,
}) {
  if (lastPublish == null) return true;
  return now.difference(lastPublish) >= minGap;
}

/// Fields every system-recognition `listen()` must pass.
///
/// `partialResults` defaults to true in the speech_to_text 7.4 constructor,
/// but several OEM recognizers only emit partials when EXTRA_PARTIAL_RESULTS
/// is set explicitly — and a missing pause/listen duration is what makes a
/// session die after a couple of seconds with no text. Kept as plain values
/// so this library does not depend on the plugin.
class SystemListenParams {
  const SystemListenParams({this.localeId});

  final String? localeId;

  /// Must be explicit: some OEM recognizers skip partials unless the extra is set.
  bool get partialResults => true;

  /// Must be explicit: a null duration lets OEM recognizers die after a few seconds.
  Duration get listenFor => kSystemListenFor;

  /// Must be explicit: null falls back to the OEM silence window, often about 1s.
  Duration get pauseFor => kSystemPauseFor;

  /// Maps to speech_to_text `ListenMode.dictation`. Confirmation mode is for
  /// a short yes/no and is what makes some OEM sessions end before a partial.
  bool get dictation => true;
}

/// Composer text for one recognition hypothesis. Empty words keep the
/// prefix so a blank partial does not wipe what the person already typed.
/// Later hypotheses replace the previous hypothesis, they do not append.
String applyRecognitionHypothesis(String prefix, String recognizedWords) {
  final words = recognizedWords.trim();
  if (words.isEmpty) return prefix;
  if (prefix.isEmpty) return words;
  return '$prefix $words';
}

/// Composer text after a voice turn auto-sends [recognizedWords].
///
/// Drops only the live hypothesis preview. Text typed after that preview
/// stays. If the person edited the preview itself, [draft] is kept whole.
String restoreComposerAfterHypothesis({
  required String prefix,
  required String recognizedWords,
  required String draft,
}) {
  final preview = applyRecognitionHypothesis(prefix, recognizedWords);
  if (!draft.startsWith(preview)) return draft;
  return '$prefix${draft.substring(preview.length)}';
}

/// Whether a watchdog armed for [armedGen] still owns the session.
///
/// A sound-level tick must not retire the watchdog: speech_to_text 7.4
/// emits `onRmsChanged` for the whole healthy session, so treating it as
/// a generation bump makes the silent-session fallback unreachable.
/// [callbackGen] is the generation captured by that session's callbacks.
bool listenWatchdogStillOwns({
  required int armedGen,
  required int currentGen,
  required bool listening,
}) {
  return listening && currentGen == armedGen;
}

/// A platform callback may change composer state only while its session
/// is still current. A late `stop()` status must not clear the next turn.
bool listenCallbackStillCurrent({
  required int callbackGen,
  required int currentGen,
  required bool listening,
}) {
  return listening && currentGen == callbackGen;
}

/// One decision for both chat pages. Already falling back once returns
/// `retryLater` so a failed local session cannot reopen the system mic.
AsrFallbackChoice decideAsrFallback({
  required bool systemStarted,
  required bool localReady,
  required bool alreadyFellBack,
  required AsrEngine engine,
}) {
  if (alreadyFellBack) return AsrFallbackChoice.retryLater;
  if (engine == AsrEngine.local && localReady) return AsrFallbackChoice.local;
  if (systemStarted) return AsrFallbackChoice.system;
  if (!localReady) return AsrFallbackChoice.retryLater;
  return AsrFallbackChoice.local;
}
