import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'l10n_test_helper.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/api_client.dart';
import 'package:mobilebot/core/providers.dart';
import 'package:mobilebot/features/bots/bot_api.dart';
import 'package:mobilebot/features/bots/bot_chat_controller.dart';
import 'package:mobilebot/features/bots/bot_chat_screen.dart';
import 'package:mobilebot/features/bots/bot_models.dart';
import 'package:mobilebot/features/bots/bots_controller.dart';
import 'package:mobilebot/features/bots/send_trace.dart';
import 'package:mobilebot/core/hub_config.dart';

final _account = Account(
  key: 'test@example.com',
  label: 'test',
  hubUrl: 'https://hub.example.com',
  token: 'tok',
  clientId: 'c1',
  addedAt: DateTime(2026),
);

const _bot = Bot(id: 'b1', name: '小助', description: 'd');

/// 1×1 PNG — the wall screenshot a handoff ask carries.
const _tinyPng =
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==';

class _FakeApi extends BotApi {
  _FakeApi() : super(ApiClient(_account));

  ConversationPage page = const ConversationPage();

  @override
  Future<ConversationPage> conversation({
    String botId = '',
    int after = 0,
    int limit = 200,
    int waitSeconds = 0,
  }) async => page;

  String? lastSent;
  BotPhase? lastPhase;
  Object? sendError;
  int sendCalls = 0;
  final sentIds = <String>[];

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
    sendCalls++;
    sentIds.add(clientMessageId ?? '');
    if (sendError != null) throw sendError!;
    return SendResult.settled(const BotReply(text: '好的'));
  }

  @override
  Future<List<PendingDelivery>> pendingDeliveries() async => const [];

  @override
  Future<List<ActiveRun>> activeRuns(String botId) async => const [];

  @override
  Future<QueueView> queueStatus(String botId, String clientMessageId) async =>
      const QueueView(state: 'queued', position: 1);

  @override
  Future<void> writeConversation(
    String clientId,
    List<Map<String, dynamic>> entries,
  ) async {}

  @override
  Future<void> ackRun(String botId, String runId) async {}
}

class _FakeSession extends SessionController {
  @override
  SessionState build() =>
      SessionState(loaded: true, accounts: [_account], active: _account);
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
  child: l10nApp(child: const BotChatScreen(bot: _bot)),
);

void main() {
  testWidgets('renders history messages and the composer', (tester) async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 2,
        entries: [
          _msg(1, 'u1', {'role': 'user', 'content': '做个表格'}),
          _msg(2, 'a1', {'role': 'assistant', 'content': '好了'}),
        ],
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('小助'), findsOneWidget);
    expect(find.text('做个表格'), findsOneWidget);
    expect(find.text('好了'), findsOneWidget);
    expect(find.text('给 Bot 下达任务…'), findsOneWidget);
  });

  testWidgets('shows the queue tag on a queued user bubble', (tester) async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          _msg(1, 'u1', {'role': 'user', 'content': '排队的事', 'queued': true}),
        ],
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('排队中，Bot 忙完后处理'), findsOneWidget);
  });

  testWidgets('failed user bubble shows the reason under resend', (
    tester,
  ) async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          _msg(1, 'u1', {
            'role': 'user',
            'content': '没发出去',
            'failed': true,
            'failureReason': 'BOT_DISABLED',
          }),
        ],
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('发送失败'), findsOneWidget);
    expect(find.text('重发'), findsOneWidget);
    expect(find.text('当前账号未开通 Bot，请联系管理员'), findsOneWidget);
  });

  testWidgets('a send that never reaches the server stays above the composer', (
    tester,
  ) async {
    final api = _FakeApi()..sendError = StateError('SocketException: offline');
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), '发不出去');
    await tester.tap(find.byIcon(Icons.arrow_upward_rounded));
    await tester.pumpAndSettle();

    expect(find.text('发不出去'), findsOneWidget);
    expect(find.byKey(const ValueKey('channel-down')), findsOneWidget);
    expect(find.text('重试'), findsOneWidget);
    // Confirmed miss: one automatic retry, same client id, still one bubble.
    expect(api.sendCalls, 2);
    expect(api.sentIds, hasLength(2));
    expect(api.sentIds[0], api.sentIds[1]);

    await tester.enterText(find.byType(TextField), '再发一条');
    await tester.tap(find.byIcon(Icons.arrow_upward_rounded));
    await tester.pumpAndSettle();

    expect(find.text('再发一条'), findsOneWidget);
    expect(api.sendCalls, 4);
    expect(api.sentIds[2], api.sentIds[3]);
    expect(api.sentIds[0], isNot(api.sentIds[2]));
    expect(api.lastSent, '再发一条');
    expect(find.byKey(const ValueKey('channel-down')), findsOneWidget);

    await tester.tap(find.byTooltip('知道了'));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('channel-down')), findsNothing);
  });

  testWidgets('copy diagnosis reads the ring before the async toast', (
    tester,
  ) async {
    SendTrace.instance.clear();
    addTearDown(SendTrace.instance.clear);
    Map<Object?, Object?>? copied;
    final api = _FakeApi()..sendError = StateError('SocketException: offline');
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), '复制诊断');
    await tester.tap(find.byIcon(Icons.arrow_upward_rounded));
    await tester.pumpAndSettle();

    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        if (call.method == 'Clipboard.setData') {
          copied = Map<Object?, Object?>.from(call.arguments as Map);
        }
        return null;
      },
    );
    addTearDown(
      () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      ),
    );

    final id = api.sentIds.first;
    await tester.tap(find.byKey(ValueKey('copy-send-trace-$id')));
    await tester.pump();
    await tester.pumpAndSettle();

    expect(copied?['text'], isA<String>());
    final text = copied!['text'] as String;
    expect(text, contains('retry'));
    expect(text, contains('id=$id'));
    expect(text, contains('reached=false'));
    expect(RegExp(r'bot=b1 reached( |$)').hasMatch(text), isFalse);
    expect(find.text('发送诊断已复制，可粘贴发给支持人员'), findsOneWidget);
  });

  testWidgets('a 5xx send shows the mapped reason and the channel bar', (
    tester,
  ) async {
    final api = _FakeApi()
      ..sendError = const HubApiException(
        'HTTP_503',
        'bad gateway',
        statusCode: 503,
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), '网关挂了');
    await tester.tap(find.byIcon(Icons.arrow_upward_rounded));
    await tester.pumpAndSettle();

    expect(find.text('发送超时，可能已送达，请勿重复发送'), findsAtLeast(2));
    expect(find.byKey(const ValueKey('channel-down')), findsOneWidget);
    expect(find.text('发送中…'), findsNothing);
    expect(find.text('发送失败'), findsOneWidget);
    // A gateway answer was seen, so the client must not post it again.
    expect(api.sendCalls, 1);

    await tester.tap(find.text('重试'));
    await tester.pumpAndSettle();
    expect(api.sendCalls, 2);
    expect(api.sentIds[0], isNot(api.sentIds[1]));
    expect(api.lastSent, '网关挂了');
  });

  testWidgets('a business rejection stays on the bubble, not the channel bar', (
    tester,
  ) async {
    final api = _FakeApi()
      ..sendError = const HubApiException(
        'DESKTOP_ASLEEP',
        'desktop is asleep',
        statusCode: 409,
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), '叫醒它');
    await tester.tap(find.byIcon(Icons.arrow_upward_rounded));
    await tester.pumpAndSettle();

    expect(find.text('桌面在休眠，请先唤醒再发送'), findsWidgets);
    expect(find.text('发送失败'), findsOneWidget);
    expect(find.byKey(const ValueKey('channel-down')), findsNothing);
    expect(find.text('发送中…'), findsNothing);
  });

  testWidgets('plan replies offer the run-it chip', (tester) async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          _msg(1, 'a1', {
            'role': 'assistant',
            'content': '安排：先搜集资料再写成表格',
            'phase': 'plan',
            'workOrder': '搜集资料并写成表格',
          }),
        ],
      );
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
      ..page = ConversationPage(
        head: 1,
        entries: [
          _msg(1, 'a1', {
            'role': 'assistant',
            'content': '任务线程的回复',
            'conversation': 'b1#run-9',
          }),
        ],
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('继续这个任务'), findsOneWidget);

    // Tapping it arms the composer banner.
    await tester.tap(find.text('继续这个任务'));
    await tester.pumpAndSettle();
    expect(find.text('下一条消息将继续该任务线程'), findsOneWidget);
  });

  testWidgets('a login-wall ask renders the takeover card, not an answer field', (
    tester,
  ) async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          _msg(1, 'a1', {
            'role': 'assistant',
            'content': '这一步需要你在当前桌面的浏览器里完成登录或验证。',
            'attentionReason': 'login_wall',
            'askUser': {
              'inputType': 'text',
              'question': '这一步需要你在当前桌面的浏览器里完成登录或验证。',
            },
            'images': [
              {'mime': 'image/png', 'data': _tinyPng},
            ],
          }),
        ],
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    // The localized reason and the takeover button replace the input card.
    expect(find.text('需要你登录'), findsOneWidget);
    expect(find.text('接管'), findsOneWidget);
    expect(find.text('输入回答'), findsNothing);
    expect(find.text('提交'), findsNothing);
  });

  testWidgets('a plain ask keeps the answer field and gains no takeover button', (
    tester,
  ) async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          _msg(1, 'a1', {
            'role': 'assistant',
            'content': '站点密码是多少?',
            'askUser': {'inputType': 'text', 'question': '站点密码是多少?'},
          }),
        ],
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('输入回答'), findsOneWidget);
    expect(find.text('提交'), findsOneWidget);
    expect(find.text('接管'), findsNothing);
  });

  testWidgets('a choice ask that rides with a handoff keeps its option chips', (
    tester,
  ) async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          _msg(1, 'a1', {
            'role': 'assistant',
            'content': '登录方式选哪个?',
            'attentionReason': 'login_wall',
            'askUser': {
              'inputType': 'choice',
              'question': '登录方式选哪个?',
              'options': ['扫码登录', '密码登录'],
            },
          }),
        ],
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    // The chips stay tappable — a handoff ask is always free text, so an
    // options ask is never treated as one.
    expect(find.text('扫码登录'), findsOneWidget);
    expect(find.text('密码登录'), findsOneWidget);
    expect(find.text('接管'), findsNothing);
  });
}
