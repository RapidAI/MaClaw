import 'dart:async';

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
  List<ConversationPage> scripted = const [];
  int _scriptedAt = 0;
  SendResult? sendResult;
  Object? sendError;
  final pollAnswers = <BotReply?>[];
  String? cancelledRunId;
  String? lastContinueSession;
  String? lastAskRun;
  String? lastSent;
  Completer<void>? sendGate;

  @override
  Future<ConversationPage> conversation({
    String botId = '',
    int after = 0,
    int limit = 200,
    int waitSeconds = 0,
  }) async {
    conversationCalls++;
    if (scripted.isNotEmpty && waitSeconds == 0) {
      final i = _scriptedAt < scripted.length
          ? _scriptedAt
          : scripted.length - 1;
      if (_scriptedAt < scripted.length) _scriptedAt++;
      return scripted[i];
    }
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
    if (sendGate != null) await sendGate!.future;
    if (sendError != null) throw sendError!;
    return sendResult ?? SendResult.settled(const BotReply(text: '好的，收到'));
  }

  int pollCalls = 0;
  final polledRuns = <String>[];

  @override
  Future<BotReply?> pollRun(String botId, String runId) async {
    pollCalls++;
    polledRuns.add(runId);
    return pollAnswers.isEmpty
        ? const BotReply(text: '跑完了')
        : pollAnswers.removeAt(0);
  }

  List<ActiveRun> runs = const [];

  @override
  Future<List<ActiveRun>> activeRuns(String botId) async => runs;

  @override
  Future<List<PendingDelivery>> pendingDeliveries() async => const [];

  final written = <Map<String, dynamic>>[];

  @override
  Future<void> writeConversation(
    String clientId,
    List<Map<String, dynamic>> entries,
  ) async {
    written.addAll(entries);
  }

  @override
  Future<void> ackRun(String botId, String runId) async {}

  @override
  Future<void> cancelRun(String botId, String runId) async {
    cancelledRunId = runId;
  }

  QueueView queueView = const QueueView(state: 'queued', position: 2);

  @override
  Future<QueueView> queueStatus(String botId, String clientMessageId) async =>
      queueView;
}

class _FakeSession extends SessionController {
  @override
  SessionState build() =>
      SessionState(loaded: true, accounts: [_account], active: _account);
}

ProviderContainer _container(_FakeApi api) => ProviderContainer(
  overrides: [
    botApiProvider.overrideWithValue(api),
    botChatBotIdProvider.overrideWithValue('b1'),
    sessionProvider.overrideWith(_FakeSession.new),
  ],
);

/// Waits until [predicate] holds or the deadline passes; chat work is async.
Future<void> _until(
  bool Function() ok, {
  Duration timeout = const Duration(seconds: 5),
}) async {
  final end = DateTime.now().add(timeout);
  while (!ok() && DateTime.now().isBefore(end)) {
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  expect(ok(), isTrue);
}

ConversationEntry _msg(
  int seq,
  String id,
  String role,
  String content, {
  int? created,
  String requestId = '',
  bool withAt = true,
  String runId = '',
  List<String> attachmentIds = const [],
}) => ConversationEntry(
  seq: seq,
  createdSeq: created ?? seq,
  botId: 'b1',
  id: id,
  kind: 'message',
  data: {
    'role': role,
    'content': content,
    if (requestId.isNotEmpty) 'requestId': requestId,
    if (runId.isNotEmpty) 'runId': runId,
    if (attachmentIds.isNotEmpty) 'attachmentIds': attachmentIds,
    if (withAt)
      'at': DateTime.fromMillisecondsSinceEpoch(
        seq * 1000,
        isUtc: true,
      ).toIso8601String(),
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

  test('history keeps an edited line at its first sequence', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 5,
        entries: [
          _msg(5, 'm1', 'user', '先', created: 1, withAt: false),
          _msg(2, 'm2', 'user', '后', created: 2, withAt: false),
        ],
      );
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);
    expect(container.read(botChatProvider).messages.map((m) => m.id), [
      'm1',
      'm2',
    ]);
  });

  test(
    'a pending send keeps its sequence so the reply stays below it',
    () async {
      final api = _FakeApi()..sendGate = Completer<void>();
      final container = _container(api);
      addTearDown(container.dispose);
      await _until(() => !container.read(botChatProvider).loadingHistory);

      final send = container.read(botChatProvider.notifier).send('先问');
      await _until(
        () => container.read(botChatProvider).messages.any((m) => m.pending),
      );
      final id = container
          .read(botChatProvider)
          .messages
          .firstWhere((m) => m.pending)
          .id;
      api.page = ConversationPage(
        head: 2,
        entries: [
          ConversationEntry(
            seq: 1,
            createdSeq: 1,
            botId: 'b1',
            id: id,
            kind: 'message',
            data: {'role': 'user', 'content': '先问'},
          ),
          ConversationEntry(
            seq: 2,
            createdSeq: 2,
            botId: 'b1',
            id: 'ans',
            kind: 'message',
            data: {'role': 'assistant', 'content': '答'},
          ),
        ],
      );
      await _until(() {
        final messages = container.read(botChatProvider).messages;
        return messages.length >= 2 && messages.first.syncSeq == 1;
      });
      final messages = container.read(botChatProvider).messages;
      expect(messages.map((m) => m.content), ['先问', '答']);
      expect(messages.first.pending, isTrue);
      api.sendGate!.complete();
      await send;
    },
  );

  test(
    'history follows the server sequence when client clocks disagree',
    () async {
      final api = _FakeApi()
        ..page = ConversationPage(
          head: 3,
          entries: [
            _msg(2, 'm-2z-bbb', 'assistant', '后发', created: 2, withAt: false),
            _msg(1, 'm-k0-aaa', 'user', '先发', created: 1, withAt: false),
          ],
        );
      final container = _container(api);
      addTearDown(container.dispose);

      await _until(() => !container.read(botChatProvider).loadingHistory);
      expect(container.read(botChatProvider).messages.map((m) => m.id), [
        'm-k0-aaa',
        'm-2z-bbb',
      ]);
    },
  );

  test('a synced line with attachment ids re-fetches the run media', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 2,
        entries: [
          _msg(1, 'm1', 'user', '看下屏幕', withAt: false),
          _msg(
            2,
            'm2',
            'assistant',
            '截图在这儿',
            withAt: false,
            runId: 'run-9',
            attachmentIds: ['shot-1'],
          ),
        ],
      )
      ..pollAnswers.add(
        const BotReply(
          text: '截图在这儿',
          images: [
            BotReplyImage(
              mime: 'image/png',
              data: 'aW1n',
              attachmentId: 'shot-1',
            ),
          ],
        ),
      );
    final container = _container(api);
    addTearDown(container.dispose);

    await _until(
      () => container
          .read(botChatProvider)
          .messages
          .any((m) => m.images.isNotEmpty),
    );
    final message = container
        .read(botChatProvider)
        .messages
        .firstWhere((m) => m.id == 'm2');
    expect(message.images.single.attachmentId, 'shot-1');
    expect(message.images.single.data, 'aW1n');
    expect(api.polledRuns, ['run-9']);
  });

  test('a synced line without a timestamp does not probe run media', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          _msg(1, 'm1', 'assistant', '纯文本', withAt: false, runId: 'run-8'),
        ],
      );
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);
    await Future<void>.delayed(const Duration(milliseconds: 100));
    // Idless probing needs a usable `at` for the freshness bound — without
    // one the line is skipped.
    expect(api.pollCalls, 0);
    expect(container.read(botChatProvider).messages.single.images, isEmpty);
  });

  // The desktop saves delivered files on its own disk, so its journal
  // projection usually carries neither bytes nor attachment ids — only a
  // runId. After a restart (or on a phone watching a desktop-sent turn) the
  // refetch must still fire for recent bubbles, or attachments vanish.
  test('a recent idless synced reply re-fetches run media', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          ConversationEntry(
            seq: 1,
            createdSeq: 1,
            botId: 'b1',
            id: 'm1',
            kind: 'message',
            data: {
              'role': 'assistant',
              'content': '文件在这儿',
              'runId': 'run-7',
              'at': DateTime.now().toIso8601String(),
            },
          ),
        ],
      )
      ..pollAnswers.add(
        const BotReply(
          text: '文件在这儿',
          files: [
            BotReplyFile(name: '报告.pdf', mime: 'application/pdf', data: 'YWJj'),
          ],
        ),
      );
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(
      () => container.read(botChatProvider).messages.any(
        (m) => m.files.isNotEmpty,
      ),
    );
    expect(api.polledRuns, ['run-7']);
    expect(container.read(botChatProvider).messages.single.files.single.name,
        '报告.pdf');
  });

  test('a stale idless synced reply does not probe run media', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          // `_msg` stamps `at` from the seq (epoch for small seqs) — far
          // outside the 48h probe window.
          _msg(1, 'm1', 'assistant', '很早的回复', runId: 'run-6'),
        ],
      );
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);
    await Future<void>.delayed(const Duration(milliseconds: 100));
    expect(api.pollCalls, 0);
    expect(container.read(botChatProvider).messages.single.files, isEmpty);
  });

  test(
    'history reads every page and keeps one copy of a server line',
    () async {
      final api = _FakeApi()
        ..scripted = [
          ConversationPage(
            head: 5,
            next: 4,
            more: true,
            entries: [
              _msg(
                4,
                'copy',
                'assistant',
                '第一条',
                created: 4,
                requestId: 'n1:target',
              ),
              _msg(2, 'm2', 'user', '后', created: 2),
            ],
          ),
          ConversationPage(
            head: 5,
            next: 5,
            more: false,
            entries: [
              _msg(5, 'm3', 'assistant', '第三条', created: 3),
              _msg(
                1,
                'orig',
                'assistant',
                '第一条',
                created: 1,
                requestId: 'n1:target',
              ),
            ],
          ),
        ];
      final container = _container(api);
      addTearDown(container.dispose);
      await _until(() => !container.read(botChatProvider).loadingHistory);
      final messages = container.read(botChatProvider).messages;
      expect(messages.map((m) => m.content), ['第一条', '后', '第三条']);
      expect(messages.first.syncSeq, 1);
      expect(api.conversationCalls, greaterThanOrEqualTo(2));
    },
  );

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
        const BotRun(runId: 'r1', status: 'running'),
      )
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
      timeout: const Duration(seconds: 8),
    );
    final messages = container.read(botChatProvider).messages;
    expect(messages.last.content, '跑完了');
    expect(messages.last.role, 'assistant');
  });

  test('queued send marks the bubble and appends the ack', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.queued(position: 2, ack: '收到，排队处理');
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
        routedTitle: '整理周报',
      )
      ..pollAnswers.addAll([const BotReply(text: '完成了')]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('干活');
    expect(container.read(botChatProvider).messages[1].ack, isTrue);
    expect(
      container.read(botChatProvider).messages[1].content,
      contains('整理周报'),
    );
    await _until(
      () => container.read(botChatProvider).activeRunId == null,
      timeout: const Duration(seconds: 8),
    );
  });

  test('a synced reply clears the queue tag on earlier user bubbles', () async {
    final api = _FakeApi()..sendResult = SendResult.queued(position: 1);
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
      () => container
          .read(botChatProvider)
          .messages
          .any((m) => m.content == '做完了'),
      timeout: const Duration(seconds: 6),
    );
    expect(
      container.read(botChatProvider).messages.where((m) => m.queued),
      isEmpty,
    );
  });

  test(
    'a queued bubble tracks its live position and clears when running',
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
      await _until(
        () =>
            container
                .read(botChatProvider)
                .messages
                .firstWhere((m) => m.id == msgId)
                .queuePosition ==
            2,
      );

      // Once the queue view leaves queued/held the tag clears.
      api.queueView = const QueueView(state: 'running', runId: 'r9');
      await _until(
        () => !container
            .read(botChatProvider)
            .messages
            .firstWhere((m) => m.id == msgId)
            .queued,
      );
    },
  );

  test(
    'a message that waited out an offline node shows why it stopped',
    () async {
      final api = _FakeApi()
        ..sendResult = SendResult.queued(
          position: 1,
          ack: '这台内网电脑暂时连不上，这条消息先排着。',
        )
        ..queueView = const QueueView(
          state: 'gave_up',
          outcome: '这台内网电脑还没连回来，这条消息先停在这里。',
        );
      final container = _container(api);
      addTearDown(container.dispose);
      await _until(() => !container.read(botChatProvider).loadingHistory);

      await container.read(botChatProvider.notifier).send('打开报表');
      await _until(
        () => container
            .read(botChatProvider)
            .messages
            .any((m) => m.content == '这台内网电脑还没连回来，这条消息先停在这里。'),
      );
      final messages = container.read(botChatProvider).messages;
      expect(messages.firstWhere((m) => m.isUser).queued, isFalse);
      expect(
        messages.where((m) => m.content == '这台内网电脑还没连回来，这条消息先停在这里。'),
        hasLength(1),
      );
      await _until(
        () => api.written.any(
          (entry) =>
              entry['data'] is Map &&
              (entry['data'] as Map)['content'] == '这台内网电脑还没连回来，这条消息先停在这里。',
        ),
      );
    },
  );

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
      timeout: const Duration(seconds: 8),
    );
    expect(
      container.read(botChatProvider).messages.map((m) => m.content),
      contains('跑完了'),
    );
  });

  test('a second send while the run executes queues server-side', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.admitted(
        const BotRun(runId: 'r1', status: 'running'),
      )
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
      ..page = ConversationPage(
        head: 2,
        entries: [
          _msg(1, 'u1', 'user', '历史一'),
          _msg(2, 'a1', 'assistant', '历史答一'),
        ],
      );
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
    await container.read(botChatProvider.notifier).retry(state.messages.single);
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
    await container.read(botChatProvider.notifier).answerQuestion(ask, '第一个');
    expect(api.lastAskRun, 'run-7');
    expect(api.lastContinueSession, 'b1#run-7');
  });

  test('cancelActiveRun forwards to the api', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.admitted(
        const BotRun(runId: 'r9', status: 'running'),
      )
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
    expect(messages.single.syncSeq, 1);
  });

  test('a second copy of an assistant line folds in by run id', () async {
    // The GUI publishes the same server line under its own bubble id and
    // request id — the run id is what says it is the same turn's answer.
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 2,
        entries: [
          _msg(1, 'm1', 'assistant', '答', withAt: false, runId: 'r7'),
          _msg(
            2,
            'm2',
            'assistant',
            '答',
            withAt: false,
            runId: 'r7',
            requestId: 'req-other',
            attachmentIds: ['shot-2'],
          ),
        ],
      );
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);
    final messages = container.read(botChatProvider).messages;
    expect(messages.length, 1);
    expect(messages.single.id, 'm1');
    expect(messages.single.attachmentIds, ['shot-2']);
    expect(messages.single.runId, 'r7');
  });

  test('a run id does not collapse two different bubbles', () async {
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
            data: {'role': 'user', 'content': '问', 'runId': 'r1'},
          ),
          ConversationEntry(
            seq: 2,
            createdSeq: 2,
            botId: 'b1',
            id: 'a1',
            kind: 'message',
            data: {'role': 'assistant', 'content': '答', 'runId': 'r1'},
          ),
        ],
      );
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);
    expect(container.read(botChatProvider).messages.map((m) => m.content), [
      '问',
      '答',
    ]);
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
        const BotRun(runId: 'r1', status: 'running'),
      )
      ..pollAnswers.addAll([const BotReply(text: '跑完了')]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('再来一次');
    await _until(
      () => container.read(botChatProvider).activeRunId == null,
      timeout: const Duration(seconds: 8),
    );

    final replies = container
        .read(botChatProvider)
        .messages
        .where((m) => m.role == 'assistant' && m.content == '跑完了')
        .toList();
    expect(replies, hasLength(1));
  });

  // The journal projection never carries media bytes, and the desktop saves
  // delivered files on its own disk (no attachment ids to refetch) — so when
  // the projection wins the race against the run poll, the reply's files and
  // images must be patched into the shown bubble instead of dropped with it.
  test('a poll reply with files patches the projected bubble', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          ConversationEntry(
            seq: 1,
            createdSeq: 1,
            botId: 'b1',
            id: 'a1',
            kind: 'message',
            data: {
              'role': 'assistant',
              'content': '文件做好了',
              'runId': 'r1',
              'at': DateTime.now().toIso8601String(),
            },
          ),
        ],
      )
      ..sendResult = SendResult.admitted(
        const BotRun(runId: 'r1', status: 'running'),
      )
      ..pollAnswers.addAll([
        const BotReply(
          text: '文件做好了',
          files: [
            BotReplyFile(name: '报告.pdf', mime: 'application/pdf', data: 'YWJj'),
          ],
        ),
      ]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('出个报告');
    await _until(
      () => container.read(botChatProvider).activeRunId == null,
      timeout: const Duration(seconds: 8),
    );

    final replies = container
        .read(botChatProvider)
        .messages
        .where((m) => m.role == 'assistant' && m.content == '文件做好了')
        .toList();
    expect(replies, hasLength(1), reason: 'no duplicate bubble');
    expect(replies.single.files.single.name, '报告.pdf');
    expect(replies.single.files.single.data, 'YWJj');
  });

  // The sync media restore may have staged one media list on the projected
  // bubble before the run poll lands the other one — the settle patch is
  // per-field: staging files must not wipe the restored images back off.
  test('settle patch merges per field over restored media', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 1,
        entries: [
          ConversationEntry(
            seq: 1,
            createdSeq: 1,
            botId: 'b1',
            id: 'a1',
            kind: 'message',
            data: {
              'role': 'assistant',
              'content': '文件做好了',
              'runId': 'r1',
              'at': DateTime.now().toIso8601String(),
            },
          ),
        ],
      )
      ..sendResult = SendResult.admitted(
        const BotRun(runId: 'r1', status: 'running'),
      )
      ..pollAnswers.addAll([
        // First poll: the sync restore's probe — images only.
        const BotReply(
          text: '文件做好了',
          images: [
            BotReplyImage(mime: 'image/png', data: 'aW1n'),
          ],
        ),
        // Second poll: this client's own run poll — files only.
        const BotReply(
          text: '文件做好了',
          files: [
            BotReplyFile(name: '报告.pdf', mime: 'application/pdf', data: 'YWJj'),
          ],
        ),
      ]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    // The restore probe fires first and stages the images.
    await _until(
      () => container
          .read(botChatProvider)
          .messages
          .any((m) => m.id == 'a1' && m.images.isNotEmpty),
    );

    await container.read(botChatProvider.notifier).send('出个报告');
    await _until(
      () => container.read(botChatProvider).activeRunId == null,
      timeout: const Duration(seconds: 8),
    );

    final bubble = container
        .read(botChatProvider)
        .messages
        .firstWhere((m) => m.id == 'a1');
    expect(bubble.images.single.data, 'aW1n', reason: 'restored images kept');
    expect(bubble.files.single.name, '报告.pdf', reason: 'files merged in');
  });

  // The echo of this client's own settled bubble comes back through the
  // long-poll with the bytes stripped. That echo must not overwrite the
  // media the run poll staged on the local bubble.
  test('our own projection echo keeps the settled media', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.admitted(
        const BotRun(runId: 'r1', status: 'running'),
      )
      ..pollAnswers.addAll([
        const BotReply(
          text: '文件做好了',
          files: [
            BotReplyFile(name: '报告.pdf', mime: 'application/pdf', data: 'YWJj'),
          ],
        ),
      ]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('出个报告');
    await _until(
      () => container.read(botChatProvider).messages.any(
        (m) => m.role == 'assistant' && m.files.isNotEmpty,
      ),
      timeout: const Duration(seconds: 8),
    );
    final bubbleId = container
        .read(botChatProvider)
        .messages
        .lastWhere((m) => m.role == 'assistant')
        .id;

    // Next sync tick returns the echo of our own projection line.
    api.page = ConversationPage(
      head: 1,
      entries: [
        ConversationEntry(
          seq: 1,
          createdSeq: 1,
          botId: 'b1',
          id: bubbleId,
          kind: 'message',
          data: {
            'role': 'assistant',
            'content': '文件做好了',
            'runId': 'r1',
            'at': DateTime.now().toIso8601String(),
          },
        ),
      ],
    );
    await _until(
      () => container
          .read(botChatProvider)
          .messages
          .any((m) => m.id == bubbleId && m.syncSeq > 0),
      timeout: const Duration(seconds: 8),
    );

    final echo = container
        .read(botChatProvider)
        .messages
        .firstWhere((m) => m.id == bubbleId);
    expect(echo.syncSeq, 1, reason: 'the echo merged');
    expect(echo.files.single.name, '报告.pdf', reason: 'media survives');
  });

  // A malformed journal entry (non-string fields, numeric seq) must not
  // throw inside the merge — one bad line would otherwise poison every
  // later sync tick on the same page and stall the transcript forever.
  test('a malformed journal entry does not stall the merge', () async {
    final api = _FakeApi()
      ..page = ConversationPage(
        head: 3,
        entries: [
          const ConversationEntry(
            seq: 1,
            createdSeq: 1,
            botId: 'b1',
            id: 'bad',
            kind: 'message',
            data: {
              'role': 42,
              'content': {'x': 1},
              'requestId': 7,
              'runId': true,
              'options': 'not-a-list',
              'images': 'not-a-list',
            },
          ),
          _msg(2, 'good', 'assistant', '正常回复', withAt: false),
        ],
      );
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);
    final messages = container.read(botChatProvider).messages;
    // The malformed line is skipped by the role guard; the valid one lands.
    expect(messages.single.id, 'good');
    expect(messages.single.content, '正常回复');
  });

  test('malformed entry fields parse defensively in the model layer', () async {
    final entry = ConversationEntry.fromJson({
      'seq': '12',
      'created_seq': 5,
      'bot_id': 'b1',
      'id': 99,
      'data': 'not-a-map',
      'at': 12345,
    });
    expect(entry.seq, 12);
    expect(entry.createdSeq, 5);
    expect(entry.data, isEmpty);

    final message = ChatMessage.fromSyncData('x', {
      'role': 'assistant',
      'content': 'hi',
      'requestId': 7,
      'runId': 'r1',
      'options': 'oops',
      'images': [
        {'mime': 'image/png', 'data': 'aW1n', 'attachment_id': 5},
      ],
    });
    expect(message.runId, 'r1');
    expect(message.requestId, 'r1'); // empty requestId falls back to runId
    expect(message.options, isEmpty);
    expect(message.images.single.data, 'aW1n');
    expect(message.images.single.attachmentId, '');
  });

  test('backgrounding pauses the journal long-poll until resumed', () async {
    final api = _FakeApi()..page = const ConversationPage(head: 0);
    final container = _container(api);
    addTearDown(container.dispose);

    await _until(() => !container.read(botChatProvider).loadingHistory);
    await _until(() => api.conversationCalls >= 2); // one long-poll round

    container.read(botChatProvider.notifier).setForeground(false);
    final paused = api.conversationCalls;
    await Future<void>.delayed(const Duration(milliseconds: 700));
    expect(api.conversationCalls, paused);

    container.read(botChatProvider.notifier).setForeground(true);
    await _until(() => api.conversationCalls > paused);
  });

  test('backgrounding pauses the run poll and resume re-attaches', () async {
    final api = _FakeApi()
      ..sendResult = SendResult.admitted(
        const BotRun(runId: 'r1', status: 'running'),
      )
      ..pollAnswers.addAll([null, null, null, const BotReply(text: '跑完了')]);
    final container = _container(api);
    addTearDown(container.dispose);
    await _until(() => !container.read(botChatProvider).loadingHistory);

    await container.read(botChatProvider.notifier).send('干活');
    expect(container.read(botChatProvider).activeRunId, 'r1');
    await _until(() => api.pollCalls >= 1, timeout: const Duration(seconds: 4));

    container.read(botChatProvider.notifier).setForeground(false);
    final paused = api.pollCalls;
    await Future<void>.delayed(const Duration(milliseconds: 2200));
    expect(api.pollCalls, paused); // no ticks while pocketed
    expect(container.read(botChatProvider).activeRunId, 'r1');

    container.read(botChatProvider.notifier).setForeground(true);
    await _until(
      () => container.read(botChatProvider).activeRunId == null,
      timeout: const Duration(seconds: 8),
    );
    expect(container.read(botChatProvider).messages.last.content, '跑完了');
  });
}
