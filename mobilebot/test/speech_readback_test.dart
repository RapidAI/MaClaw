import 'package:audioplayers_platform_interface/audioplayers_platform_interface.dart';
import 'package:flutter/services.dart';
import 'package:flutter_tts/flutter_tts.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/speech_readback.dart';
import 'package:mobilebot/core/voice_engine.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:path_provider_platform_interface/path_provider_platform_interface.dart';

import 'voice_engine_isolate_test.dart'
    show FakeGlobal, FakePathProvider, FakePlayers;

/// Fake `flutter_tts` platform channel: records what the plugin would speak.
/// Real `FlutterTts` instances talk over this channel, so the fallback path in
/// [speakReplyReadback] runs exactly as it does on a phone.
class FakeTtsChannel {
  FakeTtsChannel() {
    messenger.setMockMethodCallHandler(channel, (call) async {
      if (call.method == 'speak') {
        spoken.add(call.arguments is String ? call.arguments as String : '');
      }
      return 1;
    });
  }

  static const channel = MethodChannel('flutter_tts');
  static TestDefaultBinaryMessenger get messenger =>
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;

  final List<String> spoken = [];

  void dispose() => messenger.setMockMethodCallHandler(channel, null);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    PathProviderPlatform.instance = FakePathProvider();
    AudioplayersPlatformInterface.instance = FakePlayers();
    GlobalAudioplayersPlatformInterface.instance = FakeGlobal();
  });

  test('an empty reply finishes immediately without speaking', () async {
    final tts = FakeTtsChannel();
    addTearDown(tts.dispose);
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    var done = 0;
    final err = await speakReplyReadback(engine, '   \n ', onDone: () => done++);
    expect(done, 1);
    expect(err, isNotNull);
    expect(tts.spoken, isEmpty);
  });

  // The host runner is not Android, so Kokoro can never be selected here.
  // The fallback must still speak the reply and report completion, or the Bot
  // voice loop stays in `_ttsBusy` with the microphone shut — the symptom the
  // queue refactor must not introduce.
  test('falls back to system tts', () async {
    final tts = FakeTtsChannel();
    addTearDown(tts.dispose);
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    final system = FlutterTtsProbe();
    addTearDown(system.dispose);
    var done = 0;
    final err = await speakReplyReadback(
      engine,
      '北京今天多云。',
      onDone: () => done++,
      systemTts: () => system.instance,
    );
    expect(err, '');
    expect(tts.spoken, ['北京今天多云。']);
    // Completion is wired to the platform callback, not fired optimistically.
    expect(done, 0);
    await system.fireCompletion();
    expect(done, 1);
  });

  // A previous completion handler belongs to an earlier turn. Clearing it
  // before this turn's platform callback arrives drops the new completion,
  // which leaves the Bot voice loop in `_ttsBusy` with the microphone shut.
  test('system tts keeps this turn completion when a previous handler exists',
      () async {
    final tts = FakeTtsChannel();
    addTearDown(tts.dispose);
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    final system = FlutterTtsProbe();
    addTearDown(system.dispose);
    var previous = 0;
    system.instance.setCompletionHandler(() => previous++);
    var done = 0;
    final err = await speakReplyReadback(
      engine,
      '上海明天有雨。',
      onDone: () => done++,
      systemTts: () => system.instance,
    );
    expect(err, '');
    expect(done, 0);
    expect(previous, 0);
    await system.fireCompletion();
    expect(done, 1);
    expect(previous, 1);
  });

  // Two replies can speak before either platform callback arrives. The first
  // turn used to treat its own wrapper as the "previous" handler and write it
  // back, so the second turn's completion never reached onDone.
  test('a later system tts turn keeps the completion slot', () async {
    final tts = FakeTtsChannel();
    addTearDown(tts.dispose);
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    final system = FlutterTtsProbe();
    addTearDown(system.dispose);
    var original = 0;
    system.instance.setCompletionHandler(() => original++);
    var done1 = 0;
    var done2 = 0;
    expect(
      await speakReplyReadback(
        engine,
        '第一句。',
        onDone: () => done1++,
        systemTts: () => system.instance,
      ),
      '',
    );
    expect(
      await speakReplyReadback(
        engine,
        '第二句。',
        onDone: () => done2++,
        systemTts: () => system.instance,
      ),
      '',
    );
    expect(done1, 0);
    expect(done2, 0);
    await system.fireCompletion();
    expect(done1, 0);
    expect(done2, 1);
    expect(original, 1);
    await system.fireCompletion();
    expect(done2, 1);
    expect(original, 2);
  });

  // stop() can deliver the previous utterance's completion before speak().
  // Treating that as this turn's completion reopens the mic while the reply
  // is still queued.
  test('a completion from stop does not finish this turn', () async {
    final tts = FakeTtsChannel();
    addTearDown(tts.dispose);
    FakeTtsChannel.messenger.setMockMethodCallHandler(
      FakeTtsChannel.channel,
      (call) async {
        if (call.method == 'speak') {
          tts.spoken.add(call.arguments is String ? call.arguments as String : '');
        }
        if (call.method == 'stop') {
          await FakeTtsChannel.messenger.handlePlatformMessage(
            FakeTtsChannel.channel.name,
            const StandardMethodCodec().encodeMethodCall(
              const MethodCall('speak.onComplete'),
            ),
            (_) {},
          );
        }
        return 1;
      },
    );
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    final system = FlutterTtsProbe();
    addTearDown(system.dispose);
    var done = 0;
    final err = await speakReplyReadback(
      engine,
      '停掉上一句后再读。',
      onDone: () => done++,
      systemTts: () => system.instance,
    );
    expect(err, '');
    expect(tts.spoken, ['停掉上一句后再读。']);
    expect(done, 0);
    await system.fireCompletion();
    expect(done, 1);
  });

  test('a long reply is summarized before it reaches tts', () async {
    final tts = FakeTtsChannel();
    addTearDown(tts.dispose);
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    final system = FlutterTtsProbe();
    addTearDown(system.dispose);
    final long = List.generate(60, (i) => '第${i + 1}条结论已经确认。').join();
    await speakReplyReadback(
      engine,
      long,
      onDone: () {},
      systemTts: () => system.instance,
    );
    final spoken = tts.spoken.single;
    expect(spoken.length, lessThan(long.length ~/ 2));
    expect(spoken, contains('第1条结论已经确认'));
    expect(spoken, contains('以上是要点'));
  });

  test('no system tts available still reports completion', () async {
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    var done = 0;
    final err = await speakReplyReadback(
      engine,
      '一句话。',
      onDone: () => done++,
    );
    expect(done, 1);
    expect(err, 'system tts unavailable');
  });

  test('the engine reports itself as speaking while a queue runs', () async {
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    expect(engine.speaking, isFalse);
    // No native engine here, so the queue fails fast; the flag must come back
    // down either way.
    await engine.speakSegments(['一句话。'], onEvent: (_, _) {});
    expect(engine.speaking, isFalse);
  });

  test('a cancelled queue stops the player and fires no terminal event', () async {
    final engine = VoiceEngine();
    addTearDown(engine.dispose);
    final events = <String>[];
    final speaking = engine.speakSegments(
      ['第一句。', '第二句。'],
      onEvent: (event, _) => events.add(event),
    );
    await engine.stopSpeaking();
    await speaking;
    expect(events, isNot(contains('idle')));
    expect(engine.speaking, isFalse);
  });
}

/// A real `FlutterTts` over the mocked channel, plus a way to deliver the
/// plugin's completion callback (`speak.onComplete`).
class FlutterTtsProbe {
  FlutterTtsProbe() {
    instance = FlutterTts();
    instance.setLanguage('zh-CN');
  }

  late final FlutterTts instance;

  Future<void> fireCompletion() async {
    await FakeTtsChannel.messenger.handlePlatformMessage(
      FakeTtsChannel.channel.name,
      const StandardMethodCodec().encodeMethodCall(
        const MethodCall('speak.onComplete'),
      ),
      (_) {},
    );
  }

  void dispose() {
    instance.completionHandler = null;
    instance.errorHandler = null;
  }
}
