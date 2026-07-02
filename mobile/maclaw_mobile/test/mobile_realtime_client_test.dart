import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_client.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

void main() {
  test('encodes mobile realtime ping frames', () {
    final client = MobileRealtimeClient();
    final payload = jsonDecode(client.encodePing()) as Map<String, dynamic>;

    expect(payload['type'], 'ping');
    expect(payload['client'], 'maclaw_mobile');
  });

  test('pingOnce connects to official realtime channel and closes it',
      () async {
    Uri? connectedUri;
    final channel = _FakeWebSocketChannel();
    final client = MobileRealtimeClient(
      readToken: () async => null,
      readHubUrl: () async => 'https://tenant-a.maclaw.top',
      connect: (uri) {
        connectedUri = uri;
        return channel;
      },
    );

    await client.pingOnce();

    expect(
      connectedUri.toString(),
      'wss://tenant-a.maclaw.top/api/mobile/realtime',
    );
    expect(channel.sink.sent, hasLength(1));
    expect(jsonDecode(channel.sink.sent.single as String), {
      'type': 'ping',
      'client': 'maclaw_mobile',
    });
    expect(channel.sink.closed, isTrue);
  });

  test('parses realtime ready and task events', () {
    final ready = MobileRealtimeEvent.tryParse(
      jsonEncode({
        'type': 'ready',
        'user_id': 'user-1',
        'tenant_id': 'tenant-1',
        'server_time': '2026-07-01T00:00:00Z',
      }),
    );
    final task = MobileRealtimeEvent.tryParse({
      'type': 'digital_employee_task',
      'task_id': 'task-1',
      'status': 'done',
      'payload': {'result': 'ok'},
    });

    expect(ready?.ready, isTrue);
    expect(ready?.userId, 'user-1');
    expect(ready?.tenantId, 'tenant-1');
    expect(ready?.serverTime, DateTime.utc(2026, 7, 1));
    expect(task?.digitalEmployeeTask, isTrue);
    expect(task?.taskId, 'task-1');
    expect(task?.status, 'done');
    expect(task?.payload['result'], 'ok');
    expect(MobileRealtimeEvent.tryParse('not json'), isNull);
  });

  test('parses hub task payload as realtime event payload', () {
    final event = MobileRealtimeEvent.tryParse({
      'type': 'document_task',
      'job_id': 'mobexp_1',
      'status': 'ready',
      'task': {
        'job_id': 'mobexp_1',
        'draft_id': 'draft-1',
        'format': 'pdf',
        'status': 'ready',
      },
    });

    expect(event?.documentTask, isTrue);
    expect(event?.payload['draft_id'], 'draft-1');
    expect(event?.payload['format'], 'pdf');
  });

  test('events emits parsed realtime frames and closes channel', () async {
    Uri? connectedUri;
    final controller = StreamController<Object?>();
    final channel = _FakeWebSocketChannel(stream: controller.stream);
    final client = MobileRealtimeClient(
      readToken: () async => null,
      readHubUrl: () async => 'https://tenant-a.maclaw.top',
      connect: (uri) {
        connectedUri = uri;
        return channel;
      },
    );

    final received = <MobileRealtimeEvent>[];
    final subscription = client.events().listen(received.add);
    controller
      ..add(jsonEncode({'type': 'ready', 'user_id': 'user-1'}))
      ..add('bad frame')
      ..add(jsonEncode({'type': 'pong'}));
    await controller.close();
    await subscription.asFuture<void>();

    expect(
      connectedUri.toString(),
      'wss://tenant-a.maclaw.top/api/mobile/realtime',
    );
    expect(received.map((event) => event.type), ['ready', 'pong']);
    expect(channel.sink.closed, isTrue);
  });
}

class _FakeWebSocketChannel implements WebSocketChannel {
  final Stream _stream;

  _FakeWebSocketChannel({
    Stream? stream,
  }) : _stream = stream ?? const Stream.empty();

  @override
  final _FakeWebSocketSink sink = _FakeWebSocketSink();

  @override
  Future<void> get ready => Future<void>.value();

  @override
  Stream get stream => _stream;

  @override
  String? get protocol => null;

  @override
  int? get closeCode => null;

  @override
  String? get closeReason => null;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeWebSocketSink implements WebSocketSink {
  final List<Object?> sent = [];
  bool closed = false;
  final Completer<void> _done = Completer<void>();

  @override
  void add(event) => sent.add(event);

  @override
  void addError(Object error, [StackTrace? stackTrace]) {}

  @override
  Future addStream(Stream stream) async {
    await for (final event in stream) {
      sent.add(event);
    }
  }

  @override
  Future close([int? closeCode, String? closeReason]) async {
    closed = true;
    if (!_done.isCompleted) {
      _done.complete();
    }
  }

  @override
  Future get done => _done.future;
}
