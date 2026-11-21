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

    test('system listen asks for partials and a real pause window', () {
      final params = SystemListenParams(localeId: 'zh_CN');
      expect(params.partialResults, isTrue);
      expect(params.listenFor, kSystemListenFor);
      expect(params.pauseFor, kSystemPauseFor);
      expect(params.listenFor, greaterThan(const Duration(seconds: 10)));
      expect(params.pauseFor, greaterThan(const Duration(seconds: 1)));
      expect(params.dictation, isTrue);
      expect(params.localeId, 'zh_CN');
    });

    test('partial hypotheses replace each other in the composer', () {
      expect(applyRecognitionHypothesis('', '你'), '你');
      expect(applyRecognitionHypothesis('', '你好'), '你好');
      expect(applyRecognitionHypothesis('已有', '你好'), '已有 你好');
      expect(applyRecognitionHypothesis('已有', '  '), '已有');
      expect(applyRecognitionHypothesis('', '  你好  '), '你好');
    });

    test('voice turn end keeps text typed after the hypothesis', () {
      expect(
        restoreComposerAfterHypothesis(
          prefix: '草稿',
          recognizedWords: '你好',
          draft: '草稿 你好',
        ),
        '草稿',
      );
      expect(
        restoreComposerAfterHypothesis(
          prefix: '草稿',
          recognizedWords: '你好',
          draft: '草稿 你好补充',
        ),
        '草稿补充',
      );
      expect(
        restoreComposerAfterHypothesis(
          prefix: '草稿',
          recognizedWords: '你好',
          draft: '改过预览',
        ),
        '改过预览',
      );
      expect(
        restoreComposerAfterHypothesis(
          prefix: '草稿',
          recognizedWords: '',
          draft: '草稿手打',
        ),
        '草稿手打',
      );
    });

    test('sound level does not retire the watchdog generation', () {
      expect(
        listenWatchdogStillOwns(armedGen: 3, currentGen: 3, listening: true),
        isTrue,
      );
      expect(
        listenWatchdogStillOwns(armedGen: 3, currentGen: 4, listening: true),
        isFalse,
      );
      expect(
        listenWatchdogStillOwns(armedGen: 3, currentGen: 3, listening: false),
        isFalse,
      );
    });

    test('a late stop callback cannot clear the next listen turn', () {
      expect(
        listenCallbackStillCurrent(
          callbackGen: 2,
          currentGen: 2,
          listening: true,
        ),
        isTrue,
      );
      expect(
        listenCallbackStillCurrent(
          callbackGen: 2,
          currentGen: 3,
          listening: true,
        ),
        isFalse,
      );
      expect(
        listenCallbackStillCurrent(
          callbackGen: 2,
          currentGen: 2,
          listening: false,
        ),
        isFalse,
      );
    });

    test('PCM RMS maps onto 0..1 and a missing level stays null', () {
      expect(normalizeListenLevel(null), isNull);
      expect(normalizeListenLevel(0), 0);
      expect(normalizeListenLevel(-1), 0);
      expect(normalizeListenLevel(kPcmRmsFullScale / 2), closeTo(0.5, 1e-9));
      expect(normalizeListenLevel(kPcmRmsFullScale * 3), 1);
    });

    test('listen level publishes at most once per throttle window', () {
      expect(kListenLevelThrottle, const Duration(milliseconds: 70));
      expect(kListenLevelFlushHold, const Duration(milliseconds: 16));
      expect(kListenLevelFlushHold, lessThan(kListenLevelThrottle));
      expect(
        kListenLevelThrottle,
        greaterThanOrEqualTo(const Duration(milliseconds: 60)),
      );
      expect(
        kListenLevelThrottle,
        lessThanOrEqualTo(const Duration(milliseconds: 80)),
      );

      final t0 = DateTime.utc(2026, 1, 1);
      // First chunk always publishes; a 32ms PCM tick inside the window does not.
      expect(
        shouldPublishListenLevel(now: t0, lastPublish: null),
        isTrue,
      );
      expect(
        shouldPublishListenLevel(
          now: t0.add(const Duration(milliseconds: 32)),
          lastPublish: t0,
        ),
        isFalse,
      );
      expect(
        shouldPublishListenLevel(
          now: t0.add(const Duration(milliseconds: 69)),
          lastPublish: t0,
        ),
        isFalse,
      );
      // The next window opens at the threshold, so that value still applies.
      expect(
        shouldPublishListenLevel(
          now: t0.add(kListenLevelThrottle),
          lastPublish: t0,
        ),
        isTrue,
      );
      expect(
        shouldPublishListenLevel(
          now: t0.add(const Duration(milliseconds: 96)),
          lastPublish: t0,
        ),
        isTrue,
      );
    });
  });
}
