import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/api_client.dart';
import 'package:mobilebot/core/providers.dart';
import 'package:mobilebot/features/bots/bot_api.dart';
import 'package:mobilebot/features/bots/bot_models.dart';
import 'package:mobilebot/features/bots/bots_controller.dart';
import 'package:mobilebot/features/bots/bots_screen.dart';

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

  @override
  Future<BotAccess> fetchAccess() async => access;

  @override
  Future<List<Bot>> listBots() async => bots;
}

class _FakeSession extends SessionController {
  @override
  SessionState build() => SessionState(
        loaded: true,
        accounts: [_account],
        active: _account,
      );
}

Widget _app(_FakeApi api) => ProviderScope(
      overrides: [
        botApiProvider.overrideWithValue(api),
        sessionProvider.overrideWith(_FakeSession.new),
      ],
      child: const MaterialApp(home: BotsScreen()),
    );

const _bot = Bot(
  id: 'b1',
  name: '小助',
  description: '帮我干活',
  color: '#5B8DEF',
  shape: 'cloud',
  primary: true,
);

void main() {
  testWidgets('renders bot cards with the primary badge', (tester) async {
    final api = _FakeApi()..bots = const [_bot];
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('我的 Bot'), findsOneWidget);
    expect(find.text('小助'), findsOneWidget);
    expect(find.text('帮我干活'), findsOneWidget);
    expect(find.text('主'), findsOneWidget);
    expect(find.text('新建 Bot'), findsOneWidget);
  });

  testWidgets('shows the disabled-access empty state', (tester) async {
    final api = _FakeApi()
      ..access = const BotAccess(enabled: false, message: '未开通');
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('未开通'), findsOneWidget);
    expect(find.text('小助'), findsNothing);
  });

  testWidgets('create sheet opens from the FAB', (tester) async {
    final api = _FakeApi();
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.tap(find.text('新建 Bot'));
    await tester.pumpAndSettle();

    expect(find.text('名称'), findsOneWidget);
    expect(find.text('简介（可选）'), findsOneWidget);
  });
}
