import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/bots/bot.dart';
import 'package:maclaw_mobile/features/bots/bot_message.dart';

void main() {
  BotMessage user(String id, String content, DateTime at) => BotMessage(
        id: id,
        role: BotMessageRole.user,
        content: content,
        createdAt: at,
      );

  BotMessage assistant(String id, String content, DateTime at) => BotMessage(
        id: id,
        role: BotMessageRole.assistant,
        content: content,
        createdAt: at,
      );

  group('BotMessage JSON round trip', () {
    test('keeps the reply fields that drive cards', () {
      final original = BotMessage.fromReply(
        id: 'a1',
        reply: BotReply.fromJson(const {
          'text': '需要确认',
          'phase': 'plan',
          'handoff': true,
          'attention_reason': '请登录',
          'ask_user_secret_name': 'github_password',
          'ask_user_options_json': '["A","B"]',
          'novnc_url': '/novnc/x.html',
        }),
        createdAt: DateTime.utc(2026, 10, 10),
      );
      final restored = BotMessage.fromJson(original.toJson());
      expect(restored.id, original.id);
      expect(restored.content, original.content);
      expect(restored.phase, BotPhase.plan);
      expect(restored.handoff, isTrue);
      expect(restored.attentionReason, '请登录');
      expect(restored.question.secretName, 'github_password');
      expect(restored.question.options, ['A', 'B']);
      expect(restored.novncUrl, '/novnc/x.html');
    });

    test('keeps attachments', () {
      final original = BotMessage.fromReply(
        id: 'a2',
        reply: BotReply.fromJson(const {
          'text': 'done',
          'images': [
            {'mime': 'image/png', 'data': 'aGk='},
          ],
          'files': [
            {'name': 'r.pdf', 'mime': 'application/pdf', 'data': 'aGk='},
          ],
        }),
      );
      final restored = BotMessage.fromJson(original.toJson());
      expect(restored.images.single.data, 'aGk=');
      expect(restored.files.single.displayName, 'r.pdf');
    });

    test('omits absent optional fields rather than writing empty ones', () {
      final json = assistant('a3', 'hi', DateTime.utc(2026, 10, 10)).toJson();
      expect(json.containsKey('phase'), isFalse);
      expect(json.containsKey('handoff'), isFalse);
      expect(json.containsKey('images'), isFalse);
      expect(json['role'], 'assistant');
    });
  });

  group('BotMessage work state', () {
    test('a failed message is failed', () {
      final message = assistant('a', 'x', DateTime.utc(2026))
          .copyWith(failed: true);
      expect(message.workState, BotWorkState.failed);
    });

    test('a pending message is running', () {
      final message = user('u', 'x', DateTime.utc(2026)).copyWith(pending: true);
      expect(message.workState, BotWorkState.running);
    });

    test('a settled reply is idle', () {
      expect(
        assistant('a', 'done', DateTime.utc(2026)).workState,
        BotWorkState.idle,
      );
    });
  });

  group('botPendingReplyIsStale', () {
    final startedAt = DateTime.utc(2026, 10, 10, 12);

    test('a settled message never goes stale', () {
      final message = assistant('a', 'done', startedAt);
      expect(
        botPendingReplyIsStale(
          message,
          now: startedAt.add(const Duration(days: 3)),
        ),
        isFalse,
      );
    });

    test('a pending message inside the window is still live', () {
      final message = user('u', 'x', startedAt).copyWith(pending: true);
      expect(
        botPendingReplyIsStale(
          message,
          now: startedAt.add(const Duration(minutes: 30)),
        ),
        isFalse,
      );
    });

    test('a pending message past the window ages out', () {
      final message = user('u', 'x', startedAt).copyWith(pending: true);
      expect(
        botPendingReplyIsStale(
          message,
          now: startedAt.add(const Duration(minutes: 32)),
        ),
        isTrue,
      );
    });
  });

  group('upsertBotMessage', () {
    test('replaces a message with the same id in place', () {
      final at = DateTime.utc(2026, 10, 10);
      final pending = user('u1', 'do it', at).copyWith(pending: true);
      final settled = assistant('u1', 'done', at.add(const Duration(seconds: 5)));
      final next = upsertBotMessage([pending], settled);
      expect(next, hasLength(1));
      expect(next.single.content, 'done');
      expect(next.single.pending, isFalse);
    });

    test('appends and orders by time', () {
      final base = DateTime.utc(2026, 10, 10);
      final next = upsertBotMessage(
        [user('u2', 'second', base.add(const Duration(seconds: 2)))],
        user('u1', 'first', base),
      );
      expect(next.map((m) => m.id), ['u1', 'u2']);
    });

    test('drops the oldest beyond the retention limit', () {
      final base = DateTime.utc(2026, 10, 10);
      var messages = <BotMessage>[];
      for (var i = 0; i < 10; i++) {
        messages = upsertBotMessage(
          messages,
          user('u$i', 'm$i', base.add(Duration(seconds: i))),
          limit: 3,
        );
      }
      expect(messages, hasLength(3));
      expect(messages.first.id, 'u7');
      expect(messages.last.id, 'u9');
    });
  });

  group('BotConversation', () {
    test('needs the person when the last message is a handoff', () {
      final conversation = BotConversation(
        botId: 'b',
        messages: [
          assistant(
            'a',
            '请登录',
            DateTime.utc(2026),
          ).copyWith(handoff: true),
        ],
      );
      expect(conversation.needsUser, isTrue);
      expect(conversation.busy, isFalse);
    });

    test('is busy while a turn is pending', () {
      final conversation = BotConversation(
        botId: 'b',
        messages: [
          // Recent: the clock that ages out a pending bubble only covers a run
          // this process never adopted, not one it is still polling.
          user('u', 'x', DateTime.now().toUtc()).copyWith(pending: true),
        ],
      );
      expect(conversation.busy, isTrue);
    });

    test('an aged-out pending bubble is neither busy nor needing', () {
      final conversation = BotConversation(
        botId: 'b',
        messages: [
          user(
            'u',
            'x',
            DateTime.now().toUtc().subtract(const Duration(hours: 2)),
          ).copyWith(pending: true),
        ],
      );
      expect(conversation.busy, isFalse);
      expect(conversation.needsUser, isFalse);
    });

    test('latest picks the newest by time, not by position', () {
      final base = DateTime.utc(2026, 10, 10);
      final conversation = BotConversation(
        botId: 'b',
        messages: [
          assistant('a', 'newer', base.add(const Duration(minutes: 1))),
          assistant('a', 'older', base),
        ],
      );
      expect(conversation.latest?.content, 'newer');
    });
  });
}