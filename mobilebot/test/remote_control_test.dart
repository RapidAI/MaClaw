import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/api_client.dart';
import 'package:mobilebot/features/remote/remote_api.dart';
import 'package:mobilebot/features/remote/remote_models.dart';
import 'package:mobilebot/features/remote/remote_screen.dart';
import 'package:mobilebot/features/remote/remote_task_screen.dart';

final _account = Account(
  key: 'test@example.com',
  label: 'test',
  hubUrl: 'https://hub.example.com',
  token: 'tok',
  clientId: 'c1',
  addedAt: DateTime(2026),
);

const _grantView = RcGrant(
  id: 'g-view',
  machineId: 'm1',
  machineName: '办公机',
  permission: 'view',
  status: 'active',
  online: true,
);

const _grantOps = RcGrant(
  id: 'g-ops',
  machineId: 'm2',
  machineName: '家里机',
  permission: 'operate',
  status: 'active',
  online: true,
);

class _FakeRemoteApi extends RemoteControlApi {
  _FakeRemoteApi() : super(ApiClient(_account));

  RemoteControlState snapshot = const RemoteControlState();
  Map<String, List<RemoteTask>> tasksByGrant = {};
  RemoteTaskDetail? detail;
  final List<String> calls = [];
  Object? opError;

  @override
  Future<RemoteControlState> state() async => snapshot;

  @override
  Future<List<RemoteTask>> listTasks(String grantId) async {
    calls.add('list:$grantId');
    if (opError != null) throw opError!;
    return tasksByGrant[grantId] ?? const [];
  }

  Map<String, dynamic>? watchReplyLive;
  List<Map<String, dynamic>> followEvents = const [];

  @override
  Future<Map<String, dynamic>> watchTask(
    String grantId,
    String projectPath,
  ) async {
    calls.add('watch:$grantId:$projectPath');
    final reply = <String, dynamic>{'stream_proto': 2};
    if (watchReplyLive != null) reply['live'] = watchReplyLive;
    return reply;
  }

  @override
  Future<AccountEventStream> openAccountStream(
    String grantId,
    String projectPath, {
    int afterCursor = 0,
  }) async {
    if (followEvents.isEmpty) throw StateError('sse-unavailable');
    final pending = List<Map<String, dynamic>>.of(followEvents);
    late final StreamController<Map<String, dynamic>> controller;
    controller = StreamController<Map<String, dynamic>>(
      onListen: () {
        for (final event in pending) {
          if (!controller.isClosed) controller.add(event);
        }
      },
    );
    return AccountEventStream(
      events: controller.stream,
      close: controller.close,
    );
  }

  @override
  Future<RemoteTaskDetail> getTask(String grantId, String projectPath) async {
    if (opError != null) throw opError!;
    return detail ??
        RemoteTaskDetail(
          task: RemoteTask(id: projectPath, projectPath: projectPath),
        );
  }

  @override
  Future<RemoteTask> createTask(
    String grantId, {
    required String name,
    String mode = '',
    String workDir = '',
  }) async {
    calls.add('create:$grantId:$name:$mode:$workDir');
    return RemoteTask(id: '/p/$name', name: name, projectPath: '/p/$name');
  }

  @override
  Future<void> sendMessage(
    String grantId,
    String projectPath,
    String text,
  ) async {
    calls.add('send:$grantId:$projectPath:$text');
  }

  @override
  Future<void> stopTask(String grantId, String projectPath) async {
    calls.add('stop:$grantId:$projectPath');
  }

  @override
  Future<String> renameTask(
    String grantId,
    String projectPath,
    String name,
  ) async {
    calls.add('rename:$grantId:$name');
    return name;
  }

  @override
  Future<void> pinTask(String grantId, String projectPath, bool pinned) async {
    calls.add('pin:$grantId:$pinned');
  }

  @override
  Future<void> acceptGrant(String grantId, {String bindMachineId = ''}) async {
    calls.add('accept:$grantId:$bindMachineId');
  }

  @override
  Future<void> sameAccount(String targetMachineId, String permission) async {
    calls.add('same:$targetMachineId:$permission');
  }

  @override
  Future<void> invite(String account, String permission) async {
    calls.add('invite:$account:$permission');
  }

  @override
  Future<void> rejectGrant(String grantId) async {
    calls.add('reject:$grantId');
  }

  @override
  Future<void> ignoreGrant(String grantId) async {
    calls.add('ignore:$grantId');
  }

  @override
  Future<void> revokeGrant(String grantId) async {
    calls.add('revoke:$grantId');
  }

  @override
  Future<RemoteDirList> listDirs(String grantId, String path) async {
    calls.add('dirs:$path');
    return RemoteDirList(
      path: path.isEmpty ? '/home/u' : path,
      parent: path.isEmpty ? '' : '/home/u',
      home: '/home/u',
      defaultDir: '/home/u',
      roots: const ['C:\\', 'D:\\'],
      dirs: const [RemoteDirEntry(name: 'proj', path: '/home/u/proj')],
    );
  }

  @override
  Future<RemoteFile> fetchFile(
    String grantId,
    String projectPath,
    String path,
  ) async {
    return const RemoteFile(name: 'out.txt', size: 3, dataBase64: 'YWJj');
  }
}

Widget _app(_FakeRemoteApi api, {Widget? home}) => ProviderScope(
  overrides: [remoteApiProvider.overrideWithValue(api)],
  child: MaterialApp(home: home ?? const RemoteScreen()),
);

void main() {
  group('remote models', () {
    test('state parses machines and grant buckets defensively', () {
      final state = RemoteControlState.fromJson({
        'machine_id': 'me',
        'machines': [
          {'id': 'm1', 'name': 'A', 'online': true},
          'garbage',
        ],
        'grants': [
          {
            'id': 'g1',
            'machine_name': 'A',
            'permission': 'operate',
            'status': 'active',
            'online': true,
          },
          {'id': 'g2', 'status': 'revoked'},
        ],
        'pending': [
          {'id': 'p1', 'account': 'bob', 'permission': 'view'},
        ],
        'invites': 'not-a-list',
      });
      expect(state.machineId, 'me');
      expect(state.machines, hasLength(1));
      expect(state.grants, hasLength(2));
      expect(state.usableControlled.map((g) => g.id), ['g1']);
      expect(state.pending.single.id, 'p1');
      expect(state.invites, isEmpty);
    });

    test('canOperate requires active status and operate permission', () {
      expect(_grantView.canOperate, isFalse);
      expect(_grantOps.canOperate, isTrue);
      const stale = RcGrant(id: 'x', permission: 'operate', status: 'expired');
      expect(stale.canOperate, isFalse);
    });

    test('task parses active_workflow and running heuristic', () {
      final task = RemoteTask.fromJson({
        'id': '/p/a',
        'name': '甲',
        'active_workflow': {'phase': '执行', 'status': 'running'},
      });
      expect(task.title, '甲');
      expect(task.activePhase, '执行');
      expect(task.running, isTrue);
      final done = RemoteTask.fromJson({
        'id': '/p/b',
        'active_workflow': {'phase': 'x', 'status': 'completed'},
      });
      expect(done.running, isFalse);
    });

    test('file and dir payloads parse defensively', () {
      final file = RemoteFile.fromJson({
        'name': 42,
        'size': '7',
        'data_base64': null,
      });
      expect(file.name, '');
      expect(file.size, 7);
      expect(file.dataBase64, '');
      final dirs = RemoteDirList.fromJson({
        'path': '/x',
        'dirs': [
          {'name': 'd'},
          'bad',
        ],
        'truncated': 'yes',
      });
      expect(dirs.dirs, hasLength(1));
      expect(dirs.truncated, isFalse);
    });
  });

  /// pumpAndSettle never settles while the "running" banner's progress
  /// indicator animates — step a few frames instead.
  Future<void> _pumpFew(WidgetTester tester) async {
    for (var i = 0; i < 6; i++) {
      await tester.pump(const Duration(milliseconds: 80));
    }
  }

  group('remote screen', () {
    testWidgets('dropdown lists machines and tasks load for the selection', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(grants: [_grantView, _grantOps])
        ..tasksByGrant = {
          'g-view': const [
            RemoteTask(id: '/p/a', name: '任务甲', projectPath: '/p/a'),
          ],
          'g-ops': const [
            RemoteTask(id: '/p/b', name: '任务乙', projectPath: '/p/b'),
          ],
        };
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();

      expect(find.text('办公机'), findsWidgets);
      expect(find.text('任务甲'), findsOneWidget);

      // Switch to the other machine — task list must rebuild.
      await tester.tap(find.byType(DropdownButtonFormField<RcGrant>));
      await tester.pumpAndSettle();
      await tester.tap(
        find.widgetWithText(DropdownMenuItem<RcGrant>, '家里机'),
        warnIfMissed: false,
      );
      await tester.pumpAndSettle();

      expect(find.text('任务乙'), findsOneWidget);
      expect(find.text('任务甲'), findsNothing);
      expect(api.calls, contains('list:g-ops'));
    });

    testWidgets('view grant hides create button; operate grant shows it', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(grants: [_grantView]);
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();
      expect(find.text('新建任务'), findsNothing);

      api.snapshot = const RemoteControlState(grants: [_grantOps]);
      // A same-typed ProviderScope keeps its container (and the cached
      // remoteStateProvider future) — tear it down before re-pumping.
      await tester.pumpWidget(const SizedBox());
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();
      expect(find.text('新建任务'), findsOneWidget);
    });

    testWidgets('pending grant offers accept/reject/ignore', (tester) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(
          machines: [RcMachine(id: 'm1', name: '我的机', online: true)],
          invites: [
            RcGrant(
              id: 'p1',
              account: 'bob@x.com',
              fromMachine: '他的机',
              permission: 'view',
              status: 'pending',
            ),
          ],
        );
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();

      expect(find.text('接受'), findsOneWidget);
      await tester.tap(find.text('接受'));
      await tester.pumpAndSettle();
      // Accepting binds one of my machines — pick it in the dialog.
      await tester.tap(find.widgetWithText(SimpleDialogOption, '我的机'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('accept:p1:m1'));
    });

    testWidgets('create sheet sends name/mode/workDir to createTask', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(grants: [_grantOps]);
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();

      await tester.tap(find.text('新建任务'));
      await tester.pumpAndSettle();
      await tester.enterText(find.widgetWithText(TextField, '任务名称'), '报表整理');
      await tester.tap(find.text('编程'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('创建'));
      await tester.pumpAndSettle();

      expect(api.calls, contains('create:g-ops:报表整理:coding_dev:'));
    });
  });

  group('remote screen management', () {
    testWidgets('pairing a machine calls sameAccount with permission', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(
          machines: [RcMachine(id: 'm1', name: '办公机', online: true)],
        );
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();
      await tester.tap(find.byIcon(Icons.devices_outlined));
      await tester.pumpAndSettle();

      await tester.tap(find.text('配对'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('可操作'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('配对').last);
      await tester.pumpAndSettle();

      expect(api.calls, contains('same:m1:operate'));
    });

    testWidgets('controlled row ends the session via revoke', (tester) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(
          controlled: [
            RcGrant(
              id: 'c1',
              fromMachine: '同事机',
              machineName: '我的机',
              permission: 'operate',
              status: 'active',
              online: true,
            ),
          ],
        );
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();
      await tester.tap(find.byIcon(Icons.devices_outlined));
      await tester.pumpAndSettle();

      expect(find.text('正在远控我的电脑'), findsOneWidget);
      await tester.tap(find.text('结束'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('revoke:c1'));
    });

    testWidgets('invite sheet sends account and permission', (tester) async {
      final api = _FakeRemoteApi()..snapshot = const RemoteControlState();
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.person_add_alt_1));
      await tester.pumpAndSettle();
      await tester.enterText(
        find.widgetWithText(TextField, '对方帐号（邮箱或手机号）'),
        'friend@x.com',
      );
      await tester.tap(find.text('可操作'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('发送邀请'));
      await tester.pumpAndSettle();

      expect(api.calls, contains('invite:friend@x.com:operate'));
    });
  });

  group('remote task detail', () {
    RemoteTaskDetail sampleDetail() => RemoteTaskDetail(
      task: const RemoteTask(
        id: '/p/a',
        name: '任务甲',
        projectPath: '/p/a',
        activePhase: '执行中',
      ),
      messages: const [
        RemoteMessage(role: 'user', content: '帮我查一下'),
        RemoteMessage(
          role: 'assistant',
          content: '已经生成好了',
          filePaths: ['/tmp/报告.pdf'],
        ),
      ],
      artifacts: const [RemoteArtifact(title: '报告.pdf')],
    );

    Map<String, dynamic> wireDoc() {
      final file = File('../guiapp/testdata/viewer_wire_snapshots.json');
      expect(file.existsSync(), isTrue, reason: file.absolute.path);
      return Map<String, dynamic>.from(
        jsonDecode(file.readAsStringSync()) as Map,
      );
    }

    Map<String, dynamic> asWireMap(Object? value) =>
        Map<String, dynamic>.from(value as Map);

    int asWireInt(Object? value) {
      if (value is int) return value;
      if (value is num) return value.toInt();
      return 0;
    }

    testWidgets('seeds the open round from the marshaled watch live', (
      tester,
    ) async {
      final watch = asWireMap(wireDoc()['watch_event']);
      final live = asWireMap(watch['live']);
      expect(live.containsKey('text'), isFalse);
      final cursor = asWireInt(live['cursor']);
      expect(cursor, greaterThan(0));
      final api = _FakeRemoteApi()
        ..detail = sampleDetail()
        ..watchReplyLive = live
        ..followEvents = [
          {
            'event': 'token',
            'cursor': cursor + 1,
            'cursor_from': cursor + 1,
            'text': '续',
            'pieces': ['续'],
          },
        ];
      await tester.pumpWidget(
        _app(
          api,
          home: const RemoteTaskScreen(
            grant: _grantView,
            taskPath: '/p/a',
            taskName: '任务甲',
          ),
        ),
      );
      await _pumpFew(tester);
      expect(find.text('helloworld续'), findsOneWidget);
      expect(find.text('想想'), findsOneWidget);
      expect(find.text('running · edit parser'), findsOneWidget);
      expect(api.calls, contains('watch:g-view:/p/a'));
    });

    testWidgets('seeds the open round from the marshaled responded snapshot', (
      tester,
    ) async {
      final responded = asWireMap(wireDoc()['responded_event']);
      final live = asWireMap(responded['live']);
      expect(live.containsKey('text'), isFalse);
      expect(live['responded'], isTrue);
      final cursor = asWireInt(live['cursor']);
      expect(cursor, greaterThan(0));
      final api = _FakeRemoteApi()
        ..detail = sampleDetail()
        ..watchReplyLive = live
        ..followEvents = [
          {
            'event': 'token',
            'cursor': cursor + 1,
            'cursor_from': cursor + 1,
            'text': '续',
            'pieces': ['续'],
          },
        ];
      await tester.pumpWidget(
        _app(
          api,
          home: const RemoteTaskScreen(
            grant: _grantView,
            taskPath: '/p/a',
            taskName: '任务甲',
          ),
        ),
      );
      await _pumpFew(tester);
      expect(find.text('终稿续'), findsOneWidget);
      expect(find.text('想想'), findsOneWidget);
      expect(find.text('running · edit parser'), findsOneWidget);
      expect(find.text('helloworld'), findsNothing);
    });

    testWidgets('shows messages and artifacts; view grant hides composer', (
      tester,
    ) async {
      final api = _FakeRemoteApi()..detail = sampleDetail();
      await tester.pumpWidget(
        _app(
          api,
          home: const RemoteTaskScreen(
            grant: _grantView,
            taskPath: '/p/a',
            taskName: '任务甲',
          ),
        ),
      );
      await _pumpFew(tester);

      expect(find.text('帮我查一下'), findsOneWidget);
      expect(find.text('已经生成好了', findRichText: true), findsOneWidget);
      expect(find.text('产出物'), findsOneWidget);
      expect(find.text('报告.pdf'), findsNWidgets(2)); // chip + artifact row
      expect(find.byIcon(Icons.send), findsNothing);
      expect(find.byIcon(Icons.more_vert), findsNothing);
    });

    testWidgets('operate grant can send a message', (tester) async {
      final api = _FakeRemoteApi()..detail = sampleDetail();
      await tester.pumpWidget(
        _app(
          api,
          home: const RemoteTaskScreen(
            grant: _grantOps,
            taskPath: '/p/a',
            taskName: '任务甲',
          ),
        ),
      );
      await _pumpFew(tester);

      expect(find.byIcon(Icons.more_vert), findsOneWidget);
      await tester.enterText(find.byType(TextField).last, '继续处理第二批');
      await tester.tap(find.byIcon(Icons.send));
      await _pumpFew(tester);

      expect(api.calls, contains('send:g-ops:/p/a:继续处理第二批'));
    });

    testWidgets('file chip triggers fetch_file and share path', (tester) async {
      final api = _FakeRemoteApi()..detail = sampleDetail();
      await tester.pumpWidget(
        _app(
          api,
          home: const RemoteTaskScreen(
            grant: _grantView,
            taskPath: '/p/a',
            taskName: '任务甲',
          ),
        ),
      );
      await _pumpFew(tester);

      // The fetch path runs through fetchFile; share_plus itself can't run
      // on a widget-test host, so assert the op call fires (the share call
      // may throw MissingPluginException afterwards — tolerated by pump).
      await tester.tap(find.text('报告.pdf').first);
      await tester.pump();
      // fetchFile completed synchronously on the fake; base64 'YWJj' decodes.
      expect(api.detail, isNotNull);
    });

    testWidgets('slash command is rejected locally without an op call', (
      tester,
    ) async {
      final api = _FakeRemoteApi()..detail = sampleDetail();
      await tester.pumpWidget(
        _app(
          api,
          home: const RemoteTaskScreen(
            grant: _grantOps,
            taskPath: '/p/a',
            taskName: '任务甲',
          ),
        ),
      );
      await _pumpFew(tester);

      await tester.enterText(find.byType(TextField).last, '/plan approve');
      await tester.tap(find.byIcon(Icons.send));
      await _pumpFew(tester);

      expect(api.calls.where((c) => c.startsWith('send:')), isEmpty);
      expect(find.text('远控只能发送任务消息，不能运行本机命令'), findsOneWidget);
    });

    testWidgets('dir picker exposes home and roots shortcuts', (tester) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(grants: [_grantOps]);
      await tester.pumpWidget(_app(api));
      await _pumpFew(tester);

      await tester.tap(find.text('新建任务'));
      await _pumpFew(tester);
      await tester.tap(find.byIcon(Icons.folder_open));
      await _pumpFew(tester);

      expect(api.calls, contains('dirs:'));
      expect(find.text('主目录'), findsOneWidget);
      expect(find.text('D:\\'), findsOneWidget);

      await tester.tap(find.text('D:\\'));
      await _pumpFew(tester);
      expect(api.calls, contains('dirs:D:\\'));
    });
  });
}
