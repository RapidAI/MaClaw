/// Pure ASR engine choice shared by assistant and bot dictation.
///
/// System recognition is tried first unless the user already selected the
/// local engine and its model is ready. A failed system start falls back to
/// local only once, and only when the local model is ready. Otherwise the
/// caller should toast and retry later instead of opening a second mic.
library;

import 'voice_engine.dart';

enum AsrFallbackChoice { system, local, retryLater }

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
