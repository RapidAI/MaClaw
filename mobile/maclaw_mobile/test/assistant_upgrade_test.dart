import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/assistant/assistant_controller.dart';

void main() {
  test('shouldUpgradeAssistantToBackground uses sync budget', () {
    expect(shouldUpgradeAssistantToBackground(Duration.zero), isFalse);
    expect(
      shouldUpgradeAssistantToBackground(assistantSyncUpgradeTimeout),
      isTrue,
    );
    expect(
      shouldUpgradeAssistantToBackground(
        assistantSyncUpgradeTimeout + const Duration(seconds: 1),
      ),
      isTrue,
    );
  });

  test('extractMaclawDocumentEditFence parses rewrite block', () {
    const answer = '''
建议如下：

```maclaw-document-edit
# 新标题

改写后的正文
```

记得审阅。
''';
    expect(extractMaclawDocumentEditFence(answer), '# 新标题\n\n改写后的正文');
    expect(extractMaclawDocumentEditFence('no fence'), isNull);
  });
}
