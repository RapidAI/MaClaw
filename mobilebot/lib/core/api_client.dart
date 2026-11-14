import 'package:dio/dio.dart';

import 'account_store.dart';
import 'hub_config.dart';

/// Authenticated Hub client bound to one account. Attaches the bearer token
/// the enrollment produced; every `/api/v1/bots` route accepts it.
class ApiClient {
  final Account account;
  final Dio dio;

  /// Invoked once per response that rejects the credential outright
  /// (401 / UNAUTHORIZED / MACHINE_UNAUTHORIZED) — the session wires this
  /// to `expireActive()` so every endpoint gets expiry handling for free,
  /// not only the call sites that remember to check.
  final void Function()? onAuthRejected;

  ApiClient(this.account, {this.onAuthRejected})
      : dio = hubDio(account.hubUrl)
          ..interceptors.add(
            InterceptorsWrapper(
              onRequest: (options, handler) {
                if (account.token.isNotEmpty) {
                  options.headers['Authorization'] = 'Bearer ${account.token}';
                }
                handler.next(options);
              },
            ),
          );

  Options opts({
    Duration? receiveTimeout,
    Duration? sendTimeout,
    Map<String, dynamic>? headers,
  }) =>
      Options(
        headers: headers,
        receiveTimeout: receiveTimeout,
        sendTimeout: sendTimeout,
        validateStatus: (code) => code != null && code < 500,
      );

  Future<Map<String, dynamic>> guard(
    Future<Response<Map<String, dynamic>>> Function() call,
  ) async {
    late final Response<Map<String, dynamic>> response;
    try {
      response = await call();
    } on DioException catch (error) {
      final mapped = hubExceptionFrom(error);
      if (mapped.isAuthRejected) onAuthRejected?.call();
      throw mapped;
    }
    final status = response.statusCode ?? 0;
    if (status < 200 || status >= 300) {
      final mapped = hubExceptionFrom(
        DioException(
          requestOptions: response.requestOptions,
          response: response,
          type: DioExceptionType.badResponse,
        ),
      );
      if (mapped.isAuthRejected) onAuthRejected?.call();
      throw mapped;
    }
    return response.data ?? const <String, dynamic>{};
  }

  /// Like [guard] but keeps 202 reachable for run polls.
  Future<Response<Map<String, dynamic>>> call(
    Future<Response<Map<String, dynamic>>> Function() fn,
  ) async {
    try {
      final response = await fn();
      final status = response.statusCode ?? 0;
      if (status == 401) {
        onAuthRejected?.call();
      } else {
        final data = response.data;
        final code = data == null
            ? ''
            : (data['error'] as String? ?? data['code'] as String? ?? '')
                .trim();
        if (code == 'UNAUTHORIZED' || code == 'MACHINE_UNAUTHORIZED') {
          onAuthRejected?.call();
        }
      }
      return response;
    } on DioException catch (error) {
      final mapped = hubExceptionFrom(error);
      if (mapped.isAuthRejected) onAuthRejected?.call();
      throw mapped;
    }
  }

  String absoluteUrl(String pathOrUrl) {
    final value = pathOrUrl.trim();
    if (value.isEmpty) return value;
    if (value.startsWith('http://') || value.startsWith('https://')) {
      return value;
    }
    final base = normalizeHubUrl(account.hubUrl);
    return value.startsWith('/') ? '$base$value' : '$base/$value';
  }
}
