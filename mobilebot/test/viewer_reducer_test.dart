import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/features/remote/viewer_reducer.dart';

void main() {
  Map<String, dynamic> asMap(Object? value) =>
      Map<String, dynamic>.from(value as Map);

  void expectSubset(Object? actual, Object? expected) {
    if (expected is List) {
      expect(actual, isA<List>());
      final items = actual as List;
      for (var i = 0; i < expected.length; i++) {
        expectSubset(items[i], expected[i]);
      }
      return;
    }
    if (expected is Map) {
      final record = asMap(actual);
      expected.forEach((key, value) {
        expectSubset(record[key], value);
      });
      return;
    }
    expect(actual, expected);
  }

  test('reduceViewerEvent matches the shared golden vectors', () {
    final file = File('../guiapp/testdata/viewer_contract_vectors.json');
    expect(file.existsSync(), isTrue, reason: file.absolute.path);
    final doc = asMap(jsonDecode(file.readAsStringSync()));
    for (final item in doc['cases'] as List) {
      final spec = asMap(item);
      var state = emptyViewerState();
      for (final raw in spec['events'] as List) {
        state = reduceViewerEvent(state, asMap(raw));
      }
      expectSubset(state.toJson(), spec['expect']);
    }
  });

  Map<String, dynamic> loadWireSnapshots() {
    final file = File('../guiapp/testdata/viewer_wire_snapshots.json');
    expect(file.existsSync(), isTrue, reason: file.absolute.path);
    return asMap(jsonDecode(file.readAsStringSync()));
  }

  int asInt(Object? value) {
    if (value is int) return value;
    if (value is num) return value.toInt();
    return 0;
  }

  void expectMarshaledSnapshot(
    Map<String, dynamic> event,
    String body,
    bool responded,
  ) {
    final live = asMap(event['live']);
    expect(live.containsKey('text'), isFalse);
    expect(live['parts'], isA<List>());
    final parts = live['parts'] as List;
    expect(
      parts.any(
        (part) => part is Map && part['kind'] == 'text' && part['text'] == body,
      ),
      isTrue,
    );
    final state = reduceViewerEvent(emptyViewerState(), event);
    expect(state.body, body);
    expect(state.last, asInt(event['cursor']));
    expect(state.last, asInt(live['cursor']));
    expect(state.gap, isFalse);
    expect(state.responded, responded);
    expect(state.requestId, 'r1');
    expect(state.displayText, '问');
    expect(state.thinking, hasLength(1));
    expect(state.thinking.single.text, '想想');
    expect(state.thinking.single.done, isTrue);
    expect(state.timeline, hasLength(1));
    expect(state.timeline.single.phase, 'running');
    expect(state.timeline.single.taskId, 'T2');
    expect(state.timeline.single.title, 'edit parser');
    expect(state.timeline.single.runId, 'run-1');
  }

  test('reduces the marshaled watch live and responded snapshot', () {
    final doc = loadWireSnapshots();
    final watch = asMap(doc['watch_event']);
    final responded = asMap(doc['responded_event']);
    expectMarshaledSnapshot(watch, 'helloworld', false);
    expectMarshaledSnapshot(responded, '终稿', true);
    final seeded = seedViewerFromWatchReply({
      'stream_proto': 2,
      'live': responded['live'],
    });
    expect(seeded.body, '终稿');
    expect(seeded.last, asInt(asMap(responded['live'])['cursor']));
    expect(seeded.gap, isFalse);
    expect(seeded.responded, isTrue);
    expect(seeded.thinking.single.text, '想想');
    expect(seeded.timeline.single.taskId, 'T2');
    final nextCursor = seeded.last + 1;
    final next = reduceViewerEvent(seeded, {
      'event': 'token',
      'cursor': nextCursor,
      'cursor_from': nextCursor,
      'text': '续',
    });
    expect(next.body, '终稿续');
    expect(next.last, nextCursor);
    expect(next.gap, isFalse);
  });

  test('seedViewerFromWatchReply folds wire parts and the cursor', () {
    final state = seedViewerFromWatchReply({
      'stream_proto': 2,
      'live': {
        'request_id': 'r9',
        'display_text': '问',
        'responded': true,
        'cursor': 10,
        'parts': [
          {'kind': 'thinking', 'text': '想想', 'done': true},
          {'kind': 'text', 'text': 'helloworld'},
          {
            'kind': 'progress',
            'text': 'Coding Agent: running T2 - edit parser',
            'coding': {
              'phase': 'running',
              'task_id': 'T2',
              'title': 'edit parser',
              'run_id': 'run-1',
            },
          },
        ],
      },
    });
    expect(state.body, 'helloworld');
    expect(state.last, 10);
    expect(state.gap, isFalse);
    expect(state.responded, isTrue);
    expect(state.thinking, hasLength(1));
    expect(state.thinking.single.text, '想想');
    expect(state.thinking.single.done, isTrue);
    expect(state.timeline, hasLength(1));
    expect(state.timeline.single.phase, 'running');
    expect(state.timeline.single.taskId, 'T2');
    expect(state.timeline.single.title, 'edit parser');
    expect(state.timeline.single.runId, 'run-1');
    final next = reduceViewerEvent(state, {
      'event': 'token',
      'cursor': 11,
      'cursor_from': 11,
      'text': '续',
    });
    expect(next.body, 'helloworld续');
    expect(next.last, 11);
    expect(next.gap, isFalse);
  });

  test('seed keeps the reply cursor when the settled turn has no live', () {
    final seeded = seedViewerFromWatchReply({'stream_proto': 2, 'cursor': 40});
    expect(seeded.last, 40);
    expect(seeded.body, isEmpty);
    expect(seeded.gap, isFalse);
    final started = reduceViewerEvent(seeded, {
      'event': 'round_started',
      'request_id': 'r2',
      'cursor': 41,
      'cursor_from': 41,
      'display_text': '下一问',
    });
    expect(started.gap, isFalse);
    expect(started.last, 41);
    expect(started.requestId, 'r2');
    expect(started.displayText, '下一问');

    final held = seedViewerFromWatchReply({'stream_proto': 2, 'cursor': 40});
    final token = reduceViewerEvent(held, {
      'event': 'token',
      'cursor': 41,
      'cursor_from': 41,
      'text': '续',
    });
    expect(token.gap, isFalse);
    expect(token.last, 41);
    expect(token.body, '续');
  });

  test('seed folds parts at the reply cursor and prefers a live cursor', () {
    final fromReply = seedViewerFromWatchReply({
      'stream_proto': 2,
      'cursor': 7,
      'live': {
        'request_id': 'r1',
        'display_text': '问',
        'parts': [
          {'kind': 'text', 'text': '草稿'},
        ],
      },
    });
    expect(fromReply.body, '草稿');
    expect(fromReply.last, 7);
    expect(fromReply.gap, isFalse);
    expect(fromReply.requestId, 'r1');
    expect(fromReply.displayText, '问');

    final preferLive = seedViewerFromWatchReply({
      'stream_proto': 2,
      'cursor': 20,
      'live': {
        'request_id': 'r1',
        'cursor': 10,
        'parts': [
          {'kind': 'text', 'text': '甲'},
        ],
      },
    });
    expect(preferLive.last, 10);
    expect(preferLive.body, '甲');
    expect(preferLive.gap, isFalse);
  });

  test('a later lower watch reply does not rewind', () {
    final current = seedViewerFromWatchReply({
      'stream_proto': 2,
      'cursor': 10,
      'live': {
        'request_id': 'r1',
        'parts': [
          {'kind': 'text', 'text': '新'},
        ],
      },
    });
    final again = seedViewerFromWatchReply({
      'stream_proto': 2,
      'cursor': 4,
      'live': {
        'request_id': 'r0',
        'parts': [
          {'kind': 'text', 'text': '旧'},
        ],
      },
    }, current);
    expect(identical(again, current), isTrue);
    expect(again.last, 10);
    expect(again.body, '新');

    final gapped = current.clone()..gap = true;
    final marked = seedViewerFromWatchReply({
      'stream_proto': 2,
      'cursor': 12,
    }, gapped);
    expect(identical(marked, gapped), isFalse);
    expect(marked.gap, isFalse);
    expect(marked.body, '新');
    expect(marked.last, 12);
    expect(marked.requestId, 'r1');
    final continued = reduceViewerEvent(marked, {
      'event': 'token',
      'cursor': 13,
      'cursor_from': 13,
      'text': '续',
    });
    expect(continued.gap, isFalse);
    expect(continued.last, 13);
    expect(continued.body, '新续');
    final later = reduceViewerEvent(marked, {
      'event': 'token',
      'cursor': 20,
      'cursor_from': 20,
      'text': '又断',
    });
    expect(later.gap, isTrue);
    expect(later.last, 12);
    expect(later.body, '新');
  });

  test('watch_task asks for stream proto 2', () {
    final body = watchTaskBody(r'D:\work\demo');
    expect(body['project_path'], r'D:\work\demo');
    expect(body['stream_proto'], 2);
  });

  test('bot chat and cloud desktop stay off this stream', () {
    const files = [
      'lib/features/bots/bot_chat_controller.dart',
      'lib/features/bots/bot_desktop_screen.dart',
    ];
    for (final path in files) {
      final source = File(path).readAsStringSync();
      expect(source.contains('watch_task'), isFalse, reason: path);
      expect(
        source.contains('/api/remote-control/grants/'),
        isFalse,
        reason: path,
      );
    }
  });
}
