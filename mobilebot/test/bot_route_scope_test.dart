import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/app.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/api_client.dart';
import 'package:mobilebot/core/providers.dart';
import 'package:mobilebot/features/bots/bot_api.dart';
import 'package:mobilebot/features/bots/bot_models.dart';
import 'package:mobilebot/features/bots/bots_controller.dart';

final _account = Account(
    key: 't', label: 't', hubUrl: 'https://h.example.com',
    token: 'tok', clientId: 'c1', addedAt: DateTime(2026));

const _bot = Bot(id: 'b1', name: '小助', description: 'd');

class _FakeApi extends BotApi {
  _FakeApi() : super(ApiClient(_account));
  @override
  Future<ConversationPage> conversation(
          {String botId = '', int after = 0, int limit = 200, int waitSeconds = 0}) async =>
      const ConversationPage();
  @override
  Future<List<PendingDelivery>> pendingDeliveries() async => const [];
  @override
  Future<List<ActiveRun>> activeRuns(String botId) async => const [];
  @override
  Future<BotAccess> fetchAccess() async => const BotAccess(enabled: true);
  @override
  Future<List<Bot>> listBots() async => const [_bot];
}

class _FakeSession extends SessionController {
  @override
  SessionState build() =>
      SessionState(loaded: true, accounts: [_account], active: _account);
}

void main() {
  // Regression: botChatProvider/chatScrollTickProvider must mount inside the
  // bot route's nested ProviderScope (via declared `dependencies`), not the
  // root container — otherwise BotChatController.build hits
  // "botChatBotIdProvider not overridden" on every chat open.
  testWidgets('navigating to /bot/:id builds the chat inside the scope',
      (tester) async {
    final api = _FakeApi();
    await tester.pumpWidget(ProviderScope(
      overrides: [
        botApiProvider.overrideWithValue(api),
        sessionProvider.overrideWith(_FakeSession.new),
      ],
      child: const MaClawBotApp(),
    ));
    // Let the router settle on '/' and the bots refresh land.
    for (var i = 0; i < 10; i++) {
      await tester.pump(const Duration(milliseconds: 50));
    }
    final err1 = tester.takeException();
    expect(err1, isNull, reason: '$err1');

    // Tap the bot card → context.push('/bot/b1')
    final card = find.text('小助');
    expect(card, findsWidgets);
    await tester.tap(card.first);
    for (var i = 0; i < 10; i++) {
      await tester.pump(const Duration(milliseconds: 50));
    }
    final err2 = tester.takeException();
    expect(err2, isNull, reason: '$err2');
    expect(find.text('给 Bot 下达任务…'), findsOneWidget);
  });
}
