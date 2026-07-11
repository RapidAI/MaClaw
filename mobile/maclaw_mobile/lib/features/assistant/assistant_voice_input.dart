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

/// Maps app language preference (or effective UI language) to speech locale.
///
/// `system` and unknown non-Chinese codes resolve like the UI: Chinese only
/// when the effective language is zh*; otherwise English.
String assistantSpeechLocaleForLanguage(String language) {
  final normalized = language.trim().toLowerCase();
  if (normalized == 'system' || normalized == 'auto' || normalized.isEmpty) {
    // Defer to UI resolution rule (platform zh → Chinese, else English).
    // Callers should pass the already-resolved UI language when possible.
    return 'zh_CN';
  }
  if (normalized.startsWith('en')) {
    return 'en_US';
  }
  if (normalized.startsWith('zh-hant') ||
      normalized.startsWith('zh-tw') ||
      normalized.startsWith('zh-hk') ||
      normalized.startsWith('zh_tw') ||
      normalized.startsWith('zh_hk')) {
    return 'zh_TW';
  }
  if (normalized == 'zh' ||
      normalized.startsWith('zh_') ||
      normalized.startsWith('zh-') ||
      normalized == 'zh_cn') {
    return 'zh_CN';
  }
  // Non-Chinese explicit languages → English speech.
  return 'en_US';
}
