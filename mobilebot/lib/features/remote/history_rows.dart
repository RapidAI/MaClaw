// Typed fold of one stored conversation turn, the history counterpart of
// activity_rows.dart.
//
// A coding-agent transcript keeps tool calls in `tool_calls` and tool output in
// `tool` rows. remoteControlCodingAgentHistory drops those rows before sending
// the phone, so the stored turn used to render as one run-on paragraph. The
// desktop transcript (guiapp Overview message list) shows each call on its own
// row with an icon, the target and an outcome pill; the phone now has the same
// row set.
//
// Three sources feed the fold, in transcript order:
//   * assistant `tool_calls` -> one tool row per call (finished, or failed when
//     the outcome says so),
//   * `tool` rows -> one completion row per call id (failed when it says so),
//   * `name = 'tool'` / `task_done` user rows -> nothing (host bookkeeping).
// Thinking stays its own block, notes keep their own line, and the assistant
// prose stays the answer body. No durations are invented: the transcript has
// no wall-clock stamps, so an elapsed value is absent rather than "0s".

import 'dart:convert';

import 'activity_rows.dart';

/// One row of a stored turn.
enum HistoryRowKind {
  /// One tool call.
  tool,

  /// A short line that is not a tool: confirmation, trial, notice.
  note,
}

/// A single tool call in history. [state] follows the tool row conventions in
/// activity_rows.dart so both surfaces paint an outcome the same way.
class HistoryToolCall {
  const HistoryToolCall({
    required this.name,
    this.target = '',
    this.state = ToolState.unknown,
    this.asError = false,
  });

  final String name;
  final String target;
  final ToolState state;
  final bool asError;
}

/// A row to paint. [tool] is set for [HistoryRowKind.tool], [text] for notes.
class HistoryRow {
  const HistoryRow({
    required this.kind,
    this.tool,
    this.text = '',
    this.asError = false,
  });

  final HistoryRowKind kind;
  final HistoryToolCall? tool;
  final String text;
  final bool asError;
}

const _failureWords = {
  'failure',
  'failed',
  'error',
  'denied',
  'rejected',
  'timeout',
  'cancelled',
  'canceled',
};

bool _isFailureWord(String raw) =>
    _failureWords.contains(raw.trim().toLowerCase());

String _trimmed(Object? value) => value is String ? value.trim() : '';

/// Target argument of one stored call, first non-empty of the usual keys.
String _callTarget(Map call) {
  final fn = call['function'];
  Object? args = call['arguments'];
  if (args == null && fn is Map) args = fn['arguments'];
  if (args is String) {
    final raw = args;
    try {
      args = jsonDecode(raw);
    } on FormatException {
      // Keep the raw string: it still names the call's subject.
      return raw.trim();
    }
  }
  if (args is! Map) return '';
  for (final key in const [
    'path',
    'file_path',
    'filepath',
    'target',
    'command',
    'query',
    'pattern',
    'url',
    'title',
  ]) {
    final value = _trimmed(args[key]);
    if (value.isNotEmpty) return value;
  }
  return '';
}

String _callName(Map call) {
  final fn = call['function'];
  final name = _trimmed(call['name']);
  if (name.isNotEmpty) return name;
  if (fn is Map) return _trimmed(fn['name']);
  return '';
}

/// Rows for one tool call already carrying an outcome (`tool` row).
HistoryRow? _rowOfToolOutput(Map entry) {
  final name = _trimmed(entry['tool_name']);
  final content = _trimmed(entry['content']);
  if (name.isEmpty && content.isEmpty) return null;
  final failed =
      _isFailureWord(_trimmed(entry['tool_outcome'])) ||
      _isFailureWord(_trimmed(entry['failure_kind']));
  return HistoryRow(
    kind: HistoryRowKind.tool,
    tool: HistoryToolCall(
      name: name.isEmpty ? content : name,
      target: name.isEmpty ? '' : content,
      state: failed ? ToolState.failed : ToolState.done,
      asError: failed,
    ),
    asError: failed,
  );
}

/// Tool rows for one stored conversation.
///
/// [messages] are the stored entries as maps. Calls keep their transcript
/// order; a `tool` row that repeats a call already listed by name and target
/// is skipped so one call is not painted twice.
List<HistoryRow> historyToolRows(List<Map> messages) {
  final rows = <HistoryRow>[];
  final seen = <String>{};
  for (final entry in messages) {
    final role = _trimmed(entry['role']).toLowerCase();
    if (role == 'assistant') {
      final calls = entry['tool_calls'];
      if (calls is! List) continue;
      final failed = _isFailureWord(_trimmed(entry['tool_outcome']));
      for (final raw in calls) {
        if (raw is! Map) continue;
        final name = _callName(raw);
        if (name.isEmpty) continue;
        final target = _callTarget(raw);
        seen.add('$name\u0000$target');
        rows.add(
          HistoryRow(
            kind: HistoryRowKind.tool,
            tool: HistoryToolCall(
              name: name,
              target: target,
              state: failed ? ToolState.failed : ToolState.done,
              asError: failed,
            ),
          ),
        );
      }
      continue;
    }
    if (role != 'tool') continue;
    final row = _rowOfToolOutput(entry);
    if (row == null) continue;
    final tool = row.tool!;
    if (!seen.add('${tool.name}\u0000${tool.target}')) continue;
    rows.add(row);
  }
  return rows;
}

/// Kind of work a stored tool did, using the same name table as live rows.
ToolKind historyToolKind(String name) => toolKindFor(name);

/// How a stored turn reads on the phone.
class HistoryDisplay {
  const HistoryDisplay({this.toolRows = const [], this.answer = ''});

  /// Tool rows, in transcript order, painted under the thinking block.
  final List<HistoryRow> toolRows;

  /// Assistant prose. Empty when the turn only called tools.
  final String answer;

  bool get isEmpty => toolRows.isEmpty && answer.trim().isEmpty;
}

/// Display for one stored entry, already decoded as a map.
///
/// An assistant row keeps its `tool_calls` out of the answer text. A `tool`
/// row becomes a completion row. Any other row keeps its prose as the answer.
HistoryDisplay historyDisplayOf(Map entry) {
  final role = _trimmed(entry['role']).toLowerCase();
  if (role == 'assistant') {
    return HistoryDisplay(
      toolRows: historyToolRows([entry]),
      answer: _trimmed(entry['content']),
    );
  }
  if (role == 'tool') {
    final row = _rowOfToolOutput(entry);
    return HistoryDisplay(toolRows: [?row]);
  }
  return HistoryDisplay(answer: _trimmed(entry['content']));
}
