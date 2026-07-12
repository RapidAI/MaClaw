import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/assistant/assistant_markdown.dart';
import 'package:maclaw_mobile/shared/theme.dart';

void main() {
  test('looksLikeAssistantMarkdown detects tables and headings', () {
    expect(looksLikeAssistantMarkdown('plain sentence only'), isFalse);
    expect(
      looksLikeAssistantMarkdown('## 结论\n\n今天晴。\n\n| 项 | 值 |\n| --- | --- |\n| 温度 | 20°C |'),
      isTrue,
    );
    expect(looksLikeAssistantMarkdown('- 要点一\n- 要点二'), isTrue);
  });

  test('prepareAssistantMarkdown strips HTML entities', () {
    final prepared = prepareAssistantMarkdown('结论&ensp;：晴&#183;多云');
    expect(prepared, isNot(contains('&ensp;')));
    expect(prepared, isNot(contains('&#')));
    expect(prepared, contains('结论'));
  });

  test('prepareChatBodyForDisplay strips line-leading pictographs only', () {
    expect(prepareChatBodyForDisplay('\u{1F680} Done.'), 'Done.');
    expect(prepareChatBodyForDisplay('### \u{1F3AF} Goals'), '### Goals');
    expect(prepareChatBodyForDisplay('- \u{1F4CC} note'), '- note');
    expect(prepareChatBodyForDisplay('Score \u2B50\u2B50 high'), 'Score \u2B50\u2B50 high');
    expect(
      prepareChatBodyForDisplay('```\n\u{1F680} keep\n```'),
      '```\n\u{1F680} keep\n```',
    );
  });

  test('prepareAssistantMarkdown strips leading pictographs', () {
    final prepared = prepareAssistantMarkdown('\u{1F680} 部署完成');
    expect(prepared, '部署完成');
    expect(prepared, isNot(contains('\u{1F680}')));
  });

  testWidgets('AssistantMarkdownBody renders table and heading text',
      (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: buildMaClawTheme(Brightness.light),
        home: const Scaffold(
          body: SingleChildScrollView(
            padding: EdgeInsets.all(16),
            child: AssistantMarkdownBody(
              data: '''
## 结论

北京今天以晴到多云为主。

| 时段 | 天气 | 气温 |
| --- | --- | --- |
| 白天 | 晴 | 22°C |
| 夜间 | 多云 | 12°C |

- 注意带薄外套
''',
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('结论'), findsWidgets);
    expect(find.textContaining('北京今天以晴到多云为主'), findsWidgets);
    expect(find.textContaining('白天'), findsWidgets);
    expect(find.textContaining('22°C'), findsWidgets);
    expect(find.textContaining('薄外套'), findsWidgets);
    expect(find.textContaining('时段'), findsWidgets);
    expect(find.byType(Table), findsOneWidget);
  });
}
