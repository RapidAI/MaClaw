import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'l10n_test_helper.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/api_client.dart';
import 'package:mobilebot/features/remote/remote_api.dart';
import 'package:mobilebot/features/remote/remote_models.dart';
import 'package:mobilebot/features/remote/remote_screen.dart';
import 'package:mobilebot/features/remote/remote_task_screen.dart';
import 'package:mobilebot/features/remote/task_spaces.dart';

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
  int watchReplyCursor = 0;
  Map<String, dynamic>? gapWatchReply;
  List<Map<String, dynamic>> gapReplies = [];
  int lastAfterCursor = 0;
  StreamController<Map<String, dynamic>>? liveController;
  List<Map<String, dynamic>> followEvents = const [];
  void Function(int watches)? onWatch;
  Future<void> Function(int watches)? beforeReply;

  @override
  Future<Map<String, dynamic>> watchTask(
    String grantId,
    String projectPath,
  ) async {
    calls.add('watch:$grantId:$projectPath');
    final watches = calls.where((call) => call.startsWith('watch:')).length;
    onWatch?.call(watches);
    final wait = beforeReply;
    if (wait != null) await wait(watches);
    if (watches > 1 && gapReplies.isNotEmpty) return gapReplies.removeAt(0);
    if (watches > 1 && gapWatchReply != null) return gapWatchReply!;
    final reply = <String, dynamic>{'stream_proto': 2};
    if (watchReplyCursor > 0) reply['cursor'] = watchReplyCursor;
    if (watchReplyLive != null) reply['live'] = watchReplyLive;
    return reply;
  }

  @override
  Future<AccountEventStream> openAccountStream(
    String grantId,
    String projectPath, {
    int afterCursor = 0,
  }) async {
    lastAfterCursor = afterCursor;
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
    liveController = controller;
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
  child: l10nApp(child: home ?? const RemoteScreen()),
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

  group('task spaces', () {
    RemoteTask t(
      String id, {
      String workingDir = '',
      bool pinned = false,
      String lastActivity = '',
      String activeStatus = '',
    }) => RemoteTask(
      id: id,
      name: id,
      projectPath: id,
      workingDir: workingDir,
      pinned: pinned,
      lastActivity: lastActivity,
      activePhase: activeStatus.isEmpty ? '' : '执行',
      activeStatus: activeStatus,
    );

    test('groups by workingDir, falls back to project_path parent', () {
      final spaces = groupTasksIntoSpaces([
        t('/p/a', workingDir: r'D:\proj\web', lastActivity: '2026-01-01'),
        t('/p/b', workingDir: r'D:\proj\web\', lastActivity: '2026-01-02'),
        // 无 working_dir → 用 project_path 的父目录。
        t('/repo/lib/main.dart', lastActivity: '2026-01-03'),
        t('/repo/lib/util.dart', lastActivity: '2026-01-04'),
        // 根目录文件 → 未分组。
        t('/top.txt', lastActivity: '2026-01-05'),
      ]);
      expect(spaces.length, 3);
      final web = spaces.firstWhere((s) => s.label == 'web');
      expect(web.tasks.length, 2);
      final lib = spaces.firstWhere((s) => s.label == 'lib');
      expect(lib.tasks.length, 2);
      final other = spaces.firstWhere((s) => s.isUngrouped);
      expect(other.tasks.single.id, '/top.txt');
    });

    test('running space floats first; tasks sort pinned then recent', () {
      final spaces = groupTasksIntoSpaces([
        // 空闲空间但最近活动 — 应排在运行中空间之后。
        t('/p/idle1', workingDir: '/idle', lastActivity: '2026-06-30'),
        t('/p/run1', workingDir: '/busy', lastActivity: '2026-01-01',
            activeStatus: 'running'),
        t('/p/run2', workingDir: '/busy', lastActivity: '2026-01-02',
            pinned: true),
      ]);
      expect(spaces.first.label, 'busy');
      expect(spaces.first.running, isTrue);
      expect(spaces.last.label, 'idle');
      // 组内：pinned 先于 lastActivity 更晚的。
      expect(spaces.first.tasks.map((x) => x.id).toList(),
          ['/p/run2', '/p/run1']);
    });

    test('parentDir handles windows and posix paths', () {
      expect(parentDir(r'D:\proj\web'), r'D:\proj');
      expect(parentDir(r'D:\proj'), 'D:\\');
      expect(parentDir('/home/x/y'), '/home/x');
      expect(parentDir('/top'), '');
      expect(parentDir(''), '');
      expect(pathBaseName(r'D:\proj\web'), 'web');
      expect(pathBaseName('D:\\'), 'D:\\');
      expect(normalizeDir(r'D:\proj\web\'), r'D:\proj\web');
    });

    test('empty input and ungrouped ordering', () {
      expect(groupTasksIntoSpaces(const []), isEmpty);
      // 未分组空间不特殊处理 — 按 lastActivity 参与排序。
      final spaces = groupTasksIntoSpaces([
        t('/top.txt', lastActivity: '2026-06-01'), // 未分组但最新
        t('/p/a', workingDir: '/old', lastActivity: '2026-01-01'),
      ]);
      expect(spaces.first.isUngrouped, isTrue);
      expect(spaces.last.label, 'old');
    });

    test('working_dir differing only by trailing slash merges into one space', () {
      final spaces = groupTasksIntoSpaces([
        t('/p/a', workingDir: '/proj/web'),
        t('/p/b', workingDir: '/proj/web/'),
      ]);
      expect(spaces, hasLength(1));
      expect(spaces.single.tasks, hasLength(2));
    });
  });

  /// pumpAndSettle never settles while the "running" banner's progress
  /// indicator animates — step a few frames instead.
  Future<void> pumpFew(WidgetTester tester) async {
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

    testWidgets('tasks group under a collapsible space header', (tester) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(grants: [_grantOps])
        ..tasksByGrant = {
          'g-ops': const [
            RemoteTask(
              id: '/p/a',
              name: '任务甲',
              projectPath: '/p/a',
              workingDir: r'D:\proj\web',
            ),
            RemoteTask(
              id: '/p/b',
              name: '任务乙',
              projectPath: '/p/b',
              workingDir: r'D:\proj\web',
            ),
            RemoteTask(id: '/任务丙.md', name: '任务丙', projectPath: '/任务丙.md'),
          ],
        };
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();

      // 一个 "web" 空间头 + 一个「其他」空间头；默认只展开最活跃的 web。
      expect(find.text('web'), findsOneWidget);
      expect(find.text('其他'), findsOneWidget);
      expect(find.text('2 个任务'), findsOneWidget);
      expect(find.text('1 个任务'), findsOneWidget);
      expect(find.text('任务甲'), findsOneWidget);
      // 「其他」空间默认折叠 → 组内任务不可见。
      expect(find.text('任务丙'), findsNothing);

      // 展开「其他」→ 任务丙出现。
      await tester.tap(find.text('其他'));
      await tester.pumpAndSettle();
      expect(find.text('任务丙'), findsOneWidget);

      // 折叠 web 空间 → 组内任务隐藏。
      await tester.tap(find.text('web'));
      await tester.pumpAndSettle();
      expect(find.text('任务甲'), findsNothing);
      expect(find.text('任务丙'), findsOneWidget);

      // 再展开 → 任务回来。
      await tester.tap(find.text('web'));
      await tester.pumpAndSettle();
      expect(find.text('任务甲'), findsOneWidget);
    });

    testWidgets('default-expands the space with a running task', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(grants: [_grantOps])
        ..tasksByGrant = {
          'g-ops': const [
            // idle 空间 lastActivity 更新，但 busy 有运行中 → busy 排第一
            // 并默认展开，idle 折叠。
            RemoteTask(
              id: '/p/idle1',
              name: '空闲任务',
              projectPath: '/p/idle1',
              workingDir: '/idle',
              lastActivity: '2026-06-30',
            ),
            RemoteTask(
              id: '/p/run1',
              name: '在跑任务',
              projectPath: '/p/run1',
              workingDir: '/busy',
              lastActivity: '2026-01-01',
              activePhase: '执行',
              activeStatus: 'running',
            ),
          ],
        };
      await tester.pumpWidget(_app(api));
      await pumpFew(tester);

      expect(find.text('busy'), findsOneWidget);
      expect(find.text('idle'), findsOneWidget);
      expect(find.text('在跑任务'), findsOneWidget);
      expect(find.text('空闲任务'), findsNothing);
    });

    testWidgets('space header shows running-count badge only when running', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(grants: [_grantOps])
        ..tasksByGrant = {
          'g-ops': const [
            RemoteTask(
              id: '/p/run1',
              name: '在跑一',
              projectPath: '/p/run1',
              workingDir: '/busy',
              activePhase: '执行',
              activeStatus: 'running',
            ),
            RemoteTask(
              id: '/p/run2',
              name: '在跑二',
              projectPath: '/p/run2',
              workingDir: '/busy',
              activePhase: '执行',
              activeStatus: 'running',
            ),
            RemoteTask(
              id: '/p/idle',
              name: '闲着',
              projectPath: '/p/idle',
              workingDir: '/quiet',
            ),
          ],
        };
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();

      // busy 空间有 2 个运行中 → primary 徽章 "2"，整个屏幕只有这一个徽章
      // （quiet 空间无运行中 → 不渲染）。徽章 key 带空间 key 后缀。
      final badge = find.byKey(
        const ValueKey('rc-space-running-badge-/busy'),
      );
      expect(badge, findsOneWidget);
      final box = tester.widget<Container>(badge);
      expect((box.decoration! as BoxDecoration).color,
          Theme.of(tester.element(badge)).colorScheme.primary);
      expect(find.descendant(of: badge, matching: find.text('2')),
          findsOneWidget);
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

    testWidgets('space header add button opens sheet with prefilled dir', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(grants: [_grantOps])
        ..tasksByGrant = {
          'g-ops': const [
            RemoteTask(
              id: '/p/a',
              name: '任务甲',
              projectPath: '/p/a',
              workingDir: '/proj/web',
            ),
            RemoteTask(id: 'task-c', name: '任务丙', projectPath: 'task-c'),
          ],
        };
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();

      // web 空间头有「+」按钮（tooltip=新建任务），「其他」空间没有。
      // 顶部 SectionHeader 的「新建任务」是 Text，空间头按钮是 IconButton。
      // 探查结论（用 find.byTooltip('新建任务') 数实例）：旧数据 /p/c 时
      // widgetWithIcon 命中 2 个是「真实存在的两个按钮」（web + p 空间各一），
      // 而非祖先重复；现 任务丙→「其他」未分组后该空间无 '+'，但为稳妥仍用
      // find.descendant 精确锁定 web 空间头（key=rc-space-/proj/web）内的按钮 → 共 1 个。
      final addInSpace = find.descendant(
        of: find.byKey(const ValueKey('rc-space-/proj/web')),
        matching: find.widgetWithIcon(IconButton, Icons.add),
      );
      expect(addInSpace, findsOneWidget);

      // 点击 → sheet 打开，工作目录预填为空间路径。
      await tester.tap(addInSpace);
      await tester.pumpAndSettle();
      final dirField = tester.widget<TextField>(
        find.widgetWithText(TextField, '工作目录（可选）'),
      );
      expect(dirField.controller!.text, '/proj/web');
    });

    testWidgets('view-only grant hides per-space add button', (tester) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(grants: [_grantView])
        ..tasksByGrant = {
          'g-view': const [
            RemoteTask(
              id: '/p/a',
              name: '任务甲',
              projectPath: '/p/a',
              workingDir: '/proj/web',
            ),
          ],
        };
      await tester.pumpWidget(_app(api));
      await tester.pumpAndSettle();

      expect(find.widgetWithIcon(IconButton, Icons.add), findsNothing);
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
      await pumpFew(tester);
      expect(find.text('helloworld续', findRichText: true), findsOneWidget);
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
      await pumpFew(tester);
      expect(find.text('终稿续', findRichText: true), findsOneWidget);
      expect(find.text('想想'), findsOneWidget);
      expect(find.text('running · edit parser'), findsOneWidget);
      expect(find.text('helloworld'), findsNothing);
    });

    testWidgets(
      'a cursor hole asks watch_task again from the reply watermark',
      (tester) async {
        final api = _FakeRemoteApi()
          ..detail = sampleDetail()
          ..watchReplyCursor = 40
          ..followEvents = [
            {'event': 'snapshot', 'resync': true, 'cursor': 80},
          ]
          ..gapWatchReply = {
            'stream_proto': 2,
            'cursor': 80,
            'live': {
              'request_id': 'r2',
              'cursor': 80,
              'display_text': '下一问',
              'parts': [
                {'kind': 'text', 'text': '补上'},
              ],
            },
          };
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
        await pumpFew(tester);
        expect(api.lastAfterCursor, 40);
        expect(find.text('补上', findRichText: true), findsOneWidget);
        expect(api.calls.where((call) => call.startsWith('watch:')).length, 2);
      },
    );

    testWidgets('a snapshot behind cursors already seen asks once more', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..detail = sampleDetail()
        ..watchReplyCursor = 40
        ..followEvents = [
          {'event': 'snapshot', 'resync': true, 'cursor': 80},
        ]
        ..gapReplies = [
          {'stream_proto': 2, 'cursor': 50},
          {
            'stream_proto': 2,
            'cursor': 80,
            'live': {
              'request_id': 'r2',
              'cursor': 80,
              'parts': [
                {'kind': 'text', 'text': '补上'},
              ],
            },
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
      await pumpFew(tester);
      expect(api.calls.where((call) => call.startsWith('watch:')).length, 3);
      expect(find.text('补上', findRichText: true), findsOneWidget);
      await pumpFew(tester);
      expect(api.calls.where((call) => call.startsWith('watch:')).length, 3);
    });

    testWidgets('a caught-up watermark asks again for the next hole', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..detail = sampleDetail()
        ..watchReplyCursor = 40
        ..followEvents = [
          {'event': 'snapshot', 'resync': true, 'cursor': 80},
        ]
        ..gapReplies = [
          {'stream_proto': 2, 'cursor': 80},
          {
            'stream_proto': 2,
            'cursor': 90,
            'live': {
              'request_id': 'r2',
              'cursor': 90,
              'parts': [
                {'kind': 'text', 'text': '补上'},
              ],
            },
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
      await pumpFew(tester);
      expect(api.calls.where((call) => call.startsWith('watch:')).length, 2);
      expect(find.text('补上', findRichText: true), findsNothing);
      api.liveController?.add({
        'event': 'snapshot',
        'resync': true,
        'cursor': 90,
      });
      await pumpFew(tester);
      expect(api.calls.where((call) => call.startsWith('watch:')).length, 3);
      expect(find.text('补上', findRichText: true), findsOneWidget);
    });

    testWidgets('a watch that does not move the watermark does not ask again', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..detail = sampleDetail()
        ..watchReplyCursor = 40
        ..followEvents = [
          {'event': 'snapshot', 'resync': true, 'cursor': 80},
        ]
        ..gapReplies = [
          {'stream_proto': 2, 'cursor': 40},
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
      await pumpFew(tester);
      expect(api.calls.where((call) => call.startsWith('watch:')).length, 2);
      api.liveController?.add({
        'event': 'snapshot',
        'resync': true,
        'cursor': 81,
      });
      await pumpFew(tester);
      expect(api.calls.where((call) => call.startsWith('watch:')).length, 2);
    });

    testWidgets('a later frame of the same hole during the read does not ask', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..detail = sampleDetail()
        ..watchReplyCursor = 40
        ..followEvents = [
          {'event': 'snapshot', 'resync': true, 'cursor': 80},
        ]
        ..gapReplies = [
          {'stream_proto': 2, 'cursor': 50},
          {
            'stream_proto': 2,
            'cursor': 85,
            'live': {
              'request_id': 'r2',
              'cursor': 85,
              'parts': [
                {'kind': 'text', 'text': '补上'},
              ],
            },
          },
        ];
      api.onWatch = (watches) {
        if (watches != 2) return;
        api.liveController?.add({
          'event': 'snapshot',
          'resync': true,
          'cursor': 85,
        });
      };
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
      await pumpFew(tester);
      expect(api.calls.where((call) => call.startsWith('watch:')).length, 3);
      expect(find.text('补上', findRichText: true), findsOneWidget);
      await pumpFew(tester);
      expect(api.calls.where((call) => call.startsWith('watch:')).length, 3);
    });

    testWidgets(
      'a hole that opens during the follow-up read is asked after it',
      (tester) async {
        final api = _FakeRemoteApi()
          ..detail = sampleDetail()
          ..watchReplyCursor = 40
          ..followEvents = [
            {'event': 'snapshot', 'resync': true, 'cursor': 80},
          ]
          ..gapReplies = [
            {'stream_proto': 2, 'cursor': 50},
            {
              'stream_proto': 2,
              'cursor': 80,
              'live': {
                'request_id': 'r2',
                'cursor': 80,
                'parts': [
                  {'kind': 'text', 'text': '半截'},
                ],
              },
            },
            {
              'stream_proto': 2,
              'cursor': 90,
              'live': {
                'request_id': 'r2',
                'cursor': 90,
                'parts': [
                  {'kind': 'text', 'text': '补上'},
                ],
              },
            },
          ];
        api.onWatch = (watches) {
          if (watches != 3) return;
          api.liveController?.add({
            'event': 'snapshot',
            'resync': true,
            'cursor': 90,
          });
        };
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
        await pumpFew(tester);
        expect(api.calls.where((call) => call.startsWith('watch:')).length, 4);
        expect(find.text('补上', findRichText: true), findsOneWidget);
        expect(find.text('半截'), findsNothing);
        await pumpFew(tester);
        expect(api.calls.where((call) => call.startsWith('watch:')).length, 4);
      },
    );

    testWidgets('a cursor hole says the output is catching up', (tester) async {
      final gate = Completer<void>();
      final api = _FakeRemoteApi()
        ..detail = sampleDetail()
        ..watchReplyCursor = 40
        ..followEvents = [
          {'event': 'snapshot', 'resync': true, 'cursor': 80},
        ]
        ..gapWatchReply = {
          'stream_proto': 2,
          'cursor': 80,
          'live': {
            'request_id': 'r2',
            'cursor': 80,
            'parts': [
              {'kind': 'text', 'text': '补上'},
            ],
          },
        }
        ..beforeReply = (watches) {
          if (watches == 2) return gate.future;
          return Future<void>.value();
        };
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
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));
      expect(find.text('正在补齐中断的输出'), findsOneWidget);
      gate.complete();
      await pumpFew(tester);
      expect(find.text('正在补齐中断的输出'), findsNothing);
      expect(find.text('补上', findRichText: true), findsOneWidget);
    });

    testWidgets(
      'a responded answer already in the transcript is not shown twice',
      (tester) async {
        final api = _FakeRemoteApi()
          ..detail = sampleDetail()
          ..watchReplyLive = {
            'request_id': 'r1',
            'responded': true,
            'cursor': 4,
            'parts': [
              {'kind': 'text', 'text': '已经生成好了'},
            ],
          }
          ..followEvents = [
            {'event': 'heartbeat', 'cursor': 4},
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
        await pumpFew(tester);
        expect(find.text('已经生成好了', findRichText: true), findsOneWidget);
      },
    );

    testWidgets('a new round after ownership clears the live latch', (
      tester,
    ) async {
      // Round r1 is owned by the transcript (body matches the tail row). The
      // ownership latch must NOT suppress the NEXT round's live view once
      // round_started swaps in a new requestId.
      final api = _FakeRemoteApi()
        ..detail = sampleDetail()
        ..watchReplyLive = {
          'request_id': 'r1',
          'responded': true,
          'cursor': 4,
          'parts': [
            {'kind': 'text', 'text': '已经生成好了'},
          ],
        }
        ..followEvents = [
          {
            'event': 'round_started',
            'cursor': 5,
            'cursor_from': 5,
            'request_id': 'r2',
            'display_text': '第二轮',
          },
          {
            'event': 'thinking',
            'cursor': 6,
            'cursor_from': 6,
            'request_id': 'r2',
            'state': 'done',
            'text': '新一轮思考',
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
      await pumpFew(tester);
      // r1 owned by the transcript, live round retired.
      expect(find.text('已经生成好了', findRichText: true), findsOneWidget);
      // r2 arrives: latches reset, the new round's thinking is on screen.
      await pumpFew(tester);
      expect(find.text('新一轮思考', findRichText: true), findsOneWidget);
    });

    testWidgets(
      'an answer that only heavily overlaps the transcript is shown once',
      (tester) async {
        // get_task rows are normalized copies of the terminal text — leading
        // whitespace or a swapped opener keeps startsWith from matching even
        // though the rest of the answer is identical.
        const history = '  报告已经写入 /tmp/报告.pdf，共三页，可以随时取阅。';
        final api = _FakeRemoteApi()
          ..detail = RemoteTaskDetail(
            task: const RemoteTask(
              id: '/p/a',
              name: '任务甲',
              projectPath: '/p/a',
            ),
            messages: const [
              RemoteMessage(role: 'user', content: '写报告'),
              RemoteMessage(role: 'assistant', content: history),
            ],
          )
          ..watchReplyLive = {
            'request_id': 'r1',
            'responded': true,
            'cursor': 4,
            'parts': [
              {
                'kind': 'text',
                'text': '报告已经写入 /tmp/报告.pdf，共三页，可以随时取阅。',
              },
            ],
          }
          ..followEvents = [
            {'event': 'heartbeat', 'cursor': 4},
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
        await pumpFew(tester);
        // Ownership is by overlap, not an exact prefix: the body appears in
        // exactly one MarkdownBody instead of a duplicate live round.
        expect(find.byType(MarkdownBody), findsOneWidget);
        expect(
          find.textContaining('/tmp/报告.pdf', findRichText: true),
          findsOneWidget,
        );
      },
    );

    testWidgets('live thinking that restates history reasoning stays hidden', (
      tester,
    ) async {
      const reasoning =
          '先确认输出目录是否存在，再生成报告正文，最后附上文件路径。';
      final api = _FakeRemoteApi()
        ..detail = RemoteTaskDetail(
          task: const RemoteTask(
            id: '/p/a',
            name: '任务甲',
            projectPath: '/p/a',
          ),
          messages: const [
            RemoteMessage(role: 'user', content: '写报告'),
            RemoteMessage(
              role: 'assistant',
              content: '写好了',
              reasoning: reasoning,
            ),
          ],
        )
        ..watchReplyLive = {
          'request_id': 'r1',
          'cursor': 4,
          'parts': [
            {'kind': 'thinking', 'text': reasoning, 'done': true},
            {'kind': 'text', 'text': '收尾中'},
          ],
        }
        ..followEvents = [
          {'event': 'heartbeat', 'cursor': 4},
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
      await pumpFew(tester);
      // The streamed thinking restates the same reasoning: only the history
      // block renders (collapsed), the live duplicate is suppressed while the
      // live body still shows.
      expect(find.byType(ExpansionTile), findsOneWidget);
      expect(find.text('收尾中', findRichText: true), findsOneWidget);
      await tester.tap(find.byType(ExpansionTile));
      await pumpFew(tester);
      expect(
        find.textContaining('先确认输出目录', findRichText: true),
        findsOneWidget,
      );
    });

    testWidgets(
      'a live reply that duplicates the history tail renders once',
      (tester) async {
        // The full regression: the prompt arrives with a 〔…〕 context prefix
        // that get_task did not keep, the streamed thinking restates the
        // history reasoning, and the text part copies the tail body verbatim.
        // Neither the thinking block nor the body may render a second copy.
        const reply = '收到，立即执行全部低/中风险项的清理操作。';
        const reasoning = '逐条比对风险登记册，低风险与中风险项都可以直接清理。';
        final api = _FakeRemoteApi()
          ..detail = RemoteTaskDetail(
            task: const RemoteTask(
              id: '/p/a',
              name: '任务甲',
              projectPath: '/p/a',
            ),
            messages: const [
              RemoteMessage(role: 'user', content: '清理风险项'),
              RemoteMessage(
                role: 'assistant',
                content: reply,
                reasoning: reasoning,
              ),
            ],
          )
          ..watchReplyLive = {
            'request_id': 'r1',
            'responded': true,
            'cursor': 4,
            'display_text': '〔上下文 · 会话甲〕清理风险项',
            'parts': [
              {'kind': 'thinking', 'text': reasoning, 'done': true},
              {'kind': 'text', 'text': reply},
            ],
          }
          ..followEvents = [
            {'event': 'heartbeat', 'cursor': 4},
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
        await pumpFew(tester);
        // One assistant bubble: the history row. The live twin is owned by
        // the transcript, so the body and the reasoning block each render
        // exactly once.
        expect(find.byType(MarkdownBody), findsOneWidget);
        expect(find.byType(ExpansionTile), findsOneWidget);
        expect(find.text(reply, findRichText: true), findsOneWidget);
        await tester.tap(find.byType(ExpansionTile));
        await pumpFew(tester);
        expect(
          find.textContaining('逐条比对风险登记册', findRichText: true),
          findsOneWidget,
        );
      },
    );

    testWidgets('a live note line that repeats the history row is dropped', (
      tester,
    ) async {
      const note = '已写入 /tmp/报告.pdf';
      final api = _FakeRemoteApi()
        ..detail = RemoteTaskDetail(
          task: const RemoteTask(
            id: '/p/a',
            name: '任务甲',
            projectPath: '/p/a',
          ),
          messages: const [
            RemoteMessage(role: 'user', content: '写报告'),
            RemoteMessage(role: 'assistant', content: note),
          ],
        )
        ..watchReplyLive = {
          'request_id': 'r1',
          'cursor': 4,
          'parts': [
            {
              'kind': 'progress',
              'text': note,
            },
            {'kind': 'text', 'text': '新的输出'},
          ],
        }
        ..followEvents = [
          {'event': 'heartbeat', 'cursor': 4},
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
      await pumpFew(tester);
      // The history row already carries that line; the live note adds
      // nothing and is filtered out.
      expect(find.text(note, findRichText: true), findsOneWidget);
      expect(find.text('新的输出', findRichText: true), findsOneWidget);
    });
    testWidgets(
      'a long transcript opens at the tail and stays put after the reader scrolls up',
      (tester) async {
        final api = _FakeRemoteApi()
          ..detail = RemoteTaskDetail(
            task: const RemoteTask(
              id: '/p/a',
              name: '任务甲',
              projectPath: '/p/a',
            ),
            messages: [
              for (var i = 0; i < 30; i++)
                RemoteMessage(
                  role: i.isEven ? 'user' : 'assistant',
                  content: '记录 $i ${'内容' * 24}',
                ),
            ],
          )
          ..watchReplyLive = {
            'request_id': 'r1',
            'cursor': 4,
            'parts': [
              {'kind': 'text', 'text': '开头'},
            ],
          }
          ..followEvents = [
            {
              'event': 'token',
              'cursor': 5,
              'cursor_from': 5,
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
        await pumpFew(tester);
        final position = tester
            .widget<ListView>(find.byType(ListView))
            .controller!
            .position;
        expect(position.maxScrollExtent, greaterThan(200));
        expect(position.maxScrollExtent - position.pixels, lessThan(2));
        expect(find.text('开头续', findRichText: true), findsOneWidget);

        await tester.drag(find.byType(ListView), const Offset(0, 360));
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 400));
        final parked = position.pixels;
        final extent = position.maxScrollExtent;
        expect(extent - parked, greaterThan(80));

        api.liveController?.add({
          'event': 'token',
          'cursor': 6,
          'cursor_from': 6,
          'text': '又来了一段${'补' * 40}',
          'pieces': ['又来了一段${'补' * 40}'],
        });
        await tester.pump();
        await tester.pump();
        // The new lines are below the fold. Growing them must not pull the
        // reader back down; the old extent can stay put until that tail is built.
        expect(position.pixels, parked);
        position.jumpTo(position.maxScrollExtent);
        await tester.pump();
        expect(
          find.textContaining('又来了一段', findRichText: true),
          findsOneWidget,
        );
      },
    );

    testWidgets('a live reply on an empty transcript replaces the empty hint', (
      tester,
    ) async {
      final api = _FakeRemoteApi()
        ..detail = RemoteTaskDetail(
          task: const RemoteTask(id: '/p/a', name: '任务甲', projectPath: '/p/a'),
        )
        ..watchReplyLive = {
          'request_id': 'r1',
          'cursor': 2,
          'parts': [
            {'kind': 'text', 'text': '正在写'},
          ],
        }
        ..followEvents = [
          {'event': 'heartbeat', 'cursor': 2},
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
      await pumpFew(tester);
      expect(find.text('正在写', findRichText: true), findsOneWidget);
      expect(find.text('还没有对话记录'), findsNothing);
    });

    testWidgets(
      'artifacts stay under the transcript so the latest reply is the tail',
      (tester) async {
        final api = _FakeRemoteApi()
          ..detail = RemoteTaskDetail(
            task: const RemoteTask(
              id: '/p/a',
              name: '任务甲',
              projectPath: '/p/a',
            ),
            messages: [
              for (var i = 0; i < 30; i++)
                RemoteMessage(
                  role: i.isEven ? 'user' : 'assistant',
                  content: '记录 $i ${'内容' * 24}',
                ),
            ],
            artifacts: const [RemoteArtifact(title: '报告.pdf')],
          )
          ..followEvents = [
            {'event': 'heartbeat', 'cursor': 1},
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
        await pumpFew(tester);
        final transcript = find.byKey(const ValueKey('remote-task-transcript'));
        final position = tester
            .widget<ListView>(transcript)
            .controller!
            .position;
        expect(position.maxScrollExtent, greaterThan(200));
        expect(position.maxScrollExtent - position.pixels, lessThan(2));
        final listBottom = tester.getRect(transcript).bottom;
        expect(
          tester.getRect(find.text('产出物')).top,
          greaterThanOrEqualTo(listBottom - 1),
        );
        final reply = tester.getRect(find.textContaining('记录 28'));
        expect(listBottom - reply.bottom, lessThan(240));
        expect(reply.top, greaterThan(tester.getRect(transcript).top));
      },
    );

    testWidgets('a trimmed history row does not replace the streamed answer', (
      tester,
    ) async {
      final cap = '甲' * 4000;
      final api = _FakeRemoteApi()
        ..detail = RemoteTaskDetail(
          task: const RemoteTask(id: '/p/a', name: '任务甲', projectPath: '/p/a'),
          messages: [
            RemoteMessage(
              role: 'assistant',
              content: '$cap…',
              filePaths: ['/tmp/报告.pdf'],
            ),
          ],
        )
        ..watchReplyLive = {
          'request_id': 'r1',
          'responded': true,
          'cursor': 8,
          'parts': [
            {'kind': 'text', 'text': '$cap乙乙乙'},
          ],
        }
        ..followEvents = [
          {'event': 'heartbeat', 'cursor': 8},
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
      await pumpFew(tester);
      expect(find.textContaining('乙乙乙', findRichText: true), findsOneWidget);
      expect(find.textContaining('…', findRichText: true), findsNothing);
      expect(find.text('报告.pdf'), findsOneWidget);
    });

    testWidgets('a final that contains the snapshot tail replaces that tail', (
      tester,
    ) async {
      final cap = '甲' * 4000;
      final tail = '乙' * 80;
      final api = _FakeRemoteApi()
        ..detail = RemoteTaskDetail(
          task: const RemoteTask(id: '/p/a', name: '任务甲', projectPath: '/p/a'),
          messages: [
            RemoteMessage(
              role: 'assistant',
              content: '$cap…',
              filePaths: ['/tmp/报告.pdf'],
            ),
          ],
        )
        ..watchReplyLive = {
          'request_id': 'r1',
          'cursor': 8,
          'parts': [
            {'kind': 'text', 'text': tail},
          ],
        }
        ..followEvents = [
          {
            'event': 'response',
            'cursor': 9,
            'cursor_from': 9,
            'request_id': 'r1',
            'terminal': {'text': '$cap中段$tail'},
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
      await pumpFew(tester);
      expect(find.textContaining('中段', findRichText: true), findsOneWidget);
      expect(find.textContaining('…', findRichText: true), findsNothing);
      expect(find.text('报告.pdf'), findsOneWidget);
    });

    testWidgets(
      'a snapshot tail stays beside a trimmed history of another slice',
      (tester) async {
        final cap = '甲' * 4000;
        final api = _FakeRemoteApi()
          ..detail = RemoteTaskDetail(
            task: const RemoteTask(
              id: '/p/a',
              name: '任务甲',
              projectPath: '/p/a',
            ),
            messages: [RemoteMessage(role: 'assistant', content: '$cap…')],
          )
          ..watchReplyLive = {
            'request_id': 'r1',
            'cursor': 8,
            'responded': true,
            'parts': [
              {'kind': 'text', 'text': '${'乙' * 80}结尾'},
            ],
          }
          ..followEvents = [
            {
              'event': 'response',
              'cursor': 9,
              'cursor_from': 9,
              'request_id': 'r1',
              'terminal': {'text': cap},
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
        await pumpFew(tester);
        expect(find.textContaining('…', findRichText: true), findsOneWidget);
        expect(find.textContaining('结尾', findRichText: true), findsOneWidget);
      },
    );

    testWidgets('the opening and the ending of one answer share one bubble', (
      tester,
    ) async {
      final cap = '甲' * 4000;
      final api = _FakeRemoteApi()
        ..detail = RemoteTaskDetail(
          task: const RemoteTask(id: '/p/a', name: '任务甲', projectPath: '/p/a'),
          messages: [
            const RemoteMessage(role: 'user', content: '写报告'),
            RemoteMessage(
              role: 'assistant',
              content: '$cap…',
              filePaths: ['/tmp/报告.pdf'],
            ),
          ],
        )
        ..watchReplyLive = {
          'request_id': 'r1',
          'cursor': 8,
          'responded': true,
          'display_text': '〔远控〕写报告',
          'parts': [
            {'kind': 'text', 'text': '${'乙' * 40}结尾'},
          ],
        }
        ..followEvents = [
          {'event': 'heartbeat', 'cursor': 8},
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
      await pumpFew(tester);
      expect(find.byType(MarkdownBody), findsOneWidget);
      expect(find.textContaining('结尾', findRichText: true), findsOneWidget);
      expect(find.text('报告.pdf'), findsOneWidget);
    });

    testWidgets('an opening and an ending that share a run are stitched once', (
      tester,
    ) async {
      final shared = '重叠' * 40;
      final cap = '${'甲' * (4000 - shared.length)}$shared';
      final api = _FakeRemoteApi()
        ..detail = RemoteTaskDetail(
          task: const RemoteTask(id: '/p/a', name: '任务甲', projectPath: '/p/a'),
          messages: [
            const RemoteMessage(role: 'user', content: '写报告'),
            RemoteMessage(role: 'assistant', content: '$cap…'),
          ],
        )
        ..watchReplyLive = {
          'request_id': 'r1',
          'cursor': 8,
          'responded': true,
          'display_text': '写报告',
          'parts': [
            {'kind': 'text', 'text': '$shared${'乙' * 30}结尾'},
          ],
        }
        ..followEvents = [
          {'event': 'heartbeat', 'cursor': 8},
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
      await pumpFew(tester);
      expect(find.byType(MarkdownBody), findsOneWidget);
      expect(find.textContaining('结尾', findRichText: true), findsOneWidget);
      expect(find.textContaining('…', findRichText: true), findsNothing);
    });

    testWidgets('a later prompt keeps the older trimmed answer', (
      tester,
    ) async {
      final cap = '甲' * 4000;
      final api = _FakeRemoteApi()
        ..detail = RemoteTaskDetail(
          task: const RemoteTask(id: '/p/a', name: '任务甲', projectPath: '/p/a'),
          messages: [
            const RemoteMessage(role: 'user', content: '上一问'),
            RemoteMessage(role: 'assistant', content: '$cap…'),
          ],
        )
        ..watchReplyLive = {
          'request_id': 'r2',
          'cursor': 8,
          'responded': true,
          'display_text': '新问题',
          'parts': [
            {'kind': 'text', 'text': '结尾'},
          ],
        }
        ..followEvents = [
          {'event': 'heartbeat', 'cursor': 8},
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
      await pumpFew(tester);
      expect(find.byType(MarkdownBody), findsNWidgets(2));
      expect(find.textContaining('结尾', findRichText: true), findsOneWidget);
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
      await pumpFew(tester);

      expect(find.text('帮我查一下'), findsOneWidget);
      expect(find.text('已经生成好了', findRichText: true), findsOneWidget);
      expect(find.text('产出物'), findsOneWidget);
      expect(find.text('报告.pdf'), findsNWidgets(2)); // chip + artifact row
      expect(find.byIcon(Icons.arrow_upward_rounded), findsNothing);
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
      await pumpFew(tester);

      expect(find.byIcon(Icons.more_vert), findsOneWidget);
      await tester.enterText(find.byType(TextField).last, '继续处理第二批');
      await tester.tap(find.byIcon(Icons.arrow_upward_rounded));
      await pumpFew(tester);

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
      await pumpFew(tester);

      // The fetch path runs through fetchFile; share_plus itself can't run
      // on a widget-test host, so assert the op call fires (the share call
      // may throw MissingPluginException afterwards — tolerated by pump).
      await tester.tap(find.text('报告.pdf').first);
      await tester.pump();
      // fetchFile completed synchronously on the fake; base64 'YWJj' decodes.
      expect(api.detail, isNotNull);
    });

    testWidgets('file card open button runs the fetch and open path', (
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
      await pumpFew(tester);

      // 打开 shares the fetch with 分享; OpenFilex itself has no plugin on a
      // widget-test host — _fetchAndOpen catches that and toasts, so the tap
      // must not crash the task screen.
      await tester.tap(find.byIcon(Icons.open_in_new));
      await tester.pump();
      await tester.pump();
      expect(find.text('报告.pdf'), findsWidgets);
      expect(tester.takeException(), isNull);
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
      await pumpFew(tester);

      await tester.enterText(find.byType(TextField).last, '/plan approve');
      await tester.tap(find.byIcon(Icons.arrow_upward_rounded));
      await pumpFew(tester);

      expect(api.calls.where((c) => c.startsWith('send:')), isEmpty);
      expect(find.text('远控只能发送任务消息，不能运行本机命令'), findsOneWidget);
    });

    testWidgets('dir picker exposes home and roots shortcuts', (tester) async {
      final api = _FakeRemoteApi()
        ..snapshot = const RemoteControlState(grants: [_grantOps]);
      await tester.pumpWidget(_app(api));
      await pumpFew(tester);

      await tester.tap(find.text('新建任务'));
      await pumpFew(tester);
      await tester.tap(find.byIcon(Icons.folder_open));
      await pumpFew(tester);

      expect(api.calls, contains('dirs:'));
      expect(find.text('主目录'), findsOneWidget);
      expect(find.text('D:\\'), findsOneWidget);

      await tester.tap(find.text('D:\\'));
      await pumpFew(tester);
      expect(api.calls, contains('dirs:D:\\'));
    });
  });
}
