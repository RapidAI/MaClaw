import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:speech_to_text/speech_to_text.dart';

final assistantVoiceInputProvider = Provider<AssistantVoiceInput>(
  (ref) => SpeechToTextAssistantVoiceInput(),
);

abstract class AssistantVoiceInput {
  Future<bool> start({
    required String localeId,
    required ValueChanged<String> onText,
  });

  Future<void> stop();
}

class SpeechToTextAssistantVoiceInput implements AssistantVoiceInput {
  final SpeechToText _speech;
  bool _initialized = false;

  SpeechToTextAssistantVoiceInput({SpeechToText? speech})
      : _speech = speech ?? SpeechToText();

  @override
  Future<bool> start({
    required String localeId,
    required ValueChanged<String> onText,
  }) async {
    _initialized = _initialized || await _speech.initialize();
    if (!_initialized) return false;
    await _speech.listen(
      listenOptions: SpeechListenOptions(localeId: localeId),
      onResult: (result) {
        final text = result.recognizedWords.trim();
        if (text.isNotEmpty) {
          onText(text);
        }
      },
    );
    return true;
  }

  @override
  Future<void> stop() => _speech.stop();
}

String assistantSpeechLocaleForLanguage(String language) {
  final normalized = language.trim().toLowerCase();
  if (normalized.startsWith('en')) {
    return 'en_US';
  }
  if (normalized.startsWith('zh-hant') ||
      normalized.startsWith('zh-tw') ||
      normalized.startsWith('zh-hk')) {
    return 'zh_TW';
  }
  return 'zh_CN';
}
