import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/bots/bot.dart';
import 'package:maclaw_mobile/features/bots/bot_api.dart';
import 'package:maclaw_mobile/features/bots/bot_create_sheet.dart';
import 'package:maclaw_mobile/features/bots/bot_message.dart';
import 'package:maclaw_mobile/features/bots/bots_controller.dart';

import 'support/fake_hub.dart';

/// Scripted answers for one bot's turn lifecycle.
class TurnScript {
  TurnScript({
    required this.admitBody,
    this.admitStatus = 202,
    this.pollBodies = const <String, Map<String, dynamic>>{},
    this.pollStatus = 200,
  });

  /// Body returned for `POST /bots/{id}/messages`.
  final Map<String, dynamic> admitBody;
  final int admitStatus;

  /// Keyed by run id. A run absent from this map reads as "still running".
  final Map<String, Map<String, dynamic>> pollBodies;
  final int pollStatus;

  int sendCount = 0;
  int pollCount = 0;
}

/// Adapter that answers a [TurnScript].
FakeHubAdapter adapterFor(TurnScript script) {
  return FakeHubAdapter((request) {
    if (request.path.endsWith('/messages')) {
      script.sendCount++;
      return (status: script.admitStatus, body: script.admitBody);
    }
    if (request.path.contains('/runs/')) {
      script.pollCount++;
      final runId = Uri.decodeComponent(request.path.split('/runs/').last);
      final body = script.pollBodies[runId];
      if (body == null) {
        return (
          status: 202,
          body: const {'accepted': true, 'status': 'running'},
        );
      }
      return (status: script.pollStatus, body: body);
    }
    throw StateError('unscripted path: ${request.path}');
  });
}

/// In-memory transcript store, so these tests need no sqlite.
class MemoryBotStore extends MobileLocalStore {
  final Map<String, List<BotMessage>> transcripts = {};

  @override
  Future<List<BotMessage>> loadBotMessages(
    String botId, {
    int limit = 200,
  }) async {
    return List<BotMessage>.from(transcripts[botId] ?? const <BotMessage>[]);
  }

  @override
  Future<void> saveBotMessages(
    String botId,
    List<BotMessage> messages, {
    int limit = 200,
  }) async {
    transcripts[botId] = List<BotMessage>.from(messages);
  }

  @override
  Future<Map<String, BotMessage>> loadLatestBotMessageByBotId() async {
    final latest = <String, BotMessage>{};
    for (final entry in transcripts.entries) {
      BotMessage? newest;
      for (final message in entry.value) {
        if (newest == null || !newest.isNewerThan(message)) newest = message;
      }
      if (newest != null) latest[entry.key] = newest;
    }
    return latest;
  }

  @override
  Future<void> deleteBotMessages(String botId) async {
    transcripts.remove(botId);
  }

  @override
  Future<void> close() async {}
}

class _SignedInSessionController extends SessionController {
  @override
  Future<SessionState> build() async => SessionState.signedIn(
        hubUrl: 'https://hub.example',
        bootstrap: _botTestBootstrap(),
      );
}

MobileBootstrap _botTestBootstrap() {
  return MobileBootstrap(
    user: const MobileUser(
      userId: 'u1',
      email: 'a@example.com',
      tenantId: 'tenant-a',
    ),
    services: const MobileServices(
      hubStatus: 'online',
      llmStatus: 'available',
      searchStatus: 'available',
      documentsStatus: 'available',
      digitalEmployeesStatus: 'available',
      llmStatusPath: '/api/llm/status',
      modelsPath: '/api/llm/models',
      searchPath: '/api/mobile/search',
      documentsPath: '/api/mobile/documents',
      digitalEmployeesPath: '/api/mobile/digital-employees',
      realtimePath: '/api/mobile/realtime',
    ),
    features: const MobileFeatures(
      search: true,
      documents: true,
      backendSshSessions: true,
      digitalEmployees: true,
      pushNotifications: false,
    ),
    limits: const MobileLimits(maxUploadBytes: 1024, maxExportJobs: 2),
  );
}

/// Container wired to a scripted turn and an in-memory transcript.
ProviderContainer botContainer(TurnScript script, {MemoryBotStore? store}) {
  final container = ProviderContainer(
    overrides: [
      sessionControllerProvider.overrideWith(_SignedInSessionController.new),
      botApiProvider.overrideWithValue(
        BotApi(
          dio: dioWithFakeHub(adapterFor(script)),
          hubUrl: 'https://hub.example',
        ),
      ),
      mobileLocalStoreProvider.overrideWithValue(store ?? MemoryBotStore()),
    ],
  );
  addTearDown(container.dispose);
  return container;
}

void main() {
  group('sending a turn', () {
    test('a synchronous reply settles the pending bubble in place', () async {
      final store = MemoryBotStore();
      final container = botContainer(
        TurnScript(admitBody: const {'text': 'already done'}, admitStatus: 200),
        store: store,
      );
      final provider = botConversationProvider('b1');
      container.read(provider);

      await container.read(provider.notifier).send('do it');

      final messages = store.transcripts['b1']!;
      expect(messages, hasLength(1),
          reason: 'the pending bubble is replaced, not duplicated');
      expect(messages.single.role, BotMessageRole.assistant);
      expect(messages.single.content, 'already done');
      expect(messages.single.pending, isFalse);
    });

    test('an admitted run leaves the bubble pending and marks the bot busy',
        () async {
      final store = MemoryBotStore();
      final container = botContainer(
        TurnScript(
          admitBody: const {
            'accepted': true,
            'run_id': 'r1',
            'status': 'running',
          },
        ),
        store: store,
      );
      final provider = botConversationProvider('b1');
      container.read(provider);

      await container.read(provider.notifier).send('do it');

      final messages = store.transcripts['b1']!;
      expect(messages.single.role, BotMessageRole.user);
      expect(messages.single.pending, isTrue);
      expect(container.read(botActivityProvider).isRunning('b1'), isTrue);
    });

    test('a reply with no text and no attachment fails the turn', () async {
      final store = MemoryBotStore();
      final container = botContainer(
        TurnScript(admitBody: const {'text': '   '}, admitStatus: 200),
        store: store,
      );
      final provider = botConversationProvider('b1');
      container.read(provider);

      await container.read(provider.notifier).send('do it');

      final messages = store.transcripts['b1']!;
      expect(messages.single.role, BotMessageRole.user,
          reason: 'the person must still see what they sent');
      expect(messages.single.pending, isFalse);
      expect(messages.single.failed, isTrue);
    });

    test('an empty send is ignored', () async {
      final store = MemoryBotStore();
      final container = botContainer(
        TurnScript(admitBody: const {'text': 'x'}, admitStatus: 200),
        store: store,
      );
      final provider = botConversationProvider('b1');
      container.read(provider);

      await container.read(provider.notifier).send('   ');

      expect(store.transcripts['b1'], isNull);
    });

    test('a rejected send fails the turn and keeps the message', () async {
      final store = MemoryBotStore();
      final script = TurnScript(
        admitBody: const {'error': 'BOT_DISABLED', 'message': '未开通'},
        admitStatus: 403,
      );
      final container = botContainer(script, store: store);
      final provider = botConversationProvider('b1');
      container.read(provider);

      await container.read(provider.notifier).send('do it');

      final messages = store.transcripts['b1']!;
      expect(messages.single.pending, isFalse);
      expect(messages.single.failed, isTrue);
      expect(messages.single.attentionReason, isNotEmpty);
      expect(container.read(botActivityProvider).isRunning('b1'), isFalse);
    });

    test('a confirm re-sends with the execute phase', () async {
      final store = MemoryBotStore();
      // Admitted, not answered: the approval must reach Hub while the user
      // bubble is still the newest thing in the transcript.
      final script = TurnScript(
        admitBody: const {
          'accepted': true,
          'run_id': 'r1',
          'status': 'running',
        },
      );
      final container = botContainer(script, store: store);
      final provider = botConversationProvider('b1');
      container.read(provider);
      final notifier = container.read(provider.notifier);

      await notifier.confirmPlan(
        BotMessage.fromReply(
          id: 'a1',
          reply: const BotReply(text: '计划', phase: BotPhase.plan),
        ),
      );

      // The approval goes back to Hub as the bot's own plan with the execute
      // phase, so the bot continues from its arrangement rather than a re-typed
      // instruction. The turn is still running here, so the user bubble is
      // pending rather than already replaced by a reply.
      expect(script.sendCount, 1);
      final userMessages =
          store.transcripts['b1']!.where((message) => message.isUser).toList();
      expect(userMessages, hasLength(1));
      expect(userMessages.single.content, '计划');
      expect(userMessages.single.phase, BotPhase.execute);
      expect(userMessages.single.pending, isTrue);
    });
  });

  group('loading a stored conversation', () {
    test('an aged-out pending bubble is surfaced as failed', () async {
      final store = MemoryBotStore()
        ..transcripts['b1'] = [
          BotMessage.user(
            content: 'older turn',
            id: 'u1',
            createdAt:
                DateTime.now().toUtc().subtract(const Duration(hours: 3)),
          ).copyWith(pending: true),
        ];
      final container = botContainer(
        TurnScript(admitBody: const {'text': 'x'}, admitStatus: 200),
        store: store,
      );

      final messages =
          await container.read(botConversationProvider('b1').future);
      expect(messages.single.pending, isFalse,
          reason: 'the run that would answer it is gone');
      expect(messages.single.failed, isTrue);
    });

    test('a recent pending bubble is left alone', () async {
      final store = MemoryBotStore()
        ..transcripts['b1'] = [
          BotMessage.user(
            content: 'just sent',
            id: 'u1',
            createdAt: DateTime.now().toUtc(),
          ).copyWith(pending: true),
        ];
      final container = botContainer(
        TurnScript(admitBody: const {'text': 'x'}, admitStatus: 200),
        store: store,
      );

      final messages =
          await container.read(botConversationProvider('b1').future);
      expect(messages.single.pending, isTrue);
    });
  });

  group('BotActivity', () {
    test('markRunning then markSettled tracks the bot', () {
      var activity = const BotActivity();
      activity = activity.markRunning('b1');
      expect(activity.isRunning('b1'), isTrue);
      activity = activity.markSettled('b1', BotWorkState.idle);
      expect(activity.isRunning('b1'), isFalse);
      expect(activity.needsUser('b1'), isFalse);
    });

    test('a handoff result leaves the bot needing the user', () {
      var activity = const BotActivity().markRunning('b1');
      activity = activity.markSettled('b1', BotWorkState.waitingForUser);
      expect(activity.needsUser('b1'), isTrue);
      expect(activity.isRunning('b1'), isFalse);
    });

    test('settling clears both sets', () {
      final activity =
          const BotActivity().markSettled('b1', BotWorkState.failed);
      expect(activity.isRunning('b1'), isFalse);
      expect(activity.needsUser('b1'), isFalse);
    });

    test('a blank bot id is ignored', () {
      const activity = BotActivity();
      expect(activity.markRunning('  ').running, isEmpty);
      expect(
        activity.markSettled('', BotWorkState.failed).awaitingUser,
        isEmpty,
      );
    });
  });

  group('BotCreateSheet validation', () {
    test('rejects a blank or overlong name', () {
      expect(BotCreateSheet.validateName('   '), isNotNull);
      expect(BotCreateSheet.validateName('a' * 41), isNotNull);
      expect(BotCreateSheet.validateName('Ada'), isNull);
    });

    test('rejects an overlong description', () {
      expect(BotCreateSheet.validateDescription('a' * 201), isNotNull);
      expect(BotCreateSheet.validateDescription('ok'), isNull);
    });
  });
}