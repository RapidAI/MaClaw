import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/features/remote/activity_rows.dart';
import 'package:mobilebot/features/remote/history_rows.dart';

Map<String, dynamic> _call(String name, String args) => {
  'id': 'call_$name',
  'type': 'function',
  'function': {'name': name, 'arguments': args},
};

void main() {
  group('history tool rows', () {
    test('one stored call becomes one named row with its target', () {
      final rows = historyToolRows([
        {
          'role': 'assistant',
          'content': '',
          'tool_calls': [_call('read_file', '{"path":"E:/app/icon.dart"}')],
        },
      ]);
      expect(rows, hasLength(1));
      final tool = rows.single.tool!;
      expect(tool.name, 'read_file');
      expect(tool.target, 'E:/app/icon.dart');
      expect(tool.state, ToolState.done);
      expect(historyToolKind(tool.name), ToolKind.read);
    });

    test('a command call shows the command and maps to the run icon', () {
      final rows = historyToolRows([
        {
          'role': 'assistant',
          'content': '',
          'tool_calls': [_call('bash', '{"command":"flutter test"}')],
        },
      ]);
      expect(rows.single.tool!.target, 'flutter test');
      expect(historyToolKind('bash'), ToolKind.run);
    });

    test('several calls in one turn keep their stored order', () {
      final rows = historyToolRows([
        {
          'role': 'assistant',
          'content': '我先看两个文件',
          'tool_calls': [
            _call('read_file', '{"path":"a.dart"}'),
            _call('grep', '{"pattern":"icon"}'),
          ],
        },
      ]);
      expect(rows.map((r) => r.tool!.name), ['read_file', 'grep']);
      expect(historyToolKind('grep'), ToolKind.search);
    });

    test('a tool completion row adds the call its outcome', () {
      final rows = historyToolRows([
        {
          'role': 'tool',
          'tool_name': 'edit_file',
          'tool_outcome': 'failure',
          'content': 'a.dart',
        },
      ]);
      expect(rows.single.tool!.state, ToolState.failed);
      expect(rows.single.asError, isTrue);
    });

    test('a completion row that repeats a listed call is not drawn twice', () {
      final rows = historyToolRows([
        {
          'role': 'assistant',
          'content': '',
          'tool_calls': [_call('read_file', '{"path":"a.dart"}')],
        },
        {'role': 'tool', 'tool_name': 'read_file', 'content': 'a.dart'},
      ]);
      expect(rows, hasLength(1));
    });

    test('a failed turn marks its calls failed', () {
      final rows = historyToolRows([
        {
          'role': 'assistant',
          'content': '',
          'tool_outcome': 'failure',
          'tool_calls': [_call('bash', '{"command":"go build"}')],
        },
      ]);
      expect(rows.single.tool!.state, ToolState.failed);
    });

    test('a plain reply without calls has no rows', () {
      expect(historyToolRows([{'role': 'assistant', 'content': '好了'}]), isEmpty);
    });
  });

  group('history display', () {
    test('an assistant turn keeps prose separate from its call rows', () {
      final display = historyDisplayOf({
        'role': 'assistant',
        'content': '改完了',
        'tool_calls': [_call('edit_file', '{"path":"a.dart"}')],
      });
      expect(display.answer, '改完了');
      expect(display.toolRows, hasLength(1));
      expect(display.isEmpty, isFalse);
    });

    test('a call-only turn has rows and no answer body', () {
      final display = historyDisplayOf({
        'role': 'assistant',
        'content': '',
        'tool_calls': [_call('read_file', '{"path":"a.dart"}')],
      });
      expect(display.answer, isEmpty);
      expect(display.isEmpty, isFalse);
    });

    test('a person message stays prose', () {
      final display = historyDisplayOf({'role': 'user', 'content': '你好'});
      expect(display.answer, '你好');
      expect(display.toolRows, isEmpty);
    });
  });
}
