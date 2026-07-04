import 'package:drift/native.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/network/mobile_network_status.dart';
import 'package:maclaw_mobile/core/notifications/mobile_notification_service.dart';
import 'package:maclaw_mobile/core/security/mobile_redaction.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employees_controller.dart';
import 'package:maclaw_mobile/features/servers/server_command.dart';
import 'package:maclaw_mobile/features/servers/server_profile.dart';
import 'package:maclaw_mobile/features/servers/servers_controller.dart';
import 'package:maclaw_mobile/features/servers/servers_screen.dart';

class _TestServerProfilesController extends ServerProfilesController {
  @override
  Future<List<ServerProfile>> build() async => const [];
}

class _OneServerProfileController extends ServerProfilesController {
  @override
  Future<List<ServerProfile>> build() async => const [
        ServerProfile(
          id: 'srv-prod',
          name: 'prod-api',
          host: '10.0.0.8',
          port: 2222,
          username: 'ops',
          authMode: serverAuthModePassword,
          tag: 'prod',
          note: 'primary API node',
        ),
      ];
}

class _RecordingServerProfilesController extends ServerProfilesController {
  static final added = <({
    ServerProfile profile,
    String password,
    String privateKey,
    String privateKeyPassphrase,
  })>[];

  @override
  Future<List<ServerProfile>> build() async => const [];

  @override
  Future<void> addProfile(
    ServerProfile profile, {
    String password = '',
    String privateKey = '',
    String privateKeyPassphrase = '',
  }) async {
    added.add(
      (
        profile: profile,
        password: password,
        privateKey: privateKey,
        privateKeyPassphrase: privateKeyPassphrase,
      ),
    );
    state = AsyncData([profile]);
  }
}

class _TestServerCommandsController extends ServerCommandsController {
  @override
  Future<List<ServerCommandEntry>> build() async => const [];
}

class _RecordingServerCommandsController extends ServerCommandsController {
  static final recorded = <({String command, bool favorite})>[];

  @override
  Future<List<ServerCommandEntry>> build() async => const [];

  @override
  Future<void> record(String command, {bool favorite = false}) async {
    recorded.add((command: command, favorite: favorite));
    state = AsyncData([
      ServerCommandEntry(
        id: recorded.length.toString(),
        command: command,
        label: command,
        favorite: favorite,
        createdAt: DateTime.utc(2026, 7, 2),
        lastUsedAt: DateTime.utc(2026, 7, 2),
      ),
    ]);
  }
}

class _TestSSHAnalysisController extends SSHAnalysisController {
  @override
  Future<MobileSSHAnalysis?> build() async => null;
}

class _TestSSHAnalysisWithDraftController extends SSHAnalysisController {
  @override
  Future<MobileSSHAnalysis?> build() async => const MobileSSHAnalysis(
        summary: 'nginx 配置需要检查',
        recommendation: '先验证配置，再考虑重载服务。',
        commandDraft: 'nginx -t',
      );
}

class _RecordingSSHAnalysisController extends SSHAnalysisController {
  static final outputs = <String>[];

  @override
  Future<MobileSSHAnalysis?> build() async => null;

  @override
  Future<void> analyze(String output) async {
    outputs.add(output.trim());
    state = const AsyncData(
      MobileSSHAnalysis(
        summary: '发现 502 错误',
        recommendation: '先检查 upstream 服务状态。',
        commandDraft: 'systemctl status app --no-pager',
      ),
    );
  }
}

class _RecordingNotificationService extends MobileNotificationService {
  final shown = <({String title, String body, String? payload})>[];

  @override
  Future<void> showTaskCompleted({
    required String title,
    required String body,
    String? payload,
  }) async {
    shown.add((title: title, body: body, payload: payload));
  }
}

class _ServerDigitalEmployeesController extends DigitalEmployeesController {
  @override
  Future<List<DigitalEmployee>> build() async => const [
        DigitalEmployee(
          id: 'employee-server',
          machineId: 'prod-runner',
          name: '服务器数字员工',
          skillDescription: '远程服务器日志分析和维护建议',
          onlineStatus: 'online',
          accessPolicy: 'per_request',
          resident: true,
          runtimeMissing: false,
        ),
      ];
}

class _RecordingDigitalEmployeeTaskController
    extends DigitalEmployeeTaskController {
  static final created = <({
    String employeeId,
    String prompt,
    String taskType,
    Map<String, String> context,
  })>[];

  @override
  Future<MobileDigitalEmployeeTask?> build() async => null;

  @override
  Future<void> createTask({
    required String employeeId,
    required String prompt,
    String taskType = 'general',
    Map<String, String> context = const {},
  }) async {
    created.add(
      (
        employeeId: employeeId,
        prompt: prompt,
        taskType: taskType,
        context: context,
      ),
    );
    state = AsyncData(
      MobileDigitalEmployeeTask(
        taskId: 'task-${created.length}',
        employeeId: employeeId,
        prompt: prompt,
        status: 'queued',
        result: '',
        message: '',
        claimedBy: '',
      ),
    );
  }
}

void main() {
  test('mobile SSH output submission summary counts lines and truncates', () {
    final summary = mobileSSHOutputSubmissionSummary(
      '${'a' * 430}\nsecond line',
    );

    expect(summary.lineCount, 2);
    expect(summary.charCount, 442);
    expect(summary.preview, endsWith('...'));
    expect(summary.preview.length, 423);
  });

  test('mobile SSH output redaction removes common secrets', () {
    final redacted = redactMobileSensitiveText(
      'Authorization: Bearer secret-token\n'
      'password=super-secret token: abc123 api_key=key-1\n'
      'password="quoted password" API_KEY=\'quoted api key\'\n'
      'MYSQL_PWD=mysql-secret AWS_SECRET_ACCESS_KEY=aws-secret\n'
      'mysql --password cli-secret --token \'quoted cli token\' -e "select 1"\n'
      'https://admin:pass@example.com/logs\n'
      '-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n'
      '-----END OPENSSH PRIVATE KEY-----',
    );

    expect(redacted, contains('Authorization: Bearer [REDACTED_TOKEN]'));
    expect(redacted, contains('password=[REDACTED_SECRET]'));
    expect(redacted, contains('token=[REDACTED_SECRET]'));
    expect(redacted, contains('api_key=[REDACTED_SECRET]'));
    expect(redacted, contains('API_KEY=[REDACTED_SECRET]'));
    expect(redacted, contains('MYSQL_PWD=[REDACTED_SECRET]'));
    expect(redacted, contains('AWS_SECRET_ACCESS_KEY=[REDACTED_SECRET]'));
    expect(redacted, contains('--password [REDACTED_SECRET]'));
    expect(redacted, contains('--token [REDACTED_SECRET]'));
    expect(redacted, contains('https://[REDACTED_CREDENTIALS]@example.com'));
    expect(redacted, contains('[REDACTED_PRIVATE_KEY]'));
    expect(redacted, isNot(contains('secret-token')));
    expect(redacted, isNot(contains('super-secret')));
    expect(redacted, isNot(contains('quoted password')));
    expect(redacted, isNot(contains('quoted api key')));
    expect(redacted, isNot(contains('mysql-secret')));
    expect(redacted, isNot(contains('aws-secret')));
    expect(redacted, isNot(contains('cli-secret')));
    expect(redacted, isNot(contains('quoted cli token')));
    expect(redacted, isNot(contains('admin:pass')));
  });

  test('mobile SSH output handoff context marks server maintenance source', () {
    final context = mobileSSHOutputTaskContext(
      'first line\nsecond line',
      profile: const ServerProfile(
        id: 'srv-prod',
        name: 'prod-api',
        host: '10.0.0.8',
        port: 2222,
        username: 'ops',
        authMode: serverAuthModePrivateKey,
        tag: 'prod',
        note: 'primary API node',
      ),
    );

    expect(context, containsPair('source', 'maclaw_mobile'));
    expect(context, containsPair('handoff', 'ssh_output'));
    expect(context, containsPair('task_surface', 'servers'));
    expect(context, containsPair('line_count', '2'));
    expect(context, containsPair('char_count', '22'));
    expect(context, containsPair('server_profile_id', 'srv-prod'));
    expect(context, containsPair('server_name', 'prod-api'));
    expect(context, containsPair('server_host', '10.0.0.8'));
    expect(context, containsPair('server_port', '2222'));
    expect(context, containsPair('server_username', 'ops'));
    expect(context, containsPair('server_auth_mode', serverAuthModePrivateKey));
    expect(context, containsPair('server_tag', 'prod'));
    expect(context, containsPair('server_note', 'primary API node'));
  });

  test('mobile SSH output handoff context redacts profile metadata secrets',
      () {
    final context = mobileSSHOutputTaskContext(
      'service failed',
      profile: const ServerProfile(
        id: 'srv-prod',
        name: 'prod-api token: name-secret',
        host: 'https://root:host-pass@example.com',
        port: 2222,
        username: 'ops password=user-secret',
        authMode: serverAuthModePassword,
        tag: 'prod token: tag-secret',
        note: 'jump https://admin:pass@example.com password=note-secret',
      ),
    );

    expect(context['server_name'], 'prod-api token=[REDACTED_SECRET]');
    expect(
      context['server_host'],
      'https://[REDACTED_CREDENTIALS]@example.com',
    );
    expect(context['server_username'], 'ops password=[REDACTED_SECRET]');
    expect(context['server_tag'], 'prod token=[REDACTED_SECRET]');
    expect(
      context['server_note'],
      'jump https://[REDACTED_CREDENTIALS]@example.com '
      'password=[REDACTED_SECRET]',
    );
    expect(context['server_name'], isNot(contains('name-secret')));
    expect(context['server_host'], isNot(contains('root:host-pass')));
    expect(context['server_username'], isNot(contains('user-secret')));
    expect(context['server_tag'], isNot(contains('tag-secret')));
    expect(context['server_note'], isNot(contains('admin:pass')));
    expect(context['server_note'], isNot(contains('note-secret')));
  });

  testWidgets('servers screen exposes emergency maintenance controls',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          serverProfilesProvider.overrideWith(
            _TestServerProfilesController.new,
          ),
          serverCommandsProvider.overrideWith(
            _TestServerCommandsController.new,
          ),
          sshAnalysisProvider.overrideWith(_TestSSHAnalysisController.new),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pump();

    expect(find.text('应急服务器'), findsOneWidget);
    expect(find.text('服务器配置'), findsOneWidget);

    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('手动 SSH 终端'), findsOneWidget);
    expect(find.text('命令风险预检'), findsOneWidget);
    expect(find.text('保存常用'), findsOneWidget);
    expect(find.text('发送到终端'), findsOneWidget);

    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('AI 分析终端输出'), findsOneWidget);
  });

  testWidgets('servers screen adds profile with tag note and password',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingServerProfilesController.added.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          serverProfilesProvider.overrideWith(
            _RecordingServerProfilesController.new,
          ),
          serverCommandsProvider.overrideWith(
            _TestServerCommandsController.new,
          ),
          sshAnalysisProvider.overrideWith(_TestSSHAnalysisController.new),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pump();

    await tester.enterText(
      find.widgetWithText(TextField, '名称'),
      '生产入口',
    );
    await tester.enterText(find.widgetWithText(TextField, '标签'), '生产');
    await tester.enterText(
      find.widgetWithText(TextField, '备注'),
      '值班维护入口',
    );
    await tester.enterText(
      find.widgetWithText(TextField, 'Host'),
      '10.0.0.7',
    );
    await tester.enterText(find.widgetWithText(TextField, '端口'), '2222');
    await tester.enterText(find.widgetWithText(TextField, '用户名'), 'ops');
    await tester.enterText(
      find.widgetWithText(TextField, '密码'),
      'server-password',
    );

    await tester.tap(find.text('添加服务器'));
    await tester.pump();

    final added = _RecordingServerProfilesController.added.single;
    expect(added.password, 'server-password');
    expect(added.privateKey, isEmpty);
    expect(added.privateKeyPassphrase, isEmpty);
    expect(added.profile.name, '生产入口');
    expect(added.profile.host, '10.0.0.7');
    expect(added.profile.port, 2222);
    expect(added.profile.username, 'ops');
    expect(added.profile.tag, '生产');
    expect(added.profile.note, '值班维护入口');
    expect(added.profile.authMode, serverAuthModePassword);
    expect(find.text('生产入口'), findsWidgets);
    expect(find.textContaining('ops@10.0.0.7:2222'), findsOneWidget);
    expect(find.textContaining('值班维护入口'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 50));
  });

  testWidgets('servers screen rejects invalid port before saving profile',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingServerProfilesController.added.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          serverProfilesProvider.overrideWith(
            _RecordingServerProfilesController.new,
          ),
          serverCommandsProvider.overrideWith(
            _TestServerCommandsController.new,
          ),
          sshAnalysisProvider.overrideWith(_TestSSHAnalysisController.new),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pump();

    await tester.enterText(
      find.widgetWithText(TextField, 'Host'),
      '10.0.0.7',
    );
    await tester.enterText(find.widgetWithText(TextField, '端口'), '70000');
    await tester.enterText(find.widgetWithText(TextField, '用户名'), 'ops');
    await tester.tap(find.text('添加服务器'));
    await tester.pump();

    expect(_RecordingServerProfilesController.added, isEmpty);
    expect(find.text('请输入 1-65535 范围内的 SSH 端口。'), findsOneWidget);
  });

  testWidgets('servers screen requires private key in private-key mode',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingServerProfilesController.added.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          serverProfilesProvider.overrideWith(
            _RecordingServerProfilesController.new,
          ),
          serverCommandsProvider.overrideWith(
            _TestServerCommandsController.new,
          ),
          sshAnalysisProvider.overrideWith(_TestSSHAnalysisController.new),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pump();

    await tester.enterText(
      find.widgetWithText(TextField, 'Host'),
      '10.0.0.7',
    );
    await tester.enterText(find.widgetWithText(TextField, '端口'), '22');
    await tester.enterText(find.widgetWithText(TextField, '用户名'), 'ops');
    await tester.tap(find.text('私钥'));
    await tester.pump();
    await tester.scrollUntilVisible(
      find.text('添加服务器'),
      240,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('添加服务器'));
    await tester.pumpAndSettle();

    expect(_RecordingServerProfilesController.added, isEmpty);
    expect(find.text('私钥登录需要填写或导入私钥。'), findsOneWidget);
  });

  testWidgets('servers screen copies captured terminal output', (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final copied = <String>[];
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          mobileSshTerminalInitialOutputProvider.overrideWithValue(
            'nginx[1]: upstream timed out\nsystemd[1]: app.service failed',
          ),
          mobileClipboardWriterProvider.overrideWithValue((text) async {
            copied.add(text);
          }),
          serverProfilesProvider.overrideWith(
            _TestServerProfilesController.new,
          ),
          serverCommandsProvider.overrideWith(
            _TestServerCommandsController.new,
          ),
          sshAnalysisProvider.overrideWith(_TestSSHAnalysisController.new),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pump(const Duration(milliseconds: 300));

    final copyButton = find.byTooltip('复制终端输出');
    await tester.ensureVisible(copyButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(copyButton);
    await tester.pump();

    expect(
      copied.single,
      'nginx[1]: upstream timed out\nsystemd[1]: app.service failed',
    );
    expect(find.text('终端输出已复制'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 50));
  });

  testWidgets('servers screen notifies when SSH connection fails',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final notifications = _RecordingNotificationService();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          mobileNotificationServiceProvider.overrideWithValue(notifications),
          mobileSshSocketConnectorProvider.overrideWithValue(
            (host, port) async => throw StateError(
              'network unreachable for $host:$port',
            ),
          ),
          serverProfilesProvider.overrideWith(
            _OneServerProfileController.new,
          ),
          serverCommandsProvider.overrideWith(
            _TestServerCommandsController.new,
          ),
          sshAnalysisProvider.overrideWith(_TestSSHAnalysisController.new),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pump(const Duration(milliseconds: 300));

    final connectButton = find.text('连接 SSH');
    await tester.ensureVisible(connectButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(connectButton);
    await tester.pumpAndSettle();

    expect(notifications.shown, hasLength(1));
    expect(notifications.shown.single.title, contains('SSH'));
    expect(notifications.shown.single.body, contains('prod-api'));
    expect(notifications.shown.single.payload, 'server-profile:srv-prod');

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 50));
  });

  testWidgets('servers screen keeps AI command drafts manual and savable',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingServerCommandsController.recorded.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          serverProfilesProvider.overrideWith(
            _TestServerProfilesController.new,
          ),
          serverCommandsProvider.overrideWith(
            _RecordingServerCommandsController.new,
          ),
          sshAnalysisProvider.overrideWith(
            _TestSSHAnalysisWithDraftController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -1600));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('nginx -t'), findsOneWidget);
    expect(
      find.text('AI 只提供命令草案，不会自动执行；请先放入风险预检或复制后手动确认。'),
      findsOneWidget,
    );
    expect(find.text('放入风险预检'), findsOneWidget);
    expect(find.text('保存常用'), findsWidgets);

    final saveDraftButton = find.text('保存常用').last;
    await tester.ensureVisible(saveDraftButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(saveDraftButton);
    await tester.pump(const Duration(milliseconds: 300));

    expect(
      _RecordingServerCommandsController.recorded.single,
      (command: 'nginx -t', favorite: true),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 50));
  });

  testWidgets('servers screen confirms before saving high-risk commands',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingServerCommandsController.recorded.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          serverProfilesProvider.overrideWith(
            _TestServerProfilesController.new,
          ),
          serverCommandsProvider.overrideWith(
            _RecordingServerCommandsController.new,
          ),
          sshAnalysisProvider.overrideWith(_TestSSHAnalysisController.new),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -1100));
    await tester.pump(const Duration(milliseconds: 300));

    await tester.enterText(
      find.widgetWithText(TextField, '\u547d\u4ee4\u8349\u7a3f'),
      'rm -rf /var/lib/app',
    );
    await tester.pump();

    final saveButton = find.text('\u4fdd\u5b58\u5e38\u7528').first;
    await tester.ensureVisible(saveButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(saveButton);
    await tester.pumpAndSettle();

    expect(
      find.text(
        '\u786e\u8ba4\u4fdd\u5b58\u9ad8\u98ce\u9669\u547d\u4ee4\uff1f',
      ),
      findsOneWidget,
    );
    expect(find.textContaining('\u5220\u9664\u6570\u636e'), findsOneWidget);

    await tester.tap(find.text('\u53d6\u6d88'));
    await tester.pumpAndSettle();
    expect(_RecordingServerCommandsController.recorded, isEmpty);

    await tester.ensureVisible(saveButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(saveButton);
    await tester.pumpAndSettle();
    await tester.tap(find.text('\u786e\u8ba4\u4fdd\u5b58'));
    await tester.pumpAndSettle();

    expect(
      _RecordingServerCommandsController.recorded.single,
      (command: 'rm -rf /var/lib/app', favorite: true),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 50));
  });

  testWidgets('servers screen confirms before sending logs to AI',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingSSHAnalysisController.outputs.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          serverProfilesProvider.overrideWith(
            _TestServerProfilesController.new,
          ),
          serverCommandsProvider.overrideWith(
            _TestServerCommandsController.new,
          ),
          sshAnalysisProvider.overrideWith(
            _RecordingSSHAnalysisController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -1700));
    await tester.pump(const Duration(milliseconds: 300));

    await tester.enterText(
      find.widgetWithText(
        TextField,
        '\u7ec8\u7aef\u8f93\u51fa\u6216\u9519\u8bef\u65e5\u5fd7',
      ),
      'nginx[1]: upstream timed out\nAuthorization: Bearer secret-token',
    );
    final analyzeButton = find.text('\u5206\u6790\u8f93\u51fa');
    await tester.ensureVisible(analyzeButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(analyzeButton);
    await tester.pumpAndSettle();

    expect(
      find.text('\u53d1\u9001\u7ec8\u7aef\u8f93\u51fa\u7ed9 AI\uff1f'),
      findsOneWidget,
    );
    expect(
      find.textContaining('MaClaw \u5b98\u65b9\u670d\u52a1'),
      findsWidgets,
    );
    expect(
      find.textContaining('\u5bc6\u7801\u3001Token\u3001\u79c1\u94a5'),
      findsOneWidget,
    );

    await tester.tap(find.text('\u53d6\u6d88'));
    await tester.pumpAndSettle();
    expect(_RecordingSSHAnalysisController.outputs, isEmpty);

    await tester.ensureVisible(analyzeButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(analyzeButton);
    await tester.pumpAndSettle();
    await tester.tap(find.text('\u786e\u8ba4\u53d1\u9001'));
    await tester.pumpAndSettle();

    expect(
      _RecordingSSHAnalysisController.outputs.single,
      'nginx[1]: upstream timed out\nAuthorization: Bearer [REDACTED_TOKEN]',
    );
    expect(find.text('发现 502 错误'), findsOneWidget);
  });

  testWidgets('servers screen can hand pasted output to a digital employee',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    _RecordingDigitalEmployeeTaskController.created.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          serverProfilesProvider.overrideWith(
            _TestServerProfilesController.new,
          ),
          serverCommandsProvider.overrideWith(
            _TestServerCommandsController.new,
          ),
          sshAnalysisProvider.overrideWith(
            _TestSSHAnalysisController.new,
          ),
          digitalEmployeesProvider.overrideWith(
            _ServerDigitalEmployeesController.new,
          ),
          digitalEmployeeTaskProvider.overrideWith(
            _RecordingDigitalEmployeeTaskController.new,
          ),
          mobileNetworkStatusProvider.overrideWith(
            (ref) => Stream.value(
              MobileNetworkSnapshot(
                quality: MobileNetworkQuality.online,
                message: 'ok',
                checkedAt: DateTime.utc(2026, 7, 2),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pump();

    await tester.drag(find.byType(ListView), const Offset(0, -1700));
    await tester.pump(const Duration(milliseconds: 300));

    await tester.enterText(
      find.byType(TextField).last,
      'systemd[1]: app.service failed with exit-code\n'
      'Authorization: Bearer employee-secret-token',
    );
    final handoffButton = find.text('交给数字员工');
    await tester.ensureVisible(handoffButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(handoffButton);
    await tester.pumpAndSettle();

    expect(find.text('交给数字员工处理'), findsOneWidget);
    expect(find.textContaining('服务器数字员工'), findsOneWidget);
    expect(find.textContaining('MaClaw 官方服务'), findsWidgets);
    expect(find.textContaining('密码、Token、私钥'), findsOneWidget);
    expect(
      find.textContaining('systemd[1]: app.service failed'),
      findsWidgets,
    );

    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(_RecordingDigitalEmployeeTaskController.created, isEmpty);

    await tester.ensureVisible(handoffButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(handoffButton);
    await tester.pumpAndSettle();

    await tester.tap(find.text('确认提交'));
    await tester.pumpAndSettle();

    expect(
      _RecordingDigitalEmployeeTaskController.created.single.employeeId,
      'employee-server',
    );
    expect(
      _RecordingDigitalEmployeeTaskController.created.single.prompt,
      contains('systemd[1]: app.service failed with exit-code'),
    );
    expect(
      _RecordingDigitalEmployeeTaskController.created.single.prompt,
      contains('Authorization: Bearer [REDACTED_TOKEN]'),
    );
    expect(
      _RecordingDigitalEmployeeTaskController.created.single.prompt,
      isNot(contains('employee-secret-token')),
    );
    expect(
      _RecordingDigitalEmployeeTaskController.created.single.taskType,
      'server_maintenance',
    );
    expect(
      _RecordingDigitalEmployeeTaskController.created.single.context,
      containsPair('source', 'maclaw_mobile'),
    );
    expect(
      _RecordingDigitalEmployeeTaskController.created.single.context,
      containsPair('handoff', 'ssh_output'),
    );
    expect(
      _RecordingDigitalEmployeeTaskController.created.single.context,
      containsPair('task_surface', 'servers'),
    );
    expect(
      _RecordingDigitalEmployeeTaskController.created.single.context,
      containsPair('line_count', '2'),
    );
    expect(
      _RecordingDigitalEmployeeTaskController.created.single.prompt,
      contains('不要自动执行高风险操作'),
    );
    expect(find.textContaining('已提交给数字员工'), findsOneWidget);
  });
}
