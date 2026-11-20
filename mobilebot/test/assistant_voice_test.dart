import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/asr_fallback.dart';
import 'package:mobilebot/core/voice_engine.dart';

void main() {
  group('decideAsrFallback', () {
    test('system start failed and local ready → local', () {
      expect(
        decideAsrFallback(
          systemStarted: false,
          localReady: true,
          alreadyFellBack: false,
          engine: AsrEngine.system,
        ),
        AsrFallbackChoice.local,
      );
    });

    test('system start failed and local not ready → retryLater', () {
      expect(
        decideAsrFallback(
          systemStarted: false,
          localReady: false,
          alreadyFellBack: false,
          engine: AsrEngine.system,
        ),
        AsrFallbackChoice.retryLater,
      );
    });

    test('already fell back once → retryLater', () {
      expect(
        decideAsrFallback(
          systemStarted: false,
          localReady: true,
          alreadyFellBack: true,
          engine: AsrEngine.system,
        ),
        AsrFallbackChoice.retryLater,
      );
      expect(
        decideAsrFallback(
          systemStarted: false,
          localReady: true,
          alreadyFellBack: true,
          engine: AsrEngine.local,
        ),
        AsrFallbackChoice.retryLater,
      );
    });

    test('selected local engine and local ready → local', () {
      expect(
        decideAsrFallback(
          systemStarted: false,
          localReady: true,
          alreadyFellBack: false,
          engine: AsrEngine.local,
        ),
        AsrFallbackChoice.local,
      );
    });
  });
}
