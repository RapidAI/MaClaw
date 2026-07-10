import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_client.dart';
import 'package:maclaw_mobile/core/notifications/mobile_notification_service.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/servers/server_command.dart';
import 'package:maclaw_mobile/features/servers/server_profile.dart';
import 'package:maclaw_mobile/features/servers/servers_controller.dart';

class _FakeMobileLocalStore extends MobileLocalStore {
  List<ServerProfile> profiles;
  List<ServerCommandEntry> commands;

  _FakeMobileLocalStore(this.profiles) : commands = const [];

  @override
  Future<List<ServerProfile>> loadServerProfiles() async => profiles;

  @override
  Future<void> saveServerProfiles(List<ServerProfile> profiles) async {
    this.profiles = profiles;
  }

  @override
  Future<List<ServerCommandEntry>> loadServerCommands() async => commands;

  @override
  Future<void> saveServerCommands(List<ServerCommandEntry> commands) async {
    this.commands = commands;
  }
}

class _SignedInSessionController extends SessionController {
  @override
  Future<SessionState> build() async => SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: MobileBootstrap.fromJson({
          'user': {
            'user_id': 'u1',
            'phone_number': '19900001111',
            'tenant_id': 'tenant-a',
          },
          'services': {
            'hub_status': 'online',
            'llm_status': 'available',
            'search_status': 'available',
            'documents_status': 'available',
            'digital_employees_status': 'available',
            'search_path': '/api/mobile/search',
            'documents_path': '/api/mobile/documents',
            'digital_employees_path': '/api/mobile/digital-employees',
            'realtime_path': '/api/mobile/realtime',
          },
          'llm_access': {
            'mode': 'maclaw_official',
            'status': 'available',
            'credits_account': 'phone:19900001111',
          },
        }),
      );
}

class _RecordingApiClient extends ApiClient {
  String? analyzedOutput;
  String? analyzedBackendSessionId;
  List<ServerProfile> profiles;
  Object? profileError;
  final sessionCalls = <String>[];
  final taskCalls = <String>[];
  final fileOperationCalls = <Map<String, String>>[];

  _RecordingApiClient({
    this.profiles = const [],
    this.profileError,
  }) : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<MobileSSHAnalysis> analyzeSSHOutput(
    String output, {
    String? backendSessionId,
  }) async {
    analyzedOutput = output;
    analyzedBackendSessionId = backendSessionId;
    return const MobileSSHAnalysis(
      summary: 'summary',
      recommendation: 'recommendation',
      commandDraft: 'systemctl status app',
    );
  }

  @override
  Future<List<ServerProfile>> listServerProfiles() async {
    final error = profileError;
    if (error != null) {
      throw error;
    }
    return profiles;
  }

  @override
  Future<MobileBackendSSHSession> createBackendSSHSession({
    required String serverProfileId,
  }) async {
    sessionCalls.add('create:$serverProfileId');
    return MobileBackendSSHSession(
      sessionId: 'mobssh-1',
      serverProfileId: serverProfileId,
      backendSessionId: 'mobile-ssh:mobssh-1',
      status: 'queued',
      state: 'pending_agent',
      message: 'queued for GUI/agent',
    );
  }

  @override
  Future<MobileBackendSSHSession> attachBackendSSHSession(
    String sessionId,
  ) async {
    sessionCalls.add('attach:$sessionId');
    return MobileBackendSSHSession(
      sessionId: sessionId,
      serverProfileId: 'srv-prod',
      backendSessionId: 'mobile-ssh:$sessionId',
      status: 'attach_requested',
      state: 'attaching',
      message: 'attach queued',
    );
  }

  @override
  Future<MobileBackendSSHSession> reconnectBackendSSHSession(
    String sessionId,
  ) async {
    sessionCalls.add('reconnect:$sessionId');
    return MobileBackendSSHSession(
      sessionId: sessionId,
      serverProfileId: 'srv-prod',
      backendSessionId: 'mobile-ssh:$sessionId',
      status: 'reconnect_requested',
      state: 'reconnecting',
      message: 'reconnect queued',
    );
  }

  @override
  Future<MobileBackendSSHSession> interruptBackendSSHSession(
    String sessionId,
  ) async {
    sessionCalls.add('interrupt:$sessionId');
    return MobileBackendSSHSession(
      sessionId: sessionId,
      serverProfileId: 'srv-prod',
      backendSessionId: 'mobile-ssh:$sessionId',
      status: 'interrupt_requested',
      state: 'interrupting',
      message: 'interrupt queued',
    );
  }

  @override
  Future<MobileBackendSSHSessionInputResult> sendBackendSSHSessionInput({
    required String sessionId,
    required String input,
  }) async {
    sessionCalls.add('input:$sessionId:$input');
    return MobileBackendSSHSessionInputResult(
      sessionId: sessionId,
      status: 'input_queued',
      message: 'queued for GUI/agent',
    );
  }

  @override
  Future<void> closeBackendSSHSession(String sessionId) async {
    sessionCalls.add('close:$sessionId');
  }

  @override
  Future<MobileBackendSSHTask> startBackendSSHBackgroundTask({
    required String sessionId,
    required String command,
    int? tailLines,
  }) async {
    taskCalls.add('start:$sessionId:$command:$tailLines');
    return MobileBackendSSHTask(
      taskId: 'task-1',
      sessionId: sessionId,
      backendSessionId: 'mobile-ssh:$sessionId',
      command: command,
      status: 'running',
      logTail: 'started\n',
      claimedBy: 'desktop-agent-1',
    );
  }

  @override
  Future<List<MobileBackendSSHTask>> listBackendSSHBackgroundTasks(
    String sessionId,
  ) async {
    taskCalls.add('list:$sessionId');
    return [
      MobileBackendSSHTask(
        taskId: 'task-1',
        sessionId: sessionId,
        backendSessionId: 'mobile-ssh:$sessionId',
        command: 'journalctl -u app',
        status: 'running',
      ),
    ];
  }

  @override
  Future<MobileBackendSSHTask> getBackendSSHBackgroundTask({
    required String sessionId,
    required String taskId,
  }) async {
    taskCalls.add('check:$sessionId:$taskId');
    return MobileBackendSSHTask(
      taskId: taskId,
      sessionId: sessionId,
      status: 'running',
      logTail: 'checking\n',
    );
  }

  @override
  Future<MobileBackendSSHTask> waitBackendSSHBackgroundTask({
    required String sessionId,
    required String taskId,
    int? timeoutSeconds,
    int? tailLines,
  }) async {
    taskCalls.add('wait:$sessionId:$taskId:$timeoutSeconds:$tailLines');
    return MobileBackendSSHTask(
      taskId: taskId,
      sessionId: sessionId,
      status: 'completed',
      logTail: 'done\n',
      exitCode: 0,
    );
  }

  @override
  Future<MobileBackendSSHTask> killBackendSSHBackgroundTask({
    required String sessionId,
    required String taskId,
  }) async {
    taskCalls.add('kill:$sessionId:$taskId');
    return MobileBackendSSHTask(
      taskId: taskId,
      sessionId: sessionId,
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
    fileOperationCalls.add({
      'session_id': sessionId,
      'action': action,
      'local_path': localPath,
      'remote_path': remotePath,
    });
    return MobileBackendSSHFileOperation(
      operationId: 'file-op-1',
      sessionId: sessionId,
      backendSessionId: 'mobile-ssh:$sessionId',
      action: action,
      localPath: localPath,
      remotePath: remotePath,
      status: 'queued',
      claimedBy: 'desktop-agent-1',
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

void main() {
  test('clearing a server profile only removes phone-side metadata cache',
      () async {
    final store = _FakeMobileLocalStore(
      const [
        ServerProfile(
          id: 'srv-delete',
          name: 'prod',
          host: '10.0.0.8',
          port: 22,
          username: 'ops',
          authMode: serverAuthModePassword,
        ),
        ServerProfile(
          id: 'srv-keep',
          name: 'jump',
          host: '10.0.0.9',
          port: 22,
          username: 'root',
          authMode: serverAuthModePrivateKey,
        ),
      ],
    );
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
      ],
    );
    addTearDown(container.dispose);

    await container.read(serverProfilesProvider.future);
    await container
        .read(serverProfilesProvider.notifier)
        .clearCachedProfile('srv-delete');

    expect(store.profiles.map((profile) => profile.id), ['srv-keep']);
    expect(
      container.read(serverProfilesProvider).valueOrNull?.single.id,
      'srv-keep',
    );
  });

  test('server profiles refresh from Hub and merge with local profiles',
      () async {
    final store = _FakeMobileLocalStore(
      const [
        ServerProfile(
          id: 'local',
          name: 'local',
          host: '192.168.1.2',
          port: 22,
          username: 'root',
          authMode: serverAuthModePassword,
        ),
        ServerProfile(
          id: 'prod',
          name: 'old prod',
          host: '10.0.0.1',
          port: 22,
          username: 'old',
          authMode: serverAuthModePassword,
        ),
      ],
    );
    final api = _RecordingApiClient(
      profiles: [
        ServerProfile(
          id: 'prod',
          name: 'Prod',
          host: '10.0.0.10',
          port: 2222,
          username: 'deploy',
          authMode: serverAuthModePrivateKey,
          tag: 'desktop',
          sourceMachineId: 'desktop-agent-1',
          updatedAt: DateTime.utc(2026, 7, 6, 9),
        ),
      ],
    );
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        apiClientProvider.overrideWithValue(api),
      ],
    );
    addTearDown(container.dispose);

    final profiles = await container.read(serverProfilesProvider.future);

    expect(profiles.map((profile) => profile.id), ['local', 'prod']);
    expect(profiles.last.name, 'Prod');
    expect(profiles.last.port, 2222);
    expect(profiles.last.sourceMachineId, 'desktop-agent-1');
    expect(profiles.last.updatedAt, DateTime.utc(2026, 7, 6, 9));
    expect(store.profiles.last.authMode, serverAuthModePrivateKey);
    expect(store.profiles.last.sourceMachineId, 'desktop-agent-1');
  });

  test('server profiles keep local records when Hub refresh fails', () async {
    final store = _FakeMobileLocalStore(
      const [
        ServerProfile(
          id: 'local',
          name: 'local',
          host: '192.168.1.2',
          port: 22,
          username: 'root',
          authMode: serverAuthModePassword,
        ),
      ],
    );
    final api = _RecordingApiClient(profileError: StateError('offline'));
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        apiClientProvider.overrideWithValue(api),
      ],
    );
    addTearDown(container.dispose);

    final profiles = await container.read(serverProfilesProvider.future);

    expect(profiles, hasLength(1));
    expect(profiles.single.id, 'local');
    expect(store.profiles.single.id, 'local');
  });

  test('backend SSH realtime events preserve worker timing metadata', () async {
    final container = ProviderContainer(
      overrides: [
        apiClientProvider.overrideWithValue(null),
      ],
    );
    addTearDown(container.dispose);

    await container.read(backendSshSessionsProvider.future);
    await container
        .read(backendSshSessionsProvider.notifier)
        .applyRealtimeEvent(
          const MobileRealtimeEvent(
            type: 'ssh_session',
            payload: {
              'session_id': 'mobssh-1',
              'server_profile_id': 'srv-prod',
              'backend_session_id': 'mobile-ssh:mobssh-1',
              'status': 'connected',
              'state': 'running',
              'claimed_by': 'desktop-agent-1',
              'pending_input_count': 2,
              'output_seq': 9,
              'created_at': '2026-07-06T09:00:00Z',
              'updated_at': '2026-07-06T09:03:00Z',
            },
          ),
        );

    final session =
        container.read(backendSshSessionsProvider).valueOrNull!['mobssh-1']!;
    expect(session.backendSessionId, 'mobile-ssh:mobssh-1');
    expect(session.claimedBy, 'desktop-agent-1');
    expect(session.pendingInputCount, 2);
    expect(session.outputSeq, 9);
    expect(session.createdAt, DateTime.utc(2026, 7, 6, 9));
    expect(session.updatedAt, DateTime.utc(2026, 7, 6, 9, 3));
    expect(session.lastActivityAt, DateTime.utc(2026, 7, 6, 9, 3));

    await container
        .read(backendSshSessionsProvider.notifier)
        .applyRealtimeEvent(
          const MobileRealtimeEvent(
            type: 'ssh_session',
            payload: {
              'session_id': 'mobssh-1',
              'status': 'connected',
              'output_chunk': 'new output\n',
              'output_seq': 10,
              'updated_at': '2026-07-06T09:04:00Z',
            },
          ),
        );

    final updated =
        container.read(backendSshSessionsProvider).valueOrNull!['mobssh-1']!;
    expect(updated.serverProfileId, 'srv-prod');
    expect(updated.backendSessionId, 'mobile-ssh:mobssh-1');
    expect(updated.claimedBy, 'desktop-agent-1');
    expect(updated.pendingInputCount, 2);
    expect(updated.createdAt, DateTime.utc(2026, 7, 6, 9));
    expect(updated.outputChunk, 'new output\n');
    expect(updated.outputSeq, 10);
    expect(updated.updatedAt, DateTime.utc(2026, 7, 6, 9, 4));
    expect(updated.lastActivityAt, DateTime.utc(2026, 7, 6, 9, 4));
  });

  test('backend SSH realtime events ignore stale output sequences', () async {
    final container = ProviderContainer(
      overrides: [apiClientProvider.overrideWithValue(null)],
    );
    addTearDown(container.dispose);

    await container.read(backendSshSessionsProvider.future);
    final controller = container.read(backendSshSessionsProvider.notifier);
    await controller.applyRealtimeEvent(
      const MobileRealtimeEvent(
        type: 'ssh_session',
        payload: {
          'session_id': 'mobssh-order',
          'status': 'connected',
          'state': 'running',
          'output_chunk': 'new output\n',
          'output_seq': 12,
        },
      ),
    );
    await controller.applyRealtimeEvent(
      const MobileRealtimeEvent(
        type: 'ssh_session',
        payload: {
          'session_id': 'mobssh-order',
          'status': 'disconnected',
          'state': 'disconnect',
          'output_chunk': 'stale output\n',
          'output_seq': 11,
        },
      ),
    );

    final session = container
        .read(backendSshSessionsProvider)
        .valueOrNull!['mobssh-order']!;
    expect(session.outputSeq, 12);
    expect(session.outputChunk, 'new output\n');
    expect(session.status, 'connected');
    expect(session.state, 'running');
  });

  test('backend SSH abnormal realtime events notify once and redact details',
      () async {
    final notifications = _RecordingNotificationService();
    final container = ProviderContainer(
      overrides: [
        apiClientProvider.overrideWithValue(null),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
      ],
    );
    addTearDown(container.dispose);

    await container.read(backendSshSessionsProvider.future);
    final controller = container.read(backendSshSessionsProvider.notifier);

    await controller.applyRealtimeEvent(
      const MobileRealtimeEvent(
        type: 'ssh_session',
        payload: {
          'session_id': 'mobssh-failed',
          'server_profile_id': 'srv-prod',
          'backend_session_id': 'mobile-ssh:mobssh-failed',
          'status': 'failed',
          'state': 'connect_failed',
          'message': 'connect failed password=prod-secret',
        },
      ),
    );
    await controller.applyRealtimeEvent(
      const MobileRealtimeEvent(
        type: 'ssh_session',
        payload: {
          'session_id': 'mobssh-failed',
          'status': 'failed',
          'message': 'still failed password=prod-secret',
        },
      ),
    );

    expect(notifications.shown, hasLength(1));
    expect(notifications.shown.single.title, 'SSH 后台会话异常');
    expect(notifications.shown.single.body, contains('服务器档案 srv-prod'));
    expect(notifications.shown.single.body, contains('状态 failed'));
    expect(
      notifications.shown.single.body,
      contains('password=[REDACTED_SECRET]'),
    );
    expect(notifications.shown.single.body, isNot(contains('prod-secret')));
    expect(
      notifications.shown.single.payload,
      mobileServerProfileNotificationPayload('srv-prod'),
    );

    await controller.applyRealtimeEvent(
      const MobileRealtimeEvent(
        type: 'ssh_session',
        payload: {
          'session_id': 'mobssh-failed',
          'status': 'connected',
          'state': 'running',
        },
      ),
    );
    await controller.applyRealtimeEvent(
      const MobileRealtimeEvent(
        type: 'ssh_session',
        payload: {
          'session_id': 'mobssh-failed',
          'status': 'failed',
          'state': 'input_failed',
          'message': 'input failed token=raw-token',
        },
      ),
    );

    expect(notifications.shown, hasLength(2));
    expect(notifications.shown.last.body, contains('token=[REDACTED_SECRET]'));
    expect(notifications.shown.last.body, isNot(contains('raw-token')));
  });

  test('backend SSH session controller queues GUI agent control records',
      () async {
    final api = _RecordingApiClient();
    final container = ProviderContainer(
      overrides: [
        apiClientProvider.overrideWithValue(api),
      ],
    );
    addTearDown(container.dispose);

    await container.read(backendSshSessionsProvider.future);
    final controller = container.read(backendSshSessionsProvider.notifier);

    final created = await controller.createSession(
      serverProfileId: ' srv-prod ',
    );
    final attached = await controller.attachSession(' mobssh-1 ');
    final reconnected = await controller.reconnectSession(' mobssh-1 ');
    final interrupted = await controller.interruptSession(' mobssh-1 ');
    final input = await controller.sendInput(
      sessionId: ' mobssh-1 ',
      input: 'uptime\r',
    );
    await controller.closeSession(' mobssh-1 ');

    expect(created.serverProfileId, 'srv-prod');
    expect(attached.status, 'attach_requested');
    expect(reconnected.status, 'reconnect_requested');
    expect(interrupted.status, 'interrupt_requested');
    expect(input.status, 'input_queued');
    expect(api.sessionCalls, [
      'create:srv-prod',
      'attach:mobssh-1',
      'reconnect:mobssh-1',
      'interrupt:mobssh-1',
      'input:mobssh-1:uptime\r',
      'close:mobssh-1',
    ]);

    final cached =
        container.read(backendSshSessionsProvider).valueOrNull!['mobssh-1']!;
    expect(cached.backendSessionId, 'mobile-ssh:mobssh-1');
    expect(cached.status, 'close_requested');
    expect(cached.state, 'closing');
    expect(cached.message, contains('GUI/agent'));
  });

  test('backend SSH task realtime events update task cache', () async {
    final container = ProviderContainer(
      overrides: [
        apiClientProvider.overrideWithValue(null),
      ],
    );
    addTearDown(container.dispose);

    await container.read(backendSshTasksProvider.future);
    await container.read(backendSshTasksProvider.notifier).applyRealtimeEvent(
          const MobileRealtimeEvent(
            type: 'ssh_task',
            payload: {
              'task_id': 'task-1',
              'session_id': 'mobssh-1',
              'backend_session_id': 'mobile-ssh:mobssh-1',
              'command': 'journalctl -u app',
              'status': 'running',
              'log_tail': 'started',
            },
          ),
        );
    await container.read(backendSshTasksProvider.notifier).applyRealtimeEvent(
          const MobileRealtimeEvent(
            type: 'ssh_task',
            payload: {
              'task_id': 'task-1',
              'session_id': 'mobssh-1',
              'status': 'completed',
              'log_tail': 'done',
              'exit_code': 0,
            },
          ),
        );

    final tasks =
        container.read(backendSshTasksProvider).valueOrNull!['mobssh-1']!;
    expect(tasks, hasLength(1));
    expect(tasks.single.taskId, 'task-1');
    expect(tasks.single.backendSessionId, 'mobile-ssh:mobssh-1');
    expect(tasks.single.command, 'journalctl -u app');
    expect(tasks.single.status, 'completed');
    expect(tasks.single.logTail, 'done');
    expect(tasks.single.exitCode, 0);
  });

  test('backend SSH file operation realtime events update session cache',
      () async {
    final container = ProviderContainer(
      overrides: [apiClientProvider.overrideWithValue(null)],
    );
    addTearDown(container.dispose);

    await container.read(backendSshFileOperationsProvider.future);
    await container
        .read(backendSshFileOperationsProvider.notifier)
        .applyRealtimeEvent(
          const MobileRealtimeEvent(
            type: 'ssh_file_operation',
            payload: {
              'operation_id': 'op-1',
              'session_id': 'mobssh-1',
              'action': 'download',
              'status': 'running',
              'remote_path': '/var/log/app.log',
            },
          ),
        );
    final operations = container
        .read(backendSshFileOperationsProvider)
        .valueOrNull!['mobssh-1']!;
    expect(operations, hasLength(1));
    expect(operations.single.operationId, 'op-1');
    expect(operations.single.action, 'download');
    expect(operations.single.status, 'running');

    await container
        .read(backendSshFileOperationsProvider.notifier)
        .applyRealtimeEvent(
          const MobileRealtimeEvent(
            type: 'ssh_file_operation',
            payload: {
              'operation_id': 'op-1',
              'session_id': 'mobssh-1',
              'status': 'completed',
              'bytes_transferred': 128,
            },
          ),
        );
    final updated = container
        .read(backendSshFileOperationsProvider)
        .valueOrNull!['mobssh-1']!;
    expect(updated, hasLength(1));
    expect(updated.single.status, 'completed');
    expect(updated.single.bytesTransferred, 128);
  });

  test('server command history redacts labels but preserves executable command',
      () async {
    final store = _FakeMobileLocalStore(const []);
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
      ],
    );
    addTearDown(container.dispose);

    await container.read(serverCommandsProvider.future);

    const command = 'deploy password=prod-token';
    await container.read(serverCommandsProvider.notifier).record(
          command,
          favorite: true,
        );

    expect(store.commands, hasLength(1));
    expect(store.commands.single.command, command);
    expect(store.commands.single.command, contains('prod-token'));
    expect(store.commands.single.label, contains('password=[REDACTED_SECRET]'));
    expect(store.commands.single.label, isNot(contains('prod-token')));
    expect(store.commands.single.favorite, isTrue);
  });

  test('backend SSH task controller manages GUI agent task control records',
      () async {
    final api = _RecordingApiClient();
    final container = ProviderContainer(
      overrides: [
        apiClientProvider.overrideWithValue(api),
      ],
    );
    addTearDown(container.dispose);

    await container.read(backendSshTasksProvider.future);
    final controller = container.read(backendSshTasksProvider.notifier);

    final started = await controller.startBackgroundTask(
      sessionId: ' ssh-session-1 ',
      command: ' journalctl -u app ',
      tailLines: 80,
    );
    final listed = await controller.refreshForSession(' ssh-session-1 ');
    final checked = await controller.checkTask(
      sessionId: ' ssh-session-1 ',
      taskId: ' task-1 ',
    );
    final waited = await controller.waitTask(
      sessionId: ' ssh-session-1 ',
      taskId: ' task-1 ',
      timeoutSeconds: 30,
      tailLines: 120,
    );
    final killed = await controller.killTask(
      sessionId: ' ssh-session-1 ',
      taskId: ' task-1 ',
    );

    expect(started.command, 'journalctl -u app');
    expect(listed.single.taskId, 'task-1');
    expect(checked.logTail, 'checking\n');
    expect(waited.status, 'completed');
    expect(waited.exitCode, 0);
    expect(killed.status, 'killed');
    expect(api.taskCalls, [
      'start:ssh-session-1:journalctl -u app:80',
      'list:ssh-session-1',
      'check:ssh-session-1:task-1',
      'wait:ssh-session-1:task-1:30:120',
      'kill:ssh-session-1:task-1',
    ]);
    final cached =
        container.read(backendSshTasksProvider).valueOrNull!['ssh-session-1']!;
    expect(cached, hasLength(1));
    expect(cached.single.taskId, 'task-1');
    expect(cached.single.status, 'killed');
  });

  test('backend SSH task controller requests GUI agent file operations',
      () async {
    final api = _RecordingApiClient();
    final container = ProviderContainer(
      overrides: [
        apiClientProvider.overrideWithValue(api),
      ],
    );
    addTearDown(container.dispose);

    await container.read(backendSshTasksProvider.future);
    final operation = await container
        .read(backendSshTasksProvider.notifier)
        .requestFileOperation(
          sessionId: ' ssh-session-1 ',
          action: ' download ',
          localPath: ' mobile-downloads/app.log ',
          remotePath: ' /var/log/app.log ',
        );

    expect(operation.operationId, 'file-op-1');
    expect(operation.backendSessionId, 'mobile-ssh:ssh-session-1');
    expect(operation.status, 'queued');
    expect(api.fileOperationCalls, [
      {
        'session_id': 'ssh-session-1',
        'action': 'download',
        'local_path': 'mobile-downloads/app.log',
        'remote_path': '/var/log/app.log',
      },
    ]);
  });

  test('SSH AI analysis waits for session and redacts output before API call',
      () async {
    final api = _RecordingApiClient();
    final container = ProviderContainer(
      overrides: [
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        apiClientProvider.overrideWithValue(api),
      ],
    );
    addTearDown(container.dispose);
    await container.read(sshAnalysisProvider.future);

    await container.read(sshAnalysisProvider.notifier).analyze(
          'Authorization: Bearer raw-token\n'
          'password=prod-password\n'
          'https://admin:pass@example.com',
          backendSessionId: 'mobile-ssh:sess token=backend-secret',
        );

    expect(
      api.analyzedOutput,
      contains('Authorization: Bearer [REDACTED_TOKEN]'),
    );
    expect(api.analyzedOutput, contains('password=[REDACTED_SECRET]'));
    expect(
      api.analyzedOutput,
      contains('https://[REDACTED_CREDENTIALS]@example.com'),
    );
    expect(api.analyzedOutput, isNot(contains('raw-token')));
    expect(api.analyzedOutput, isNot(contains('prod-password')));
    expect(api.analyzedOutput, isNot(contains('admin:pass')));
    expect(
      api.analyzedBackendSessionId,
      'mobile-ssh:sess token=[REDACTED_SECRET]',
    );
    expect(
      container.read(sshAnalysisProvider).valueOrNull?.commandDraft,
      'systemctl status app',
    );
  });
}
