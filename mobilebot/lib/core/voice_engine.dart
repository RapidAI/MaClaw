/// Voice engine orchestration — settings (engine/voice prefs), lazy native
/// loads, synthesis + playback, local transcription. Heavy FFI calls run on
/// worker isolates so the UI thread never blocks on a 2–8s inference.
library;

import 'dart:async';
import 'dart:isolate';
import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:speech_to_text/speech_to_text.dart' show LocaleName;

import 'locale.dart';
import 'native_voice.dart';
import 'voice_assets.dart';

enum TtsEngine { system, kokoro }

/// 'system' = speech_to_text; 'local' = bundled SenseVoice (used when the
/// platform recognizer is unavailable, e.g. GMS-free devices).
enum AsrEngine { system, local }

/// The recognizer locale for dictation, picked from what the platform
/// recognizer actually offers. 'zh_CN' used to be hardcoded — on a
/// recognizer without it the listen intent carries
/// EXTRA_LANGUAGE=zh-CN and recognition dies with
/// language-not-supported before a single word is heard. Preference:
/// mainland zh first, any other Chinese locale next. A non-empty list
/// with no Chinese locale returns null so the recognizer keeps its own
/// default.
///
/// An empty list must not return null. speech_to_text 7.4 on Android
/// turns a missing localeId into `Locale.getDefault().toLanguageTag()`
/// (`zh-Hans-CN`) and some OEM recognizers reject that listen outright.
String? pickAsrLocale(
  List<LocaleName> available, {
  String? fallbackLocaleId,
}) {
  String? best;
  var bestScore = 0;
  for (final l in available) {
    final id = l.localeId.toLowerCase().replaceAll('_', '-');
    final score = id == 'zh-cn'
        ? 4
        : (id == 'zh' || id.startsWith('zh-'))
        ? 3
        : 0;
    if (score > bestScore) {
      best = l.localeId;
      bestScore = score;
    }
  }
  if (bestScore > 0) return best;
  if (available.isNotEmpty) return null;
  final fallback = fallbackLocaleId?.trim();
  if (fallback != null && fallback.isNotEmpty) return fallback;
  return 'zh_CN';
}

/// One spoken segment of a readback: its text, the synthesized WAV once it
/// exists, or the error that stopped it.
class _SpeechSegment {
  _SpeechSegment(this.text);
  final String text;
  Uint8List? wav;
  String? error;
  Future<Uint8List?>? pending;
}

final voiceEngineProvider = Provider<VoiceEngine>((ref) => VoiceEngine());

class VoiceEngine {
  final AudioPlayer _player = AudioPlayer();
  void Function()? _onDone;

  VoiceEngine() {
    _player.onPlayerComplete.listen((_) => _onPlayerComplete());
  }
  final VoiceAssets assets = VoiceAssets();

  /// (stage, progress 0..1) of the background model download, or null when
  /// idle/done. The settings sheet mirrors this so an auto-download started
  /// at launch still shows its progress to a user who opens the sheet.
  final ValueNotifier<({String stage, double progress})?> downloadProgress =
      ValueNotifier(null);

  static const _kTtsEngine = 'voice.tts.engine';
  static const _kTtsVoice = 'voice.tts.voice';
  static const _kAsrEngine = 'voice.asr.engine';

  TtsEngine ttsEngine = TtsEngine.system;
  AsrEngine asrEngine = AsrEngine.system;
  String ttsVoice = 'zf_xiaoyi';

  bool _ttsLoaded = false;
  bool _asrLoaded = false;
  bool _prefsLoaded = false;

  Future<void> ensurePrefs() async {
    if (_prefsLoaded) return;
    _prefsLoaded = true;
    final p = await SharedPreferences.getInstance();
    ttsEngine = p.getString(_kTtsEngine) == 'kokoro'
        ? TtsEngine.kokoro
        : TtsEngine.system;
    asrEngine = p.getString(_kAsrEngine) == 'local'
        ? AsrEngine.local
        : AsrEngine.system;
    final v = p.getString(_kTtsVoice);
    if (v != null && VoiceAssets.voiceIds.contains(v)) ttsVoice = v;
  }

  Future<void> setTtsEngine(TtsEngine e) async {
    ttsEngine = e;
    final p = await SharedPreferences.getInstance();
    await p.setString(_kTtsEngine, e.name);
  }

  Future<void> setAsrEngine(AsrEngine e) async {
    asrEngine = e;
    final p = await SharedPreferences.getInstance();
    await p.setString(_kAsrEngine, e.name);
  }

  Future<void> setTtsVoice(String id) async {
    if (!VoiceAssets.voiceIds.contains(id)) return;
    ttsVoice = id;
    _ttsLoaded = false; // reload with the new voice next speak
    _ttsVoiceGen++; // an in-flight load for the old voice must not mark loaded
    final p = await SharedPreferences.getInstance();
    await p.setString(_kTtsVoice, id);
  }

  /// The local library actually opened and has every export. `supported`
  /// alone only says dlopen returned a handle; a partial build opens and then
  /// throws on the first call, which reads as "no engine" forever.
  bool get kvAvailable => NativeVoice.usable;

  bool get kokoroAvailable => kvAvailable;

  Future<bool> kokoroReady() async =>
      kokoroAvailable && await VoiceAssets.ttsReady();

  Future<bool> localAsrReady() async =>
      kvAvailable && await VoiceAssets.asrReady();

  /// Fire-and-forget model provisioning, kicked at app start. Restores are
  /// free (files already in the external media dir); a fresh install
  /// downloads ~330 MB in the background — no user action needed.
  Future<void> autoEnsure({String? hubBase}) async {
    // The probe result is cached for the process: the .so lives inside the
    // APK and cannot be swapped without a restart, so re-opening it on every
    // call only leaks a dlopen refcount and re-runs the export checks. A
    // replaced library is picked up on the next launch, when `_probed` is
    // false again. Callers that must force a re-probe (an explicit retry)
    // call `NativeVoice.resetProbe()` first.
    if (!NativeVoice.usable) return;
    if (await kokoroReady() && await localAsrReady()) return;
    try {
      await assets.ensureAll(
        hubBase: hubBase,
        onProgress: (s, p) => downloadProgress.value = (stage: s, progress: p),
      );
    } on Object catch (e) {
      downloadProgress.value = (
        stage: sharedL10n.voiceStageDownloadFailedRetry('$e'),
        progress: -1,
      );
      return;
    }
    downloadProgress.value = null;
  }

  /// Whether Kokoro should handle readback right now.
  Future<bool> get useKokoro async {
    await ensurePrefs();
    return ttsEngine == TtsEngine.kokoro && await kokoroReady();
  }

  // In-flight engine loads, shared by concurrent callers so two speaks (or
  // two transcribes) started together do not each spawn an Isolate.run that
  // reloads the same Go-side manager. Cleared in `finally` so a failure can
  // be retried by the next call.
  Future<String?>? _ttsLoading;
  Future<String?>? _asrLoading;
  int _ttsVoiceGen = 0;

  Future<String?> _ensureTtsLoaded() async {
    if (!NativeVoice.usable) return 'native voice unavailable';
    if (_ttsLoaded) return null;
    final inFlight = _ttsLoading;
    if (inFlight != null) return inFlight;
    final gen = _ttsVoiceGen;
    final voice = ttsVoice;
    // Read the snapshots into locals: the Isolate.run closure must not
    // capture `this` (the engine holds an AudioPlayer, which is not sendable).
    final model = _modelPathSnapshot!;
    final voiceDir = _voiceDirSnapshot!;
    final future = () async {
      try {
        final err = await _runTtsLoad(model, voiceDir, voice);
        // A voice switch mid-load must not mark the new voice as loaded.
        if (err == null && gen == _ttsVoiceGen) _ttsLoaded = true;
        return err;
      } finally {
        _ttsLoading = null;
      }
    }();
    _ttsLoading = future;
    return future;
  }

  // Static so the Isolate.run closure cannot pick up an implicit `this`
  // context — an instance method touching fields would send the engine (and
  // its AudioPlayer) to the worker, and the send fails as unsendable.
  static Future<String?> _runTtsLoad(
    String model,
    String voiceDir,
    String voice,
  ) => Isolate.run(() => NativeVoice.loadTts(model, voiceDir, voice));

  static Future<Uint8List> _runTts(String text, double speed) =>
      Isolate.run(() => NativeVoice.tts(text, speed: speed));

  // Paths are looked up once and snapshotted for the worker isolate (the
  // isolate can't call path_provider).
  String? _modelPathSnapshot;
  String? _voiceDirSnapshot;
  String? _asrPathSnapshot;

  Future<void> _snapshotPaths() async {
    _modelPathSnapshot ??= await VoiceAssets.kokoroPath();
    _voiceDirSnapshot ??= await VoiceAssets.voiceDir();
    _asrPathSnapshot ??= await VoiceAssets.asrPath();
  }

  // ---- Speech queue ------------------------------------------------------
  // Long replies used to be one up-front synthesis of the whole text, so
  // playback only started after every chunk had been generated, and a reply
  // over `_speechTextLimit` was simply cut off. The queue speaks in segments:
  // one segment is synthesized while the previous one plays, so the first
  // audio starts after a single short inference.
  final List<_SpeechSegment> _segments = [];
  bool _speaking = false;
  int _speechGen = 0;
  double _speechSpeed = 1.0;

  /// True while a segmented readback owns the player (queued, generating or
  /// playing). The mic stays shut while this is true.
  bool get speaking => _speaking;

  /// Whether a segmented readback is in a lifecycle callback right now.
  /// A completion arriving outside one belongs to a clip the queue has
  /// already moved past — the single-clip path — and is ignored here.
  bool _speechCallback = false;

  void _onPlayerComplete() {
    if (_speechCallback) return;
    _onDone?.call();
  }

  /// Speak [segments] in order, reporting lifecycle changes through [onEvent]:
  /// `start` once (audio is about to play — load and the first synthesis are
  /// done), `idle` once when the queue drains, `failed` with a reason when
  /// synthesis or playback breaks (later segments still play). A cancelled
  /// queue (see [stopSpeaking]) fires no terminal event.
  /// Returns the error of the first failed segment, or null when every segment
  /// was spoken.
  Future<String?> speakSegments(
    List<String> segments, {
    required void Function(String event, String detail) onEvent,
    double speed = 1.0,
  }) async {
    final gen = ++_speechGen;
    _segments
      ..clear()
      ..addAll(
        segments.where((s) => s.trim().isNotEmpty).map(_SpeechSegment.new),
      );
    if (_segments.isEmpty) return null;
    _speechSpeed = speed > 0 ? speed : 1.0;
    _speaking = true;

    await _snapshotPaths();
    final loadErr = await _ensureTtsLoaded();
    if (gen != _speechGen) {
      return null;
    }
    if (loadErr != null) {
      _speechCallback = true;
      try {
        _finishSpeech(gen, onEvent);
      } finally {
        _speechCallback = false;
      }
      return loadErr;
    }
    // The first clip replaces whatever the player holds; stop() can emit the
    // PREVIOUS clip's completion asynchronously, so the callback flag is on
    // for it.
    _speechCallback = true;
    try {
      await _player.stop();
    } on Object {
      // A player with nothing to stop is not an error.
    } finally {
      _speechCallback = false;
    }
    final errors = <String>[];
    var started = false;
    for (var i = 0; gen == _speechGen && i < _segments.length; i++) {
      final wav = await _synthesizeSegment(i);
      if (gen != _speechGen) break;
      if (wav == null) {
        final err = _segments[i].error ?? 'kokoro synthesize failed';
        errors.add(err);
        onEvent('failed', err);
        continue;
      }
      if (!started) {
        started = true;
        onEvent('start', '');
      }
      // Synthesize ahead: the next clip is ready while this one plays, so
      // playback rarely waits on inference after the first segment.
      if (i + 1 < _segments.length) unawaited(_synthesizeSegment(i + 1));
      try {
        await _playSegment(i, wav, gen, onEvent);
      } on Object catch (e) {
        errors.add('voice playback failed: $e');
        onEvent('failed', 'voice playback failed: $e');
      }
    }
    if (gen == _speechGen) {
      _speechCallback = true;
      try {
        _finishSpeech(gen, onEvent);
      } finally {
        _speechCallback = false;
      }
    }
    return errors.isEmpty ? null : errors.first;
  }

  /// Synthesizes segments ahead of playback, so the next clip is ready while
  /// the current one is still playing. Failures are stored on the segment.
  Future<Uint8List?> _synthesizeSegment(int idx) async {
    final seg = _segments[idx];
    if (seg.wav != null || seg.error != null) return seg.wav;
    final pending = seg.pending;
    if (pending != null) return pending;
    final future = () async {
      try {
        final wav = await _runTts(seg.text, _speechSpeed);
        seg.wav = wav;
        return wav;
      } on Object catch (e) {
        seg.error = 'kokoro synthesize failed: $e';
        return null;
      }
    }();
    seg.pending = future;
    return future;
  }

  /// Plays one synthesized segment and waits for the player to finish it.
  /// The first segment replaces whatever the player holds, which is why
  /// [speakSegments] stops the previous clip only once, up front.
  Future<void> _playSegment(
    int idx,
    Uint8List wav,
    int gen,
    void Function(String, String) onEvent,
  ) async {
    final done = Completer<void>();
    _speechCallback = true;
    try {
      _onDone = () {
        if (!done.isCompleted) done.complete();
      };
      await _player.play(BytesSource(wav));
    } finally {
      _speechCallback = false;
    }
    await done.future;
    _onDone = null;
  }

  void _finishSpeech(int gen, void Function(String, String) onEvent) {
    if (gen != _speechGen) return;
    _speaking = false;
    _segments.clear();
    _onDone = null;
    onEvent('idle', '');
  }

  /// Cut a segmented readback short (mic press, page dispose, engine switch).
  /// The pending `_playSegment` wait ends on the player's complete event; the
  /// generation bump makes [speakSegments] drop the rest of the queue.
  Future<void> stopSpeaking() async {
    _speechGen++;
    _speaking = false;
    _segments.clear();
    _onDone = null;
    if (!_speechCallback) await _player.stop();
  }

  /// Load the engine, synthesize, play WAV — completion fires [onDone].
  /// Returns an error string instead of playing on failure.
  Future<String?> speakKokoro(
    String text, {
    double speed = 1.0,
    required void Function() onDone,
  }) async {
    await _snapshotPaths();
    final loadErr = await _ensureTtsLoaded();
    if (loadErr != null) return loadErr;
    final Uint8List wav;
    try {
      wav = await _runTts(text, speed);
    } on Object catch (e) {
      return 'kokoro synthesize failed: $e';
    }
    // Sequence matters: stop() can emit the PREVIOUS clip's completion
    // asynchronously. Arming _onDone only after play() starts means a stale
    // completion can never fire the new turn's callback (which would reopen
    // the mic while our own TTS is still playing → echo feedback).
    _onDone = null;
    try {
      await _player.stop();
      await _player.play(BytesSource(wav));
    } on Object catch (e) {
      return 'voice playback failed: $e';
    }
    _onDone = onDone;
    return null;
  }

  Future<String?> _ensureAsrLoaded() async {
    if (!NativeVoice.usable) return 'native voice unavailable';
    if (_asrLoaded) return null;
    final inFlight = _asrLoading;
    if (inFlight != null) return inFlight;
    // The path is read into a local here, and the actual isolate work lives in
    // the static `_runAsrLoad`: a closure built inside an instance method picks
    // up `this` as its context (a sibling field write is enough), and sending
    // that context to a worker isolate fails in release/AOT builds with
    // "object is unsendable - Instance of 'VoiceEngine'" — the engine holds an
    // AudioPlayer.
    final model = _asrPathSnapshot;
    final future = () async {
      try {
        if (model == null || !await VoiceAssets.asrReady()) {
          _asrPathSnapshot = null;
          return 'asr model missing or failed validation';
        }
        final err = await _runAsrLoad(model);
        if (err == null) {
          _asrLoaded = true;
          return null;
        }
        // Header looked fine but the weights failed to load. Drop the file so
        // the next ensure does not treat the same bytes as ready forever.
        _asrLoaded = false;
        _asrPathSnapshot = null;
        await VoiceAssets.rejectAsrFile();
        return err;
      } finally {
        _asrLoading = null;
      }
    }();
    _asrLoading = future;
    return future;
  }

  // Static, like _runTtsLoad: an instance method here would give the closure
  // an implicit `this` context again.
  static Future<String?> _runAsrLoad(String model) =>
      Isolate.run(() => NativeVoice.loadAsr(model));

  static Future<String> _runAsr(Uint8List pcm16) =>
      Isolate.run(() => NativeVoice.asr(pcm16));

  /// Report each transcription step, so a silent microphone and a model that
  /// never finishes inference do not both look like "nothing happened".
  void Function(String stage)? onTranscribeStage;

  /// Upper bound for one transcription. Phone inference is seconds; a
  /// wedged native call used to leave the mic shut with no message at all.
  static const transcribeTimeout = Duration(seconds: 45);

  /// PCM s16le mono 16 kHz → transcript (off-isolate).
  Future<String> transcribe(Uint8List pcm16) async {
    onTranscribeStage?.call('loading');
    await _snapshotPaths();
    final err = await _ensureAsrLoaded();
    if (err != null) throw StateError(err);
    onTranscribeStage?.call('transcribing');
    try {
      return await _runAsr(pcm16).timeout(transcribeTimeout);
    } on TimeoutException {
      // Drop the cached manager: the stuck call owns the library mutex, so
      // the next attempt must load it fresh.
      _asrLoaded = false;
      _asrPathSnapshot = null;
      throw StateError('transcribe timed out');
    } finally {
      onTranscribeStage?.call('done');
    }
  }

  void dispose() {
    _player.dispose();
    if (NativeVoice.usable) {
      if (_ttsLoaded) NativeVoice.unloadTts();
      if (_asrLoaded) NativeVoice.unloadAsr();
    }
  }
}
