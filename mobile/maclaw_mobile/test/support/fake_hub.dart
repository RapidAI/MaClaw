import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';

/// One request BotApi made, captured for assertions.
class RecordedBotRequest {
  RecordedBotRequest(this.options);

  final RequestOptions options;

  String get method => options.method;

  String get path => options.path;

  Map<String, dynamic> get body {
    final data = options.data;
    if (data is Map) return Map<String, dynamic>.from(data);
    if (data is String && data.isNotEmpty) {
      try {
        final decoded = jsonDecode(data);
        if (decoded is Map) return Map<String, dynamic>.from(decoded);
      } on FormatException {
        return const {};
      }
    }
    return const {};
  }

  Map<String, String> get headers =>
      options.headers.map((key, value) => MapEntry(key, value.toString()));
}

/// A scripted stand-in for the Hub, wired in as Dio's HTTP adapter.
///
/// Dio only has factory constructors, so a test cannot subclass it. Replacing
/// the adapter is the supported seam: every request still goes through the real
/// Dio pipeline — interceptors, `validateStatus`, JSON decoding — so what the
/// tests exercise is the same path the app uses.
class FakeHubAdapter implements HttpClientAdapter {
  FakeHubAdapter(this.respond);

  /// Produces the status and body for one request.
  final ({int status, Map<String, dynamic> body}) Function(
    RecordedBotRequest request,
  ) respond;

  final List<RecordedBotRequest> requests = [];

  /// Requests whose path contains [fragment], in order.
  Iterable<RecordedBotRequest> requestsTo(String fragment) =>
      requests.where((request) => request.path.contains(fragment));

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    final request = RecordedBotRequest(options);
    requests.add(request);
    final answer = respond(request);
    return ResponseBody.fromString(
      jsonEncode(answer.body),
      answer.status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

/// A Dio pointed at [FakeHubAdapter], so nothing touches the network.
Dio dioWithFakeHub(
  FakeHubAdapter adapter, {
  String hubUrl = 'https://hub.example',
}) {
  final dio = Dio(BaseOptions(baseUrl: hubUrl));
  dio.httpClientAdapter = adapter;
  return dio;
}

/// Adapter that fails every request, for the error paths.
class FailingHubAdapter implements HttpClientAdapter {
  FailingHubAdapter(this.error);

  final Object error;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    throw error;
  }

  @override
  void close({bool force = false}) {}
}

/// A DioException shaped like a non-2xx answer from Hub.
DioException httpFailure(int status, {Map<String, dynamic>? data}) {
  return DioException(
    requestOptions: RequestOptions(path: '/api/v1/bots'),
    response: Response<Map<String, dynamic>>(
      requestOptions: RequestOptions(path: '/api/v1/bots'),
      statusCode: status,
      data: data ?? const <String, dynamic>{},
    ),
    type: DioExceptionType.badResponse,
  );
}
