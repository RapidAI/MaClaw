import 'package:dio/dio.dart';

import '../../core/api/official_service.dart';
import '../../core/storage/secure_vault.dart';
import 'bot.dart';

/// Raised when a bot call fails with a Hub error code this client can explain.
///
/// The UI shows [message] verbatim; every other failure falls back to the
/// generic transport copy in [botFailureMessage].
class BotApiException implements Exception {
  final String code;
  final String message;
  final int? statusCode;

  const BotApiException(this.code, this.message, {this.statusCode});

  bool get isUnauthorized =>
      code == 'MACHINE_UNAUTHORIZED' || code == 'UNAUTHORIZED';

  /// The bot feature is off for this user or the settings store is unavailable.
  bool get isFeatureDisabled =>
      code == 'BOT_DISABLED' || code == 'SETTINGS_UNAVAILABLE';

  @override
  String toString() => 'BotApiException($code): $message';
}

/// All `/api/v1/bots` traffic.
///
/// This is separate from `ApiClient` because the bot turn protocol needs its
/// own timeouts and, more importantly, its own status handling: sending with
/// `Prefer: respond-async` returns `202 {run_id}` and the reply comes from a
/// later poll, while an older Hub answers the same call with the finished reply
/// inline. [sendMessage] hides that split behind [BotMessageResult].
///
/// The credential is the caller's viewer token from the Hub session. Hub's bot
/// routes accept it directly, so no machine enrollment is needed on mobile.
class BotApi {
  final Dio _dio;
  final SecureVault _vault;
  final String _hubUrl;

  BotApi({
    SecureVault? vault,
    Dio? dio,
    String hubUrl = maclawDefaultHubCenterUrl,
  })  : _vault = vault ?? const SecureVault(),
        _hubUrl = normalizeDiscoveredHubUrl(hubUrl),
        _dio = discoveredHubDio(dio, hubUrl: hubUrl) {
    _dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) async {
          try {
            final token = await _vault.readToken();
            if (token != null && token.isNotEmpty) {
              options.headers['Authorization'] = 'Bearer $token';
            }
          } on Object {
            // Tests and hosts without secure storage still call Hub.
          }
          handler.next(options);
        },
      ),
    );
  }

  String get hubUrl => _hubUrl;

  static const Duration _turnConnectTimeout = Duration(seconds: 20);
  static const Duration _turnSendTimeout = Duration(seconds: 30);
  static const Duration _defaultReceiveTimeout = Duration(seconds: 30);

  /// A single run poll is a short read. A slow one is retried rather than
  /// treated as the end of the turn, so this is well under the turn timeout.
  static const Duration _pollReceiveTimeout = Duration(seconds: 15);

  /// 202 is success for an admitted run and for a poll still in flight, so
  /// every bot read accepts 2xx and reports the status instead of throwing.
  static bool _acceptAnySuccess(int? code) =>
      code != null && code >= 200 && code < 300;

  Options _options({
    Duration? receiveTimeout,
    Map<String, dynamic>? headers,
  }) {
    return Options(
      headers: headers,
      connectTimeout: _turnConnectTimeout,
      sendTimeout: _turnSendTimeout,
      receiveTimeout: receiveTimeout ?? _defaultReceiveTimeout,
      validateStatus: _acceptAnySuccess,
    );
  }

  String _botPath(String botId, [String suffix = '']) {
    final id = botId.trim();
    if (id.isEmpty) {
      throw ArgumentError('botId is required');
    }
    return '/api/v1/bots/${Uri.encodeComponent(id)}$suffix';
  }

  /// Run a call and turn a non-2xx answer into a [BotApiException].
  ///
  /// Every bot read accepts 2xx at the transport level — a 202 is a valid
  /// answer for an admitted run — so the status has to be inspected here.
  /// Without that, a 500 carrying an unrelated body would be parsed as if it
  /// were a bot reply.
  Future<Map<String, dynamic>> _guard(
    Future<Response<Map<String, dynamic>>> Function() call,
  ) async {
    late final Response<Map<String, dynamic>> response;
    try {
      response = await call();
    } on DioException catch (error) {
      throw botApiExceptionFrom(error);
    }
    final status = response.statusCode ?? 0;
    if (status < 200 || status >= 300) {
      throw botApiExceptionFrom(
        DioException(
          requestOptions: response.requestOptions,
          response: response,
          type: DioExceptionType.badResponse,
        ),
      );
    }
    return response.data ?? const <String, dynamic>{};
  }

  /// Whether this user may use bots. Call before showing the rail so a disabled
  /// tenant shows the admin's reason instead of an empty list.
  Future<BotAccess> fetchAccess() async {
    final data = await _guard(
      () => _dio.get<Map<String, dynamic>>(
        '/api/v1/bots/access',
        options: _options(),
      ),
    );
    return BotAccess.fromJson(data);
  }

  Future<List<Bot>> listBots() async {
    final data = await _guard(
      () => _dio.get<Map<String, dynamic>>(
        '/api/v1/bots',
        options: _options(),
      ),
    );
    final raw = data['items'];
    if (raw is! List) return const [];
    return [
      for (final item in raw)
        if (item is Map) Bot.fromJson(Map<String, dynamic>.from(item)),
    ].where((bot) => bot.id.isNotEmpty).toList(growable: false);
  }

  Future<Bot> createBot({
    required String name,
    String description = '',
  }) async {
    final data = await _guard(
      () => _dio.post<Map<String, dynamic>>(
        '/api/v1/bots',
        data: {
          'name': name.trim(),
          'description': description.trim(),
        },
        options: _options(),
      ),
    );
    return Bot.fromJson(data);
  }

  Future<void> renameBot(
    String botId, {
    required String name,
    String description = '',
  }) async {
    await _guard(
      () => _dio.patch<Map<String, dynamic>>(
        _botPath(botId),
        data: {
          'name': name.trim(),
          'description': description.trim(),
        },
        options: _options(),
      ),
    );
  }

  Future<void> deleteBot(String botId) async {
    await _guard(
      () => _dio.delete<Map<String, dynamic>>(
        _botPath(botId),
        options: _options(),
      ),
    );
  }

  /// Send one turn.
  ///
  /// A Hub that honours `Prefer: respond-async` admits the command and answers
  /// `202 {run_id}`; the reply is then read from [pollRun]. A Hub that ignores
  /// the preference answers `200` with the finished reply, reported here as a
  /// settled result with nothing to poll.
  Future<BotMessageResult> sendMessage(
    String botId, {
    required String content,
    BotPhase? phase,
  }) async {
    final text = content.trim();
    if (text.isEmpty) {
      throw ArgumentError('content is required');
    }
    final response = await _guard(
      () => _dio.post<Map<String, dynamic>>(
        _botPath(botId, '/messages'),
        data: {
          'content': text,
          if (phase != null) 'phase': phase.wireValue,
        },
        options: _options(headers: const {'Prefer': 'respond-async'}),
      ),
    );
    final run = BotRun.fromJson(response);
    // A run id is what makes a turn pollable. Its absence means the Hub answered
    // with the finished reply instead, which is settled with nothing to poll.
    return run.accepted
        ? BotMessageResult.admitted(run)
        : BotMessageResult.settled(BotReply.fromJson(response));
  }

  /// Read an admitted run.
  ///
  /// Returns null while the run is still going. A finished run returns the same
  /// reply a synchronous call would have produced.
  Future<BotReply?> pollRun(String botId, String runId) async {
    final id = runId.trim();
    if (id.isEmpty) {
      throw ArgumentError('runId is required');
    }
    final response = await _pollCall(botId, id);
    if (response == null) return null;
    return BotReply.fromJson(response);
  }

  /// One run poll, where a 202 is an expected "still working" answer.
  Future<Map<String, dynamic>?> _pollCall(String botId, String runId) async {
    late final Response<Map<String, dynamic>> response;
    try {
      response = await _dio.get<Map<String, dynamic>>(
        _botPath(botId, '/runs/${Uri.encodeComponent(runId)}'),
        options: _options(receiveTimeout: _pollReceiveTimeout),
      );
    } on DioException catch (error) {
      throw botApiExceptionFrom(error);
    }
    final status = response.statusCode ?? 0;
    if (status == 202) return null;
    if (status < 200 || status >= 300) {
      throw botApiExceptionFrom(
        DioException(
          requestOptions: response.requestOptions,
          response: response,
          type: DioExceptionType.badResponse,
        ),
      );
    }
    return response.data ?? const <String, dynamic>{};
  }

  /// Watch the bot's desktop.
  ///
  /// Holding refreshes the pin that keeps the desktop up while a person is
  /// watching; releasing lets Hub stop it. The hold expires shortly after the
  /// last poll, so a closed page does not pin the desktop forever.
  Future<BotDesktop> watchDesktop(
    String botId, {
    int epoch = 0,
    bool release = false,
  }) async {
    final data = await _guard(
      () => _dio.post<Map<String, dynamic>>(
        _botPath(botId, '/desktop'),
        data: {
          if (release) 'release': true,
          if (epoch > 0) 'epoch': epoch,
        },
        options: _options(),
      ),
    );
    return BotDesktop.fromJson(data);
  }

  /// Hand a login or captcha value to the bot's desktop.
  ///
  /// The value goes straight to MaClawSrv and is neither logged nor stored.
  Future<void> fillSecret(
    String botId, {
    required String name,
    required String value,
  }) async {
    await _guard(
      () => _dio.post<Map<String, dynamic>>(
        _botPath(botId, '/secret-fill'),
        data: {
          'name': name.trim(),
          'value': value,
        },
        options: _options(),
      ),
    );
  }

  /// Absolute URL for a Hub-relative path, for the noVNC desktop link.
  String absoluteUrl(String path) {
    return maclawHubAbsoluteUrl(hubUrl: _hubUrl, pathOrUrl: path);
  }
}

/// The two ways a turn can come back.
class BotMessageResult {
  /// Set when Hub admitted the command and the reply is still to be polled.
  final BotRun? run;

  /// Set when the reply already arrived with the send.
  final BotReply? reply;

  const BotMessageResult._({this.run, this.reply});

  factory BotMessageResult.admitted(BotRun run) => BotMessageResult._(run: run);

  factory BotMessageResult.settled(BotReply reply) =>
      BotMessageResult._(reply: reply);

  bool get isAdmitted => run != null;

  String? get runId => run?.runId;
}

/// Turn a Hub or transport failure into copy worth showing.
///
/// A Hub error carries its own message and code; [botErrorCodeLabel] covers the
/// codes a person can act on. Anything else is reported as a network or server
/// problem instead of leaking a Dio message into the chat bubble.
String botFailureMessage(Object error) {
  if (error is BotApiException) {
    return error.message.isNotEmpty
        ? error.message
        : botErrorCodeLabel(error.code);
  }
  if (error is DioException) {
    final code = error.response?.statusCode ?? 0;
    if (code == 401 || code == 403) return '登录已过期，请重新登录';
    switch (error.type) {
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.receiveTimeout:
      case DioExceptionType.sendTimeout:
        return '网络超时，请检查网络后重试';
      case DioExceptionType.connectionError:
        return '无法连接 MaClaw Hub，请检查网络';
      case DioExceptionType.badCertificate:
        return 'Hub 证书不受信任';
      case DioExceptionType.cancel:
        return '已取消';
      default:
        return code >= 500 ? 'MaClaw Hub 服务异常，请稍后重试' : '请求失败，请稍后重试';
    }
  }
  if (error is ArgumentError) {
    return '请求参数不完整';
  }
  return '操作失败，请重试';
}

/// Chinese copy for the Hub bot error codes this client can act on.
String botErrorCodeLabel(String code) => switch (code) {
      'BOT_DISABLED' => '当前账号未开通 Bot 功能，请联系管理员',
      'SETTINGS_UNAVAILABLE' => 'Bot 配置暂不可用，请稍后重试',
      'MACHINE_UNAUTHORIZED' || 'UNAUTHORIZED' => '登录已过期，请重新登录',
      'INVALID_BOT_SETTINGS' => 'Bot 信息不合法，请检查后重试',
      'NOT_FOUND' => '该 Bot 不存在或已被删除',
      _ => 'Bot 操作失败，请重试',
    };

/// Convert a Dio failure into a [BotApiException] so the UI sees one error
/// type. Hub answers errors as `{"error": CODE, "message": text}`.
BotApiException botApiExceptionFrom(Object error) {
  if (error is BotApiException) return error;
  if (error is DioException) {
    final status = error.response?.statusCode;
    final data = error.response?.data;
    var code = '';
    var message = '';
    if (data is Map) {
      code = (data['error'] as String? ?? data['code'] as String? ?? '').trim();
      message = (data['message'] as String? ?? '').trim();
    }
    if (code.isEmpty) {
      code = switch (status) {
        401 || 403 => 'MACHINE_UNAUTHORIZED',
        404 => 'NOT_FOUND',
        _ => '',
      };
    }
    if (message.isEmpty) {
      message =
          code.isNotEmpty ? botErrorCodeLabel(code) : botFailureMessage(error);
    }
    return BotApiException(code, message, statusCode: status);
  }
  return BotApiException('', botFailureMessage(error));
}
