import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/assistant/assistant_input_history.dart';

void main() {
  group('buildAssistantInputRecallPrompts', () {
    test('prefers conversation newest-first then history, de-dupes', () {
      final prompts = buildAssistantInputRecallPrompts(
        conversationUserMessages: const [
          'first question',
          'second question',
          'First Question', // de-dupe against first
        ],
        historyQueries: const [
          'second question', // already present
          'from history',
        ],
        maxItems: 10,
      );
      expect(prompts, [
        'First Question',
        'second question',
        'from history',
      ]);
    });

    test('respects maxItems', () {
      final prompts = buildAssistantInputRecallPrompts(
        conversationUserMessages: List.generate(20, (i) => 'q$i'),
        maxItems: 5,
      );
      expect(prompts.length, 5);
      expect(prompts.first, 'q19');
    });
  });

  group('filterAssistantInputRecallPrompts', () {
    test('filters case-insensitively', () {
      final filtered = filterAssistantInputRecallPrompts(
        const ['Check nginx', '磁盘空间', 'nginx reload'],
        'NGINX',
      );
      expect(filtered, ['Check nginx', 'nginx reload']);
    });
  });

  group('AssistantInputHistoryBrowser', () {
    test('steps older then newer and restores draft', () {
      final browser = AssistantInputHistoryBrowser();
      const prompts = ['newest', 'mid', 'oldest'];

      expect(browser.stepOlder(prompts, currentInput: 'draft text'), 'newest');
      expect(browser.isBrowsing, isTrue);
      expect(browser.positionDisplay(prompts), 1);

      expect(browser.stepOlder(prompts, currentInput: 'ignored'), 'mid');
      expect(browser.positionDisplay(prompts), 2);

      expect(browser.stepOlder(prompts, currentInput: 'ignored'), 'oldest');
      // Stays on oldest
      expect(browser.stepOlder(prompts, currentInput: 'ignored'), 'oldest');

      expect(browser.stepNewer(prompts), 'mid');
      expect(browser.stepNewer(prompts), 'newest');
      expect(browser.stepNewer(prompts), 'draft text');
      expect(browser.isBrowsing, isFalse);
    });

    test('empty prompts do nothing', () {
      final browser = AssistantInputHistoryBrowser();
      expect(browser.stepOlder(const [], currentInput: 'x'), isNull);
      expect(browser.isBrowsing, isFalse);
    });
  });
}
