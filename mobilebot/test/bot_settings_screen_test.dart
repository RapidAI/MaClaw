import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/api_client.dart';
import 'package:mobilebot/core/providers.dart';
import 'package:mobilebot/features/bots/bot_api.dart';
import 'package:mobilebot/features/bots/bot_models.dart';
import 'package:mobilebot/features/bots/bot_settings_screen.dart';
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

  bool updateCalled = false;

  @override
  Future<BotAccess> fetchAccess() async => const BotAccess(enabled: true);

  @override
  Future<List<Bot>> listBots() async => const [];

  @override
  Future<void> updateBot(String botId,
      {String? name, String? description}) async {
    updateCalled = true;
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

const _bot = Bot(
  id: 'b1',
  name: '小助',
  description: '帮我干活',
  color: '#5B8DEF',
  shape: 'cloud',
  primary: true,
);

void main() {
  testWidgets('a blank name is rejected before any API call', (tester) async {
    final api = _FakeApi();
    await tester.pumpWidget(ProviderScope(
      overrides: [
        botApiProvider.overrideWithValue(api),
        sessionProvider.overrideWith(_FakeSession.new),
      ],
      child: const MaterialApp(home: BotSettingsScreen(bot: _bot)),
    ));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField).first, '   ');
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();

    expect(find.text('名称不能为空'), findsOneWidget);
    expect(api.updateCalled, isFalse);
  });

  testWidgets('BotStyleScreen stops loading when no api is bound',
      (tester) async {
    await tester.pumpWidget(ProviderScope(
      overrides: [
        botApiProvider.overrideWithValue(null),
        sessionProvider.overrideWith(_FakeSession.new),
      ],
      child: const MaterialApp(home: BotStyleScreen(bot: _bot)),
    ));
    await tester.pumpAndSettle();

    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(find.text('人格与记忆'), findsOneWidget);
  });
}
