import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:speech_to_text/speech_to_text.dart';

final assistantVoiceInputProvider = Provider<AssistantVoiceInput>(
  (ref) => SpeechToTextAssistantVoiceInput(),
);

const assistantVoiceStartupGracePeriod = Duration(milliseconds: 750);

/// Filters a terminal platform status that can belong to the recognizer's
/// preceding session. Native no-speech timeouts arrive after this short grace
/// period, while an actual transcript is accepted immediately.
bool assistantVoiceShouldForwardTerminalStatus({
  required bool hasRecognizedSpeech,
  required DateTime? listeningStartedAt,
  required DateTime now,
}) {
  if (hasRecognizedSpeech) return true;
  final startedAt = listeningStartedAt;
  return startedAt != null &&
      now.difference(startedAt) >= assistantVoiceStartupGracePeriod;
}

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
  int _sessionGeneration = 0;
  int _activeSessionGeneration = 0;
  bool _hasRecognizedSpeech = false;
  DateTime? _listeningStartedAt;
  ValueChanged<String>? _onStatus;
  bool _callbacksInstalled = false;
  Future<void> _platformOperationTail = Future<void>.value();

  SpeechToTextAssistantVoiceInput({SpeechToText? speech})
      : _speech = speech ?? SpeechToText();

  @override
  Future<bool> start({
    required String localeId,
    required ValueChanged<String> onText,
    ValueChanged<String>? onStatus,
  }) async {
    final sessionGeneration = ++_sessionGeneration;
    // Do not associate platform callbacks with a request that is merely
    // waiting behind another native operation. The recognizer keeps its
    // callbacks globally, so events during that wait have no reliable session
    // identity and must be ignored.
    _activeSessionGeneration = 0;
    _onStatus = onStatus;
    // Invalidate stale terminal events before this operation reaches the
    // platform. The queued operation also prevents listen/stop overlap while
    // a permission prompt or a native recognizer transition is still pending.
    _hasRecognizedSpeech = false;
    _listeningStartedAt = null;
    return _enqueuePlatformOperation(() async {
      if (sessionGeneration != _sessionGeneration) return false;
      try {
        if (!_initialized) {
          final initialized = await _speech.initialize(
            onStatus: _handleStatus,
            onError: (_) => _handleError(),
          );
          if (sessionGeneration != _sessionGeneration || !initialized) {
            if (!initialized && sessionGeneration == _sessionGeneration) {
              _initialized = false;
            }
            return false;
          }
          _initialized = true;
          _callbacksInstalled = true;
        }
        if (!_callbacksInstalled || sessionGeneration != _sessionGeneration) {
          return false;
        }
        await _speech.listen(
          listenOptions: SpeechListenOptions(
            localeId: localeId,
            partialResults: true,
            listenMode: ListenMode.dictation,
            // An error already closes the active session in [_handleError].
            // Let the plugin cancel its native recognizer too, so a permanent
            // Android/iOS error cannot leave the microphone service running.
            cancelOnError: true,
            // Avoid native defaults that can end an untouched session almost
            // immediately, while keeping a bounded session for battery use.
            pauseFor: const Duration(seconds: 5),
            listenFor: const Duration(minutes: 5),
          ),
          onResult: (result) {
            // Result callbacks, like status callbacks, are owned by the
            // plugin rather than tagged with a native session ID. Do not let a
            // final result from a stopped recognizer overwrite the draft after
            // a restart, or arrive after this session has already completed.
            if (_activeSessionGeneration != sessionGeneration ||
                sessionGeneration != _sessionGeneration) {
              return;
            }
            final text = result.recognizedWords.trim();
            if (text.isNotEmpty) {
              // A few recognizers deliver a result without first reporting a
              // listening status. That result still proves this is the active
              // session, so its following terminal status is not stale.
              _hasRecognizedSpeech = true;
              onText(text);
            }
          },
        );
        if (sessionGeneration != _sessionGeneration) return false;
        // A prior native recognizer can emit its final status while the next
        // `listen` method call is still in flight. Associate callbacks only
        // once that call returns, then publish the active state ourselves so
        // clients do not depend on the platform's timing for `listening`.
        _activeSessionGeneration = sessionGeneration;
        _listeningStartedAt = DateTime.now();
        _onStatus?.call(SpeechToText.listeningStatus);
        return true;
      } on Object {
        // Permission denial and platform speech-service failures should leave
        // the assistant usable for typed input.
        if (sessionGeneration == _sessionGeneration) {
          _initialized = false;
          _callbacksInstalled = false;
          _activeSessionGeneration = 0;
        }
        return false;
      }
    });
  }

  Future<T> _enqueuePlatformOperation<T>(Future<T> Function() operation) {
    final result = _platformOperationTail.then((_) => operation());
    _platformOperationTail = result.then<void>(
      (_) {},
      onError: (_) {},
    );
    return result;
  }

  @override
  Future<void> stop() async {
    _sessionGeneration++;
    _activeSessionGeneration = 0;
    _hasRecognizedSpeech = false;
    _listeningStartedAt = null;
    await _enqueuePlatformOperation(() async {
      try {
        await _speech.stop();
      } on Object {
        // Stopping is best effort; dispose must not surface a platform error.
      }
    });
  }

  /// `SpeechToText.initialize` keeps these handlers for the lifetime of the
  /// platform recognizer. They must therefore consult the mutable active
  /// session instead of closing over the very first [start] call.
  void _handleStatus(String status) {
    // Generation zero is intentionally the "no active session" sentinel.
    // In particular, before the first start both counters are zero, so a
    // lingering native callback must not be treated as a live session.
    if (_activeSessionGeneration == 0 ||
        _activeSessionGeneration != _sessionGeneration) {
      return;
    }
    if (status == SpeechToText.listeningStatus) {
      _listeningStartedAt = DateTime.now();
      _onStatus?.call(status);
      return;
    }
    final terminal = status == SpeechToText.doneStatus ||
        status == SpeechToText.notListeningStatus;
    if (terminal &&
        assistantVoiceShouldForwardTerminalStatus(
          hasRecognizedSpeech: _hasRecognizedSpeech,
          listeningStartedAt: _listeningStartedAt,
          now: DateTime.now(),
        )) {
      _finishActiveSession();
      _onStatus?.call(status);
    }
  }

  void _handleError() {
    if (_activeSessionGeneration != 0 &&
        _activeSessionGeneration == _sessionGeneration) {
      _finishActiveSession();
      _onStatus?.call('error');
    }
  }

  void _finishActiveSession() {
    _activeSessionGeneration = 0;
    _hasRecognizedSpeech = false;
    _listeningStartedAt = null;
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
