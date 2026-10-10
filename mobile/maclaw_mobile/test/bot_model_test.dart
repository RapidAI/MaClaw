import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/bots/bot.dart';
import 'package:maclaw_mobile/features/bots/bot_message.dart';

void main() {
  group('Bot parsing', () {
    test('reads the Hub bot shape', () {
      final bot = Bot.fromJson(const {
        'id': 'bot-1',
        'name': 'Ada',
        'description': 'research',
        'instance_id': 'inst-1',
        'created_at': '2026-10-10T10:00:00Z',
      });
      expect(bot.id, 'bot-1');
      expect(bot.title, 'Ada');
      expect(bot.instanceId, 'inst-1');
      expect(bot.createdAt, isNotNull);
    });

    test('falls back to the desktop client title field', () {
      final bot = Bot.fromJson(const {'id': 'bot-2', 'title': 'Grace'});
      expect(bot.title, 'Grace');
    });

    test('falls back to description then id for a blank name', () {
      expect(Bot.fromJson(const {'id': 'b', 'description': 'night duty'}).title,
          'night duty');
      expect(Bot.fromJson(const {'id': 'b'}).title, 'b');
    });

    test('initials handle a CJK name and a two-word name', () {
      expect(const Bot(id: 'x', name: '日报助手').initials, '日');
      expect(const Bot(id: 'x', name: 'Ada Lovelace').initials, 'AL');
      // A blank name falls back to the id, so the avatar is still stable.
      expect(const Bot(id: 'x', name: '  ').initials, 'X');
    });
  });

  group('BotReply', () {
    test('parses the full reply the Hub returns', () {
      final reply = BotReply.fromJson(const {
        'text': 'opened it',
        'novnc_url': '/novnc/vnc.html',
        'handoff': true,
        'attention_reason': '需要你登录',
        'ask_user_input_type': 'text',
        'ask_user_question': '用哪个账号？',
        'ask_user_options_json': '["A","B"]',
        'images': [
          {'mime': 'image/png', 'data': 'aGk='},
        ],
        'files': [
          {'name': 'a.docx', 'mime': 'application/vnd.ms-word', 'data': 'aGk='},
        ],
      });
      expect(reply.text, 'opened it');
      expect(reply.handoff, isTrue);
      expect(reply.needsDesktop, isTrue);
      expect(reply.question.question, '用哪个账号？');
      expect(reply.question.options, ['A', 'B']);
      expect(reply.images.single.mime, 'image/png');
      expect(reply.files.single.displayName, 'a.docx');
    });

    test('a handoff reply reads as waiting for the user', () {
      final reply = BotReply.fromJson(const {
        'text': '请在桌面完成登录',
        'handoff': true,
      });
      expect(reply.workState, BotWorkState.waitingForUser);
    });

    test('an unanswered question outranks a desktop handoff', () {
      final reply = BotReply.fromJson(const {
        'text': 'which account?',
        'handoff': true,
        'ask_user_question': '用哪个账号？',
      });
      expect(reply.workState, BotWorkState.awaitingAnswer);
    });

    test('a plain reply is idle', () {
      expect(
        BotReply.fromJson(const {'text': 'done'}).workState,
        BotWorkState.idle,
      );
    });

    test('a reply with no text and no attachment is empty', () {
      expect(BotReply.fromJson(const {'text': '   '}).isEmpty, isTrue);
      expect(
        BotReply.fromJson(const {
          'files': [
            {'name': 'a.txt', 'mime': 'text/plain', 'data': 'aGk='},
          ],
        }).isEmpty,
        isFalse,
      );
    });
  });

  group('parseBotAskOptions', () {
    test('accepts a bare array string', () {
      expect(parseBotAskOptions('["a","b"]'), ['a', 'b']);
    });

    test('accepts an options or choices wrapper', () {
      expect(parseBotAskOptions('{"options":["a"]}'), ['a']);
      expect(parseBotAskOptions('{"choices":["b"]}'), ['b']);
    });

    test('accepts object options with a label', () {
      expect(parseBotAskOptions('[{"label":"Yes"}]'), ['Yes']);
    });

    test('malformed JSON yields no options instead of throwing', () {
      expect(parseBotAskOptions('not json'), isEmpty);
      expect(parseBotAskOptions(''), isEmpty);
      expect(parseBotAskOptions(null), isEmpty);
    });
  });

  group('BotPhase', () {
    test('maps to and from the wire value', () {
      expect(BotPhase.plan.wireValue, 'plan');
      expect(BotPhase.execute.wireValue, 'execute');
      expect(BotPhaseWire.fromWire('plan'), BotPhase.plan);
      expect(BotPhaseWire.fromWire(' EXECUTE '), BotPhase.execute);
      expect(BotPhaseWire.fromWire('other'), isNull);
    });
  });

  group('BotRun', () {
    test('an admitted run carries its id', () {
      final run = BotRun.fromJson(const {'run_id': 'r1', 'status': 'running'});
      expect(run.accepted, isTrue);
      expect(run.isRunning, isTrue);
    });

    test('a reply object is not a run', () {
      expect(BotRun.fromJson(const {'text': 'done'}).accepted, isFalse);
    });
  });

  group('BotAccess', () {
    test('carries the admin reason when disabled', () {
      final access = BotAccess.fromJson(const {
        'enabled': false,
        'message': '服务器没有开通bot功能',
      });
      expect(access.enabled, isFalse);
      expect(access.message, '服务器没有开通bot功能');
    });
  });
}