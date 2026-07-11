import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/storage/secure_vault.dart';

class _StreamAdapter implements HttpClientAdapter {
  _StreamAdapter(this.body, {this.contentType = 'text/event-stream'});

  final String body;
  final String contentType;

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return ResponseBody.fromString(
      body,
      200,
      headers: {
        Headers.contentTypeHeader: [contentType],
      },
    );
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('searchWithContextStream parses meta delta done SSE', () async {
    final sse = StringBuffer()
      ..writeln('event: meta')
      ..writeln(
        'data: ${jsonEncode({
          'citations': [
            {
              'title': 'Src',
              'url': 'https://example.test',
              'snippet': 'ok',
            }
          ],
          'llm_mode': 'maclaw_official',
          'llm_request_id': 'req-1',
          'status': 'streaming',
        })}',
      )
      ..writeln()
      ..writeln('event: delta')
      ..writeln('data: ${jsonEncode({'text': '结论：'})}')
      ..writeln()
      ..writeln('event: delta')
      ..writeln('data: ${jsonEncode({'text': '正常'})}')
      ..writeln()
      ..writeln('event: done')
      ..writeln(
        'data: ${jsonEncode({
          'answer': '结论：正常',
          'citations': [
            {
              'title': 'Src',
              'url': 'https://example.test',
              'snippet': 'ok',
            }
          ],
          'llm_mode': 'maclaw_official',
          'llm_request_id': 'req-1',
          'status': 'ready',
        })}',
      )
      ..writeln();

    final dio = Dio(BaseOptions(baseUrl: 'https://tenant-a.maclaw.top'));
    dio.httpClientAdapter = _StreamAdapter(sse.toString());
    final client = ApiClient(
      dio: dio,
      vault: const SecureVault(),
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    final snapshots = await client
        .searchWithContextStream('status')
        .toList()
        .timeout(const Duration(seconds: 3));

    expect(snapshots, isNotEmpty);
    expect(snapshots.first.streaming, isTrue);
    expect(snapshots.first.citations, hasLength(1));
    expect(snapshots.last.streaming, isFalse);
    expect(snapshots.last.answer, '结论：正常');
    expect(snapshots.last.llmRequestId, 'req-1');
    expect(
      snapshots.any((s) => s.streaming && s.answer.contains('结论')),
      isTrue,
    );
  });

  test('searchWithContextStream falls back when response is JSON', () async {
    final payload = jsonEncode({
      'answer': 'JSON path',
      'citations': <dynamic>[],
      'llm_mode': 'maclaw_official',
      'status': 'ready',
    });
    final dio = Dio(BaseOptions(baseUrl: 'https://tenant-a.maclaw.top'));
    dio.httpClientAdapter = _StreamAdapter(
      payload,
      contentType: 'application/json',
    );
    final client = ApiClient(
      dio: dio,
      vault: const SecureVault(),
      hubUrl: 'https://tenant-a.maclaw.top',
    );
    final snapshots = await client.searchWithContextStream('q').toList();
    expect(snapshots, hasLength(1));
    expect(snapshots.single.answer, 'JSON path');
    expect(snapshots.single.streaming, isFalse);
  });

  test('searchWithContextStream parses tool_call and tool_result events', () async {
    final sse = StringBuffer()
      ..writeln('event: meta')
      ..writeln(
        'data: ${jsonEncode({
          'citations': <dynamic>[],
          'agent': true,
          'tools': ['web_search', 'web_fetch'],
          'status': 'streaming',
        })}',
      )
      ..writeln()
      ..writeln('event: tool_call')
      ..writeln(
        'data: ${jsonEncode({
          'id': 'call_1',
          'name': 'web_search',
          'arguments': '{"query":"status"}',
        })}',
      )
      ..writeln()
      ..writeln('event: tool_result')
      ..writeln(
        'data: ${jsonEncode({
          'id': 'call_1',
          'name': 'web_search',
          'result': '找到 1 条结果',
        })}',
      )
      ..writeln()
      ..writeln('event: delta')
      ..writeln('data: ${jsonEncode({'text': '结论：ok'})}')
      ..writeln()
      ..writeln('event: done')
      ..writeln(
        'data: ${jsonEncode({
          'answer': '结论：ok',
          'citations': <dynamic>[],
          'agent': true,
          'status': 'ready',
        })}',
      )
      ..writeln();

    final dio = Dio(BaseOptions(baseUrl: 'https://tenant-a.maclaw.top'));
    dio.httpClientAdapter = _StreamAdapter(sse.toString());
    final client = ApiClient(
      dio: dio,
      vault: const SecureVault(),
      hubUrl: 'https://tenant-a.maclaw.top',
    );
    final snapshots = await client.searchWithContextStream('status').toList();
    expect(snapshots, isNotEmpty);
    expect(
      snapshots.any((s) => s.toolEvents.any((e) => e.name == 'web_search')),
      isTrue,
    );
    expect(snapshots.last.answer, '结论：ok');
    expect(snapshots.last.agent, isTrue);
    expect(snapshots.last.streaming, isFalse);
  });

  test('searchWithContextStream rejects non-2xx status', () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://tenant-a.maclaw.top'));
    dio.httpClientAdapter = _StatusAdapter(503, 'nope');
    final client = ApiClient(
      dio: dio,
      vault: const SecureVault(),
      hubUrl: 'https://tenant-a.maclaw.top',
    );
    await expectLater(
      client.searchWithContextStream('q').toList(),
      throwsA(isA<StateError>()),
    );
  });

  test('searchWithContextStream prefers messages[] over legacy context', () async {
    Map<String, dynamic>? body;
    final dio = Dio(BaseOptions(baseUrl: 'https://tenant-a.maclaw.top'));
    dio.httpClientAdapter = _CapturingAdapter(
      onBody: (decoded) => body = decoded,
      responseBody: jsonEncode({
        'answer': 'ok',
        'citations': <dynamic>[],
        'status': 'ready',
      }),
      contentType: 'application/json',
    );
    final client = ApiClient(
      dio: dio,
      vault: const SecureVault(),
      hubUrl: 'https://tenant-a.maclaw.top',
    );

    await client
        .searchWithContextStream(
          '当前问题',
          messages: const [
            {'role': 'user', 'content': '上一轮'},
            {'role': 'assistant', 'content': '上一答'},
          ],
          context: const ['user: 旧格式'],
        )
        .toList();

    expect(body, isNotNull);
    expect(body!['query'], '当前问题');
    expect(body!['stream'], isTrue);
    expect(body!['messages'], isA<List>());
    expect(body!['messages'], hasLength(2));
    // Legacy context is still sent so older hubs keep multi-turn fallback.
    expect(body!['context'], isA<List>());
    expect(body!['context'], contains('user: 旧格式'));
  });
}

class _CapturingAdapter implements HttpClientAdapter {
  _CapturingAdapter({
    required this.onBody,
    required this.responseBody,
    this.contentType = 'application/json',
  });

  final void Function(Map<String, dynamic> body) onBody;
  final String responseBody;
  final String contentType;

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    if (requestStream != null) {
      final chunks = <int>[];
      await for (final piece in requestStream) {
        chunks.addAll(piece);
      }
      final decoded = jsonDecode(utf8.decode(chunks));
      if (decoded is Map<String, dynamic>) {
        onBody(decoded);
      } else if (decoded is Map) {
        onBody(Map<String, dynamic>.from(decoded));
      }
    }
    return ResponseBody.fromString(
      responseBody,
      200,
      headers: {
        Headers.contentTypeHeader: [contentType],
      },
    );
  }
}

class _StatusAdapter implements HttpClientAdapter {
  _StatusAdapter(this.statusCode, this.body);

  final int statusCode;
  final String body;

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return ResponseBody.fromString(
      body,
      statusCode,
      headers: {
        Headers.contentTypeHeader: ['text/event-stream'],
      },
    );
  }
}
