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
  final String error;
  final bool loading;

  const BotsState({
    this.access = const BotAccess(enabled: true),
    this.bots = const [],
    this.error = '',
    this.loading = true,
  });

  BotsState copyWith({
    BotAccess? access,
    List<Bot>? bots,
    String? error,
    bool? loading,
  }) =>
      BotsState(
        access: access ?? this.access,
        bots: bots ?? this.bots,
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
    state = state.copyWith(loading: true, error: '');
    try {
      final access = await api.fetchAccess();
      if (!access.enabled) {
        state = state.copyWith(access: access, bots: const [], loading: false);
        return;
      }
      final bots = await api.listBots();
      state = state.copyWith(access: access, bots: bots, loading: false);
    } on Object catch (error) {
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
      {String? color, String? shape}) async {
    final api = _api;
    if (api == null) throw StateError('not signed in');
    final bot = await api.createBot(
        name: name, description: description, color: color, shape: shape);
    state = state.copyWith(bots: [...state.bots, bot]);
    return bot;
  }

  Future<void> remove(Bot bot) async {
    final api = _api;
    if (api == null) return;
    await api.deleteBot(bot.id);
    state = state.copyWith(
      bots: state.bots.where((b) => b.id != bot.id).toList(),
    );
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
