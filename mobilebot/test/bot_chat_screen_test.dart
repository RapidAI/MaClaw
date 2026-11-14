import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/api_client.dart';
import 'package:mobilebot/core/providers.dart';
import 'package:mobilebot/features/bots/bot_api.dart';
import 'package:mobilebot/features/bots/bot_chat_controller.dart';
import 'package:mobilebot/features/bots/bot_chat_screen.dart';
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

const _bot = Bot(id: 'b1', name: '小助', description: 'd');

class _FakeApi extends BotApi {
  _FakeApi() : super(ApiClient(_account));

  ConversationPage page = const ConversationPage();

  @override
  Future<ConversationPage> conversation({
    String botId = '',
    int after = 0,
    int limit = 200,
    int waitSeconds = 0,
  }) async =>
      page;

  String? lastSent;
  BotPhase? lastPhase;

  @override
  Future<SendResult> sendMessage(
    String botId, {
    required String content,
    BotPhase? phase,
    String? clientMessageId,
    String? continueSession,
    String? askRun,
  }) async {
    lastSent = content;
    lastPhase = phase;
    return SendResult.settled(const BotReply(text: '好的'));
  }

  @override
  Future<List<PendingDelivery>> pendingDeliveries() async => const [];

  @override
  Future<List<ActiveRun>> activeRuns(String botId) async => const [];

  @override
  Future<QueueView> queueStatus(
          String botId, String clientMessageId) async =>
      const QueueView(state: 'queued', position: 1);

  @override
  Future<void> writeConversation(
      String clientId, List<Map<String, dynamic>> entries) async {}

  @override
  Future<void> ackRun(String botId, String runId) async {}
}

class _FakeSession extends SessionController {
  @override
  SessionState build() => SessionState(
        loaded: true,
        accounts: [_account],
        active: _account,
      );
}

ConversationEntry _msg(int seq, String id, Map<String, dynamic> data) =>
    ConversationEntry(
      seq: seq,
      createdSeq: seq,
      botId: 'b1',
      id: id,
      kind: 'message',
      data: data,
    );

Widget _app(_FakeApi api) => ProviderScope(
      overrides: [
        botApiProvider.overrideWithValue(api),
        sessionProvider.overrideWith(_FakeSession.new),
        botChatBotIdProvider.overrideWithValue('b1'),
      ],
      child: const MaterialApp(home: BotChatScreen(bot: _bot)),
    );

void main() {
  testWidgets('renders history messages and the composer', (tester) async {
    final api = _FakeApi()
      ..page = ConversationPage(head: 2, entries: [
        _msg(1, 'u1', {'role': 'user', 'content': '做个表格'}),
        _msg(2, 'a1', {'role': 'assistant', 'content': '好了'}),
      ]);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('小助'), findsOneWidget);
    expect(find.text('做个表格'), findsOneWidget);
    expect(find.text('好了'), findsOneWidget);
    expect(find.text('给 Bot 下达任务…'), findsOneWidget);
  });

  testWidgets('shows the queue tag on a queued user bubble', (tester) async {
    final api = _FakeApi()
      ..page = ConversationPage(head: 1, entries: [
        _msg(1, 'u1', {'role': 'user', 'content': '排队的事', 'queued': true}),
      ]);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('排队中，Bot 忙完后处理'), findsOneWidget);
  });

  testWidgets('plan replies offer the run-it chip', (tester) async {
    final api = _FakeApi()
      ..page = ConversationPage(head: 1, entries: [
        _msg(1, 'a1', {
          'role': 'assistant',
          'content': '安排：先搜集资料再写成表格',
          'phase': 'plan',
          'workOrder': '搜集资料并写成表格',
        }),
      ]);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('按这个安排执行'), findsOneWidget);

    // Tapping sends the fixed confirm sentence with phase=execute and the
    // chip folds away once the user message lands (foldPlanConfirmation).
    await tester.tap(find.text('按这个安排执行'));
    await tester.pumpAndSettle();
    expect(api.lastSent, botConfirmTaskText);
    expect(api.lastPhase, BotPhase.execute);
    expect(find.text('按这个安排执行'), findsNothing);
  });

  testWidgets('per-run thread replies offer the continue chip', (tester) async {
    final api = _FakeApi()
      ..page = ConversationPage(head: 1, entries: [
        _msg(1, 'a1', {
          'role': 'assistant',
          'content': '任务线程的回复',
          'conversation': 'b1#run-9',
        }),
      ]);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('继续这个任务'), findsOneWidget);

    // Tapping it arms the composer banner.
    await tester.tap(find.text('继续这个任务'));
    await tester.pumpAndSettle();
    expect(find.text('下一条消息将继续该任务线程'), findsOneWidget);
  });
}
