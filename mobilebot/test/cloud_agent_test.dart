import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/l10n/gen/app_localizations.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/api_client.dart';
import 'package:mobilebot/features/cloud/cloud_api.dart';
import 'package:mobilebot/features/cloud/cloud_models.dart';
import 'package:mobilebot/features/cloud/cloud_screen.dart';

import 'l10n_test_helper.dart';

final _account = Account(
  key: 'test@example.com',
  label: 'test',
  hubUrl: 'https://hub.example.com',
  token: 'tok',
  clientId: 'c1',
  addedAt: DateTime(2026),
);

class _FakeCloudApi extends CloudApi {
  _FakeCloudApi() : super(ApiClient(_account));

  CloudInstanceList snapshot = const CloudInstanceList();
  List<CloudTask> tasks = const [];
  CloudTaskDetail detail = const CloudTaskDetail(taskId: 'task-1');
  final List<String> calls = [];

  @override
  Future<CloudInstanceList> listInstances() async => snapshot;

  @override
  Future<String> openGrant(String instanceId) async {
    calls.add('grant:$instanceId');
    return 'grant-$instanceId';
  }

  @override
  Future<List<CloudTask>> listTasks(String grantId) async {
    calls.add('list:$grantId');
    return tasks;
  }

  @override
  Future<CloudTask> createTask(String grantId, {required String title}) async {
    calls.add('create:$grantId:$title');
    return CloudTask(id: 'task-new', title: title);
  }

  @override
  Future<CloudInstance> stopInstance(String id) async {
    calls.add('stop:$id');
    return CloudInstance(id: id, status: cloudStatusStopped);
  }

  @override
  Future<CloudInstance> resumeInstance(String id) async {
    calls.add('resume:$id');
    return CloudInstance(id: id, status: cloudStatusIdle);
  }

  @override
  Future<CloudTaskDetail> getTask(String grantId, String taskId) async {
    calls.add('get:$grantId:$taskId');
    return detail;
  }

  @override
  Future<void> sendMessage(
    String grantId,
    String taskId,
    String text, {
    String clientMessageId = '',
  }) async {
    calls.add('send:$grantId:$taskId:$text');
  }

  @override
  Future<void> stopTask(String grantId, String taskId) async {
    calls.add('stop-task:$grantId:$taskId');
  }
}

void main() {
  test('instance list keeps known statuses and a blank unknown', () {
    final list = CloudInstanceList.fromJson({
      'enabled': true,
      'instances': [
        {'id': 'a', 'name': '会话', 'status': 'running', 'kind': 'session'},
        {'id': 'b', 'status': 'mystery'},
        'garbage',
      ],
    });
    expect(list.enabled, isTrue);
    expect(list.instances, hasLength(2));
    expect(list.instances.first.status, cloudStatusRunning);
    expect(list.instances.first.canStop, isTrue);
    expect(list.instances.last.status, 'mystery');
    expect(list.instances.last.canStop, isFalse);
    expect(list.instances.last.canResume, isFalse);
  });

  test('transcript lines split on the first newline only', () {
    final detail = CloudTaskDetail.fromJson({
      'task_id': 'task-1',
      'messages': ['user\nhello\nthere', 'assistant\nworld', 'plain'],
      'as_error': [false, true],
      'files': [
        ['a.txt'],
      ],
      'turn_status': 'finished',
    });
    expect(detail.turnStatus, 'finished');
    expect(detail.messages[0].role, 'user');
    expect(detail.messages[0].content, 'hello\nthere');
    expect(detail.messages[0].files, ['a.txt']);
    expect(detail.messages[1].asError, isTrue);
    expect(detail.messages[2].role, isEmpty);
    expect(detail.messages[2].content, 'plain');
  });

  test('turn status labels known values and keeps an unknown raw', () {
    final s = lookupAppLocalizations(const Locale('zh'));
    expect(cloudTurnLabel(s, cloudTurnRunning), '回合进行中');
    expect(cloudTurnLabel(s, 'mystery'), 'mystery');
    expect(cloudTurnLabel(s, ''), '');
  });

  test('only stopped can resume; failed provision cannot', () {
    expect(
      const CloudInstance(id: '1', status: cloudStatusStopped).canResume,
      isTrue,
    );
    expect(
      const CloudInstance(
        id: '1',
        status: cloudStatusProvisionFailed,
      ).canResume,
      isFalse,
    );
    expect(
      const CloudInstance(id: '1', status: cloudStatusIdle).canStop,
      isTrue,
    );
    expect(
      const CloudInstance(id: '1', status: cloudStatusProvisioning).canStop,
      isTrue,
    );
  });

  testWidgets('cloud list shows instance status and tasks, not a machine', (
    tester,
  ) async {
    final api = _FakeCloudApi()
      ..snapshot = const CloudInstanceList(
        enabled: true,
        instances: [
          CloudInstance(id: 'cag-1', name: '账号云端', status: cloudStatusRunning),
        ],
      )
      ..tasks = const [CloudTask(id: 'task-1', title: '写文档')];
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: l10nApp(child: const CloudScreen()),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('账号云端'), findsWidgets);
    expect(find.text('运行中'), findsOneWidget);
    expect(find.text('写文档'), findsOneWidget);
    expect(find.text('远程机'), findsNothing);
    expect(api.calls, contains('list:grant-cag-1'));
  });
}
