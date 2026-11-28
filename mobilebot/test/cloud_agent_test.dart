import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:mobilebot/l10n/gen/app_localizations.dart';
import 'package:shared_preferences/shared_preferences.dart';
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

  /// 加载更早（基础页已发出后）返回的内容；null 时复用 [detail]。首载与
  /// 更早页都传 40，用「是否已发过基础页」区分。
  CloudTaskDetail? earlierDetail;
  var _servedBasePage = false;
  int lastHistoryLimit = 0;
  String? deletedTaskId;
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
  Future<CloudTaskDetail> getTask(
    String grantId,
    String taskId, {
    int historyLimit = 0,
  }) async {
    lastHistoryLimit = historyLimit;
    calls.add('get:$grantId:$taskId:$historyLimit');
    if (historyLimit > 0 && earlierDetail != null && _servedBasePage) {
      return earlierDetail!;
    }
    _servedBasePage = true;
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

  @override
  Future<void> deleteTask(String grantId, String taskId) async {
    deletedTaskId = taskId;
    calls.add('delete:$grantId:$taskId');
  }

  @override
  Future<void> renameTask(
    String grantId,
    String taskId,
    String title,
  ) async {
    calls.add('rename:$grantId:$taskId:$title');
  }
}


Future<void> pumpFew(WidgetTester tester) async {
  // 轮询 Timer 在跑，pumpAndSettle 永不静止：远控测试同款，用短泵推进帧。
  for (var i = 0; i < 6; i++) {
    await tester.pump(const Duration(milliseconds: 80));
  }
}

Future<void> teardownTree(WidgetTester tester) async {
  // 卸载会话屏，dispose 取消轮询 Timer，避免挂起的 Timer 拖垮用例。
  await tester.pumpWidget(const SizedBox());
  await tester.pump(const Duration(milliseconds: 1));
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
    await pumpFew(tester);
    expect(find.text('账号云端'), findsWidgets);
    expect(find.text('运行中'), findsOneWidget);
    expect(find.text('写文档'), findsOneWidget);
    expect(find.text('远程机'), findsNothing);
    expect(api.calls, contains('list:grant-cag-1'));
    await teardownTree(tester);
  });

  testWidgets('assistant transcript is markdown; user and error stay plain', (
    tester,
  ) async {
    final api = _FakeCloudApi()
      ..detail = const CloudTaskDetail(
        taskId: 'task-1',
        messages: [
          CloudMessage(role: 'user', content: '**mine** https://example.com'),
          CloudMessage(role: 'assistant', content: '**done** https://example.com'),
          CloudMessage(role: 'assistant', content: '**failed**', asError: true),
        ],
      );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: l10nApp(
          child: const CloudTaskScreen(
            instance: CloudInstance(id: 'cag-1', name: '账号云端'),
            task: CloudTask(id: 'task-1', title: '写文档'),
          ),
        ),
      ),
    );
    await pumpFew(tester);
    expect(find.text('**mine** https://example.com'), findsOneWidget);
    expect(find.text('**failed**'), findsOneWidget);
    expect(find.text('**done** https://example.com'), findsNothing);
    expect(find.textContaining('done'), findsWidgets);
    expect(find.textContaining('example.com'), findsWidgets);
    await teardownTree(tester);
  });

  test('task summary rows parse status and updated_at; badge maps states', () {
    final tasks = [
      for (final item in [
        {
          'id': 't1',
          'title': 'A',
          'status': 'running',
          'updated_at': '2026-11-28T10:00:00Z',
        },
        {'id': 't2', 'title': 'B', 'status': 'finished'},
        {'id': 't3', 'title': 'C'},
      ])
        CloudTask.fromJson(item as Map),
    ];
    expect(tasks[0].status, cloudTurnRunning);
    expect(tasks[0].updatedAt, '2026-11-28T10:00:00Z');
    expect(tasks[1].status, cloudTurnFinished);
    expect(tasks[2].status, isEmpty);
    final s = lookupAppLocalizations(const Locale('zh'));
    final scheme = ColorScheme.fromSeed(seedColor: Colors.blue);
    expect(cloudTaskBadge(s, scheme, tasks[0])?.label, '执行中');
    expect(cloudTaskBadge(s, scheme, tasks[1])?.label, '已完成');
    expect(cloudTaskBadge(s, scheme, tasks[2]), isNull);
  });

  test('detail parses message_total and per-row events', () {
    final detail = CloudTaskDetail.fromJson({
      'task_id': 'task-1',
      'messages': ['user\n问题', 'assistant\n答复'],
      'as_error': [false, false],
      'request_ids': ['req-1', 'req-1'],
      'events': [
        [],
        [
          {'seq': 1, 'kind': 'progress', 'text': '正在读取文件'},
          {'seq': 2, 'kind': 'thinking', 'text': '想一想'},
        ],
      ],
      'message_total': 2,
      'turn_status': 'finished',
    });
    expect(detail.messageTotal, 2);
    expect(detail.messages[0].requestId, 'req-1');
    expect(detail.messages[1].progressRows.map((e) => e.text), ['正在读取文件']);
    expect(detail.messages[1].thinkingText, '想一想');
    expect(detail.messages[0].thinkingText, isEmpty);
    // 旧 runtime 不带这些字段时不崩：窗口即全部。
    final legacy = CloudTaskDetail.fromJson({
      'task_id': 'task-1',
      'messages': ['user\nhi'],
      'as_error': [false],
      'files': [],
      'turn_status': '',
    });
    expect(legacy.messageTotal, 1);
  });

  testWidgets('detail screen renders restored progress and thinking rows', (
    tester,
  ) async {
    final api = _FakeCloudApi()
      ..detail = CloudTaskDetail.fromJson({
        'task_id': 'task-1',
        'messages': ['user\n问题', 'assistant\n答复'],
        'as_error': [false, false],
        'request_ids': ['req-1', 'req-1'],
        'events': [
          [],
          [
            {'seq': 1, 'kind': 'progress', 'text': '正在读取文件'},
            {'seq': 2, 'kind': 'thinking', 'text': '这是推理内容'},
          ],
        ],
        'message_total': 2,
        'turn_status': 'finished',
      });
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: l10nApp(
          child: const CloudTaskScreen(
            instance: CloudInstance(id: 'cag-1', name: '账号云端'),
            task: CloudTask(id: 'task-1', title: '写文档'),
          ),
        ),
      ),
    );
    await pumpFew(tester);
    expect(find.text('正在读取文件'), findsOneWidget);
    expect(find.text('思考过程'), findsOneWidget);
    // 思考默认折叠；展开后可见内容。
    expect(find.text('这是推理内容'), findsNothing);
    await tester.tap(find.text('思考过程'));
    await pumpFew(tester);
    expect(find.text('这是推理内容'), findsOneWidget);
    await teardownTree(tester);
  });

  testWidgets('paged history shows the count, loads earlier rows once', (
    tester,
  ) async {
    final api = _FakeCloudApi()
      ..detail = const CloudTaskDetail(
        taskId: 'task-1',
        messages: [
          CloudMessage(role: 'user', content: ' newer question ', requestId: 'r2'),
          CloudMessage(role: 'assistant', content: 'newer answer', requestId: 'r2'),
        ],
        messageTotal: 4,
        turnStatus: cloudTurnFinished,
      )
      ..earlierDetail = const CloudTaskDetail(
        taskId: 'task-1',
        messages: [
          CloudMessage(role: 'user', content: 'earlier question', requestId: 'r1'),
          CloudMessage(role: 'assistant', content: 'earlier answer', requestId: 'r1'),
          CloudMessage(role: 'user', content: ' newer question ', requestId: 'r2'),
          CloudMessage(role: 'assistant', content: 'newer answer', requestId: 'r2'),
        ],
        messageTotal: 4,
        turnStatus: cloudTurnFinished,
      );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: l10nApp(
          child: const CloudTaskScreen(
            instance: CloudInstance(id: 'cag-1', name: '账号云端'),
            task: CloudTask(id: 'task-1', title: '写文档'),
          ),
        ),
      ),
    );
    await pumpFew(tester);
    expect(find.text('已显示 2/4 条'), findsOneWidget);
    expect(find.text('earlier question'), findsNothing);
    await tester.tap(find.byKey(const ValueKey('cloud-load-earlier')));
    await pumpFew(tester);
    expect(api.lastHistoryLimit, 40);
    expect(find.text('earlier question'), findsOneWidget);
    expect(find.text('earlier answer'), findsOneWidget);
    expect(find.text('已显示 4/4 条'), findsOneWidget);
    // 全量后不再出现加载按钮。
    expect(find.byKey(const ValueKey('cloud-load-earlier')), findsNothing);
    await teardownTree(tester);
  });

  testWidgets('send echoes optimistically and stays until the page folds it in', (
    tester,
  ) async {
    final api = _FakeCloudApi()
      ..detail = const CloudTaskDetail(taskId: 'task-1', messageTotal: 0);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: l10nApp(
          child: const CloudTaskScreen(
            instance: CloudInstance(id: 'cag-1', name: '账号云端'),
            task: CloudTask(id: 'task-1', title: '写文档'),
          ),
        ),
      ),
    );
    await pumpFew(tester);
    await tester.enterText(find.byType(TextField), '帮我查一下');
    await tester.tap(find.byIcon(Icons.send));
    await pumpFew(tester);
    expect(find.text('帮我查一下'), findsOneWidget);
    expect(api.calls, contains('send:grant-cag-1:task-1:帮我查一下'));
    await teardownTree(tester);
  });

  testWidgets('drawer lists tasks with status chips; swipe deletes after confirm', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final api = _FakeCloudApi()
      ..tasks = const [
        CloudTask(id: 'task-1', title: '写文档', status: cloudTurnFinished),
        CloudTask(id: 'task-2', title: '旧任务', status: cloudTurnRunning),
      ];
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: l10nApp(
          child: const CloudTaskScreen(
            instance: CloudInstance(id: 'cag-1', name: '账号云端'),
            task: CloudTask(id: 'task-1', title: '写文档'),
          ),
        ),
      ),
    );
    await pumpFew(tester);
    await tester.tap(find.byTooltip('会话列表'));
    await pumpFew(tester);
    expect(find.text('旧任务'), findsOneWidget);
    expect(find.text('执行中'), findsOneWidget);
    expect(find.text('已完成'), findsOneWidget);
    // 左滑露出删除，确认弹窗后才真删。
    await tester.drag(find.text('旧任务'), const Offset(-400, 0));
    await pumpFew(tester);
    expect(find.textContaining('确认删除任务'), findsOneWidget);
    expect(api.deletedTaskId, isNull);
    await tester.tap(find.text('删除'));
    await pumpFew(tester);
    expect(api.deletedTaskId, 'task-2');
    expect(find.text('任务已删除'), findsOneWidget);
    expect(api.calls, contains('delete:grant-cag-1:task-2'));
    await teardownTree(tester);
  });

  testWidgets('drawer long-press renames the task', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final api = _FakeCloudApi()
      ..tasks = const [CloudTask(id: 'task-2', title: '旧任务')];
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: l10nApp(
          child: const CloudTaskScreen(
            instance: CloudInstance(id: 'cag-1', name: '账号云端'),
            task: CloudTask(id: 'task-1', title: '写文档'),
          ),
        ),
      ),
    );
    await pumpFew(tester);
    await tester.tap(find.byTooltip('会话列表'));
    await pumpFew(tester);
    await tester.longPress(find.text('旧任务'));
    await pumpFew(tester);
    await tester.enterText(find.byType(TextField).last, '新任务名');
    await tester.tap(find.text('重命名'));
    await pumpFew(tester);
    expect(api.calls, contains('rename:grant-cag-1:task-2:新任务名'));
    await teardownTree(tester);
  });

  testWidgets('reopening the cloud tab returns to the last conversation', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({
      'mobilebot.cloud.lastTask':
          '{"instance_id":"cag-1","task_id":"task-1"}',
    });
    resetCloudAutoOpenForTests();
    final api = _FakeCloudApi()
      ..snapshot = const CloudInstanceList(
        enabled: true,
        instances: [CloudInstance(id: 'cag-1', name: '账号云端')],
      )
      ..tasks = const [CloudTask(id: 'task-1', title: '写文档')];
    // 自动恢复走 context.push，需要真实路由栈。
    final router = GoRouter(
      initialLocation: '/cloud',
      routes: [
        GoRoute(path: '/cloud', builder: (_, _) => const CloudScreen()),
        GoRoute(
          path: '/cloud/task',
          builder: (context, state) {
            final extra = state.extra;
            if (extra is ({CloudInstance instance, CloudTask task})) {
              return CloudTaskScreen(
                instance: extra.instance,
                task: extra.task,
              );
            }
            return const Scaffold(body: SizedBox());
          },
        ),
      ],
    );
    addTearDown(router.dispose);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: MaterialApp.router(
          locale: const Locale('zh'),
          localizationsDelegates: AppLocalizations.localizationsDelegates,
          supportedLocales: AppLocalizations.supportedLocales,
          routerConfig: router,
        ),
      ),
    );
    await pumpFew(tester);
    // 直接落进会话界面：composer 在这里，实例列表页没有输入框。
    expect(find.text('发消息…'), findsOneWidget);
    expect(api.calls, contains('list:grant-cag-1'));
    await teardownTree(tester);
  });

  testWidgets('a stale saved task clears instead of reopening', (tester) async {
    SharedPreferences.setMockInitialValues({
      'mobilebot.cloud.lastTask':
          '{"instance_id":"cag-1","task_id":"task-gone"}',
    });
    resetCloudAutoOpenForTests();
    final api = _FakeCloudApi()
      ..snapshot = const CloudInstanceList(
        enabled: true,
        instances: [CloudInstance(id: 'cag-1', name: '账号云端')],
      )
      ..tasks = const [CloudTask(id: 'task-1', title: '写文档')];
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: l10nApp(child: const CloudScreen()),
      ),
    );
    await pumpFew(tester);
    // 任务不在列表里：留在实例列表页，保存项被清掉。
    expect(find.text('发消息…'), findsNothing);
    expect(find.text('账号云端'), findsWidgets);
    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getString('mobilebot.cloud.lastTask'), isNull);
  });

  testWidgets('a request split across pages shows its events once', (
    tester,
  ) async {
    // 首载窗口只含 r2 的用户行，过程行挂在那里；更早页把 r1 的助手行带回
    // 来后，r2 的旧挂载必须按新页剥掉。
    final api = _FakeCloudApi()
      ..detail = CloudTaskDetail.fromJson({
        'task_id': 'task-1',
        'messages': ['user\n问题二'],
        'request_ids': ['r2'],
        'events': [
          [
            {'seq': 1, 'kind': 'progress', 'text': '过程二'},
          ],
        ],
        'message_total': 3,
        'turn_status': 'finished',
      })
      ..earlierDetail = CloudTaskDetail.fromJson({
        'task_id': 'task-1',
        'messages': ['user\n问题一', 'assistant\n答复一', 'user\n问题二'],
        'request_ids': ['r1', 'r1', 'r2'],
        'events': [
          [],
          [
            {'seq': 1, 'kind': 'progress', 'text': '过程一'},
          ],
          [],
        ],
        'message_total': 3,
        'turn_status': 'finished',
      });
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: l10nApp(
          child: const CloudTaskScreen(
            instance: CloudInstance(id: 'cag-1', name: '账号云端'),
            task: CloudTask(id: 'task-1', title: '写文档'),
          ),
        ),
      ),
    );
    await pumpFew(tester);
    expect(find.text('过程二'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('cloud-load-earlier')));
    await pumpFew(tester);
    expect(find.text('答复一'), findsOneWidget);
    expect(find.text('过程一'), findsOneWidget);
    // r2 的过程行以新页（空）为准，不重复显示旧窗里那份。
    expect(find.text('过程二'), findsNothing);
    await teardownTree(tester);
  });

  testWidgets('drawer switches to another task through the screen callback', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final api = _FakeCloudApi()
      ..tasks = const [
        CloudTask(id: 'task-1', title: '写文档', status: cloudTurnFinished),
        CloudTask(id: 'task-2', title: '旧任务'),
      ];
    // 抽屉切换走 context.push，需要真实路由栈；首屏记录由测试变量兜底。
    var current = (
      instance: const CloudInstance(id: 'cag-1', name: '账号云端'),
      task: const CloudTask(id: 'task-1', title: '写文档'),
    );
    final router = GoRouter(
      initialLocation: '/cloud/task',
      routes: [
        GoRoute(path: '/cloud', builder: (_, _) => const CloudScreen()),
        GoRoute(
          path: '/cloud/task',
          builder: (context, state) {
            final extra = state.extra ?? current;
            if (extra
                is ({CloudInstance instance, CloudTask task})) {
              return CloudTaskScreen(
                instance: extra.instance,
                task: extra.task,
              );
            }
            return const Scaffold(body: SizedBox());
          },
        ),
      ],
    );
    addTearDown(router.dispose);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [cloudApiProvider.overrideWithValue(api)],
        child: MaterialApp.router(
          locale: const Locale('zh'),
          localizationsDelegates: AppLocalizations.localizationsDelegates,
          supportedLocales: AppLocalizations.supportedLocales,
          routerConfig: router,
        ),
      ),
    );
    await pumpFew(tester);
    await tester.tap(find.byTooltip('会话列表'));
    await pumpFew(tester);
    await tester.tap(find.text('旧任务'));
    await pumpFew(tester);
    // 新会话推入栈顶：现在有两层聊天屏（两个 composer），抽屉已收回。
    expect(find.text('发消息…'), findsNWidgets(2));
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getString('mobilebot.cloud.lastTask');
    expect(saved, contains('task-2'));
    await teardownTree(tester);
  });
}
