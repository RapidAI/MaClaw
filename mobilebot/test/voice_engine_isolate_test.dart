import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:audioplayers_platform_interface/audioplayers_platform_interface.dart';
import 'package:path_provider_platform_interface/path_provider_platform_interface.dart';
import 'package:plugin_platform_interface/plugin_platform_interface.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/voice_engine.dart';

/// In-memory stand-in for the audioplayers plugin channels. The host test
/// runner has no Android (or any) plugin implementation, and `VoiceEngine`
/// creates an `AudioPlayer` in its constructor; without this every test that
/// constructs the engine dies on a MissingPluginException before reaching the
/// logic it means to check.
class _FakePlayers extends AudioplayersPlatformInterface
    with MockPlatformInterfaceMixin {
  final Map<String, StreamController<AudioEvent>> events = {};

  @override
  Future<void> create(String playerId) async {
    events[playerId] ??= StreamController<AudioEvent>.broadcast();
  }

  @override
  Future<void> dispose(String playerId) async {
    await events.remove(playerId)?.close();
  }

  @override
  Future<void> stop(String playerId) async {}

  @override
  Future<void> pause(String playerId) async {}

  @override
  Future<void> resume(String playerId) async {}

  @override
  Future<void> release(String playerId) async {}

  @override
  Future<void> seek(String playerId, Duration position) async {}

  @override
  Future<void> setBalance(String playerId, double balance) async {}

  @override
  Future<void> setVolume(String playerId, double volume) async {}

  @override
  Future<void> setReleaseMode(String playerId, ReleaseMode releaseMode) async {}

  @override
  Future<void> setPlaybackRate(String playerId, double playbackRate) async {}

  @override
  Future<void> setSourceUrl(
    String playerId,
    String url, {
    bool? isLocal,
    String? mimeType,
  }) async {}

  @override
  Future<void> setSourceBytes(
    String playerId,
    Uint8List bytes, {
    String? mimeType,
  }) async {}

  @override
  Future<void> setAudioContext(String playerId, AudioContext ctx) async {}

  @override
  Future<void> setPlayerMode(String playerId, PlayerMode mode) async {}

  @override
  Future<int?> getDuration(String playerId) async => null;

  @override
  Future<int?> getCurrentPosition(String playerId) async => null;

  @override
  Future<void> emitLog(String playerId, String message) async {}

  @override
  Future<void> emitError(String playerId, String code, String message) async {}

  @override
  Stream<AudioEvent> getEventStream(String playerId) =>
      events.putIfAbsent(
        playerId,
        () => StreamController<AudioEvent>.broadcast(),
      ).stream;
}

class _FakeGlobal extends GlobalAudioplayersPlatformInterface
    with MockPlatformInterfaceMixin {
  @override
  Future<void> init() async {}

  @override
  Future<void> setGlobalAudioContext(AudioContext ctx) async {}

  @override
  Future<void> emitGlobalLog(String message) async {}

  @override
  Future<void> emitGlobalError(String code, String message) async {}

  @override
  Stream<GlobalAudioEvent> getGlobalEventStream() => const Stream.empty();
}

class _FakePathProvider extends PathProviderPlatform
    with MockPlatformInterfaceMixin {
  @override
  Future<String?> getApplicationSupportPath() async =>
      Directory.systemTemp.createTempSync('voice_test_').path;

  @override
  Future<String?> getApplicationDocumentsPath() async =>
      Directory.systemTemp.createTempSync('voice_test_').path;

  @override
  Future<String?> getTemporaryPath() async =>
      Directory.systemTemp.createTempSync('voice_test_').path;

  @override
  Future<String?> getExternalStoragePath() async =>
      Directory.systemTemp.createTempSync('voice_test_').path;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    PathProviderPlatform.instance = _FakePathProvider();
    AudioplayersPlatformInterface.instance = _FakePlayers();
    GlobalAudioplayersPlatformInterface.instance = _FakeGlobal();
  });

  // A local transcription on a real phone used to die with
  // "Invalid argument(s): Illegal argument in isolate message: object is
  // unsendable - ... Instance of 'VoiceEngine'". The Isolate.run closure in
  // _ensureAsrLoaded read a field of `this` that a sibling branch also wrote,
  // so the compiler attached the enclosing instance as the closure context
  // and sent it (with its AudioPlayer) to the worker isolate.
  //
  // NativeVoice is unavailable outside Android, so these calls stop before any
  // inference. The point is that they stop with the engine's own error rather
  // than an unsendable-object failure from isolate setup.
  test('local transcription does not send the engine to the isolate', () async {
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    final stages = <String>[];
    engine.onTranscribeStage = stages.add;

    await expectLater(
      engine.transcribe(Uint8List(1600)),
      throwsA(
        isA<StateError>().having(
          (e) => e.message,
          'message',
          isNot(contains('unsendable')),
        ),
      ),
    );
    expect(stages.first, 'loading');
  });

  test('kokoro load path does not send the engine to the isolate', () async {
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    // Mutable field on the engine: whether the closure picked up `this` is
    // exactly what breaks the isolate send.
    engine.ttsVoice = 'zf_xiaoyi';
    final err = await engine.speakKokoro('你好', onDone: () {});
    expect(err, isNotNull);
    expect(err, isNot(contains('unsendable')));
  });
}
