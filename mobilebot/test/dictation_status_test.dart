import 'package:audioplayers_platform_interface/audioplayers_platform_interface.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/dictation.dart';
import 'package:mobilebot/core/voice_engine.dart';
import 'package:path_provider_platform_interface/path_provider_platform_interface.dart';

import 'voice_engine_isolate_test.dart'
    show FakePlayers, FakeGlobal, FakePathProvider;

/// The line above the composer names the engine at work. Switching to the
/// local engine is the normal path on a phone whose system recognizer is
/// unavailable, so the line must never explain the switch: a report showed
/// 「系统识别不可用，已转本地识别」 under every dictation, which reads as a
/// failure while the text arrives fine.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late ComposerDictation dictation;

  setUp(() {
    // ComposerDictation builds its own AudioRecorder; the host has no record
    // plugin, and an unhandled channel answer fails the test after it ran.
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('com.llfbandit.record/messages'), (_) async => null);
    PathProviderPlatform.instance = FakePathProvider();
    AudioplayersPlatformInterface.instance = FakePlayers();
    GlobalAudioplayersPlatformInterface.instance = FakeGlobal();
    dictation = ComposerDictation(
      controller: TextEditingController(),
      engine: VoiceEngine(),
      toast: (_) {},
      localeId: () => 'zh_CN',
      onLocalModelNeeded: () {},
      nativeVoiceSupported: () => false,
      voiceInputSupported: true,
      messages: () => const DictationMessages(
        missingEngine: '缺引擎',
        downloading: '下载中',
        notHeard: '没听清',
        transcribeFailed: '识别失败',
        transcribeFailedWith: _withReason,
        micPermission: '没权限',
        micOpenFailed: '开麦失败',
      ),
    );
  });

  tearDown(() => dictation.dispose());

  String? status({required bool listening, required bool recognizing, required bool local}) {
    dictation.debugSetState(
      listening: listening,
      recognizing: recognizing,
      local: local,
    );
    return dictation.statusText(
      listenSystem: '使用系统识别…',
      listenLocal: '使用本地识别…',
      recognizingSystem: '系统识别中…',
      recognizingLocal: '本地识别中…',
    );
  }

  test('a local dictation says the local engine, not why it was chosen', () {
    expect(status(listening: true, recognizing: false, local: true), '使用本地识别…');
    expect(status(listening: true, recognizing: true, local: true), '本地识别中…');
  });

  test('the system engine is named the same way', () {
    expect(status(listening: true, recognizing: false, local: false), '使用系统识别…');
  });

  test('nothing is shown once listening stopped', () {
    expect(status(listening: false, recognizing: false, local: true), isNull);
  });
}

String _withReason(String reason) => '识别失败：$reason';
