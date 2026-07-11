import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/features/assistant/assistant_controller.dart';

void main() {
  test('shouldUpgradeAssistantToBackground uses max interactive budget', () {
    expect(shouldUpgradeAssistantToBackground(Duration.zero), isFalse);
    expect(
      shouldUpgradeAssistantToBackground(assistantSyncMaxInteractiveTimeout),
      isTrue,
    );
    expect(
      shouldUpgradeAssistantToBackground(
        assistantSyncMaxInteractiveTimeout + const Duration(seconds: 1),
      ),
      isTrue,
    );
    expect(
      shouldUpgradeAssistantToBackground(const Duration(seconds: 35)),
      isFalse,
    );
  });

  test('shouldUpgradeAssistantOnIdle uses idle budget', () {
    expect(shouldUpgradeAssistantOnIdle(Duration.zero), isFalse);
    expect(
      shouldUpgradeAssistantOnIdle(assistantSyncIdleUpgradeTimeout),
      isTrue,
    );
    expect(
      shouldUpgradeAssistantOnIdle(const Duration(seconds: 30)),
      isFalse,
    );
  });

  test('assistantBackgroundHandoffText is not a hard failure message', () {
    const job = MobileAgentJob(jobId: 'mobagent_1', status: 'running');
    final text = assistantBackgroundHandoffText(job, 'timeout');
    expect(text, contains('mobagent_1'));
    expect(text, contains('后台'));
    expect(text, isNot(contains('超过 35s')));
    expect(text, isNot(contains('出错')));
  });

  test('assistantBackgroundProgressText includes status', () {
    const job = MobileAgentJob(
      jobId: 'mobagent_2',
      status: 'running',
      message: 'ssh connect',
      progress: 0.4,
    );
    final text = assistantBackgroundProgressText(job);
    expect(text, contains('mobagent_2'));
    expect(text, contains('running'));
    expect(text, contains('40%'));
    expect(text, contains('ssh connect'));
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
