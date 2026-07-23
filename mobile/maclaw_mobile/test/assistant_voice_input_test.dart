import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/assistant/assistant_voice_input.dart';

void main() {
  test('maps speech language preferences to platform locales', () {
    expect(assistantSpeechLocaleForLanguage('en'), 'en_US');
    expect(assistantSpeechLocaleForLanguage('en-GB'), 'en_US');
    expect(assistantSpeechLocaleForLanguage('zh-Hant'), 'zh_TW');
    expect(assistantSpeechLocaleForLanguage('zh-HK'), 'zh_TW');
    expect(assistantSpeechLocaleForLanguage('zh-CN'), 'zh_CN');
    expect(assistantSpeechLocaleForLanguage(''), 'zh_CN');
  });

  test('filters stale terminal statuses but permits real timeouts', () {
    final startedAt = DateTime.utc(2026, 7, 23, 8);

    expect(
      assistantVoiceShouldForwardTerminalStatus(
        hasRecognizedSpeech: false,
        listeningStartedAt: startedAt,
        now: startedAt.add(const Duration(milliseconds: 100)),
      ),
      isFalse,
    );
    expect(
      assistantVoiceShouldForwardTerminalStatus(
        hasRecognizedSpeech: false,
        listeningStartedAt: startedAt,
        now: startedAt.add(assistantVoiceStartupGracePeriod),
      ),
      isTrue,
    );
    expect(
      assistantVoiceShouldForwardTerminalStatus(
        hasRecognizedSpeech: true,
        listeningStartedAt: startedAt,
        now: startedAt,
      ),
      isTrue,
    );
  });
}
