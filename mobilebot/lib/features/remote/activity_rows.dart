// Typed fold of one live viewer turn, matching the desktop process timeline
// in guiapp/frontend/src/components/ai/CodingAgentProgressTimeline.tsx:
// a collapsible summary line, then thought rows, tool rows and short banner
// rows, in arrival order.
//
// ViewerReduceState keeps thinking, timeline, cards, and notices in separate
// lists. Every item is stamped with `seq` from the shared arrival counter, so
// the lists alone lose the interleaving of a real turn. This file sorts the
// items back into one sequence and classifies each one.
//
// The coding progress rows arrive as `ViewerTimelineRow.text` carrying a
// "Coding Agent Event: {json}" payload (parseCodingAgentEventProgress in
// CodingAgentProgressStatus.tsx). That payload is what tells a tool_started
// apart from a tool_finished, and what names the tool — the reducer's phase
// and title alone cannot. Legacy `Coding Agent: running T1 - title` lines keep
// the old phase/title shape.
//
// No durations are invented: the reducer keeps no wall-clock stamps, so an
// elapsed value is absent rather than "0s". Dedup and the reducer protocol
// stay in viewer_reducer.dart.

import 'dart:convert';

import 'viewer_reducer.dart';

/// Row kinds aligned with the desktop process timeline.
enum ActivityRowKind {
  /// Collapsible count line. Derived, never part of the arrival order.
  summary,

  /// One thinking block.
  thinking,

  /// One tool call (started / finished / failed) or one non-tool summary.
  tool,

  /// A task-status banner such as "执行中 · 改 mobilebot 界面".
  status,

  /// Confirmation, notice, trial, or other note that is not coding progress.
  note,
}

/// Kind of work a tool row did, mirroring `CodingAgentProcessKind` on the
/// desktop so both surfaces pick the same icon and verb.
enum ToolKind { run, read, write, edit, find, search, explore, todo, agent, other }

/// `tool_started` → running. `tool_finished` → done / failed.
enum ToolState { running, done, failed, unknown }

/// One display row. [seq] is the arrival stamp; the summary uses -1.
class ActivityRow {
  const ActivityRow({
    required this.kind,
    required this.seq,
    this.text = '',
    this.title = '',
    this.phase = '',
    this.toolName = '',
    this.toolAction = '',
    this.toolTarget = '',
    this.toolKind = ToolKind.other,
    this.toolState = ToolState.unknown,
    this.done = false,
    this.asError = false,
    this.thinkingCount = 0,
    this.toolCount = 0,
    this.noteCount = 0,
  });

  final ActivityRowKind kind;
  final int seq;

  /// Note / banner body. For tool rows, the raw payload when it could not be
  /// parsed into a name.
  final String text;

  /// Row title (task title for banners, tool name for unparsed tools).
  final String title;

  /// Legacy coding phase token (`running`, `completed`, …).
  final String phase;

  final String toolName;
  final String toolAction;
  final String toolTarget;
  final ToolKind toolKind;
  final ToolState toolState;

  /// Thinking block finished (or a finished tool). Drives the live shimmer.
  final bool done;
  final bool asError;

  final int thinkingCount;
  final int toolCount;
  final int noteCount;

  bool get isSummary => kind == ActivityRowKind.summary;

  /// True for a tool row the payload named; those get the desktop chip layout.
  bool get isNamedTool => kind == ActivityRowKind.tool && toolName.isNotEmpty;
}

/// Labels the screen already owns. The fold stays free of BuildContext and
/// generated l10n so tests can pass the four locales directly.
class ActivityRowLabels {
  const ActivityRowLabels({
    required this.confirmInvalid,
    required this.confirmRequired,
    required this.failures,
  });

  final String confirmInvalid;
  final String confirmRequired;

  /// Already-formatted failure line, e.g. "失败 2".
  final String failures;
}

/// Tool name → work kind. Keys are the names the coding agent emits in
/// `tool_started.detail`; the desktop table (SESSION_ICON_BY_TOOL in
/// assistantToolCall.ts) is the source for these.
const Map<String, ToolKind> _toolKinds = {
  'bash': ToolKind.run,
  'run_terminal': ToolKind.run,
  'shell': ToolKind.run,
  'powershell': ToolKind.run,
  'ssh': ToolKind.run,
  'exec_command': ToolKind.run,
  'read_file': ToolKind.read,
  'read_files': ToolKind.read,
  'read_code': ToolKind.read,
  'read': ToolKind.read,
  'write_file': ToolKind.write,
  'write': ToolKind.write,
  'create_file': ToolKind.write,
  'edit_file': ToolKind.edit,
  'edit_lines': ToolKind.edit,
  'edit': ToolKind.edit,
  'str_replace': ToolKind.edit,
  'apply_patch': ToolKind.edit,
  'git_diff': ToolKind.edit,
  'list_directory': ToolKind.find,
  'list_dir': ToolKind.find,
  'search_files': ToolKind.find,
  'search': ToolKind.find,
  'glob': ToolKind.find,
  'ripgrep': ToolKind.search,
  'code_search': ToolKind.search,
  'grep_search': ToolKind.search,
  'grep': ToolKind.search,
  'web_search': ToolKind.search,
  'session_search': ToolKind.search,
  'search_code': ToolKind.search,
  'code_navigation': ToolKind.explore,
  'todo_write': ToolKind.todo,
  'delegate_task': ToolKind.agent,
  'spawn_coding_agent': ToolKind.agent,
};

/// Phrase fallbacks for tools the table does not name (the desktop
/// SESSION_ICON_PHRASES list).
final List<(RegExp, ToolKind)> _toolPhrases = [
  (RegExp(r'执行命令|run command'), ToolKind.run),
  (RegExp(r'读取文件|read file'), ToolKind.read),
  (RegExp(r'写入文件|write file'), ToolKind.write),
  (RegExp(r'编辑文件|edit file'), ToolKind.edit),
  (RegExp(r'列出目录|搜索文件|查找文件|list directory|search files'), ToolKind.find),
  (RegExp(r'探索源码|探索代码|explore source'), ToolKind.explore),
  (RegExp(r'检索内容|搜索网页|搜索网络|search content|web search'), ToolKind.search),
  (RegExp(r'委派任务|delegate task'), ToolKind.agent),
  (RegExp(r'待办|(?:^|[^a-z0-9_])todo(?:[^a-z0-9_]|$)'), ToolKind.todo),
];

/// Work kind for one tool. An `ssh_` prefix is dropped first, as on desktop.
ToolKind toolKindFor(String name, {String action = ''}) {
  final raw = name.trim().toLowerCase();
  if (raw.isEmpty) return ToolKind.other;
  final bare = raw.startsWith('ssh_') ? raw.substring(4) : raw;
  final named = _toolKinds[bare] ?? _toolKinds[raw];
  if (named != null) return named;
  final phrase = '$action $name'.trim();
  if (phrase.isEmpty) return ToolKind.other;
  for (final (pattern, kind) in _toolPhrases) {
    if (pattern.hasMatch(phrase)) return kind;
  }
  return ToolKind.other;
}

/// Parsed shape of one `Coding Agent Event:` payload (the desktop
/// parseCodingAgentEventProgress fields this UI uses).
class CodingEvent {
  const CodingEvent({
    required this.event,
    required this.phase,
    required this.title,
    required this.detail,
    required this.target,
    required this.outcome,
    this.durationMs,
  });

  final String event;
  final String phase;
  final String title;

  /// Tool name for tool events; empty for task_status.
  final String detail;
  final String target;
  final String outcome;
  final int? durationMs;
}

const String _codingEventPrefix = 'Coding Agent Event:';

final RegExp _legacyLine = RegExp(
  r'^Coding Agent:\s*([a-z]+)(?:\s+(T\d+))?(?:\s+-\s*(.*))?$',
  caseSensitive: false,
);

String _str(Object? value) => value is String ? value : '';

int? _ms(Object? value) {
  if (value is int) return value < 0 ? null : value;
  if (value is num) {
    final n = value.toInt();
    return n < 0 ? null : n;
  }
  return null;
}

/// Parses one coding progress payload. Returns null when the text is neither
/// an event payload nor a legacy `Coding Agent:` line.
CodingEvent? parseCodingEvent(String raw) {
  final trimmed = raw.trim();
  if (trimmed.startsWith(_codingEventPrefix)) {
    try {
      final decoded = jsonDecode(trimmed.substring(_codingEventPrefix.length).trim());
      if (decoded is! Map) return null;
      if (_str(decoded['agent']) != 'coding') return null;
      return CodingEvent(
        event: _str(decoded['event']),
        phase: _str(decoded['phase']),
        title: _str(decoded['title']),
        detail: _str(decoded['detail']),
        target: _str(decoded['target']),
        outcome: _str(decoded['outcome']),
        durationMs: _ms(decoded['duration_ms']) ?? _ms(decoded['durationMs']),
      );
    } on FormatException {
      return null;
    }
  }
  final match = _legacyLine.firstMatch(trimmed);
  if (match == null) return null;
  return CodingEvent(
    event: '',
    phase: (match.group(1) ?? '').toLowerCase(),
    title: (match.group(3) ?? '').trim(),
    detail: '',
    target: match.group(2) ?? '',
    outcome: '',
  );
}

ToolState _toolStateOf(CodingEvent event) {
  switch (event.event.trim().toLowerCase()) {
    case 'tool_started':
      return ToolState.running;
    case 'tool_finished':
      final outcome = event.outcome.trim().toLowerCase();
      if (outcome == 'failure' || outcome == 'failed' || outcome == 'error') {
        return ToolState.failed;
      }
      return ToolState.done;
    default:
      return ToolState.unknown;
  }
}

/// True for a payload that is a task-level status banner rather than a tool.
bool _isStatusEvent(CodingEvent event) {
  final name = event.event.trim().toLowerCase();
  return name.isEmpty || name == 'task_status';
}

ActivityRow _rowOfEvent(ViewerTimelineRow row, CodingEvent event) {
  if (_isStatusEvent(event)) {
    return ActivityRow(
      kind: ActivityRowKind.status,
      seq: row.seq,
      title: event.title,
      phase: event.phase,
      text: row.text,
    );
  }
  final state = _toolStateOf(event);
  return ActivityRow(
    kind: ActivityRowKind.tool,
    seq: row.seq,
    toolName: event.detail,
    toolAction: event.event.trim().toLowerCase(),
    toolTarget: event.target,
    toolKind: toolKindFor(event.detail),
    toolState: state,
    title: event.title,
    phase: event.phase,
    done: state == ToolState.done,
    asError: state == ToolState.failed,
    text: row.text,
  );
}

/// Folds [state] into seq-sorted typed rows, optionally prefixed by a summary.
///
/// Empty thinking is dropped, as is a timeline row with no text at all.
/// [suppressThinking] drops thought rows when history already owns the text.
/// [historyTexts] drops a note whose trimmed text the history tail shows.
/// Trial status and the failure count have no arrival stamp; they sort after
/// every stamped row. The summary counts only rows that survived the filters
/// and is omitted when nothing remains.
List<ActivityRow> foldActivityRows(
  ViewerReduceState state, {
  ActivityRowLabels? labels,
  bool suppressThinking = false,
  Set<String> historyTexts = const {},
  String recovery = '',
  bool includeSummary = true,
}) {
  final rows = <ActivityRow>[];
  final seenNotes = <String>{};

  void addNote(String line, {required int seq, bool asError = false}) {
    final trimmed = line.trim();
    if (trimmed.isEmpty ||
        historyTexts.contains(trimmed) ||
        !seenNotes.add(trimmed)) {
      return;
    }
    rows.add(
      ActivityRow(kind: ActivityRowKind.note, seq: seq, text: line, asError: asError),
    );
  }

  if (!suppressThinking) {
    for (final block in state.thinking) {
      if (block.text.trim().isEmpty) continue;
      rows.add(
        ActivityRow(
          kind: ActivityRowKind.thinking,
          seq: block.seq,
          text: block.text,
          done: block.done,
        ),
      );
    }
  }

  for (final row in state.timeline) {
    if (row.text.trim().isEmpty) continue;
    final event = parseCodingEvent(row.text);
    if (event != null) {
      rows.add(_rowOfEvent(row, event));
      continue;
    }
    final title = row.title.isNotEmpty ? row.title : row.text;
    rows.add(
      ActivityRow(
        kind: ActivityRowKind.status,
        seq: row.seq,
        title: title,
        phase: row.phase,
        text: row.text,
      ),
    );
  }

  for (final card in state.cards) {
    if (card.expired) {
      if (labels != null) {
        addNote(labels.confirmInvalid, seq: card.seq, asError: true);
      }
    } else if (card.text.isNotEmpty) {
      addNote(card.text, seq: card.seq, asError: card.asError);
    } else if (card.kind == 'confirmation' && labels != null) {
      addNote(labels.confirmRequired, seq: card.seq);
    }
  }

  for (final note in state.notices) {
    if (note.text.isNotEmpty) {
      addNote(note.text, seq: note.seq, asError: note.asError);
    }
  }

  // Stamped after every arrival so a live item is never reordered behind a
  // status the reducer did not stamp.
  const tail = 1 << 30;
  if (state.trialStatus.isNotEmpty) addNote(state.trialStatus, seq: tail);
  if (state.trialFailures > 0 && labels != null) {
    addNote(labels.failures, seq: tail + 1, asError: true);
  }
  // Recovery is the screen's own text, not a reducer arrival.
  if (recovery.trim().isNotEmpty) addNote(recovery, seq: tail + 2);

  rows.sort((a, b) {
    final bySeq = a.seq.compareTo(b.seq);
    if (bySeq != 0) return bySeq;
    return a.kind.index.compareTo(b.kind.index);
  });

  if (!includeSummary || rows.isEmpty) return rows;

  var thinkingCount = 0;
  var toolCount = 0;
  var noteCount = 0;
  for (final row in rows) {
    switch (row.kind) {
      case ActivityRowKind.thinking:
        thinkingCount++;
      case ActivityRowKind.tool:
        toolCount++;
      case ActivityRowKind.note:
        noteCount++;
      case ActivityRowKind.status:
      case ActivityRowKind.summary:
        break;
    }
  }
  return [
    ActivityRow(
      kind: ActivityRowKind.summary,
      seq: -1,
      thinkingCount: thinkingCount,
      toolCount: toolCount,
      noteCount: noteCount,
    ),
    ...rows,
  ];
}
