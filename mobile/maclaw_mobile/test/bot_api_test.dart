import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/bots/bot.dart';
import 'package:maclaw_mobile/features/bots/bot_api.dart';

import 'support/fake_hub.dart';

void main() {
  BotApi apiWith(FakeHubAdapter adapter) =>
      BotApi(dio: dioWithFakeHub(adapter), hubUrl: 'https://hub.example');

  FakeHubAdapter always(
    int status,
    Map<String, dynamic> body,
  ) {
    return FakeHubAdapter((_) => (status: status, body: body));
  }

  group('access and list', () {
    test('reads the access flag and its reason', () async {
      final adapter =
          always(200, {'enabled': false, 'message': '未开通'});
      final access = await apiWith(adapter).fetchAccess();
      expect(access.enabled, isFalse);
      expect(access.message, '未开通');
    });

    test('parses the bot list and drops entries without an id', () async {
      final adapter = always(200, {
        'items': [
          {'id': 'b1', 'name': 'Ada'},
          {'name': 'no id'},
        ],
      });
      final bots = await apiWith(adapter).listBots();
      expect(bots.map((bot) => bot.id), ['b1']);
    });

    test('an empty item list is not an error', () async {
      expect(await apiWith(always(200, const {})).listBots(), isEmpty);
    });
  });

  group('create, rename and delete', () {
    test('create posts the trimmed name and description', () async {
      final adapter = always(201, {'id': 'b9', 'name': 'Ada'});
      final bot = await apiWith(adapter).createBot(
        name: '  Ada  ',
        description: '  research ',
      );
      expect(bot.id, 'b9');
      final request = adapter.requests.single;
      expect(request.path, '/api/v1/bots');
      expect(request.body['name'], 'Ada');
      expect(request.body['description'], 'research');
    });

    test('rename patches the bot path with the escaped id', () async {
      final adapter = always(200, const {});
      await apiWith(adapter).renameBot('a/b', name: 'New');
      final request = adapter.requests.single;
      expect(request.path, '/api/v1/bots/a%2Fb');
      expect(request.body['name'], 'New');
    });

    test('delete hits the bot path', () async {
      final adapter = always(200, const {});
      await apiWith(adapter).deleteBot('b1');
      expect(adapter.requests.single.path, '/api/v1/bots/b1');
    });

    test('a blank id is rejected before any request', () async {
      final adapter = always(200, const {});
      expect(() => apiWith(adapter).deleteBot('  '), throwsArgumentError);
      expect(adapter.requests, isEmpty);
    });
  });

  group('turn protocol', () {
    test('an admitted turn asks for async and yields a run to poll', () async {
      final adapter = FakeHubAdapter((request) {
        if (request.path.endsWith('/messages')) {
          return (
            status: 202,
            body: const {
              'accepted': true,
              'run_id': 'r1',
              'status': 'running',
            },
          );
        }
        return (status: 200, body: const {'text': 'done'});
      });
      final result = await apiWith(adapter).sendMessage('b1', content: 'do it');
      expect(result.isAdmitted, isTrue);
      expect(result.runId, 'r1');

      final send = adapter.requests.single;
      expect(send.path, '/api/v1/bots/b1/messages');
      expect(send.body['content'], 'do it');
      // Prefer is what asks Hub to answer 202 instead of holding the connection
      // for the whole turn.
      expect(send.headers['Prefer'], 'respond-async');
    });

    test('a synchronous reply is settled with nothing to poll', () async {
      final adapter = always(200, {'text': 'already done'});
      final result = await apiWith(adapter).sendMessage('b1', content: 'hi');
      expect(result.isAdmitted, isFalse);
      expect(result.reply?.text, 'already done');
    });

    test('an admitted phase is sent on the wire', () async {
      final adapter = always(200, {'text': 'ok'});
      await apiWith(adapter).sendMessage(
        'b1',
        content: '确认',
        phase: BotPhase.execute,
      );
      expect(adapter.requests.single.body['phase'], 'execute');
    });

    test('an empty message is rejected before any request', () async {
      final adapter = always(200, const {});
      expect(
        () => apiWith(adapter).sendMessage('b1', content: '   '),
        throwsArgumentError,
      );
      expect(adapter.requests, isEmpty);
    });

    test('a 202 poll returns null so the caller keeps polling', () async {
      final adapter = always(202, const {'accepted': true, 'run_id': 'r1'});
      expect(await apiWith(adapter).pollRun('b1', 'r1'), isNull);
      expect(adapter.requests.single.path, '/api/v1/bots/b1/runs/r1');
    });

    test('a finished poll returns the reply', () async {
      final adapter = always(200, {'text': 'all done'});
      final reply = await apiWith(adapter).pollRun('b1', 'r1');
      expect(reply?.text, 'all done');
    });

    test('a run id is escaped into the poll path', () async {
      final adapter = always(202, const {});
      await apiWith(adapter).pollRun('b1', 'r/1');
      expect(adapter.requests.single.path, '/api/v1/bots/b1/runs/r%2F1');
    });

    test('a blank run id is rejected before any request', () async {
      final adapter = always(202, const {});
      expect(() => apiWith(adapter).pollRun('b1', ' '), throwsArgumentError);
      expect(adapter.requests, isEmpty);
    });

    test('a 500 on the send is an error, not a bot reply', () async {
      // A 500 body must never be read as if it were the turn's result.
      final adapter = always(500, const {'error': 'INTERNAL', 'message': 'boom'});
      await expectLater(
        apiWith(adapter).sendMessage('b1', content: 'do it'),
        throwsA(isA<BotApiException>()),
      );
    });
  });

  group('desktop and secrets', () {
    test('a desktop hold posts to the desktop path', () async {
      final adapter = always(200, {
        'novnc_url': '/novnc/vnc.html',
        'user_control': true,
        'attention_reason': 'login',
      });
      final desktop = await apiWith(adapter).watchDesktop('b1', epoch: 3);
      expect(desktop.isRunning, isTrue);
      expect(desktop.userControl, isTrue);
      expect(desktop.attentionReason, 'login');
      final request = adapter.requests.single;
      expect(request.path, '/api/v1/bots/b1/desktop');
      expect(request.body['epoch'], 3);
      expect(request.body.containsKey('release'), isFalse);
    });

    test('a desktop release sends the release flag', () async {
      final adapter = always(200, const {'released': true});
      await apiWith(adapter).watchDesktop('b1', release: true);
      expect(adapter.requests.single.body['release'], isTrue);
    });

    test('a secret fill posts name and value', () async {
      final adapter = always(200, const {'filled': 'github_password'});
      await apiWith(adapter).fillSecret(
        'b1',
        name: 'github_password',
        value: 's3cret',
      );
      final request = adapter.requests.single;
      expect(request.path, '/api/v1/bots/b1/secret-fill');
      expect(request.body['name'], 'github_password');
      expect(request.body['value'], 's3cret');
    });
  });

  group('failures become one error type', () {
    test('a Hub error keeps its code and message', () async {
      final dio = Dio(BaseOptions(baseUrl: 'https://hub.example'))
        ..httpClientAdapter = FailingHubAdapter(
          httpFailure(
            403,
            data: const {
              'error': 'BOT_DISABLED',
              'message': '服务器没有开通bot功能',
            },
          ),
        );
      await expectLater(
        BotApi(dio: dio, hubUrl: 'https://hub.example').listBots(),
        throwsA(
          isA<BotApiException>()
              .having((e) => e.code, 'code', 'BOT_DISABLED')
              .having((e) => e.message, 'message', '服务器没有开通bot功能')
              .having((e) => e.isFeatureDisabled, 'isFeatureDisabled', isTrue),
        ),
      );
    });

    test('a 403 with no body still explains itself', () {
      final error = botApiExceptionFrom(httpFailure(403));
      expect(error.isUnauthorized, isTrue);
      expect(error.message, isNotEmpty);
    });

    test('a 404 without a code maps to NOT_FOUND', () {
      final error = botApiExceptionFrom(httpFailure(404));
      expect(error.code, 'NOT_FOUND');
      expect(error.statusCode, 404);
    });

    test('a non-2xx response body is read for its code', () {
      final error = botApiExceptionFrom(
        httpFailure(
          400,
          data: const {'error': 'INVALID_BOT_SETTINGS', 'message': 'bad'},
        ),
      );
      expect(error.code, 'INVALID_BOT_SETTINGS');
      expect(error.message, 'bad');
    });
  });

  group('error copy', () {
    test('an unauthorized status maps to the sign-in copy', () {
      expect(botFailureMessage(httpFailure(401)), contains('重新登录'));
    });

    test('a timeout maps to the network copy', () {
      expect(
        botFailureMessage(
          DioException(
            requestOptions: RequestOptions(path: '/x'),
            type: DioExceptionType.receiveTimeout,
          ),
        ),
        contains('网络'),
      );
    });

    test('a connection error maps to the unreachable copy', () {
      expect(
        botFailureMessage(
          DioException(
            requestOptions: RequestOptions(path: '/x'),
            type: DioExceptionType.connectionError,
          ),
        ),
        contains('无法连接'),
      );
    });

    test('the code label covers the codes a person can act on', () {
      expect(botErrorCodeLabel('BOT_DISABLED'), contains('管理员'));
      expect(botErrorCodeLabel('NOT_FOUND'), contains('不存在'));
      expect(botErrorCodeLabel('SOMETHING_NEW'), isNotEmpty);
    });

    test('a plain exception becomes generic copy, not a raw message', () {
      expect(
        botFailureMessage(StateError('raw detail')),
        isNot(contains('raw detail')),
      );
    });
  });
}