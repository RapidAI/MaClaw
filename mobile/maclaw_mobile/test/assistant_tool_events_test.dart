import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';

void main() {
  test('summarizeAssistantToolEvents collapses repeated ssh chips', () {
    final events = <AssistantToolEvent>[
      for (var i = 0; i < 9; i++) ...[
        const AssistantToolEvent(kind: 'call', id: 'ssh', name: 'ssh'),
        const AssistantToolEvent(kind: 'result', id: 'ssh', name: 'ssh'),
      ],
      const AssistantToolEvent(kind: 'call', id: 'ssh', name: 'ssh'),
    ];
    final summaries = summarizeAssistantToolEvents(events);
    expect(summaries, hasLength(1));
    expect(summaries.single.name, 'ssh');
    expect(summaries.single.callCount, 10);
    expect(summaries.single.resultCount, 9);
    expect(summaries.single.inProgress, isTrue);
  });

  test('summarizeAssistantToolEvents keeps order across tools', () {
    final events = const [
      AssistantToolEvent(kind: 'call', id: '1', name: 'web_search'),
      AssistantToolEvent(kind: 'result', id: '1', name: 'web_search'),
      AssistantToolEvent(kind: 'call', id: '2', name: 'ssh'),
      AssistantToolEvent(kind: 'result', id: '2', name: 'ssh'),
    ];
    final summaries = summarizeAssistantToolEvents(events);
    expect(summaries.map((s) => s.name).toList(), ['web_search', 'ssh']);
  });

  test('retainRecentAssistantToolEvents keeps tail', () {
    final events = [
      for (var i = 0; i < 50; i++)
        AssistantToolEvent(kind: 'call', id: '$i', name: 'ssh'),
    ];
    final kept = retainRecentAssistantToolEvents(events, maxEvents: 40);
    expect(kept, hasLength(40));
    expect(kept.first.id, '10');
    expect(kept.last.id, '49');
  });
}
