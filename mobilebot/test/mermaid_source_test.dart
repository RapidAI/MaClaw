import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/components/mermaid/mermaid.dart';

void main() {
  group('prepareMermaidSource', () {
    test('accepts a flowchart and keeps the body', () {
      const raw = 'flowchart LR\n  A[开始] --> B[结束]';
      final decision = prepareMermaidSource(raw);
      expect(decision.accepted, isTrue);
      expect(decision.source, raw);
    });

    test('repairs model casing without touching labels', () {
      final decision = prepareMermaidSource(
        'SequenceDiagram\n  Alice->>Bob: Graph TD stays\n  End',
      );
      expect(decision.accepted, isTrue);
      expect(decision.source, startsWith('sequenceDiagram\n'));
      expect(decision.source, contains('Graph TD stays'));
    });

    test('rejects empty, oversized, and over-budget sources', () {
      expect(
        prepareMermaidSource('   ').reason,
        MermaidRejectReason.empty,
      );
      expect(
        prepareMermaidSource('A' * (maxMermaidSourceLength + 1)).reason,
        MermaidRejectReason.tooLong,
      );
      final edges = List.filled(maxMermaidEdgeCount + 1, 'A --> B').join('\n');
      expect(
        prepareMermaidSource('flowchart LR\n$edges').reason,
        MermaidRejectReason.overBudget,
      );
    });
  });

  group('splitMermaidFences', () {
    test('lifts mermaid fences and leaves other fences in place', () {
      const md = '''
先看流程：

```mermaid
flowchart TD
  A --> B
```

然后是代码：

```dart
void main() {}
```
''';
      final split = splitMermaidFences(md);
      expect(split.fences, hasLength(1));
      expect(split.fences.single.language, 'mermaid');
      expect(split.fences.single.body, contains('A --> B'));
      expect(split.markdown, isNot(contains('flowchart TD')));
      expect(split.markdown, contains('```dart'));
      expect(split.markdown, contains('先看流程'));
    });

    test('ignores an unclosed streaming fence', () {
      const md = '```mermaid\nflowchart LR\n  A --> B';
      expect(extractMarkdownFences(md), isEmpty);
      expect(splitMermaidFences(md).markdown, md);
    });

    test('recognises the mmd alias and skips indented fences', () {
      const md = '文本\n\n```mmd\npie\n  "a": 1\n```\n\n    ```mermaid\n    not a fence\n    ```\n';
      final fences = extractMarkdownFences(md);
      expect(fences, hasLength(1));
      expect(fences.single.isMermaid, isTrue);
    });
  });

  group('mermaid document', () {
    test('script embeds source as JSON and stays strict', () {
      final script = mermaidRenderScript(
        source: 'flowchart LR\n  A["</script>"] --> B',
        dark: true,
      );
      expect(script, contains('securityLevel: "strict"'));
      expect(script, contains('theme: "dark"'));
      expect(script, contains('\\u003C/script\\u003E'));
      expect(script, isNot(contains('</script>')));
      expect(script, contains('MermaidHost.postMessage'));
    });

    test('does not build a script for a rejected source', () {
      expect(mermaidRenderScriptForSource('  '), isNull);
    });

    test('parses host events', () {
      final ok = MermaidHostEvent.tryParse('{"ok":true,"height":240}');
      expect(ok?.ok, isTrue);
      expect(ok?.height, 240);
      final failed = MermaidHostEvent.tryParse(
        r'{"ok":false,"error":"Parse error\nline 2"}',
      );
      expect(failed?.ok, isFalse);
      expect(failed?.error, 'Parse error\nline 2');
      expect(MermaidHostEvent.tryParse('not json'), isNull);
    });

    test('shell is offline and points at the bundled script', () {
      final html = buildMermaidShellHtml();
      expect(html, contains('src="mermaid.min.js"'));
      expect(html, isNot(contains('http://')));
      expect(html, isNot(contains('https://')));
      expect(mermaidShellAsset, 'assets/mermaid/mermaid_shell.html');
      expect(mermaidScriptAsset, 'assets/mermaid/mermaid.min.js');
    });

    test('composed page inlines the engine and stays offline', () {
      final page = composeMermaidPage(
        shellHtml: buildMermaidShellHtml(),
        engineSource: 'var x = "</script>";',
      );
      expect(page, isNot(contains('src="mermaid.min.js"')));
      expect(page, contains(r'<\/script>'));
      expect(page, isNot(contains('</script>";')));
      expect(page, isNot(contains('http://')));
      expect(page, isNot(contains('https://')));
    });
  });
}
