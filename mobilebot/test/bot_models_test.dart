import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/features/bots/bot_models.dart';

void main() {
  group('Bot', () {
    test('parses hub shape incl. appearance and primary', () {
      final bot = Bot.fromJson({
        'id': 'b1',
        'name': '  小助 ',
        'description': '帮我干活',
        'instance_id': 'i9',
        'desktop_mode': 'isolated',
        'color': '#2563eb',
        'shape': 'cloud',
        'primary': true,
        'created_at': '2026-01-02T03:04:05Z',
      });
      expect(bot.id, 'b1');
      expect(bot.name, '小助');
      expect(bot.title, '小助');
      expect(bot.desktopMode, 'isolated');
      expect(bot.color, '#2563eb');
      expect(bot.shape, 'cloud');
      expect(bot.primary, isTrue);
      expect(bot.createdAt, isNotNull);
    });

    test('title falls back to description then id', () {
      expect(
          Bot.fromJson({'id': 'b2', 'description': ' d '}).title, 'd');
      expect(Bot.fromJson({'id': 'b3'}).title, 'b3');
    });

    test('initials use first letters of words', () {
      expect(Bot.fromJson({'id': 'x', 'name': 'Research Bot'}).initials,
          'RB');
      expect(Bot.fromJson({'id': 'x', 'name': '研'}).initials, '研');
    });

    test('initials keep emoji and grapheme clusters whole', () {
      expect(
          Bot.fromJson({'id': 'x', 'name': '🤖助手'}).initials, '🤖');
      expect(Bot.fromJson({'id': 'x', 'name': '👨‍💻 助手'}).initials,
          '👨‍💻助');
    });
  });

  group('FeedNote / SaidNote', () {
    test('FeedNote parses cross-note fields', () {
      final note = FeedNote.fromJson({
        'id': 'n1',
        'from': 'b1',
        'target': 'b2',
        'kind': 'delegate',
        'text': '去做',
        'order': '跑报表',
        'topic': 'progress',
        'run_id': 'r1',
        'created_at': '2026-01-02T03:04:05Z',
        'artifacts': ['report.xlsx'],
      });
      expect(note.kind, 'delegate');
      expect(note.order, '跑报表');
      expect(note.artifacts, ['report.xlsx']);
      expect(note.at, isNotNull);
    });

    test('SaidNote merges artifacts and files[].name, dedupes', () {
      final note = SaidNote.fromJson({
        'id': 's1',
        'from': 'b1',
        'kind': 'say',
        'text': '搞定了',
        'created_at': '2026-01-02T03:04:05Z',
        'artifacts': ['a.png'],
        'files': [
          {'name': 'a.png'},
          {'name': 'b.txt'},
        ],
        'answered_by': ['b2'],
      });
      expect(note.isMine, isFalse);
      expect(note.files, ['a.png', 'b.txt']);
      expect(note.answeredBy, ['b2']);
    });

    test('SaidNote.isMine for the persons own room messages', () {
      expect(
          SaidNote.fromJson({'from': 'human', 'kind': 'room'}).isMine,
          isTrue);
    });
  });

  group('BotRoom / BotAssignment / MaterialUpload', () {
    test('BotRoom parses members and attention map', () {
      final room = BotRoom.fromJson({
        'id': 'r1',
        'name': '项目组',
        'members': ['b1', 'b2'],
        'attention': {'b1': 'mentions', 'b2': 'all'},
        'created_at': '2026-01-02T03:04:05Z',
      });
      expect(room.members, ['b1', 'b2']);
      expect(room.attention['b1'], 'mentions');
    });

    test('BotAssignment parses state and report', () {
      final a = BotAssignment.fromJson({
        'id': 'a1',
        'owner_bot': 'b1',
        'target_bot': 'b2',
        'task': '整理数据',
        'state': 'running',
        'last_report': '做到一半',
        'open_question': '要哪种格式？',
        'artifacts': ['out.csv'],
        'result_note_id': 'n9',
        'created_at': '2026-01-02T03:04:05Z',
      });
      expect(a.state, 'running');
      expect(a.openQuestion, '要哪种格式？');
      expect(a.artifacts, ['out.csv']);
    });

    test('MaterialUpload parses path once done', () {
      final up = MaterialUpload.fromJson({
        'upload_id': 'u1',
        'name': 'x.zip',
        'received': 100,
        'total': 100,
        'done': true,
        'path': '~/素材/b/x.zip',
      });
      expect(up.done, isTrue);
      expect(up.path, '~/素材/b/x.zip');
    });
  });

  group('BotCredits', () {
    test('parses limited quota', () {
      final c = BotCredits.fromJson({
        'applies': true,
        'unlimited': false,
        'limit': 100,
        'used': 42.5,
        'remaining': 57.5,
      });
      expect(c.applies, isTrue);
      expect(c.unlimited, isFalse);
      expect(c.used, 42.5);
      expect(c.remaining, 57.5);
    });
  });

  group('parseBotAskOptions', () {
    test('accepts list of strings', () {
      expect(parseBotAskOptions(['a', 'b']), ['a', 'b']);
    });

    test('parses JSON string payloads', () {
      expect(parseBotAskOptions('["yes","no"]'), ['yes', 'no']);
      expect(parseBotAskOptions('{"options":["x"]}'), ['x']);
    });

    test('handles option objects with labels', () {
      final opts = parseBotAskOptions([
        {'label': '甲'},
        {'value': 'v2', 'label': '乙'},
      ]);
      expect(opts, isNotEmpty);
      expect(opts.first, contains('甲'));
    });

    test('junk input yields empty', () {
      expect(parseBotAskOptions('not json'), isEmpty);
      expect(parseBotAskOptions(null), isEmpty);
      expect(parseBotAskOptions(42), isEmpty);
    });
  });

  group('BotQuestion', () {
    test('secret question is answerable and secret', () {
      final q = BotQuestion.fromJson({
        'ask_user_secret_name': 'api_key',
        'ask_user_question': '需要密钥',
      });
      expect(q.isSecret, isTrue);
      expect(q.isAnswerable, isTrue);
    });

    test('choice question is answerable via options', () {
      final q = BotQuestion.fromJson({
        'ask_user_options_json': '["A","B"]',
      });
      expect(q.isChoice, isTrue);
      expect(q.isAnswerable, isTrue);
      expect(q.isSecret, isFalse);
    });
  });

  group('SupplementResult / FollowUpRoute', () {
    test('supplement parses accepted outcome', () {
      final r = SupplementResult.fromJson({
        'supplement_id': 'sup-1',
        'accepted': true,
        'state': 'queued',
        'detail': '下一步生效',
        'run_id': 'r1',
      });
      expect(r.accepted, isTrue);
      expect(r.state, 'queued');
      expect(r.runId, 'r1');
    });

    test('followup parses routed and unrouted shapes', () {
      final routed = FollowUpRoute.fromJson({
        'routed': true,
        'reason': 'routed',
        'bot_id': 'b2',
        'run_id': 'r9',
        'supplement_id': 'sup-9',
      });
      expect(routed.routed, isTrue);
      expect(routed.botId, 'b2');

      final missed = FollowUpRoute.fromJson({'routed': false, 'reason': 'run_ended'});
      expect(missed.routed, isFalse);
      expect(missed.reason, 'run_ended');
    });
  });

  group('DesktopProxyMode / computer', () {
    test('proxy mode view parses locked state and warning', () {
      final m = DesktopProxyMode.fromJson({
        'mode': 'direct',
        'user_mode': 'direct',
        'tenant_default': 'smart',
        'locked': false,
        'applied': false,
        'warning': '下次打开生效',
      });
      expect(m.mode, 'direct');
      expect(m.applied, isFalse);
      expect(m.warning, '下次打开生效');
    });

    test('computer caps and result parse', () {
      final caps = DesktopComputerCaps.fromJson(
          {'restart_allowed': true, 'reset_allowed': false});
      expect(caps.restartAllowed, isTrue);
      expect(caps.resetAllowed, isFalse);

      final r = ComputerResult.fromJson(
          {'action': 'restart', 'container': 'c1', 'status': 'restarted'});
      expect(r.action, 'restart');
      expect(r.status, 'restarted');
    });
  });
}
