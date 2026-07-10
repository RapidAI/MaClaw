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
}
