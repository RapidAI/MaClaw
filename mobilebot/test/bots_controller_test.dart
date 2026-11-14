import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/api_client.dart';
import 'package:mobilebot/core/hub_config.dart';
import 'package:mobilebot/core/providers.dart';
import 'package:mobilebot/features/bots/bot_api.dart';
import 'package:mobilebot/features/bots/bot_models.dart';
import 'package:mobilebot/features/bots/bots_controller.dart';

final _account = Account(
  key: 'test@example.com',
  label: 'test',
  hubUrl: 'https://hub.example.com',
  token: 'tok',
  clientId: 'c1',
  addedAt: DateTime(2026),
);

class _FakeApi extends BotApi {
  _FakeApi() : super(ApiClient(_account));

  BotAccess access = const BotAccess(enabled: true);
  List<Bot> bots = const [];
  Object? refreshError;
  String? deletedBotId;
  Future<List<Bot>>? pendingBots;

  @override
  Future<BotAccess> fetchAccess() async {
    if (refreshError != null) throw refreshError!;
    return access;
  }

  @override
  Future<List<Bot>> listBots() async {
    if (refreshError != null) throw refreshError!;
    if (pendingBots != null) return pendingBots!;
    return bots;
  }

  @override
  Future<Bot> createBot({
    required String name,
    String description = '',
    String? color,
    String? shape,
  }) async =>
      Bot(id: 'new-1', name: name, description: description);

  @override
  Future<void> deleteBot(String botId) async {
    deletedBotId = botId;
  }
}

class _FakeSession extends SessionController {
  @override
  SessionState build() => SessionState(
        loaded: true,
        accounts: [_account],
        active: _account,
      );
}

ProviderContainer _container(_FakeApi api) => ProviderContainer(
      overrides: [
        botApiProvider.overrideWithValue(api),
        sessionProvider.overrideWith(_FakeSession.new),
      ],
    );

Future<void> _settle(ProviderContainer container) async {
  // build() schedules refresh() in a microtask; let it finish.
  for (var i = 0; i < 50 && container.read(botsProvider).loading; i++) {
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
}

const _botA = Bot(id: 'a', name: '小A', description: 'd1');
const _botB = Bot(id: 'b', name: '小B', description: 'd2');

void main() {
  test('refresh loads access and bot list', () async {
    final api = _FakeApi()..bots = const [_botA, _botB];
    final container = _container(api);
    addTearDown(container.dispose);

    await _settle(container);
    final state = container.read(botsProvider);
    expect(state.loading, isFalse);
    expect(state.access.enabled, isTrue);
    expect(state.bots.map((b) => b.id), ['a', 'b']);
  });

  test('disabled access short-circuits without listing bots', () async {
    final api = _FakeApi()
      ..access = const BotAccess(enabled: false, message: 'no quota')
      ..bots = const [_botA];
    final container = _container(api);
    addTearDown(container.dispose);

    await _settle(container);
    final state = container.read(botsProvider);
    expect(state.access.enabled, isFalse);
    expect(state.bots, isEmpty);
  });

  test('refresh error lands in state', () async {
    final api = _FakeApi()..refreshError = StateError('network down');
    final container = _container(api);
    addTearDown(container.dispose);

    await _settle(container);
    final state = container.read(botsProvider);
    expect(state.loading, isFalse);
    expect(state.error, contains('network down'));
  });

  test('BOT_DISABLED lands the disabled state without expiring', () async {
    final api = _FakeApi()
      ..refreshError =
          const HubApiException('BOT_DISABLED', '未开通 Bot 功能', statusCode: 403);
    final container = _container(api);
    addTearDown(container.dispose);

    await _settle(container);
    final state = container.read(botsProvider);
    expect(state.access.enabled, isFalse);
    expect(state.access.message, '未开通 Bot 功能');
    // A feature denial is not a dead credential — stay signed in.
    expect(container.read(sessionProvider).expired, isFalse);
  });

  test('an unauthorized refresh marks the session expired', () async {
    final api = _FakeApi()
      ..refreshError =
          const HubApiException('UNAUTHORIZED', '凭证过期', statusCode: 401);
    final container = _container(api);
    addTearDown(container.dispose);

    await _settle(container);
    expect(container.read(botsProvider).error, isNotEmpty);
    expect(container.read(sessionProvider).expired, isTrue);
  });

  test('a late reply from the previous api is dropped on rebuild', () async {
    final slow = Completer<List<Bot>>();
    final oldApi = _FakeApi()..pendingBots = slow.future;
    final newApi = _FakeApi()..bots = const [_botB];
    final container = ProviderContainer(overrides: [
      botApiProvider.overrideWithValue(oldApi),
      sessionProvider.overrideWith(_FakeSession.new),
    ]);
    addTearDown(container.dispose);

    // The initial refresh starts against the old api, then the provider is
    // rebound (account switch) before it resolves.
    await Future<void>.delayed(Duration.zero);
    container.updateOverrides([
      botApiProvider.overrideWithValue(newApi),
      sessionProvider.overrideWith(_FakeSession.new),
    ]);
    await _settle(container);
    expect(container.read(botsProvider).bots.map((b) => b.id), ['b']);

    // The stale response resolves after the rebuild — its writes must not
    // stamp the old account's list over the new one.
    slow.complete(const [_botA]);
    await Future<void>.delayed(const Duration(milliseconds: 20));
    expect(container.read(botsProvider).bots.map((b) => b.id), ['b']);
  });

  test('create appends the new bot and remove drops it', () async {
    final api = _FakeApi()..bots = const [_botA];
    final container = _container(api);
    addTearDown(container.dispose);
    await _settle(container);

    final created =
        await container.read(botsProvider.notifier).create('新Bot', 'd');
    expect(created.id, 'new-1');
    expect(container.read(botsProvider).bots.map((b) => b.id),
        ['a', 'new-1']);

    await container.read(botsProvider.notifier).remove(_botA);
    expect(api.deletedBotId, 'a');
    expect(container.read(botsProvider).bots.map((b) => b.id), ['new-1']);
  });
}
