/// One composer dictation session, shared by every chat surface.
///
/// The assistant, bot chat and remote task pages used to carry their own copy
/// of this logic (system recognition first, local SenseVoice fallback, a
/// silent-session watchdog and a PCM meter). Three copies drifting apart is
/// how a page ends up "listening" with no text. [ComposerDictation] owns the
/// session; a page only supplies a [TextEditingController] sink, a way to
/// notice the local model is behind, and a toast.
///
/// Session generation rules (learned the hard way on the other two pages):
/// - A callback belongs to the session it was started for. UI reset
///   (`notListening`, an error) must NOT bump the generation: results and
///   terminal states arrive after it, and the watchdog still owns that
///   generation until something retires it.
/// - A sound-level callback only keeps the watchdog alive. Bumping the
///   generation on every level would make the watchdog never fire on a dead
///   session.
library;

import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:record/record.dart';
import 'package:speech_to_text/speech_to_text.dart' as stt;

import 'asr_fallback.dart';
import 'mic_recorder.dart';
import 'voice_engine.dart';

/// Composer dictation for one page.
class ComposerDictation {
  ComposerDictation({
    required this.controller,
    required this.engine,
    required this.toast,
    required this.localeId,
    required this.onLocalModelNeeded,
    required this.nativeVoiceSupported,
    required this.voiceInputSupported,
    required this.messages,
  }) : _speech = stt.SpeechToText(),
       _recorder = sharedRecorder();

  final TextEditingController controller;
  final VoiceEngine engine;
  final void Function(String message) toast;

  /// `speechLocaleId(Localizations.localeOf(context))`, read at each start.
  final String Function() localeId;

  /// Downloads the local model in the background, e.g. `engine.autoEnsure`.
  final void Function() onLocalModelNeeded;

  /// Read at each use, not captured once: `autoEnsure` may resetProbe and
  /// re-probe after this page opened, and a stale `false` would keep
  /// reporting "no engine" for a library that came back.
  final bool Function() nativeVoiceSupported;
  final bool voiceInputSupported;

  /// Read at each toast so a language change needs no new controller.
  final DictationMessages Function() messages;

  final stt.SpeechToText _speech;
  AudioRecorder? _recorder;

  /// Live PCM level for the composer meter. Null means this path has no
  /// samples (system recognition) — the composer pulses instead.
  final ValueNotifier<double?> level = ValueNotifier<double?>(null);

  bool listening = false;
  bool recognizing = false;
  AsrEngine activeAsr = AsrEngine.system;

  int _gen = 0;
  Timer? _watchdog;
  bool _uiReset = false;
  String _prefix = '';
  bool _disposed = false;

  /// The page repaints when a status line changes.
  VoidCallback? onStateChanged;

  void _notify() {
    if (_disposed) return;
    onStateChanged?.call();
  }

  /// The one line above the composer. It names the engine at work, never why
  /// one was skipped: switching to the local engine is the normal path on a
  /// phone whose system recognizer is unavailable, and announcing that on
  /// every dictation reads like an error. Only a failure with no engine left
  /// is reported, as a toast.
  String? statusText({
    required String listenSystem,
    required String listenLocal,
    required String recognizingSystem,
    required String recognizingLocal,
  }) {
    if (!listening && !recognizing) return null;
    if (recognizing) {
      return activeAsr == AsrEngine.local ? recognizingLocal : recognizingSystem;
    }
    return activeAsr == AsrEngine.local ? listenLocal : listenSystem;
  }

  /// Test seam: puts the controller in the state a running dictation has,
  /// without the platform recognizer.
  @visibleForTesting
  void debugSetState({
    required bool listening,
    required bool recognizing,
    required bool local,
  }) {
    this.listening = listening;
    this.recognizing = recognizing;
    activeAsr = local ? AsrEngine.local : AsrEngine.system;
  }

  void _begin(AsrEngine value) {
    _uiReset = false;
    listening = true;
    recognizing = false;
    activeAsr = value;
    _notify();
  }

  void _cancelWatchdog() {
    _watchdog?.cancel();
    _watchdog = null;
  }

  /// Retires the current generation: late callbacks of this session no longer
  /// write the composer, and the watchdog gives up.
  void _retire() {
    _gen++;
    _uiReset = false;
    _cancelWatchdog();
  }

  /// Drops the listening chrome without retiring the generation.
  void _markUiReset() {
    _uiReset = true;
    listening = false;
    _notify();
  }

  bool _owns(int gen) =>
      !_disposed &&
      listenCallbackStillCurrent(
        callbackGen: gen,
        currentGen: _gen,
        listening: listening,
        uiReset: _uiReset,
      );

  void _armWatchdog(int gen) {
    _cancelWatchdog();
    _watchdog = Timer(kListenSilentWatchdog, () {
      if (_disposed ||
          !listenWatchdogStillOwns(
            armedGen: gen,
            currentGen: _gen,
            listening: listening,
            uiReset: _uiReset,
          )) {
        return;
      }
      unawaited(_recoverSilent());
    });
  }

  /// Mic button: stop an open session, or open one.
  Future<void> toggle() async {
    if (listening) {
      _retire();
      try {
        await _speech.stop();
      } on Object {
        // Session already gone.
      }
      try {
        await _recorder?.stop();
      } on Object {
        // Not recording locally.
      }
      level.value = null;
      listening = false;
      _notify();
      return;
    }
    if (!voiceInputSupported) return;
    await engine.ensurePrefs();
    final choice = decideAsrFallback(
      systemStarted: false,
      localReady: await engine.localAsrReady(),
      alreadyFellBack: false,
      engine: engine.asrEngine,
    );
    if (choice != AsrFallbackChoice.local) {
      final started = await _startSystem();
      if (started) return;
      final readyNow = await engine.localAsrReady();
      if (decideAsrFallback(
            systemStarted: false,
            localReady: readyNow,
            alreadyFellBack: false,
            engine: engine.asrEngine,
          ) ==
          AsrFallbackChoice.retryLater) {
        await _fallbackLocal(localReady: false);
        return;
      }
    }
    await _fallbackLocal(localReady: await engine.localAsrReady());
  }

  Future<void> _recoverSilent() async {
    // Retire first: stop()'s late terminal state must not clear the local
    // capture that starts right after.
    _retire();
    try {
      await _speech.stop();
    } on Object {
      // Dead platform session.
    }
    try {
      await _recorder?.stop();
    } on Object {
      // Not recording locally.
    }
    level.value = null;
    listening = false;
    _notify();
    await _fallbackLocal(localReady: await engine.localAsrReady());
  }

  Future<void> _fallbackLocal({required bool localReady}) async {
    activeAsr = AsrEngine.local;
    _notify();
    if (!localReady) {
      if (!nativeVoiceSupported()) {
        toast(messages().missingEngine);
        return;
      }
      onLocalModelNeeded();
      toast(messages().downloading);
      return;
    }
    await _localDictate();
  }

  /// System recognition writes straight into the composer. Returns false when
  /// the recognizer never started, so the caller can fall back.
  ///
  /// On-device recognition is tried first: without it every listen goes to
  /// the default (usually cloud) recognizer, which hears nothing on a phone
  /// without GMS or network. A phone with no on-device model refuses that
  /// attempt outright, so the second try keeps the default recognizer.
  Future<bool> _startSystem() async {
    if (await _startSystemWith(onDevice: true)) return true;
    if (_disposed) return false;
    return _startSystemWith(onDevice: false);
  }

  Future<bool> _startSystemWith({required bool onDevice}) async {
    final gen = ++_gen;
    final bool ok;
    try {
      ok = await _speech
          .initialize(
            onError: (_) {
              if (!_owns(gen)) return;
              // The platform is done, but a terminal state or the final
              // hypothesis may still be in the channel. Only reset the
              // chrome; the watchdog keeps this generation and falls back.
              _markUiReset();
            },
            onStatus: (status) {
              if (!_owns(gen)) return;
              if (!listenStatusEndsTurn(status)) {
                _cancelWatchdog();
                return;
              }
              _markUiReset();
            },
          )
          .timeout(kSystemListenProbeTimeout);
    } on Object {
      _retire();
      return false;
    }
    if (!ok || _disposed) return false;
    _prefix = controller.text;
    final fallbackLocaleId = localeId();
    _uiReset = false;
    try {
      final before = await _probe(fallbackLocaleId);
      if (_disposed) return false;
      if (systemListenUnavailable(before.probe)) {
        _retire();
        return false;
      }
      _begin(AsrEngine.system);
      final params = SystemListenParams(
        localeId: before.localeId,
        onDevice: onDevice,
      );
      await _speech.listen(
        listenOptions: stt.SpeechListenOptions(
          localeId: params.localeId,
          partialResults: params.partialResults,
          listenFor: params.listenFor,
          pauseFor: params.pauseFor,
          listenMode: stt.ListenMode.dictation,
          onDevice: params.onDevice,
        ),
        onSoundLevelChange: (_) => _cancelWatchdog(),
        onResult: (result) {
          if (!_owns(gen) || _disposed) return;
          _cancelWatchdog();
          final text = applyRecognitionHypothesis(
            _prefix,
            result.recognizedWords,
          );
          controller.value = TextEditingValue(
            text: text,
            selection: TextSelection.collapsed(offset: text.length),
          );
          // Words are on screen: retire so the watchdog cannot open a second
          // mic for the same utterance.
          if (result.recognizedWords.trim().isNotEmpty) _retire();
        },
      );
    } on Object {
      _retire();
      listening = false;
      _notify();
      return false;
    }
    if (_disposed) return false;
    final after = await _probe(fallbackLocaleId);
    if (_disposed) return false;
    if (systemListenUnavailable(after.probe)) {
      _retire();
      try {
        await _speech.stop();
      } on Object {
        // Never started.
      }
      listening = false;
      _notify();
      return false;
    }
    _armWatchdog(gen);
    return true;
  }

  Future<({SystemListenProbe probe, String? localeId})> _probe(
    String fallbackLocaleId,
  ) async {
    var granted = false;
    var timedOut = false;
    try {
      granted = await _speech.hasPermission.timeout(kSystemListenProbeTimeout);
    } on TimeoutException {
      timedOut = true;
    } on Object {
      granted = false;
    }
    var locales = const <stt.LocaleName>[];
    if (!timedOut) {
      try {
        locales = await _speech.locales().timeout(kSystemListenProbeTimeout);
      } on TimeoutException {
        timedOut = true;
      } on Object {
        locales = const [];
      }
    }
    final err = _speech.lastError;
    return (
      probe: SystemListenProbe(
        permissionGranted: timedOut || granted,
        localeCount: locales.length,
        permanentError: err?.permanent ?? false,
        errorCode: err?.errorMsg ?? '',
        timedOut: timedOut,
      ),
      localeId: pickAsrLocale(locales, fallbackLocaleId: fallbackLocaleId),
    );
  }

  Future<void> _localDictate() async {
    if (!await engine.localAsrReady()) {
      if (!nativeVoiceSupported()) {
        toast(messages().missingEngine);
        return;
      }
      onLocalModelNeeded();
      toast(messages().downloading);
      return;
    }
    final pcm = await _capturePcm();
    if (pcm == null || _disposed) return;
    if (pcm.length < 32000) {
      toast(messages().notHeard);
      return;
    }
    try {
      final text = (await engine.transcribe(pcm)).trim();
      if (RegExp(r'[\w一-鿿]{2,}').hasMatch(text)) {
        final current = controller.text;
        final merged = current.isEmpty ? text : '$current $text';
        controller.value = TextEditingValue(
          text: merged,
          selection: TextSelection.collapsed(offset: merged.length),
        );
      } else {
        toast(messages().notHeard);
      }
    } on Object catch (error) {
      // Say what the engine said: "识别失败" alone left a downloaded model
      // that fails to load indistinguishable from a silent microphone.
      final reason = '$error'.trim();
      if (!await engine.localAsrReady()) {
        // Weights were dropped by a failed verification. Retry the system
        // path only when the user chose local and we came from system.
        if (engine.asrEngine == AsrEngine.local) {
          if (await _startSystem()) return;
        }
        if (!nativeVoiceSupported()) {
          toast(messages().missingEngine);
          return;
        }
        onLocalModelNeeded();
        toast(messages().downloading);
        return;
      }
      toast(
        reason.isEmpty
            ? messages().transcribeFailed
            : messages().transcribeFailedWith(reason),
      );
    }
  }

  Future<Uint8List?> _capturePcm() async {
    final recorder = _recorder ??= sharedRecorder();
    if (!await recorder.hasPermission()) {
      toast(messages().micPermission);
      return null;
    }
    late final Stream<Uint8List> stream;
    try {
      stream = await recorder.startStream(
        const RecordConfig(
          encoder: AudioEncoder.pcm16bits,
          sampleRate: 16000,
          numChannels: 1,
        ),
      );
    } on Object {
      toast(messages().micOpenFailed);
      return null;
    }
    if (!_disposed) {
      _begin(AsrEngine.local);
      level.value = 0;
    }
    final buf = BytesBuilder(copy: false);
    var lastLoud = DateTime.now();
    DateTime? lastLevelPublish;
    double? lastRms;
    var levelPending = false;
    try {
      await for (final chunk in stream) {
        if (!listening || _disposed) break;
        buf.add(chunk);
        var sum = 0.0;
        final n = chunk.length ~/ 2;
        for (var i = 0; i < n; i++) {
          final sample = (chunk[i * 2] | (chunk[i * 2 + 1] << 8)).toSigned(16);
          sum += sample * sample;
        }
        final rms = n > 0 ? math.sqrt(sum / n) : 0.0;
        lastRms = rms;
        final now = DateTime.now();
        final publish = shouldPublishListenLevel(
          now: now,
          lastPublish: lastLevelPublish,
        );
        levelPending = !publish;
        if (!_disposed && publish) {
          level.value = normalizeListenLevel(rms);
          lastLevelPublish = now;
        }
        if (rms > 400) lastLoud = now;
        final totalMs = buf.length / 32;
        final silenceMs = now.difference(lastLoud).inMilliseconds;
        if ((totalMs > 700 && silenceMs > 1200) || totalMs > 20000) {
          await recorder.stop();
          break;
        }
      }
    } on Object {
      // Stream error ends the turn with whatever was captured.
    }
    if (!_disposed && levelPending && lastRms != null) {
      level.value = normalizeListenLevel(lastRms);
      // One frame so the last level paints before it is cleared.
      await Future<void>.delayed(kListenLevelFlushHold);
    }
    await recorder.stop();
    if (!_disposed) {
      level.value = null;
      listening = false;
      _notify();
    }
    return buf.takeBytes();
  }

  void dispose() {
    _disposed = true;
    _retire();
    listening = false;
    recognizing = false;
    level.value = null;
    unawaited(_stopQuietly());
    level.dispose();
  }

  Future<void> _stopQuietly() async {
    try {
      await _speech.stop();
    } on Object {
      // Nothing to stop.
    }
    try {
      await _recorder?.stop();
    } on Object {
      // Nothing recording.
    }
  }
}

/// Toast wording for one page, resolved from its localizations.
class DictationMessages {
  final String missingEngine;
  final String downloading;
  final String notHeard;
  final String transcribeFailed;

  /// Same failure, with the engine's own words so a broken model load is not
  /// reported as a silent microphone.
  final String Function(String reason) transcribeFailedWith;
  final String micPermission;
  final String micOpenFailed;

  const DictationMessages({
    required this.missingEngine,
    required this.downloading,
    required this.notHeard,
    required this.transcribeFailed,
    required this.transcribeFailedWith,
    required this.micPermission,
    required this.micOpenFailed,
  });
}
