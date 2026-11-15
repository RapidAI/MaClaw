import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/hub_config.dart';
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
  }) =>
      BotsState(
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

  Future<Bot> create(String name, String description,
      {String? color, String? shape, bool needsComputer = true}) async {
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
        quota: botHasComputer(bot.desktopMode) ? state.quota.drop() : state.quota,
      );
    }
  }
}

final botsProvider =
    NotifierProvider<BotsController, BotsState>(BotsController.new);

String failureMessage(Object error) {
  if (error is HubApiException) {
    return error.message.isNotEmpty ? error.message : '操作失败，请重试';
  }
  final text = error.toString();
  return text.isEmpty ? '操作失败，请重试' : text;
}
