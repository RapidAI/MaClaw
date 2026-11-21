import 'dart:async';

import 'package:characters/characters.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/hub_config.dart';
import '../../core/locale.dart';
import '../../core/providers.dart';
import 'bot_api.dart';
import 'bot_models.dart';

final botApiProvider = Provider<BotApi?>((ref) {
  final client = ref.watch(apiClientProvider);
  return client == null ? null : BotApi(client);
});

class BotsState {
  final BotAccess access;
  final List<Bot> bots;
  final DesktopBotQuota quota;
  final String error;
  final bool loading;

  const BotsState({
    this.access = const BotAccess(enabled: true),
    this.bots = const [],
    this.quota = const DesktopBotQuota(),
    this.error = '',
    this.loading = true,
  });

  BotsState copyWith({
    BotAccess? access,
    List<Bot>? bots,
    DesktopBotQuota? quota,
    String? error,
    bool? loading,
  }) => BotsState(
    access: access ?? this.access,
    bots: bots ?? this.bots,
    quota: quota ?? this.quota,
    error: error ?? this.error,
    loading: loading ?? this.loading,
  );
}

class BotsController extends Notifier<BotsState> {
  BotApi? get _api => ref.read(botApiProvider);

  @override
  BotsState build() {
    final api = ref.watch(botApiProvider);
    if (api == null) {
      return const BotsState(loading: false);
    }
    Future.microtask(refresh);
    return const BotsState();
  }

  Future<void> refresh() async {
    final api = _api;
    if (api == null) return;
    // A mid-flight account switch rebuilds this controller with the new
    // client — writes from the old request would stamp the previous
    // account's list onto the fresh state. Drop them.
    bool stale() => !identical(ref.read(botApiProvider), api);
    state = state.copyWith(loading: true, error: '');
    try {
      final access = await api.fetchAccess();
      if (stale()) return;
      if (!access.enabled) {
        state = state.copyWith(access: access, bots: const [], loading: false);
        return;
      }
      final bots = await api.listBots();
      if (stale()) return;
      state = state.copyWith(
        access: access,
        bots: bots,
        quota: api.desktopQuota,
        loading: false,
      );
    } on Object catch (error) {
      if (stale()) return;
      noteAuthFailure(ref, error);
      if (error is HubApiException && error.code == 'BOT_DISABLED') {
        // Hub answers 403 BOT_DISABLED for accounts without bot access —
        // render the dedicated empty state instead of a raw error.
        state = state.copyWith(
          access: BotAccess(enabled: false, message: error.message),
          bots: const [],
          loading: false,
        );
        return;
      }
      state = state.copyWith(loading: false, error: failureMessage(error));
    }
  }

  Future<Bot> create(
    String name,
    String description, {
    String? color,
    String? shape,
    bool needsComputer = true,
  }) async {
    final api = _api;
    if (api == null) throw StateError('not signed in');
    final Bot created;
    try {
      created = await api.createBot(
        name: name,
        description: description,
        color: color,
        shape: shape,
        needsComputer: needsComputer,
      );
    } on Object catch (error) {
      // The grant may have shrunk since the last list refresh — resync so
      // the create sheet's lock reflects the server's actual state.
      if (error is HubApiException &&
          error.code == 'DESKTOP_QUOTA_EXCEEDED' &&
          identical(ref.read(botApiProvider), api)) {
        unawaited(refresh());
      }
      rethrow;
    }
    final bot = created.desktopMode.isNotEmpty
        ? created
        : Bot(
            id: created.id,
            name: created.name,
            description: created.description,
            instanceId: created.instanceId,
            desktopMode: needsComputer ? 'shared' : 'none',
            color: created.color,
            shape: created.shape,
            primary: created.primary,
            createdAt: created.createdAt,
          );
    if (identical(ref.read(botApiProvider), api)) {
      final usesComputer = botHasComputer(bot.desktopMode);
      state = state.copyWith(
        bots: [...state.bots, bot],
        quota: usesComputer ? state.quota.bump() : state.quota,
      );
    }
    return bot;
  }

  Future<void> remove(Bot bot) async {
    final api = _api;
    if (api == null) return;
    await api.deleteBot(bot.id);
    if (identical(ref.read(botApiProvider), api)) {
      state = state.copyWith(
        bots: state.bots.where((b) => b.id != bot.id).toList(),
        quota: botHasComputer(bot.desktopMode)
            ? state.quota.drop()
            : state.quota,
      );
    }
  }
}

final botsProvider = NotifierProvider<BotsController, BotsState>(
  BotsController.new,
);

String failureMessage(Object error) {
  if (error is HubApiException) {
    return userFacingSendFailure(
      code: error.code,
      message: error.message,
      statusCode: error.statusCode,
    );
  }
  return userFacingSendFailure(message: error.toString());
}

/// True when the send never reached Hub: network, a gateway 5xx, or a
/// failure before the request left. A timeout or other uncertain failure
/// may already have been accepted, so it is not "never reached". A Hub
/// business rejection (4xx other than a gateway) is a message problem,
/// not a down channel.
bool sendNeverReachedServer(Object error) {
  if (sendOutcomeUncertain(error)) return false;
  if (error is HubApiException) {
    return _sendNeverReached(
      code: error.code,
      message: error.message,
      statusCode: error.statusCode,
    );
  }
  // Anything that is not a Hub answer and not a timeout never left the
  // client: network, or a failure before the request was written.
  return true;
}

/// True only when the request is confirmed not to have left the device.
/// An HTTP answer, including 5xx, means some server saw it. A reset or
/// timeout may also have been written. Neither is safe to auto-retry.
bool sendConfirmedUnsent(Object error) {
  if (sendOutcomeUncertain(error)) return false;
  if (error is HubApiException) {
    if (error.statusCode != null) return false;
    return _confirmedUnsent(code: error.code, message: error.message);
  }
  return _confirmedUnsent(message: error.toString());
}

bool _confirmedUnsent({String code = '', String message = ''}) {
  final rawCode = code.trim().toUpperCase();
  if (rawCode.startsWith('HTTP_')) {
    final status = int.tryParse(rawCode.substring(5));
    if (status != null) return false;
  }
  return _hasAny('$code $message'.toLowerCase(), const [
    'socketexception',
    'clientexception',
    'connection errored',
    'connection error',
    'connection refused',
    'failed host lookup',
    'network is unreachable',
    '无法连接',
    '無法連接',
    '网络错误',
    '網絡錯誤',
    'network error',
    'offline',
  ]);
}

/// True when the request may already have reached Hub. Retrying it would
/// risk a second turn, so the bubble must say it may already be delivered.
bool sendOutcomeUncertain(Object error) {
  if (error is HubApiException) {
    return _sendOutcomeUncertain(
      code: error.code,
      message: error.message,
      statusCode: error.statusCode,
    );
  }
  return _sendOutcomeUncertain(message: error.toString());
}

bool _sendOutcomeUncertain({
  String code = '',
  String message = '',
  int? statusCode,
}) {
  if (statusCode == 408) return true;
  final rawCode = code.trim().toUpperCase();
  if (rawCode == 'HTTP_408' || rawCode == 'HTTP_504') return true;
  if (_looksLikeTimeout('$code $message')) return true;
  return _hasAny('$code $message'.toLowerCase(), const [
    'connection reset',
    'connection abort',
    'broken pipe',
  ]);
}

bool _looksLikeTimeout(String blob) {
  return _hasAny(blob.toLowerCase(), const [
    'connection timed out',
    'timed out',
    'timeout',
    'time out',
    'deadline exceeded',
    '没有在时限内',
    '超时',
    '逾时',
    '逾時',
  ]);
}

bool _sendNeverReached({
  String code = '',
  String message = '',
  int? statusCode,
}) {
  if (statusCode != null && statusCode >= 500) return true;
  final rawCode = code.trim().toUpperCase();
  if (rawCode == 'HTTP_502' ||
      rawCode == 'HTTP_503' ||
      rawCode == 'UNKNOWN' ||
      rawCode == 'HTTP_NULL') {
    return true;
  }
  if (rawCode.startsWith('HTTP_')) {
    final status = int.tryParse(rawCode.substring(5));
    if (status != null && status >= 500) return true;
    if (status != null) return false;
  }
  final blob = '$code $message'.toLowerCase();
  return _hasAny(blob, const [
    'socketexception',
    'clientexception',
    'connection errored',
    'connection error',
    'connection refused',
    'connection reset',
    'failed host lookup',
    'network is unreachable',
    'software caused connection abort',
    '无法连接',
    '無法連接',
    '网络错误',
    '網絡錯誤',
    'network error',
    'offline',
    'hub down',
  ]);
}

/// One readable line under a failed send bubble.
///
/// Known Hub codes and operator copy become a concrete next step. Unknown
/// text is flattened and cut so a long payload cannot stretch the bubble.
/// Display-time only: storage and the journal keep the raw error.
String userFacingSendFailure({
  String code = '',
  String message = '',
  int? statusCode,
  int maxChars = 48,
}) {
  final rawCode = code.trim();
  final rawMessage = message.trim();
  final mapped = _mappedSendFailure(
    rawCode.isNotEmpty ? rawCode : _codeInFailureText(rawMessage),
    rawMessage,
    statusCode,
  );
  final text = mapped.isNotEmpty ? mapped : _flattenFailureText(rawMessage);
  if (text.isEmpty) return sharedL10n.opFailedRetry;
  return _clipFailureLine(text, maxChars);
}

String _mappedSendFailure(String code, String message, int? statusCode) {
  final blob = '$code $message'.toLowerCase();
  final s = sharedL10n;
  if (_hasAny(blob, const [
    '等待回复超时',
    '等待回覆超時',
    'timed out waiting for the reply',
  ])) {
    return s.chatReplyTimedOut;
  }
  if (_looksLikeTimeout(blob) ||
      code == 'HTTP_408' ||
      code == 'HTTP_500' ||
      code == 'HTTP_502' ||
      code == 'HTTP_503' ||
      code == 'HTTP_504' ||
      statusCode == 408 ||
      (statusCode != null && statusCode >= 500)) {
    return s.chatSendMaybeDelivered;
  }
  if (_hasAny(blob, const [
    'llm_service_credits_exhausted',
    'llm_service_credits_required',
    'llm_service_credits_insufficient',
    'credits limit is exhausted',
    'credits are exhausted',
    'grant-backed service group',
  ])) {
    return sharedL10n.chatSendCreditsExhausted;
  }
  if (_hasAny(blob, const ['llm_service_grant_expired', 'grant has expired'])) {
    return sharedL10n.chatSendCreditsExpired;
  }
  if (_hasAny(blob, const ['service restarted', 'run interrupted'])) {
    return sharedL10n.chatSendServiceRestarted;
  }
  if (_hasAny(blob, const [
        'unavailable',
        '不可达',
        'maclawsrv_request_failed',
        'node_offline',
      ]) ||
      code == 'HTTP_502' ||
      code == 'HTTP_503' ||
      statusCode == 502 ||
      statusCode == 503) {
    return s.netConnectionError;
  }
  return switch (code) {
    'BOT_DISABLED' => s.chatSendBotDisabled,
    'SETTINGS_UNAVAILABLE' => s.chatSendSettingsUnavailable,
    'MACHINE_UNAUTHORIZED' ||
    'UNAUTHORIZED' ||
    'HTTP_401' ||
    'HTTP_403' => s.chatSendSessionExpired,
    'INVALID_BOT_SETTINGS' || 'INVALID_INPUT' => s.chatSendInvalidInput,
    'BOT_NOT_FOUND' || 'NOT_FOUND' || 'HTTP_404' => s.chatSendBotMissing,
    'BOT_HAS_RUNNING_TASK' ||
    'BOT_TURN_BUSY' ||
    'DESKTOP_BUSY' => s.chatSendBotBusy,
    'DESKTOP_ASLEEP' => s.chatSendDesktopAsleep,
    'DESKTOP_NOT_RUNNING' || 'NO_DESKTOP' => s.chatSendDesktopNotRunning,
    'DESKTOP_QUOTA_EXCEEDED' => s.chatSendDesktopQuota,
    'CLOUD_DESKTOP_NOT_ASSIGNED' => s.chatSendCloudDesktopMissing,
    'RATE_LIMITED' || 'HTTP_429' => s.chatSendRateLimited,
    'RUN_NOT_RUNNING' => s.chatSendRunEnded,
    _ => _statusFallback(statusCode),
  };
}

String _statusFallback(int? statusCode) {
  if (statusCode == 401 || statusCode == 403) {
    return sharedL10n.chatSendSessionExpired;
  }
  if (statusCode != null && statusCode >= 500) {
    return sharedL10n.netServerError;
  }
  return '';
}

final _failureCode = RegExp(r'\b[A-Z][A-Z0-9_]{2,}\b');

String _codeInFailureText(String text) {
  final match = _failureCode.firstMatch(text);
  return match == null ? '' : match.group(0)!;
}

bool _hasAny(String blob, List<String> needles) {
  for (final needle in needles) {
    if (blob.contains(needle)) return true;
  }
  return false;
}

String _flattenFailureText(String raw) {
  var text = raw.replaceAll(RegExp(r'\s+'), ' ').trim();
  const prefixes = [
    'HubApiException',
    'Exception',
    'StateError',
    'DioException',
  ];
  for (final prefix in prefixes) {
    if (text.startsWith('$prefix:')) {
      text = text.substring(prefix.length + 1).trim();
    } else if (text.startsWith(prefix)) {
      text = text.substring(prefix.length).trim();
    }
  }
  if (text.startsWith(':')) text = text.substring(1).trim();
  if (text.startsWith('(') && text.endsWith(')') && text.length > 2) {
    text = text.substring(1, text.length - 1).trim();
  }
  return text;
}

String _clipFailureLine(String text, int maxChars) {
  final limit = maxChars < 8 ? 8 : maxChars;
  final chars = text.characters;
  if (chars.length <= limit) return text;
  return '${chars.take(limit - 1)}…';
}
