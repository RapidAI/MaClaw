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
    ValueChanged<String>? onStatus,
  });

  Future<void> stop();
}

class SpeechToTextAssistantVoiceInput implements AssistantVoiceInput {
  final SpeechToText _speech;
  bool _initialized = false;
  ValueChanged<String>? _onStatus;

  SpeechToTextAssistantVoiceInput({SpeechToText? speech})
      : _speech = speech ?? SpeechToText();

  @override
  Future<bool> start({
    required String localeId,
    required ValueChanged<String> onText,
    ValueChanged<String>? onStatus,
  }) async {
    try {
      _onStatus = onStatus;
      _initialized = _initialized ||
          await _speech.initialize(
            onStatus: (status) => _onStatus?.call(status),
            onError: (_) => _onStatus?.call('error'),
          );
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
    } on Object {
      // Permission denial and platform speech-service failures should leave
      // the assistant usable for typed input.
      _initialized = false;
      return false;
    }
  }

  @override
  Future<void> stop() async {
    try {
      await _speech.stop();
    } on Object {
      // Stopping is best effort; dispose must not surface a platform error.
    }
  }
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
