import 'package:drift/native.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_client.dart';
import 'package:maclaw_mobile/core/network/mobile_network_status.dart';
import 'package:maclaw_mobile/core/notifications/mobile_notification_service.dart';
import 'package:maclaw_mobile/core/security/mobile_redaction.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
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
        backendSessionId: 'mobile-ssh:sess-analysis',
      );
}

class _RecordingSSHAnalysisController extends SSHAnalysisController {
  static final outputs = <String>[];
  static final backendSessionIds = <String?>[];

  @override
  Future<MobileSSHAnalysis?> build() async => null;

  @override
  Future<void> analyze(String output, {String? backendSessionId}) async {
    outputs.add(output.trim());
    backendSessionIds.add(backendSessionId);
    state = const AsyncData(
      MobileSSHAnalysis(
        summary: '发现 502 错误',
        recommendation: '先检查 upstream 服务状态。',
        commandDraft: 'systemctl status app --no-pager',
      ),
    );
  }
}

class _FakeBackendSSHApiClient extends ApiClient {
  final Object? createError;
  final createdProfileIds = <String>[];
  final attachedSessionIds = <String>[];
  final interruptedSessionIds = <String>[];
  List<MobileBackendSSHSession> backendSessions;
  final inputs = <({String sessionId, String input})>[];
  final backgroundTasks =
      <({String sessionId, String command, int? tailLines})>[];
  final fileOperations = <({
    String sessionId,
    String action,
    String localPath,
    String remotePath,
  })>[];
  final listedTaskSessions = <String>[];
  final waitedTasks = <({
    String sessionId,
    String taskId,
    int? timeoutSeconds,
    int? tailLines
  })>[];
  final killedTasks = <({String sessionId, String taskId})>[];
  final closedSessionIds = <String>[];

  _FakeBackendSSHApiClient({
    this.createError,
    this.backendSessions = const [],
  }) : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<List<MobileBackendSSHSession>> listBackendSSHSessions() async {
    return backendSessions;
  }

  @override
  Future<MobileBackendSSHSession> createBackendSSHSession({
    required String serverProfileId,
  }) async {
    final error = createError;
    if (error != null) throw error;
    createdProfileIds.add(serverProfileId);
    return MobileBackendSSHSession(
      sessionId: 'ssh-session-$serverProfileId',
      serverProfileId: serverProfileId,
      backendSessionId: 'mobile-ssh:ssh-session-$serverProfileId',
      status: 'connected',
      state: 'running',
      claimedBy: 'desktop-agent-1',
      outputSeq: 1,
      recentOutput: 'backend session ready\n',
    );
  }

  @override
  Future<MobileBackendSSHSession> reconnectBackendSSHSession(
    String sessionId,
  ) async {
    return MobileBackendSSHSession(
      sessionId: sessionId,
      serverProfileId: 'srv-prod',
      status: 'connected',
      recentOutput: 'backend session reconnected\n',
    );
  }

  @override
  Future<MobileBackendSSHSession> attachBackendSSHSession(
    String sessionId,
  ) async {
    attachedSessionIds.add(sessionId);
    return MobileBackendSSHSession(
      sessionId: sessionId,
      serverProfileId: 'srv-prod',
      backendSessionId: 'mobile-ssh:$sessionId',
      status: 'connected',
      state: 'running',
      claimedBy: 'desktop-agent-1',
      recentOutput: 'GUI/agent evidence line Hub session $sessionId backend_session_id mobile-ssh:$sessionId claimed_by desktop-agent-1 output_seq 2\nattached existing backend session\n',
      outputSeq: 2,
    );
  }

  @override
  Future<MobileBackendSSHSession> interruptBackendSSHSession(
    String sessionId,
  ) async {
    interruptedSessionIds.add(sessionId);
    return MobileBackendSSHSession(
      sessionId: sessionId,
      serverProfileId: 'srv-prod',
      status: 'interrupt_requested',
      state: 'interrupting',
      message: 'Interrupt queued for MaClaw GUI agent',
    );
  }

  @override
  Future<MobileBackendSSHSessionInputResult> sendBackendSSHSessionInput({
    required String sessionId,
    required String input,
  }) async {
    inputs.add((sessionId: sessionId, input: input));
    return MobileBackendSSHSessionInputResult(
      sessionId: sessionId,
      output: 'ran: ${input.trim()}\n',
      status: 'accepted',
    );
  }

  @override
  Future<MobileBackendSSHTask> startBackendSSHBackgroundTask({
    required String sessionId,
    required String command,
    int? tailLines,
  }) async {
    backgroundTasks.add(
      (
        sessionId: sessionId,
        command: command,
        tailLines: tailLines,
      ),
    );
    return MobileBackendSSHTask(
      taskId: 'task-1',
      sessionId: sessionId,
      backendSessionId: 'mobile-ssh:$sessionId',
      command: command,
      status: 'running',
      logTail: 'background task accepted\n',
      claimedBy: 'desktop-agent-1',
    );
  }

  @override
  Future<List<MobileBackendSSHTask>> listBackendSSHBackgroundTasks(
    String sessionId,
  ) async {
    listedTaskSessions.add(sessionId);
    return [
      MobileBackendSSHTask(
        taskId: 'task-1',
        sessionId: sessionId,
        backendSessionId: 'mobile-ssh:$sessionId',
        command: backgroundTasks.isEmpty
            ? 'journalctl -u app -n 200 --no-pager'
            : backgroundTasks.last.command,
        status: 'running',
        logTail: 'background task accepted\n',
      ),
    ];
  }

  @override
  Future<MobileBackendSSHTask> waitBackendSSHBackgroundTask({
    required String sessionId,
    required String taskId,
    int? timeoutSeconds,
    int? tailLines,
  }) async {
    waitedTasks.add(
      (
        sessionId: sessionId,
        taskId: taskId,
        timeoutSeconds: timeoutSeconds,
        tailLines: tailLines,
      ),
    );
    return MobileBackendSSHTask(
      taskId: taskId,
      sessionId: sessionId,
      backendSessionId: 'mobile-ssh:$sessionId',
      status: 'completed',
      exitCode: 0,
      logTail: 'task completed\n',
    );
  }

  @override
  Future<MobileBackendSSHTask> killBackendSSHBackgroundTask({
    required String sessionId,
    required String taskId,
  }) async {
    killedTasks.add((sessionId: sessionId, taskId: taskId));
    return MobileBackendSSHTask(
      taskId: taskId,
      sessionId: sessionId,
      backendSessionId: 'mobile-ssh:$sessionId',
      status: 'killed',
      message: 'terminated',
    );
  }

  @override
  Future<MobileBackendSSHFileOperation> requestBackendSSHFileOperation({
    required String sessionId,
    required String action,
    String localPath = '',
    String remotePath = '',
  }) async {
    fileOperations.add(
      (
        sessionId: sessionId,
        action: action,
        localPath: localPath,
        remotePath: remotePath,
      ),
    );
    return MobileBackendSSHFileOperation(
      operationId: 'file-op-1',
      sessionId: sessionId,
      backendSessionId: 'mobile-ssh:$sessionId',
      action: action,
      localPath: localPath,
      remotePath: remotePath,
      status: 'queued',
      message: 'file operation queued for GUI agent',
      claimedBy: 'desktop-agent-1',
    );
  }

  @override
  Future<void> closeBackendSSHSession(String sessionId) async {
    closedSessionIds.add(sessionId);
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
  test('backend session output submission summary counts lines and truncates',
      () {
    final summary = mobileSSHOutputSubmissionSummary(
      '${'a' * 430}\nsecond line',
    );

    expect(summary.lineCount, 2);
    expect(summary.charCount, 442);
    expect(summary.preview, endsWith('...'));
    expect(summary.preview.length, 423);
  });

  test('backend session output redaction removes common secrets', () {
    final redacted = redactMobileSensitiveText(
      'Authorization: Bearer secret-token\n'
      'password=super-secret token: abc123 api_key=key-1\n'
      'password="quoted password" API_KEY=\'quoted api key\'\n'
      'MYSQL_PWD=mysql-secret AWS_SECRET_ACCESS_KEY=aws-secret\n'
      'mysql --password cli-secret --token \'quoted cli token\' -e "select 1"\n'
      'https://admin:pass@example.com/logs\n'
      'postgres://dbuser:db-pass@db.internal/app\n'
      'redis://:redis-pass@cache.internal:6379/0\n'
      'ssh://root:ssh-pass@jump.internal\n'
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
    expect(redacted, contains('postgres://[REDACTED_CREDENTIALS]@db.internal'));
    expect(redacted, contains('redis://[REDACTED_CREDENTIALS]@cache.internal'));
    expect(redacted, contains('ssh://[REDACTED_CREDENTIALS]@jump.internal'));
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
    expect(redacted, isNot(contains('dbuser:db-pass')));
    expect(redacted, isNot(contains('redis-pass')));
    expect(redacted, isNot(contains('root:ssh-pass')));
  });

  test('digital employee output prompt uses backend session wording', () {
    final prompt = digitalEmployeeOutputPrompt('systemctl status nginx');

    expect(prompt, contains('MaClaw GUI/agent'));
    expect(prompt, contains('\u540e\u53f0 SSH \u4f1a\u8bdd\u8f93\u51fa'));
    expect(prompt, isNot(contains('SSH \u7ec8\u7aef\u8f93\u51fa')));
  });

  test('backend session output handoff context marks server maintenance source',
      () {
    final context = mobileSSHOutputTaskContext(
      'first line\nsecond line',
      backendSessionId: 'mobile-ssh:sess-123',
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
    expect(context, containsPair('backend_session_id', 'mobile-ssh:sess-123'));
    expect(context, containsPair('line_count', '2'));
    expect(context, containsPair('char_count', '22'));
    expect(context, containsPair('manual_confirmation_required', 'true'));
    expect(
      context,
      containsPair(
        'execution_boundary',
        'draft_only_until_mobile_user_confirms',
      ),
    );
    expect(
      context,
      containsPair(
        'manual_confirmation_scope',
        'destructive_or_high_risk_server_operations',
      ),
    );
    expect(context, containsPair('server_profile_id', 'srv-prod'));
    expect(context, containsPair('server_name', 'prod-api'));
    expect(context, containsPair('server_host', '10.0.0.8'));
    expect(context, containsPair('server_port', '2222'));
    expect(context, containsPair('server_username', 'ops'));
    expect(context, containsPair('server_auth_mode', serverAuthModePrivateKey));
    expect(context, containsPair('server_tag', 'prod'));
    expect(context, containsPair('server_note', 'primary API node'));
  });

  test(
      'backend session output handoff context redacts profile metadata secrets',
      () {
    final context = mobileSSHOutputTaskContext(
      'service failed',
      backendSessionId: 'mobile-ssh:sess token: backend-secret',
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

    expect(
      context['backend_session_id'],
      'mobile-ssh:sess token=[REDACTED_SECRET]',
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
    expect(context['backend_session_id'], isNot(contains('backend-secret')));
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
    expect(
      find.text('\u540e\u53f0\u670d\u52a1\u5668\u6863\u6848'),
      findsOneWidget,
    );

    await tester.scrollUntilVisible(
      find.text('GUI/agent 后台 SSH 会话', skipOffstage: false),
      240,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('GUI/agent 后台 SSH 会话'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('GUI/agent 文件操作', skipOffstage: false),
      240,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('GUI/agent 文件操作'), findsOneWidget);
    final commandRiskTitle = find.text(
      '\u547d\u4ee4\u98ce\u9669\u9884\u68c0',
      skipOffstage: false,
    );
    await tester.scrollUntilVisible(
      commandRiskTitle,
      240,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('命令风险预检'), findsOneWidget);
    expect(find.text('保存常用'), findsOneWidget);
    expect(find.text('投递到后台会话'), findsOneWidget);

    await tester.drag(find.byType(ListView), const Offset(0, -600));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('AI 分析后台会话输出'), findsOneWidget);
  });

  testWidgets('servers screen uses Hub-synced backend server profiles only',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
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

    expect(
      find.text('\u540e\u53f0\u670d\u52a1\u5668\u6863\u6848'),
      findsOneWidget,
    );
    expect(
      find.text('\u540c\u6b65\u670d\u52a1\u5668\u6863\u6848'),
      findsOneWidget,
    );
    expect(
      find.textContaining(
        '\u624b\u673a\u53ea\u53d1\u8d77\u540e\u53f0\u4f1a\u8bdd\u3001\u53d1\u9001\u786e\u8ba4\u540e\u7684\u8f93\u5165\u5e76\u67e5\u770b\u8f93\u51fa',
      ),
      findsOneWidget,
    );
    expect(
      find.textContaining('MaClaw GUI/agent \u6388\u6743\u914d\u7f6e'),
      findsOneWidget,
    );
    expect(find.text('未接管'), findsOneWidget);
    expect(find.text('连接中'), findsNothing);
    expect(find.text('已连接'), findsNothing);
    expect(find.text('未连接'), findsNothing);
    expect(find.text('prod-api'), findsOneWidget);
    expect(find.textContaining('ops@10.0.0.8:2222'), findsOneWidget);
    expect(find.widgetWithText(TextField, 'Host'), findsNothing);
    expect(find.widgetWithText(TextField, '\u7aef\u53e3'), findsNothing);
    expect(find.widgetWithText(TextField, '\u7528\u6237\u540d'), findsNothing);
    expect(find.widgetWithText(TextField, '\u5bc6\u7801'), findsNothing);
    expect(find.text('\u79c1\u94a5'), findsNothing);
    expect(
      find.byTooltip('\u6e05\u9664\u672c\u673a\u7f13\u5b58'),
      findsOneWidget,
    );
    expect(find.byTooltip('\u5220\u9664\u670d\u52a1\u5668'), findsNothing);
  });

  testWidgets('servers screen copies captured backend session output',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final copied = <String>[];
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          mobileBackendSshInitialOutputProvider.overrideWithValue(
            'GUI/agent 后台会话证据：Hub session mobssh-12345 · '
            'backend_session_id mobile-ssh-mobssh-12345 · '
            'claimed_by MaClaw GUI agent worker · output_seq 2\n'
            'nginx[1]: upstream timed out\n'
            'systemd[1]: app.service failed\n'
            'Authorization: Bearer backend-output-token\n'
            'password=backend-output-password',
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

    await tester.drag(find.byType(ListView), const Offset(0, -300));
    await tester.pump(const Duration(milliseconds: 300));

    expect(
      find.byTooltip('\u590d\u5236\u540e\u53f0\u4f1a\u8bdd\u8f93\u51fa'),
      findsOneWidget,
    );
    expect(
      find.byTooltip('\u590d\u5236\u7ec8\u7aef\u8f93\u51fa'),
      findsNothing,
    );

    final copyButton = find.byTooltip('复制后台会话输出');
    await tester.ensureVisible(copyButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(copyButton);
    await tester.pump();

    expect(copied.single, contains('GUI/agent 后台会话证据'));
    expect(copied.single, contains('Hub session mobssh-12345'));
    expect(copied.single, contains('backend_session_id mobile-ssh-mobssh-12345'));
    expect(copied.single, contains('claimed_by MaClaw GUI agent worker'));
    expect(copied.single, contains('output_seq 2'));
    expect(copied.single, contains('nginx[1]: upstream timed out'));
    expect(copied.single, contains('Authorization: Bearer [REDACTED_TOKEN]'));
    expect(copied.single, contains('password=[REDACTED_SECRET]'));
    expect(copied.single, isNot(contains('backend-output-token')));
    expect(copied.single, isNot(contains('backend-output-password')));
    expect(find.text('后台会话输出已复制'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 50));
  });


  testWidgets('servers screen blocks copied backend output without GUI agent evidence line',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final copied = <String>[];
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          mobileBackendSshInitialOutputProvider.overrideWithValue(
            'nginx[1]: upstream timed out\n'
            'systemd[1]: app.service failed',
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

    await tester.drag(find.byType(ListView), const Offset(0, -300));
    await tester.pump(const Duration(milliseconds: 300));

    final copyButton = find.byTooltip('复制后台会话输出');
    await tester.ensureVisible(copyButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(copyButton);
    await tester.pump();

    expect(copied, isEmpty);
    expect(find.text('等待 GUI/agent 证据行后再复制后台会话输出'), findsOneWidget);

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
          apiClientProvider.overrideWithValue(
            _FakeBackendSSHApiClient(
              createError: StateError('backend session unavailable'),
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

    final connectButton = find.text('请求后台会话');
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

  testWidgets('servers screen appends backend agent realtime output',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final api = _FakeBackendSSHApiClient();
    final copied = <String>[];
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(api),
          mobileClipboardWriterProvider.overrideWithValue((text) async {
            copied.add(text);
          }),
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

    final connectButton = find.text('请求后台会话');
    await tester.ensureVisible(connectButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(connectButton);
    await tester.pumpAndSettle();

    final context = tester.element(find.byType(ServersScreen));
    final container = ProviderScope.containerOf(context);
    await container
        .read(backendSshSessionsProvider.notifier)
        .applyRealtimeEvent(
          const MobileRealtimeEvent(
            type: 'ssh_session',
            payload: {
              'session_id': 'ssh-session-srv-prod',
              'server_profile_id': 'srv-prod',
              'status': 'connected',
              'state': 'running',
              'backend_session_id': 'mobile-ssh:ssh-session-srv-prod',
              'claimed_by': 'MaClaw GUI agent worker',
              'output_chunk': 'GUI/agent evidence line Hub session ssh-session-srv-prod backend_session_id mobile-ssh:ssh-session-srv-prod claimed_by MaClaw GUI agent worker output_seq 2\nagent output chunk\n',
              'output_seq': 2,
            },
          ),
        );
    await tester.pump();

    final copyButton = find.byTooltip('复制后台会话输出');
    await tester.ensureVisible(copyButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(copyButton);
    await tester.pump();

    expect(copied.single, contains('GUI/agent evidence line'));
    expect(copied.single, contains('Hub session ssh-session-srv-prod'));
    expect(copied.single, contains('backend_session_id mobile-ssh:ssh-session-srv-prod'));
    expect(copied.single, contains('claimed_by MaClaw GUI agent worker'));
    expect(copied.single, contains('output_seq 2'));
    expect(copied.single, contains('backend session ready'));
    expect(copied.single, contains('agent output chunk'));
  });

  testWidgets('servers screen sends backend session id with AI analysis',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final api = _FakeBackendSSHApiClient();
    _RecordingSSHAnalysisController.outputs.clear();
    _RecordingSSHAnalysisController.backendSessionIds.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(api),
          serverProfilesProvider.overrideWith(
            _OneServerProfileController.new,
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
                checkedAt: DateTime.utc(2026, 7, 6),
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

    final connectButton = find.text('请求后台会话');
    await tester.ensureVisible(connectButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(connectButton);
    await tester.pumpAndSettle();

    final context = tester.element(find.byType(ServersScreen));
    final container = ProviderScope.containerOf(context);
    await container
        .read(backendSshSessionsProvider.notifier)
        .applyRealtimeEvent(
          const MobileRealtimeEvent(
            type: 'ssh_session',
            payload: {
              'session_id': 'ssh-session-srv-prod',
              'server_profile_id': 'srv-prod',
              'backend_session_id': 'mobile-ssh:ssh-session-srv-prod',
              'status': 'connected',
              'state': 'running',
              'output_chunk': 'GUI/agent evidence line Hub session ssh-session-srv-prod backend_session_id mobile-ssh:ssh-session-srv-prod claimed_by MaClaw GUI agent worker output_seq 2\njournalctl output chunk\n',
              'claimed_by': 'MaClaw GUI agent worker',
              'output_seq': 2,
            },
          ),
        );
    await tester.pump();

    final analyzeButton = find.text('交给 AI 分析');
    await tester.ensureVisible(analyzeButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(analyzeButton);
    await tester.pumpAndSettle();

    expect(find.text('发送后台会话输出给 AI？'), findsOneWidget);
    await tester.tap(find.text('确认发送'));
    await tester.pumpAndSettle();

    expect(
      _RecordingSSHAnalysisController.outputs.single,
      contains('journalctl output chunk'),
    );
    expect(
      _RecordingSSHAnalysisController.backendSessionIds.single,
      'mobile-ssh:ssh-session-srv-prod',
    );
  });

  testWidgets('servers screen lists and attaches existing backend session',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final api = _FakeBackendSSHApiClient(
      backendSessions: [
        MobileBackendSSHSession(
          sessionId: 'ssh-existing-1',
          serverProfileId: 'srv-prod',
          backendSessionId: 'mobile-ssh:ssh-existing-1',
          status: 'connected',
          state: 'running',
          pendingInputCount: 1,
          claimedBy: 'desktop-agent-1',
          message: 'Managed by MaClaw GUI agent',
          recentOutput: 'previous backend output\n',
          outputSeq: 7,
          createdAt: DateTime(2026, 7, 6, 9, 30),
          updatedAt: DateTime(2026, 7, 6, 9, 59),
          lastActivityAt: DateTime(2026, 7, 6, 10),
        ),
      ],
    );
    final copied = <String>[];
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(api),
          mobileClipboardWriterProvider.overrideWithValue((text) async {
            copied.add(text);
          }),
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
                checkedAt: DateTime.utc(2026, 7, 6),
              ),
            ),
          ),
        ],
        child: const MaterialApp(home: Scaffold(body: ServersScreen())),
      ),
    );
    await tester.pumpAndSettle();

    final attachButton = find.text('\u9644\u7740');
    expect(find.text('ssh-existing-1'), findsOneWidget);
    expect(find.textContaining('状态 connected'), findsOneWidget);
    expect(find.textContaining('desktop-agent-1'), findsOneWidget);
    expect(find.textContaining('mobile-ssh:ssh-existing-1'), findsOneWidget);
    expect(find.textContaining('创建 2026-07-06 09:30'), findsOneWidget);
    expect(find.textContaining('更新 2026-07-06 09:59'), findsOneWidget);
    expect(find.textContaining('最后活动'), findsOneWidget);
    expect(find.textContaining('输出序号 7'), findsOneWidget);
    expect(find.textContaining('待处理输入 1'), findsOneWidget);
    await tester.ensureVisible(attachButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(attachButton);
    await tester.pumpAndSettle();

    expect(api.attachedSessionIds, ['ssh-existing-1']);

    final copyButton = find.byTooltip('复制后台会话输出');
    await tester.ensureVisible(copyButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(copyButton);
    await tester.pump();

    expect(copied.single, contains('GUI/agent evidence line'));
    expect(copied.single, contains('Hub session ssh-existing-1'));
    expect(copied.single, contains('backend_session_id mobile-ssh:ssh-existing-1'));
    expect(copied.single, contains('claimed_by desktop-agent-1'));
    expect(copied.single, contains('output_seq 2'));
    expect(copied.single, contains('attached existing backend session'));
  });

  testWidgets('servers screen queues backend SSH interrupt through Hub',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final api = _FakeBackendSSHApiClient();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(api),
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
                checkedAt: DateTime.utc(2026, 7, 6),
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

    final connectButton = find.text('请求后台会话');
    await tester.ensureVisible(connectButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(connectButton);
    await tester.pump();

    final interruptButton = find.text('\u4e2d\u65ad');
    await tester.ensureVisible(interruptButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(interruptButton);
    await tester.pump();

    expect(api.interruptedSessionIds, ['ssh-session-srv-prod']);
  });

  testWidgets('servers screen starts GUI agent background task from command',
      (tester) async {
    final store = MobileLocalStore(executor: NativeDatabase.memory());
    final api = _FakeBackendSSHApiClient();
    _RecordingServerCommandsController.recorded.clear();
    addTearDown(store.close);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mobileLocalStoreProvider.overrideWithValue(store),
          apiClientProvider.overrideWithValue(api),
          serverProfilesProvider.overrideWith(
            _OneServerProfileController.new,
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
                checkedAt: DateTime.utc(2026, 7, 6),
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

    final connectButton = find.text('请求后台会话');
    await tester.ensureVisible(connectButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(connectButton);
    await tester.pumpAndSettle();

    final commandField = find.widgetWithText(
      TextField,
      '命令草稿',
      skipOffstage: false,
    );
    await tester.scrollUntilVisible(
      commandField,
      240,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pump(const Duration(milliseconds: 300));
    await tester.enterText(commandField, 'journalctl -u app -n 200 --no-pager');
    await tester.pump();

    final taskButton = find.text('作为后台任务运行');
    await tester.ensureVisible(taskButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(taskButton);
    await tester.pumpAndSettle();

    expect(api.backgroundTasks, [
      (
        sessionId: 'ssh-session-srv-prod',
        command: 'journalctl -u app -n 200 --no-pager',
        tailLines: 80,
      ),
    ]);
    expect(api.inputs, isEmpty);
    expect(
      _RecordingServerCommandsController.recorded.single,
      (
        command: 'journalctl -u app -n 200 --no-pager',
        favorite: false,
      ),
    );
    expect(find.text('后台任务请求已提交给 GUI/agent'), findsOneWidget);
    await tester.scrollUntilVisible(
      find.text('GUI/agent 后台任务', skipOffstage: false),
      -240,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('GUI/agent 后台任务'), findsOneWidget);
    expect(find.text('task-1'), findsOneWidget);
    expect(find.textContaining('状态 running'), findsOneWidget);

    final refreshTasksButton = find.byTooltip('刷新后台任务');
    await tester.ensureVisible(refreshTasksButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(refreshTasksButton);
    await tester.pumpAndSettle();

    expect(api.listedTaskSessions, ['ssh-session-srv-prod']);

    final waitButton = find.text('等待');
    await tester.ensureVisible(waitButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(waitButton);
    await tester.pumpAndSettle();

    expect(
      api.waitedTasks.single,
      (
        sessionId: 'ssh-session-srv-prod',
        taskId: 'task-1',
        timeoutSeconds: 30,
        tailLines: 120,
      ),
    );
    expect(find.textContaining('状态 completed'), findsOneWidget);

    final killButton = find.text('终止');
    await tester.ensureVisible(killButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(killButton);
    await tester.pumpAndSettle();

    expect(
      api.killedTasks.single,
      (
        sessionId: 'ssh-session-srv-prod',
        taskId: 'task-1',
      ),
    );
    expect(find.textContaining('状态 killed'), findsOneWidget);

    await tester.scrollUntilVisible(
      find.text('GUI/agent 文件操作', skipOffstage: false),
      240,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pump(const Duration(milliseconds: 300));

    final remotePathField = find.widgetWithText(
      TextField,
      '远端路径',
      skipOffstage: false,
    );
    await tester.enterText(remotePathField, '/var/log/app.log');
    await tester.pump();

    final fileOperationButton = find.text('请求文件操作');
    await tester.ensureVisible(fileOperationButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(fileOperationButton);
    await tester.pumpAndSettle();

    expect(
      api.fileOperations.single,
      (
        sessionId: 'ssh-session-srv-prod',
        action: 'stat',
        localPath: '',
        remotePath: '/var/log/app.log',
      ),
    );
    expect(api.inputs, isEmpty);

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
    expect(find.text('后台会话：mobile-ssh:sess-analysis'), findsOneWidget);
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
    await tester.pump(const Duration(milliseconds: 300));

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
        '\u540e\u53f0\u4f1a\u8bdd\u8f93\u51fa\u6216\u9519\u8bef\u65e5\u5fd7',
      ),
      'nginx[1]: upstream timed out\nAuthorization: Bearer secret-token',
    );
    final analyzeButton = find.text('\u5206\u6790\u8f93\u51fa');
    await tester.ensureVisible(analyzeButton);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(analyzeButton);
    await tester.pumpAndSettle();

    expect(
      find.text(
        '\u53d1\u9001\u540e\u53f0\u4f1a\u8bdd\u8f93\u51fa\u7ed9 AI\uff1f',
      ),
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
