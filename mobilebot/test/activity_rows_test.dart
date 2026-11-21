import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/features/remote/activity_rows.dart';
import 'package:mobilebot/features/remote/viewer_reducer.dart';

ViewerTimelineRow row(String text, int seq) =>
    ViewerTimelineRow(text: text, seq: seq);

String toolEvent({
  required String event,
  required String detail,
  String target = '',
  String outcome = '',
  String phase = 'running',
  String title = 'T1 改界面',
}) {
  final outcomePart = outcome.isEmpty ? '' : ',"outcome":"$outcome"';
  return 'Coding Agent Event: {"agent":"coding","event":"$event",'
      '"phase":"$phase","task_id":"T1","title":"$title",'
      '"detail":"$detail","target":"$target"$outcomePart}';
}

void main() {
  group('parseCodingEvent', () {
    test('reads the coding event payload', () {
      final parsed = parseCodingEvent(
        toolEvent(event: 'tool_started', detail: 'read_file', target: 'a/b.dart'),
      );
      expect(parsed, isNotNull);
      expect(parsed!.event, 'tool_started');
      expect(parsed.detail, 'read_file');
      expect(parsed.target, 'a/b.dart');
      expect(parsed.title, 'T1 改界面');
      expect(parsed.phase, 'running');
    });

    test('keeps a legacy Coding Agent: line as a phase row', () {
      final parsed = parseCodingEvent('Coding Agent: running T2 - 跑测试');
      expect(parsed, isNotNull);
      expect(parsed!.event, isEmpty);
      expect(parsed.phase, 'running');
      expect(parsed.title, '跑测试');
      expect(parsed.target, 'T2');
    });

    test('returns null for prose and for another agent', () {
      expect(parseCodingEvent('开始读文件'), isNull);
      expect(
        parseCodingEvent('Coding Agent Event: {"agent":"other","event":"x"}'),
        isNull,
      );
      expect(parseCodingEvent('Coding Agent Event: {broken'), isNull);
    });
  });

  group('toolKindFor', () {
    test('maps the desktop tool table', () {
      expect(toolKindFor('bash'), ToolKind.run);
      expect(toolKindFor('ssh_read_file'), ToolKind.read);
      expect(toolKindFor('apply_patch'), ToolKind.edit);
      expect(toolKindFor('ripgrep'), ToolKind.search);
      expect(toolKindFor('list_directory'), ToolKind.find);
      expect(toolKindFor('delegate_task'), ToolKind.agent);
    });

    test('falls back to the desktop phrase list, then other', () {
      expect(toolKindFor('读取文件'), ToolKind.read);
      expect(toolKindFor('unknown_tool', action: '执行命令'), ToolKind.run);
      expect(toolKindFor('unknown_tool'), ToolKind.other);
      expect(toolKindFor(''), ToolKind.other);
    });
  });

  group('foldActivityRows', () {
    test('keeps arrival order across thinking and tool rows', () {
      final state = emptyViewerState()
        ..thinking = [
          ViewerThinking(text: '先看代码', done: true, seq: 0),
          ViewerThinking(text: '再改代码', done: false, seq: 2),
        ]
        ..timeline = [
          row(toolEvent(event: 'tool_started', detail: 'read_file'), 1),
          row(
            toolEvent(
              event: 'tool_finished',
              detail: 'read_file',
              outcome: 'success',
            ),
            3,
          ),
        ];
      final rows = foldActivityRows(state).where((r) => !r.isSummary).toList();
      expect(
        [for (final r in rows) r.kind],
        [
          ActivityRowKind.thinking,
          ActivityRowKind.tool,
          ActivityRowKind.thinking,
          ActivityRowKind.tool,
        ],
      );
      expect(rows[1].toolName, 'read_file');
      expect(rows[1].toolState, ToolState.running);
      expect(rows[1].toolKind, ToolKind.read);
      expect(rows[3].toolState, ToolState.done);
    });

    test('a failed tool finish is marked as an error', () {
      final state = emptyViewerState()
        ..timeline = [
          row(
            toolEvent(
              event: 'tool_finished',
              detail: 'bash',
              outcome: 'failure',
            ),
            1,
          ),
        ];
      final tool = foldActivityRows(
        state,
      ).firstWhere((r) => r.kind == ActivityRowKind.tool);
      expect(tool.toolState, ToolState.failed);
      expect(tool.asError, isTrue);
      expect(tool.toolKind, ToolKind.run);
    });

    test('a task_status event stays a banner, not a tool', () {
      final state = emptyViewerState()
        ..timeline = [
          row(toolEvent(event: 'task_status', detail: ''), 1),
        ];
      final rows = foldActivityRows(state).where((r) => !r.isSummary).toList();
      expect(rows, hasLength(1));
      expect(rows.single.kind, ActivityRowKind.status);
      expect(rows.single.title, 'T1 改界面');
    });

    test('summary counts only the rows that survive the filters', () {
      final state = emptyViewerState()
        ..thinking = [
          ViewerThinking(text: '   ', done: true, seq: 0),
          ViewerThinking(text: '有效思考', done: true, seq: 1),
        ]
        ..timeline = [
          row('', 2),
          row(toolEvent(event: 'tool_started', detail: 'bash'), 3),
        ]
        ..notices = [ViewerNotice(text: '注意一件事', seq: 4)];
      final summary = foldActivityRows(state).first;
      expect(summary.isSummary, isTrue);
      expect(summary.thinkingCount, 1);
      expect(summary.toolCount, 1);
      expect(summary.noteCount, 1);
    });

    test('suppressed thinking and history notes are dropped', () {
      final state = emptyViewerState()
        ..thinking = [ViewerThinking(text: '历史里已有', done: true, seq: 0)]
        ..notices = [
          ViewerNotice(text: '重复行', seq: 1),
          ViewerNotice(text: '新行', seq: 2),
        ];
      final rows = foldActivityRows(
        state,
        suppressThinking: true,
        historyTexts: {'重复行'},
      ).where((r) => !r.isSummary).toList();
      expect(rows, hasLength(1));
      expect(rows.single.text, '新行');
    });

    test('trial and recovery notes sort after stamped rows', () {
      final state = emptyViewerState()
        ..trialStatus = '结果：通过'
        ..trialFailures = 0
        ..timeline = [row(toolEvent(event: 'tool_started', detail: 'bash'), 9)];
      final rows = foldActivityRows(state, recovery: '正在补齐').where(
        (r) => !r.isSummary,
      ).toList();
      expect(rows.last.text, '正在补齐');
      expect(rows[rows.length - 2].text, '结果：通过');
    });

    test('an empty turn yields no rows and no summary', () {
      expect(foldActivityRows(emptyViewerState()), isEmpty);
    });
  });
}
