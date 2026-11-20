/// Voice engine orchestration — settings (engine/voice prefs), lazy native
/// loads, synthesis + playback, local transcription. Heavy FFI calls run on
/// worker isolates so the UI thread never blocks on a 2–8s inference.
library;

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

final voiceEngineProvider = Provider<VoiceEngine>((ref) => VoiceEngine());

class VoiceEngine {
  final AudioPlayer _player = AudioPlayer();
  void Function()? _onDone;

  VoiceEngine() {
    _player.onPlayerComplete.listen((_) => _onDone?.call());
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
    final p = await SharedPreferences.getInstance();
    await p.setString(_kTtsVoice, id);
  }

  bool get kokoroAvailable => NativeVoice.supported;

  Future<bool> kokoroReady() async =>
      kokoroAvailable && await VoiceAssets.ttsReady();

  Future<bool> localAsrReady() async =>
      NativeVoice.supported && await VoiceAssets.asrReady();

  /// Fire-and-forget model provisioning, kicked at app start. Restores are
  /// free (files already in the external media dir); a fresh install
  /// downloads ~330 MB in the background — no user action needed.
  Future<void> autoEnsure({String? hubBase}) async {
    if (!NativeVoice.supported) return;
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

  Future<String?> _ensureTtsLoaded() async {
    if (!NativeVoice.supported) return 'native voice unavailable';
    if (_ttsLoaded) return null;
    final err = await Isolate.run(
      () => NativeVoice.loadTts(
        _modelPathSnapshot!,
        _voiceDirSnapshot!,
        ttsVoice,
      ),
    );
    if (err == null) _ttsLoaded = true;
    return err;
  }

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
      wav = await Isolate.run(() => NativeVoice.tts(text, speed: speed));
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

  Future<void> stopSpeaking() => _player.stop();

  Future<String?> _ensureAsrLoaded() async {
    if (!NativeVoice.supported) return 'native voice unavailable';
    if (_asrLoaded) return null;
    if (!await VoiceAssets.asrReady()) {
      _asrPathSnapshot = null;
      return 'asr model missing or failed validation';
    }
    final err = await Isolate.run(() => NativeVoice.loadAsr(_asrPathSnapshot!));
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
  }

  /// PCM s16le mono 16 kHz → transcript (off-isolate).
  Future<String> transcribe(Uint8List pcm16) async {
    await _snapshotPaths();
    final err = await _ensureAsrLoaded();
    if (err != null) throw StateError(err);
    return Isolate.run(() => NativeVoice.asr(pcm16));
  }

  void dispose() {
    _player.dispose();
    if (NativeVoice.supported) {
      if (_ttsLoaded) NativeVoice.unloadTts();
      if (_asrLoaded) NativeVoice.unloadAsr();
    }
  }
}
