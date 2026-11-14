import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/api_client.dart';
import 'package:mobilebot/core/providers.dart';
import 'package:mobilebot/features/bots/bot_api.dart';
import 'package:mobilebot/features/bots/bot_chat_controller.dart';
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

  int conversationCalls = 0;
  ConversationPage page = const ConversationPage();
  SendResult? sendResult;
  Object? sendError;
  final pollAnswers = <BotReply?>[];
  String? cancelledRunId;
  String? lastContinueSession;
  String? lastAskRun;
  String? lastSent;

  @override
  Future<ConversationPage> conversation({
    String botId = '',
    int after = 0,
    int limit = 200,
    int waitSeconds = 0,
  }) async {
    conversationCalls++;
    return page;
  }

  @override
  Future<SendResult> sendMessage(
    String botId, {
    required String content,
    BotPhase? phase,
    String? clientMessageId,
    String? continueSession,
    String? askRun,
  }) async {
    lastContinueSession = continueSession;
    lastAskRun = askRun;
    lastSent = content;
    if (sendError != null) throw sendError!;
    return sendResult ??
        SendResult.settled(const BotReply(text: '好的，收到'));
  }

  @override
  Future<BotReply?> pollRun(String botId, String runId) async =>
      pollAnswers.isEmpty
          ? const BotReply(text: '跑完了')
          : pollAnswers.removeAt(0);

  List<ActiveRun> runs = const [];

  @override
  Future<List<ActiveRun>> activeRuns(String botId) async => runs;

  @override
  Future<List<PendingDelivery>> pendingDeliveries() async => const [];

  @override
  Future<void> writeConversation(
      String clientId, List<Map<String, dynamic>> entries) async {}

  @override
  Future<void> ackRun(String botId, String runId) async {}

  @override
  Future<void> cancelRun(String botId, String runId) async {
    cancelledRunId = runId;
  }

  QueueView queueView = const QueueView(state: 'queued', position: 2);

  @override
  Future<QueueView> queueStatus(
          String botId, String clientMessageId) async =>
      queueView;
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
        botChatBotIdProvider.overrideWithValue('b1'),
        sessionProvider.overrideWith(_FakeSession.new),
      ],
    );

/// Waits until [predicate] holds or the deadline passes; chat work is async.
Future<void> _until(bool Function() ok,
    {Duration timeout = const Duration(seconds: 5)}) async {
  final end = DateTime.now().add(timeout);
  while (!ok() && DateTime.now().isBefore(end)) {
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  expect(ok(), isTrue);
}

ConversationEntry _msg(int seq, String id, String role, String content) =>
    ConversationEntry(
      seq: seq,
      createdSeq: seq,
      botId: 'b1',
      id: id,
      kind: 'message',
      data: {
        'role': role,
        'content': content,
        'at': DateTime.fromMillisecondsSinceEpoch(seq * 1000, isUtc: true)
            .toIso8601String(),
      },
    );

void main() {
  test('history merges journal entries in created order', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 3,
        entries: [
          _msg(3, 'm3', 'assistant', '第三条'),
          _msg(1, 'm1', 'user', '第一条'),
          _msg(2, 'm2', 'assistant', '第二条'),
        ],
      );
    final container = _container(api);
    addTearDown(container.dispose);

    await _until(() => !container.read(botChatProvider).loadingHistory);
    final messages = container.read(botChatProvider).messages;
    expect(messages.map((m) => m.id), ['m1', 'm2', 'm3']);
    expect(messages.first.role, 'user');
  });

  test('send settles a synchronous reply', () async {
    final api = _FakeApi();
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('你好');
    final state = container.read(botChatProvider);
    expect(state.sending, isFalse);
    expect(state.messages.length, 2);
    expect(state.messages[0].role, 'user');
    expect(state.messages[0].pending, isFalse);
    expect(state.messages[1].content, '好的，收到');
  });

  test('send admitted run polls until the reply lands', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.admitted(
          const BotRun(runId: 'r1', status: 'running'))
      ..pollAnswers.addAll([null, const BotReply(text: '跑完了')]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('干活');
    expect(container.read(botChatProvider).activeRunId, 'r1');

    // The composer frees right after admission; wait for the poll to land
    // the reply (two ticks at ~1.6s each).
    expect(container.read(botChatProvider).sending, isFalse);
    await _until(
        () => container.read(botChatProvider).activeRunId == null,
        timeout: const Duration(seconds: 8));
    final messages = container.read(botChatProvider).messages;
    expect(messages.last.content, '跑完了');
    expect(messages.last.role, 'assistant');
  });

  test('queued send marks the bubble and appends the ack', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.queued(
          position: 2, ack: '收到，排队处理');
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('忙完帮我做这个');
    final state = container.read(botChatProvider);
    expect(state.sending, isFalse);
    expect(state.activeRunId, isNull);
    expect(state.messages[0].queued, isTrue);
    expect(state.messages[0].pending, isFalse);
    expect(state.messages[1].ack, isTrue);
    expect(state.messages[1].content, '收到，排队处理');
  });

  test('admitted run shows the ack bubble while polling', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.admitted(
          const BotRun(runId: 'r1', status: 'running'),
          ack: '好的，这就去',
          routedTitle: '整理周报')
      ..pollAnswers.addAll([const BotReply(text: '完成了')]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('干活');
    expect(container.read(botChatProvider).messages[1].ack, isTrue);
    expect(container.read(botChatProvider).messages[1].content,
        contains('整理周报'));
    await _until(
        () => container.read(botChatProvider).activeRunId == null,
        timeout: const Duration(seconds: 8));
  });

  test('a synced reply clears the queue tag on earlier user bubbles', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.queued(position: 1);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('排队的事');
    expect(container.read(botChatProvider).messages[0].queued, isTrue);

    // The queued run finishes elsewhere; its reply lands via the journal.
    api.page = ConversationPage(
      head: 3,
      entries: [
        _msg(1, 'u1', 'user', '排队的事'),
        _msg(2, 'a1', 'assistant', '做完了'),
      ],
    );
    // Trigger a sync by forcing a reload through refresh-equivalent path:
    // _syncOnce runs on a timer; emulate by re-entering history load.
    await container.read(botChatProvider.notifier).send('');
    await _until(
        () => container.read(botChatProvider).messages
            .any((m) => m.content == '做完了'),
        timeout: const Duration(seconds: 6));
    expect(
        container
            .read(botChatProvider)
            .messages
            .where((m) => m.queued),
        isEmpty);
  });

  test('a queued bubble tracks its live position and clears when running',
      () async {
    final api = _FakeApi()
      ..sendResult = SendResult.queued(position: 1)
      ..queueView = const QueueView(state: 'queued', position: 2);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('排队的事');
    final msgId = container
        .read(botChatProvider)
        .messages
        .firstWhere((m) => m.isUser)
        .id;

    // The sync loop polls queue/{cmid} and lands the live position.
    await _until(() =>
        container
            .read(botChatProvider)
            .messages
            .firstWhere((m) => m.id == msgId)
            .queuePosition ==
        2);

    // Once the queue view leaves queued/held the tag clears.
    api.queueView = const QueueView(state: 'running', runId: 'r9');
    await _until(() => !container
        .read(botChatProvider)
        .messages
        .firstWhere((m) => m.id == msgId)
        .queued);
  });

  test('a still-running turn is adopted after reopening the chat', () async {
    final api = _FakeApi()
      ..runs = const [
        ActiveRun(runId: 'r-live', status: 'running', task: '跑着的任务'),
      ]
      ..pollAnswers.add(const BotReply(text: '跑完了'));
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    // The recovered run resumes polling; the composer stays free so the
    // person can still queue messages or answer a pending ask card.
    await _until(() => container.read(botChatProvider).activeRunId == 'r-live');
    expect(container.read(botChatProvider).sending, isFalse);

    await _until(
        () => container.read(botChatProvider).activeRunId == null,
        timeout: const Duration(seconds: 8));
    expect(
        container
            .read(botChatProvider)
            .messages
            .map((m) => m.content),
        contains('跑完了'));
  });

  test('a second send while the run executes queues server-side', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.admitted(
          const BotRun(runId: 'r1', status: 'running'))
      ..pollAnswers.addAll([null, null]); // keep the run in flight
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('第一件事');
    expect(container.read(botChatProvider).sending, isFalse);
    expect(container.read(botChatProvider).activeRunId, 'r1');

    // While the turn runs, the next message goes out and lands in the
    // Hub queue — the composer must not be locked out.
    api.sendResult = SendResult.queued(position: 1);
    final queuedNext = await container
        .read(botChatProvider.notifier)
        .send('排队第二件事');
    expect(queuedNext, isTrue);
    final parked = container
        .read(botChatProvider)
        .messages
        .firstWhere((m) => m.isUser && m.content == '排队第二件事');
    expect(parked.queued, isTrue);
    expect(parked.queuePosition, 1);
    expect(container.read(botChatProvider).activeRunId, 'r1');
  });

  test('/clear keeps only in-flight bubbles locally', () async {
    final api = _FakeApi()
      ..page = ConversationPage(head: 2, entries: [
        _msg(1, 'u1', 'user', '历史一'),
        _msg(2, 'a1', 'assistant', '历史答一'),
      ]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);
    expect(container.read(botChatProvider).messages, hasLength(2));

    await container.read(botChatProvider.notifier).send('/clear');
    expect(container.read(botChatProvider).messages, isEmpty);
    expect(api.lastSent, isNull); // never reaches the Hub
  });

  test('failed send marks the bubble and retry resends it', () async {
    final api = _FakeApi()..sendError = StateError('hub down');
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('试一次');
    var state = container.read(botChatProvider);
    expect(state.messages.single.failed, isTrue);
    expect(state.sending, isFalse);

    // Hub recovers; retry drops the failed bubble and sends again.
    api.sendError = null;
    await container
        .read(botChatProvider.notifier)
        .retry(state.messages.single);
    state = container.read(botChatProvider);
    expect(state.messages.where((m) => m.failed), isEmpty);
    expect(state.messages.last.content, '好的，收到');
  });

  test('continueTask threads the next send via continue_session', () async {
    final api = _FakeApi();
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    final notifier = container.read(botChatProvider.notifier);
    notifier.continueTask('b1#run-42');
    expect(container.read(botChatProvider).continueThread, 'b1#run-42');

    // Foreign threads are rejected.
    notifier.continueTask('other-bot#x');
    expect(container.read(botChatProvider).continueThread, isEmpty);
    notifier.continueTask('b1#run-42');

    await notifier.send('接着做');
    expect(api.lastContinueSession, 'b1#run-42');
    expect(container.read(botChatProvider).continueThread, isEmpty);
  });

  test('answering an ask routes back to the asking run and thread', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          ConversationEntry(
            seq: 1,
            createdSeq: 1,
            botId: 'b1',
            id: 'ask1',
            kind: 'message',
            data: {
              'role': 'assistant',
              'content': '选哪个？',
              'runId': 'run-7',
              'conversation': 'b1#run-7',
              'at': DateTime(2026).toIso8601String(),
            },
          ),
        ],
      );
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    final ask = container.read(botChatProvider).messages.single;
    await container
        .read(botChatProvider.notifier)
        .answerQuestion(ask, '第一个');
    expect(api.lastAskRun, 'run-7');
    expect(api.lastContinueSession, 'b1#run-7');
  });

  test('cancelActiveRun forwards to the api', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.admitted(
          const BotRun(runId: 'r9', status: 'running'))
      ..pollAnswers.addAll([null, null, const BotReply(text: 'done')]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('任务');
    expect(container.read(botChatProvider).activeRunId, 'r9');
    await container.read(botChatProvider.notifier).cancelActiveRun();
    expect(api.cancelledRunId, 'r9');
  });

  test('journal patch updates ask/options flags in place', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 2,
        entries: [
          _msg(1, 'q1', 'assistant', '选哪个？'),
          const ConversationEntry(
            seq: 2,
            createdSeq: 2,
            botId: 'b1',
            id: 'q1',
            kind: 'message',
            data: {'patch': true, 'askResolved': true},
          ),
        ],
      );
    final container = _container(api);
    addTearDown(container.dispose);

    await _until(() => !container.read(botChatProvider).loadingHistory);
    final messages = container.read(botChatProvider).messages;
    expect(messages.single.id, 'q1');
    expect(messages.single.askResolved, isTrue);
  });

  // A poll-settled reply that the journal already showed (another client
  // or a Hub projection landed it first) must not appear twice — and must
  // not be re-projected back into the journal either.
  test('a poll reply already in the journal is not duplicated', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 2,
        entries: [
          ConversationEntry(
            seq: 1,
            createdSeq: 1,
            botId: 'b1',
            id: 'u1',
            kind: 'message',
            data: {
              'role': 'user',
              'content': '干活',
              'at': DateTime.now().toIso8601String(),
            },
          ),
          ConversationEntry(
            seq: 2,
            createdSeq: 2,
            botId: 'b1',
            id: 'a1',
            kind: 'message',
            data: {
              'role': 'assistant',
              'content': '跑完了',
              'at': DateTime.now().toIso8601String(),
            },
          ),
        ],
      )
      ..sendResult = SendResult.admitted(
          const BotRun(runId: 'r1', status: 'running'))
      ..pollAnswers.addAll([const BotReply(text: '跑完了')]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('再来一次');
    await _until(
        () => container.read(botChatProvider).activeRunId == null,
        timeout: const Duration(seconds: 8));

    final replies = container
        .read(botChatProvider)
        .messages
        .where((m) => m.role == 'assistant' && m.content == '跑完了')
        .toList();
    expect(replies, hasLength(1));
  });
}
