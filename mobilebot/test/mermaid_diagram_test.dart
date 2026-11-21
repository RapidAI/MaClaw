import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/components/mermaid/mermaid.dart';

void main() {
  testWidgets('accepted source reaches the stand-in, not a platform view', (
    tester,
  ) async {
    MermaidRenderRequest? seen;
    await tester.pumpWidget(
      MaterialApp(
        home: MermaidDiagram(
          source: 'flowchart LR\n  A --> B',
          dark: false,
          viewBuilder: (context, request) {
            seen = request;
            return const Text('diagram-stand-in');
          },
        ),
      ),
    );

    expect(find.text('diagram-stand-in'), findsOneWidget);
    expect(seen, isNotNull);
    expect(seen!.source, contains('A --> B'));
    expect(seen!.script, contains('securityLevel: "strict"'));
    expect(seen!.dark, isFalse);
  });

  testWidgets('empty source shows the refusal and skips the view', (
    tester,
  ) async {
    var built = false;
    await tester.pumpWidget(
      MaterialApp(
        home: MermaidDiagram(
          source: '   ',
          languageCode: 'zh',
          viewBuilder: (context, request) {
            built = true;
            return const SizedBox.shrink();
          },
        ),
      ),
    );

    expect(built, isFalse);
    expect(find.text('Mermaid 图表是空的。'), findsOneWidget);
  });

  testWidgets('over-budget source keeps the original text visible', (
    tester,
  ) async {
    final edges = List.filled(maxMermaidEdgeCount + 1, 'A --> B').join('\n');
    final source = 'flowchart LR\n$edges';
    await tester.pumpWidget(
      MaterialApp(
        locale: const Locale('en'),
        home: MermaidDiagram(
          source: source,
          languageCode: 'en',
          viewBuilder: (context, request) => const Text('should-not-build'),
        ),
      ),
    );

    expect(find.text('should-not-build'), findsNothing);
    expect(
      find.textContaining('rendering limit'),
      findsOneWidget,
    );
    expect(find.textContaining('A --> B'), findsOneWidget);
  });
}
