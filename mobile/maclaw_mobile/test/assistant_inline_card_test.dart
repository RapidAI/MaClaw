import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/assistant/assistant_inline_card.dart';

void main() {
  testWidgets('AssistantInlineCard renders title actions and resolves',
      (tester) async {
    final tapped = <String>[];

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AssistantInlineCard(
            title: '可以继续',
            description: '把回答落到草稿或员工。',
            summaryLines: const ['问：状态如何'],
            testId: 'card-1',
            actions: assistantDefaultNextStepActions(),
            onAction: tapped.add,
          ),
        ),
      ),
    );

    expect(find.text('可以继续'), findsOneWidget);
    expect(find.text('把回答落到草稿或员工。'), findsOneWidget);
    expect(find.text('问：状态如何'), findsOneWidget);
    expect(find.text('整理为草稿'), findsOneWidget);
    expect(find.text('派给员工'), findsOneWidget);

    await tester.tap(find.text('整理为草稿'));
    await tester.pump();
    expect(tapped, ['draft']);
  });

  testWidgets('AssistantInlineCard hides actions when resolved', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: AssistantInlineCard(
            title: '可以继续',
            resolved: true,
            resolvedLabel: '已打开数字员工',
            actions: [
              AssistantInlineCardAction(key: 'employee', label: '派给员工'),
            ],
          ),
        ),
      ),
    );

    expect(find.text('已打开数字员工'), findsOneWidget);
    expect(find.text('派给员工'), findsNothing);
  });

  test('assistantDefaultNextStepActions includes core task keys', () {
    final keys =
        assistantDefaultNextStepActions().map((action) => action.key).toList();
    expect(keys, containsAll(['draft', 'employee', 'documents']));
    expect(keys, isNot(contains('share')));
    expect(keys, isNot(contains('copy')));
  });
}
