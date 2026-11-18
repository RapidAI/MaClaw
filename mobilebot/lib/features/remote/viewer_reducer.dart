// Pure fold of one viewer-contract frame. Locked to the same JSON vectors as
// the desktop reducer in guiapp/frontend/src/components/ai/reduceViewerEvent.ts.

class ViewerThinking {
  ViewerThinking({this.text = '', this.done = false, this.seq = 0});
  String text;
  bool done;

  /// Arrival order across ALL live items (thinking/timeline/cards/notices),
  /// stamped from the state's global counter — the typed lists alone lose
  /// the interleaving of a real turn.
  int seq;

  Map<String, dynamic> toJson() => {'text': text, 'done': done, 'seq': seq};
}

class ViewerTimelineRow {
  ViewerTimelineRow({
    this.phase = '',
    this.taskId = '',
    this.title = '',
    this.runId = '',
    this.text = '',
    this.seq = 0,
  });
  String phase;
  String taskId;
  String title;
  String runId;
  String text;

  /// Arrival order across all live items — see [ViewerThinking.seq].
  int seq;

  Map<String, dynamic> toJson() => {
    'phase': phase,
    'taskId': taskId,
    'title': title,
    'runId': runId,
    if (text.isNotEmpty) 'text': text,
    'seq': seq,
  };
}

class ViewerCard {
  ViewerCard({
    this.requestId = '',
    this.kind = '',
    this.expired = false,
    this.confirmationId = '',
    this.text = '',
    this.asError = false,
    this.seq = 0,
  });
  String requestId;
  String kind;
  bool expired;
  String confirmationId;
  String text;
  bool asError;

  /// Arrival order across all live items — see [ViewerThinking.seq].
  int seq;

  Map<String, dynamic> toJson() => {
    'requestId': requestId,
    'kind': kind,
    'expired': expired,
    'confirmationId': confirmationId,
    'text': text,
    'asError': asError,
    'seq': seq,
  };
}

class ViewerNotice {
  ViewerNotice({this.text = '', this.asError = false, this.seq = 0});
  String text;
  bool asError;

  /// Arrival order across all live items — see [ViewerThinking.seq].
  int seq;

  Map<String, dynamic> toJson() => {
    'text': text,
    'asError': asError,
    'seq': seq,
  };
}

class ViewerReduceState {
  ViewerReduceState({
    this.last = 0,
    this.requestId = '',
    this.body = '',
    List<ViewerThinking>? thinking,
    List<ViewerTimelineRow>? timeline,
    List<ViewerCard>? cards,
    List<ViewerNotice>? notices,
    this.visible = '',
    this.trialStatus = '',
    this.trialFailures = 0,
    this.gap = false,
    this.responded = false,
    this.displayText = '',
    this.nextSeq = 0,
  }) : thinking = thinking ?? <ViewerThinking>[],
       timeline = timeline ?? <ViewerTimelineRow>[],
       cards = cards ?? <ViewerCard>[],
       notices = notices ?? <ViewerNotice>[];

  int last;
  String requestId;
  String body;
  List<ViewerThinking> thinking;
  List<ViewerTimelineRow> timeline;
  List<ViewerCard> cards;
  List<ViewerNotice> notices;
  String visible;
  String trialStatus;
  int trialFailures;
  bool gap;
  bool responded;
  String displayText;

  /// Monotonic arrival counter stamping every live item's [seq].
  int nextSeq;

  ViewerReduceState clone() => ViewerReduceState(
    last: last,
    requestId: requestId,
    body: body,
    thinking: [
      for (final block in thinking)
        ViewerThinking(text: block.text, done: block.done, seq: block.seq),
    ],
    timeline: [
      for (final row in timeline)
        ViewerTimelineRow(
          phase: row.phase,
          taskId: row.taskId,
          title: row.title,
          runId: row.runId,
          text: row.text,
          seq: row.seq,
        ),
    ],
    cards: [
      for (final card in cards)
        ViewerCard(
          requestId: card.requestId,
          kind: card.kind,
          expired: card.expired,
          confirmationId: card.confirmationId,
          text: card.text,
          asError: card.asError,
          seq: card.seq,
        ),
    ],
    notices: [
      for (final note in notices)
        ViewerNotice(text: note.text, asError: note.asError, seq: note.seq),
    ],
    visible: visible,
    trialStatus: trialStatus,
    trialFailures: trialFailures,
    gap: gap,
    responded: responded,
    displayText: displayText,
  );

  Map<String, dynamic> toJson() => {
    'last': last,
    'requestId': requestId,
    'body': body,
    'thinking': [for (final block in thinking) block.toJson()],
    'timeline': [for (final row in timeline) row.toJson()],
    'cards': [for (final card in cards) card.toJson()],
    'notices': [for (final note in notices) note.toJson()],
    'visible': visible,
    'trialStatus': trialStatus,
    'trialFailures': trialFailures,
    'gap': gap,
    'responded': responded,
    'displayText': displayText,
    'nextSeq': nextSeq,
  };
}

ViewerReduceState emptyViewerState() => ViewerReduceState();

bool _known(String name) {
  const names = {
    'snapshot',
    'round_started',
    'token',
    'thinking',
    'progress',
    'heartbeat',
    'workbench_steps',
    'new_round',
    'stream_done',
    'card',
    'response',
  };
  return names.contains(name);
}

int _num(Object? value) {
  if (value is int) return value < 0 ? 0 : value;
  if (value is num) {
    final n = value.toInt();
    return n < 0 ? 0 : n;
  }
  return 0;
}

String _text(Object? value) => value is String ? value : '';

/// True when a UTF-16 code unit would paint as a hollow square. Tab, LF, and
/// CR stay. This matches the desktop reducer: controls, U+25A1, BMP private
/// use, and U+FFF0–U+FFFF.
bool _chatTextHasTofu(String value) {
  for (var i = 0; i < value.length; i++) {
    final c = value.codeUnitAt(i);
    if (c < 0x20) {
      if (c != 0x09 && c != 0x0a && c != 0x0d) return true;
      continue;
    }
    if ((c >= 0x7f && c <= 0x9f) ||
        c == 0x25a1 ||
        (c >= 0xe000 && c <= 0xf8ff) ||
        c >= 0xfff0) {
      return true;
    }
  }
  return false;
}

/// Drops stream markers and other tofu from text that is about to be shown.
/// A clean string is returned unchanged. A leading U+0002 is not handled
/// here; the caller keeps that marker as the rejected-round reset.
String _visibleChatText(String value) {
  if (value.isEmpty || !_chatTextHasTofu(value)) return value;
  final out = StringBuffer();
  for (var i = 0; i < value.length; i++) {
    final c = value.codeUnitAt(i);
    if (c < 0x20) {
      if (c == 0x09 || c == 0x0a || c == 0x0d) out.writeCharCode(c);
      continue;
    }
    if ((c >= 0x7f && c <= 0x9f) ||
        c == 0x25a1 ||
        (c >= 0xe000 && c <= 0xf8ff) ||
        c >= 0xfff0) {
      continue;
    }
    out.writeCharCode(c);
  }
  return out.toString();
}

List<String> _pieces(Map<String, dynamic> event) {
  final raw = event['pieces'];
  if (raw is List && raw.isNotEmpty) {
    return [for (final piece in raw) piece?.toString() ?? ''];
  }
  final joined = _text(event['text']);
  return joined.isEmpty ? const [] : [joined];
}

String _applyPieces(String body, List<String> pieces) {
  var next = body;
  for (final piece in pieces) {
    if (piece.isEmpty) continue;
    // A leading U+0002 replaces the draft. The suffix is the new body.
    if (piece.startsWith('\x02')) {
      next = _visibleChatText(piece.substring(1));
      continue;
    }
    final visible = _visibleChatText(piece);
    if (visible.isNotEmpty) next += visible;
  }
  return next;
}

void _seal(ViewerReduceState state) {
  for (final block in state.thinking) {
    block.done = true;
  }
}

void _appendThinking(ViewerReduceState state, String delta) {
  final body = _visibleChatText(delta);
  if (body.isEmpty) return;
  for (var i = state.thinking.length - 1; i >= 0; i--) {
    if (!state.thinking[i].done) {
      state.thinking[i].text += body;
      return;
    }
  }
  state.thinking.add(ViewerThinking(text: body, seq: state.nextSeq++));
}

/// Fills the seq of items created without one (creation sites stamp via the
/// counter; this keeps explicit construction sites terse).
int _stamp(ViewerReduceState state) => state.nextSeq++;

final _legacyLine = RegExp(
  r'^Coding Agent:\s*([a-z]+)(?:\s+(T\d+))?(?:\s+-\s*(.*))?$',
  caseSensitive: false,
);
final _credential = RegExp(r'^credential-card-.+');

ViewerTimelineRow? _rowFromText(String raw) {
  final match = _legacyLine.firstMatch(raw.trim());
  if (match == null) return null;
  var phase = (match.group(1) ?? '').toLowerCase();
  if (phase == 'unknown') phase = '';
  return ViewerTimelineRow(
    phase: phase,
    taskId: match.group(2) ?? '',
    title: (match.group(3) ?? '').trim(),
    text: raw,
  );
}

String _taskOf(Map coding) {
  final raw = coding['task_id'];
  if (raw is String && raw.isNotEmpty) return raw;
  final alt = coding['taskId'];
  return alt is String ? alt : '';
}

String _runOf(Map coding) {
  final raw = coding['run_id'];
  if (raw is String && raw.isNotEmpty) return raw;
  final alt = coding['runId'];
  return alt is String ? alt : '';
}

void _apply(ViewerReduceState state, Map<String, dynamic> event) {
  final name = _text(event['event']);
  final requestId = _text(event['request_id']);
  if (requestId.isNotEmpty &&
      name == 'round_started' &&
      requestId != state.requestId) {
    state.requestId = requestId;
    state.body = '';
    state.thinking = [];
    state.responded = false;
    state.gap = false;
    state.displayText = _text(event['display_text']);
  } else if (requestId.isNotEmpty && state.requestId.isEmpty) {
    state.requestId = requestId;
  }
  switch (name) {
    case 'round_started':
      final display = _text(event['display_text']);
      if (display.isNotEmpty) state.displayText = display;
      return;
    case 'token':
      _seal(state);
      var body = event['replace'] == true ? '' : state.body;
      state.body = _applyPieces(body, _pieces(event));
      return;
    case 'thinking':
      final mode = _text(event['state']).isEmpty
          ? 'delta'
          : _text(event['state']);
      if (mode == 'started') {
        final tail = state.thinking.isEmpty ? null : state.thinking.last;
        if (tail == null || tail.done) {
          state.thinking.add(ViewerThinking(seq: _stamp(state)));
        }
        return;
      }
      if (mode == 'done') {
        final doneText = _visibleChatText(_text(event['text']));
        for (var i = state.thinking.length - 1; i >= 0; i--) {
          if (!state.thinking[i].done) {
            if (doneText.isNotEmpty) state.thinking[i].text = doneText;
            state.thinking[i].done = true;
            return;
          }
        }
        state.thinking.add(
          ViewerThinking(text: doneText, done: true, seq: _stamp(state)),
        );
        return;
      }
      for (final delta in _pieces(event)) {
        if (delta.isNotEmpty) _appendThinking(state, delta);
      }
      return;
    case 'progress':
      final raw = _text(event['text']);
      final coding = event['coding'];
      if (coding is Map) {
        state.timeline.add(
          ViewerTimelineRow(
            phase: _text(coding['phase']),
            taskId: _taskOf(coding),
            title: _text(coding['title']),
            runId: _runOf(coding),
            seq: _stamp(state),
          ),
        );
        return;
      }
      final row = raw.isEmpty ? null : _rowFromText(raw);
      if (row != null) {
        row.seq = _stamp(state);
        state.timeline.add(row);
      }
      return;
    case 'new_round':
    case 'stream_done':
      _seal(state);
      if (name == 'stream_done') state.responded = false;
      return;
    case 'card':
      final kind = _text(event['kind']).isEmpty
          ? 'notice'
          : _text(event['kind']);
      if (kind == 'notice' && _credential.hasMatch(requestId)) {
        for (final card in state.cards) {
          if (card.requestId == requestId) card.expired = true;
        }
        final note = _visibleChatText(_text(event['text']));
        if (note.isNotEmpty) {
          state.notices.add(
            ViewerNotice(
              text: note,
              asError: event['as_error'] == true,
              seq: _stamp(state),
            ),
          );
        }
        return;
      }
      if (kind == 'confirmation') {
        final confirmation = event['confirmation'];
        final id = confirmation is Map ? _text(confirmation['id']) : '';
        state.cards.add(
          ViewerCard(
            requestId: requestId,
            kind: kind,
            confirmationId: id,
            text: _visibleChatText(_text(event['text'])),
            seq: _stamp(state),
          ),
        );
        return;
      }
      final note = _visibleChatText(_text(event['text']));
      if (note.isNotEmpty) {
        state.notices.add(
          ViewerNotice(
            text: note,
            asError: event['as_error'] == true,
            seq: _stamp(state),
          ),
        );
      }
      return;
    case 'response':
      if (requestId.isNotEmpty &&
          state.requestId.isNotEmpty &&
          requestId != state.requestId &&
          !state.responded) {
        return;
      }
      _seal(state);
      state.responded = true;
      final terminal = event['terminal'] is Map
          ? event['terminal'] as Map
          : (event['response'] is Map ? event['response'] as Map : const {});
      final body = _visibleChatText(_text(terminal['text']));
      final summary = _visibleChatText(
        _text(terminal['trial_reflect_summary']).isNotEmpty
            ? _text(terminal['trial_reflect_summary'])
            : _text(terminal['trace_summary']),
      );
      state.visible = body.isNotEmpty ? body : summary;
      state.trialStatus = _text(terminal['trial_reflect_status']);
      state.trialFailures = _num(terminal['trial_reflect_failures']);
      if (state.body.isEmpty && body.isNotEmpty) state.body = body;
      final confirmation = terminal['confirmation'];
      if (confirmation is Map && _text(confirmation['id']).isNotEmpty) {
        state.cards.add(
          ViewerCard(
            requestId: requestId,
            kind: 'confirmation',
            confirmationId: _text(confirmation['id']),
            seq: _stamp(state),
          ),
        );
      }
      return;
    default:
      break;
  }
}

void _applyLiveParts(ViewerReduceState state, List parts) {
  final body = StringBuffer();
  for (final item in parts) {
    if (item is! Map) continue;
    // parts preserve the snapshot's real arrival order — stamp each created
    // item with the shared counter so the merged transcript interleaves
    // thinking/progress/text exactly as the turn produced them.
    final seq = state.nextSeq++;
    final kind = _text(item['kind']);
    if (kind == 'text') {
      body.write(_visibleChatText(_text(item['text'])));
    } else if (kind == 'thinking') {
      state.thinking.add(
        ViewerThinking(
          text: _visibleChatText(_text(item['text'])),
          done: item['done'] == true,
          seq: seq,
        ),
      );
    } else if (kind == 'progress') {
      final coding = item['coding'];
      if (coding is Map) {
        state.timeline.add(
          ViewerTimelineRow(
            phase: _text(coding['phase']),
            taskId: _taskOf(coding),
            title: _text(coding['title']),
            runId: _runOf(coding),
            text: _text(item['text']),
            seq: seq,
          ),
        );
      } else {
        final raw = _text(item['text']);
        final row = raw.isEmpty ? null : _rowFromText(raw);
        if (row != null) {
          row.seq = seq;
          state.timeline.add(row);
        }
      }
    }
  }
  state.body = body.toString();
}

ViewerReduceState _fromLive(Map<String, dynamic> event, int cursor) {
  final next = emptyViewerState()..last = cursor;
  final live = event['live'];
  if (live is! Map) {
    next.gap = true;
    return next;
  }
  next.requestId = _text(live['request_id']);
  next.displayText = _text(live['display_text']);
  next.responded = live['responded'] == true;
  // Wire snapshots carry parts. live.text is only the older golden shape.
  final parts = live['parts'];
  if (parts is List) {
    _applyLiveParts(next, parts);
    return next;
  }
  next.body = _visibleChatText(_text(live['text']));
  final blocks = live['thinking'];
  if (blocks is List) {
    next.thinking = [
      for (final item in blocks)
        if (item is Map)
          ViewerThinking(
            text: _visibleChatText(_text(item['text'])),
            done: item['done'] == true,
            seq: next.nextSeq++,
          ),
    ];
  }
  return next;
}

/// Folds one frame. Returns the same instance when the event is ignored.
ViewerReduceState reduceViewerEvent(
  ViewerReduceState state,
  Map<String, dynamic> event,
) {
  final name = _text(event['event']);
  if (!_known(name)) return state;
  final cursor = _num(event['cursor']);
  final hasFrom = event.containsKey('cursor_from');
  final from = hasFrom ? _num(event['cursor_from']) : cursor;
  if (cursor <= 0 && !hasFrom) {
    final next = state.clone();
    _apply(next, event);
    return next;
  }
  if (name == 'snapshot' && event['resync'] == true && cursor > state.last) {
    if (event['live'] is! Map) {
      final next = state.clone();
      next.gap = true;
      return next;
    }
    return _fromLive(event, cursor);
  }
  if (cursor <= state.last) return state;
  if (from == state.last + 1) {
    final next = state.clone();
    _apply(next, event);
    next.last = cursor;
    next.gap = false;
    return next;
  }
  final requestId = _text(event['request_id']);
  if (from > state.last + 1 &&
      name == 'round_started' &&
      requestId.isNotEmpty &&
      requestId != state.requestId) {
    final next = state.clone();
    _apply(next, event);
    next.last = cursor;
    next.gap = false;
    return next;
  }
  final next = state.clone();
  next.gap = true;
  return next;
}

/// watch_task body the phone sends. Proto 2 is the shared viewer contract.
Map<String, dynamic> watchTaskBody(String projectPath) => {
  'project_path': projectPath,
  'stream_proto': 2,
};

/// Seeds the phone reducer from a watch_task reply, folding onto [prev].
///
/// The reply cursor is the folded watermark. A settled turn omits live and
/// still carries that cursor; ignoring it leaves last at 0, so the next SSE
/// frame gaps for the rest of the stream. A live cursor above 0 wins, so
/// frames between the snapshot and the watermark can still append. A cursor
/// already passed returns the same instance.
ViewerReduceState seedViewerFromWatchReply(
  Map<String, dynamic> reply, [
  ViewerReduceState? prev,
]) {
  final base = prev ?? emptyViewerState();
  final raw = reply['live'];
  final live = raw is Map ? Map<String, dynamic>.from(raw) : null;
  final liveCursor = live == null ? 0 : _num(live['cursor']);
  final at = liveCursor > 0 ? liveCursor : _num(reply['cursor']);
  final requestId = live == null ? '' : _text(live['request_id']);
  if (live == null || requestId.isEmpty) {
    if (at > base.last) {
      // Drop the latch when the watermark moves, so the next dropped frame
      // is a new hole. The screen still owes a cursor it already saw above
      // this mark; that debt is not this fold.
      final next = base.clone();
      next.last = at;
      next.gap = false;
      return next;
    }
    return base;
  }
  if (at <= 0) return base;
  return reduceViewerEvent(base, {
    'event': 'snapshot',
    'resync': true,
    'cursor': at,
    'live': live,
  });
}
