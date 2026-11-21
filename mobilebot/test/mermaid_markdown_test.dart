import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/components/mermaid/mermaid.dart';

const _flow = '''
先看流程：

```mermaid
flowchart LR
  A[开始] --> B[结束]
```

普通说明还在。
''';

const _offscreen = '''
```mermaid
flowchart TD
  Hidden --> Later
```
''';

Widget _host(Widget child) {
  return MaterialApp(
    home: Scaffold(
      body: SingleChildScrollView(child: child),
    ),
  );
}

TapGestureRecognizer? _linkRecognizer(InlineSpan span) {
  if (span is TextSpan) {
    final recognizer = span.recognizer;
    if (recognizer is TapGestureRecognizer) return recognizer;
    for (final child in span.children ?? const <InlineSpan>[]) {
      final found = _linkRecognizer(child);
      if (found != null) return found;
    }
  }
  return null;
}

Widget _standIn(BuildContext context, MermaidRenderRequest request) {
  return Text('diagram:${request.source.trim().split('\n').first}');
}

void main() {
  testWidgets('plain markdown stays markdown and does not build a diagram', (
    tester,
  ) async {
    var built = false;
    await tester.pumpWidget(
      _host(
        MermaidMarkdown(
          data: '只有**正文**，没有图表。',
          selectable: true,
          diagramBuilder: (context, request) {
            built = true;
            return const Text('should-not-build');
          },
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(MarkdownBody), findsOneWidget);
    expect(find.byType(MermaidDiagram), findsNothing);
    expect(find.byKey(const Key('mermaid-lazy-placeholder')), findsNothing);
    expect(find.textContaining('只有', findRichText: true), findsOneWidget);
    expect(built, isFalse);
  });

  testWidgets('a closed fence becomes a diagram and other fences stay text', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(
        MermaidMarkdown(
          data: '''
```mermaid
flowchart LR
  A --> B
```

```dart
void main() {}
```
''',
          lazy: false,
          diagramBuilder: _standIn,
        ),
      ),
    );

    expect(find.text('diagram:flowchart LR'), findsOneWidget);
    expect(find.byType(MermaidDiagram), findsOneWidget);
    expect(find.textContaining('void main()', findRichText: true), findsOneWidget);
    expect(find.textContaining('```dart', findRichText: true), findsNothing);
  });

  testWidgets('an unclosed fence stays markdown until it closes', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(
        MermaidMarkdown(
          data: '```mermaid\nflowchart LR\n  A --> B',
          diagramBuilder: _standIn,
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(MermaidDiagram), findsNothing);
    expect(find.byKey(const Key('mermaid-lazy-placeholder')), findsNothing);
    expect(find.textContaining('flowchart LR', findRichText: true), findsOneWidget);
  });

  testWidgets('mmd alias and a second fence each get their own diagram', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(
        MermaidMarkdown(
          data: '''
```mmd
pie
  "a": 1
```

中间文字

```mermaid
sequenceDiagram
  Alice->>Bob: hi
```
''',
          lazy: false,
          diagramBuilder: _standIn,
        ),
      ),
    );

    expect(find.byType(MermaidDiagram), findsNWidgets(2));
    expect(find.text('diagram:pie'), findsOneWidget);
    expect(find.text('diagram:sequenceDiagram'), findsOneWidget);
    expect(find.textContaining('中间文字', findRichText: true), findsOneWidget);
  });

  testWidgets('an off-screen fence stays a placeholder until it is visible', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const SizedBox(height: 1200),
                MermaidMarkdown(data: _offscreen, diagramBuilder: _standIn),
              ],
            ),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(MermaidDiagram), findsNothing);
    expect(find.byKey(const Key('mermaid-lazy-placeholder')), findsOneWidget);
    expect(tester.getSize(find.byKey(const Key('mermaid-lazy-placeholder'))).height, 96);

    tester.state<ScrollableState>(find.byType(Scrollable)).position.jumpTo(1200);
    await tester.pump();

    expect(find.text('diagram:flowchart TD'), findsOneWidget);
    expect(find.byKey(const Key('mermaid-lazy-placeholder')), findsNothing);
  });

  testWidgets('a visible fence loads after layout without waiting for a scroll', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(MermaidMarkdown(data: _flow, diagramBuilder: _standIn)),
    );
    expect(find.byType(MermaidDiagram), findsNothing);

    await tester.pump();

    expect(find.text('diagram:flowchart LR'), findsOneWidget);
    expect(find.textContaining('先看流程', findRichText: true), findsOneWidget);
    expect(find.textContaining('普通说明还在', findRichText: true), findsOneWidget);
    expect(find.byKey(const Key('mermaid-lazy-placeholder')), findsNothing);
  });

  testWidgets('link taps, style, and selectability pass through unchanged', (
    tester,
  ) async {
    String? tapped;
    final sheet = MarkdownStyleSheet(p: const TextStyle(fontSize: 19));
    await tester.pumpWidget(
      _host(
        MermaidMarkdown(
          data: '详见 [文档](https://example.com/doc)。',
          selectable: true,
          styleSheet: sheet,
          onTapLink: (text, href, title) => tapped = href,
          diagramBuilder: _standIn,
        ),
      ),
    );
    await tester.pump();

    final body = tester.widget<MarkdownBody>(find.byType(MarkdownBody));
    expect(body.selectable, isTrue);
    expect(body.styleSheet, same(sheet));
    expect(body.onTapLink, isNotNull);

    final selectable = tester.widget<SelectableText>(find.byType(SelectableText));
    final span = selectable.textSpan;
    expect(span, isA<TextSpan>());
    final recognizer = _linkRecognizer(span!);
    expect(recognizer, isNotNull);
    recognizer!.onTap?.call();
    expect(tapped, 'https://example.com/doc');
  });

  testWidgets('dark and language reach the diagram built after visibility', (
    tester,
  ) async {
    MermaidRenderRequest? seen;
    String? language;
    await tester.pumpWidget(
      _host(
        MermaidMarkdown(
          data: '```mermaid\nflowchart LR\n  A --> B\n```',
          dark: true,
          languageCode: 'zh',
          diagramBuilder: (context, request) {
            seen = request;
            language = Localizations.localeOf(context).languageCode;
            return const Text('diagram-ready');
          },
        ),
      ),
    );
    await tester.pump();

    final diagram = tester.widget<MermaidDiagram>(find.byType(MermaidDiagram));
    expect(diagram.dark, isTrue);
    expect(diagram.languageCode, 'zh');
    expect(seen?.dark, isTrue);
    expect(find.text('diagram-ready'), findsOneWidget);
    // The widget forwards the code; it does not override the app locale.
    expect(language, isNot('zh'));
  });
}
