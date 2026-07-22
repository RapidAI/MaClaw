import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_client.dart';
import 'package:maclaw_mobile/core/notifications/mobile_notification_service.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employees_controller.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee_prompt.dart';

class _FakeMobileLocalStore extends MobileLocalStore {
  MobileDigitalEmployeeTask? lastTask;
  List<DigitalEmployeePromptEntry> prompts = const [];
  bool failTaskSave = false;
  bool failPromptSave = false;

  @override
  Future<MobileDigitalEmployeeTask?> loadLastDigitalEmployeeTask() async {
    return lastTask;
  }

  @override
  Future<List<MobileDigitalEmployeeTask>> loadRecentDigitalEmployeeTasks({
    int limit = 20,
  }) async {
    final task = lastTask;
    return task == null ? const [] : [task];
  }

  @override
  Future<void> saveLastDigitalEmployeeTask(
    MobileDigitalEmployeeTask task,
  ) async {
    if (failTaskSave) throw StateError('task cache unavailable');
    lastTask = task;
  }

  @override
  Future<List<DigitalEmployeePromptEntry>> loadDigitalEmployeePrompts() async {
    return prompts;
  }

  @override
  Future<void> saveDigitalEmployeePrompts(
    List<DigitalEmployeePromptEntry> entries,
  ) async {
    if (failPromptSave) throw StateError('prompt cache unavailable');
    prompts = entries;
  }
}

class _RecordingApiClient extends ApiClient {
  String createdEmployeeId = '';
  String createdPrompt = '';
  String createdTaskType = '';
  Map<String, String> createdContext = const {};

  _RecordingApiClient() : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<MobileDigitalEmployeeTask> createDigitalEmployeeTask({
    required String employeeId,
    required String prompt,
    String taskType = 'general',
    Map<String, String> context = const {},
  }) async {
    createdEmployeeId = employeeId;
    createdPrompt = prompt;
    createdTaskType = taskType;
    createdContext = context;
    return MobileDigitalEmployeeTask(
      taskId: 'task-created-1',
      employeeId: employeeId,
      prompt: prompt,
      taskType: taskType,
      context: context,
      status: 'queued',
      result: '',
      message: '任务已提交',
      claimedBy: '',
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
    int? notificationId,
  }) async {
    shown.add((title: title, body: body, payload: payload));
  }
}

class _FailingNotificationService extends MobileNotificationService {
  @override
  Future<void> showTaskCompleted({
    required String title,
    required String body,
    String? payload,
    int? notificationId,
  }) async {
    throw StateError('notification plugin unavailable');
  }
}

void main() {
  test('digital employee progress patches update state without notifying',
      () async {
    final store = _FakeMobileLocalStore();
    final notifications = _RecordingNotificationService();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
      ],
    );
    addTearDown(container.dispose);

    await container.read(digitalEmployeeTaskProvider.future);

    const progress = MobileRealtimeEvent(
      type: 'digital_employee_task',
      payload: {
        'task_id': 'task-progress-1',
        'employee_id': 'employee-1',
        'prompt': '检查磁盘',
        'status': 'in_progress',
        'result': '磁盘使用率 42%',
        'message': '生成中',
        'claimed_by': 'srv-prod',
      },
    );

    await container
        .read(digitalEmployeeTaskProvider.notifier)
        .applyRealtimeEvent(progress);
    // Identical snapshot must be ignored (no extra store write).
    store.lastTask = null;
    await container
        .read(digitalEmployeeTaskProvider.notifier)
        .applyRealtimeEvent(progress);

    final current = container.read(digitalEmployeeTaskProvider).valueOrNull;
    expect(current?.taskId, 'task-progress-1');
    expect(current?.status, 'in_progress');
    expect(current?.result, contains('磁盘使用率'));
    expect(store.lastTask, isNull);
    expect(notifications.shown, isEmpty);
  });

  test('digital employee realtime completion notifies once', () async {
    final store = _FakeMobileLocalStore();
    final notifications = _RecordingNotificationService();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
      ],
    );
    addTearDown(container.dispose);

    await container.read(digitalEmployeeTaskProvider.future);

    const event = MobileRealtimeEvent(
      type: 'digital_employee_task',
      payload: {
        'task_id': 'task-remote-1',
        'employee_id': 'employee-1',
        'prompt': '检查生产服务器错误日志',
        'status': 'done',
        'result': 'Nginx 502 已恢复',
        'message': '远程数字员工已完成排查',
        'claimed_by': 'srv-prod',
      },
    );

    await container
        .read(digitalEmployeeTaskProvider.notifier)
        .applyRealtimeEvent(event);
    await container
        .read(digitalEmployeeTaskProvider.notifier)
        .applyRealtimeEvent(event);

    expect(store.lastTask?.taskId, 'task-remote-1');
    expect(store.lastTask?.status, 'done');
    expect(notifications.shown, hasLength(1));
    expect(notifications.shown.single.title, '数字员工任务完成');
    expect(notifications.shown.single.body, contains('远程数字员工已完成排查'));
    expect(
      notifications.shown.single.payload,
      'digital-employee-task:task-remote-1',
    );
  });

  test('digital employee realtime accepts top-level task id fallback',
      () async {
    final store = _FakeMobileLocalStore();
    final notifications = _RecordingNotificationService();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
      ],
    );
    addTearDown(container.dispose);

    await container.read(digitalEmployeeTaskProvider.future);

    final event = MobileRealtimeEvent.tryParse({
      'type': 'digital_employee_task',
      'task_id': 'task-top-1',
      'status': 'done',
      'payload': {
        'employee_id': 'employee-1',
        'prompt': 'check remote server',
        'result': 'ok',
        'message': 'done',
        'claimed_by': 'srv-prod',
      },
    });

    await container
        .read(digitalEmployeeTaskProvider.notifier)
        .applyRealtimeEvent(event!);

    expect(store.lastTask?.taskId, 'task-top-1');
    expect(store.lastTask?.status, 'done');
    expect(notifications.shown, hasLength(1));
    expect(
      notifications.shown.single.payload,
      'digital-employee-task:task-top-1',
    );
  });

  test('digital employee notifications redact sensitive task messages',
      () async {
    final store = _FakeMobileLocalStore();
    final notifications = _RecordingNotificationService();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
      ],
    );
    addTearDown(container.dispose);

    await container.read(digitalEmployeeTaskProvider.future);

    const event = MobileRealtimeEvent(
      type: 'digital_employee_task',
      payload: {
        'task_id': 'task-secret-1',
        'employee_id': 'employee-1',
        'prompt': 'check production host',
        'status': 'done',
        'result': 'ok',
        'message': 'done token: remote-message-secret password=prod-password',
        'claimed_by': 'srv-prod',
      },
    );

    await container
        .read(digitalEmployeeTaskProvider.notifier)
        .applyRealtimeEvent(event);

    expect(notifications.shown, hasLength(1));
    expect(
      notifications.shown.single.body,
      contains('token=[REDACTED_SECRET]'),
    );
    expect(
      notifications.shown.single.body,
      contains('password=[REDACTED_SECRET]'),
    );
    expect(
      notifications.shown.single.body,
      isNot(contains('remote-message-secret')),
    );
    expect(notifications.shown.single.body, isNot(contains('prod-password')));
    expect(
      notifications.shown.single.payload,
      'digital-employee-task:task-secret-1',
    );
  });

  test('digital employee task creation redacts secrets at API boundary',
      () async {
    final store = _FakeMobileLocalStore();
    final apiClient = _RecordingApiClient();
    final notifications = _RecordingNotificationService();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        apiClientProvider.overrideWithValue(apiClient),
        mobileNotificationServiceProvider.overrideWithValue(notifications),
      ],
    );
    addTearDown(container.dispose);

    await container.read(digitalEmployeeTaskProvider.future);

    const prompt = 'check server token=raw-secret-token password=prod-password';
    await container.read(digitalEmployeeTaskProvider.notifier).createTask(
      employeeId: 'employee-1',
      prompt: prompt,
      taskType: 'server_maintenance',
      context: const {
        'source': 'maclaw_mobile',
        'log_tail': 'Authorization: Bearer raw-context-token',
        'private_key':
            '-----BEGIN PRIVATE KEY-----\nraw-key\n-----END PRIVATE KEY-----',
      },
    );

    expect(apiClient.createdEmployeeId, 'employee-1');
    expect(apiClient.createdPrompt, contains('token=[REDACTED_SECRET]'));
    expect(apiClient.createdPrompt, contains('password=[REDACTED_SECRET]'));
    expect(apiClient.createdPrompt, isNot(contains('raw-secret-token')));
    expect(apiClient.createdPrompt, isNot(contains('prod-password')));
    expect(apiClient.createdTaskType, 'server_maintenance');
    expect(apiClient.createdContext['source'], 'maclaw_mobile');
    expect(
      apiClient.createdContext['log_tail'],
      contains('Bearer [REDACTED_TOKEN]'),
    );
    expect(
      apiClient.createdContext['private_key'],
      '[REDACTED_PRIVATE_KEY]',
    );
    expect(
      apiClient.createdContext.toString(),
      isNot(contains('raw-context-token')),
    );
    expect(apiClient.createdContext.toString(), isNot(contains('raw-key')));
    expect(store.lastTask?.prompt, apiClient.createdPrompt);
    expect(store.lastTask?.context, apiClient.createdContext);
    expect(store.prompts, hasLength(1));
    expect(store.prompts.single.prompt, contains('token=[REDACTED_SECRET]'));
    expect(store.prompts.single.prompt, contains('password=[REDACTED_SECRET]'));
    expect(store.prompts.single.prompt, isNot(contains('raw-secret-token')));
    expect(store.prompts.single.prompt, isNot(contains('prod-password')));
  });

  test('prompt history failure does not fail Hub task creation', () async {
    final store = _FakeMobileLocalStore()..failPromptSave = true;
    final apiClient = _RecordingApiClient();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        apiClientProvider.overrideWithValue(apiClient),
        mobileNotificationServiceProvider.overrideWithValue(
          _RecordingNotificationService(),
        ),
      ],
    );
    addTearDown(container.dispose);
    await container.read(digitalEmployeeTaskProvider.future);

    await container.read(digitalEmployeeTaskProvider.notifier).createTask(
          employeeId: 'employee-1',
          prompt: 'inspect the server',
        );

    final state = container.read(digitalEmployeeTaskProvider);
    expect(state.hasError, isFalse);
    expect(state.valueOrNull?.taskId, 'task-created-1');
    expect(apiClient.createdEmployeeId, 'employee-1');
  });

  test('recent task cache failure keeps created Hub task live', () async {
    final store = _FakeMobileLocalStore()..failTaskSave = true;
    final apiClient = _RecordingApiClient();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        apiClientProvider.overrideWithValue(apiClient),
        mobileNotificationServiceProvider.overrideWithValue(
          _RecordingNotificationService(),
        ),
      ],
    );
    addTearDown(container.dispose);
    await container.read(digitalEmployeeTaskProvider.future);

    await container.read(digitalEmployeeTaskProvider.notifier).createTask(
          employeeId: 'employee-1',
          prompt: 'inspect the server',
        );

    final state = container.read(digitalEmployeeTaskProvider);
    expect(state.hasError, isFalse);
    expect(state.valueOrNull?.taskId, 'task-created-1');
    expect(store.lastTask, isNull);
  });

  test('notification failure keeps created Hub task live', () async {
    final store = _FakeMobileLocalStore();
    final apiClient = _RecordingApiClient();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        apiClientProvider.overrideWithValue(apiClient),
        mobileNotificationServiceProvider.overrideWithValue(
          _FailingNotificationService(),
        ),
      ],
    );
    addTearDown(container.dispose);
    await container.read(digitalEmployeeTaskProvider.future);

    await container.read(digitalEmployeeTaskProvider.notifier).createTask(
          employeeId: 'employee-1',
          prompt: 'inspect the server',
        );

    final state = container.read(digitalEmployeeTaskProvider);
    expect(state.hasError, isFalse);
    expect(state.valueOrNull?.taskId, 'task-created-1');
  });

  test('realtime task remains live when its local cache write fails', () async {
    final store = _FakeMobileLocalStore()..failTaskSave = true;
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        mobileNotificationServiceProvider.overrideWithValue(
          _RecordingNotificationService(),
        ),
      ],
    );
    addTearDown(container.dispose);
    await container.read(digitalEmployeeTaskProvider.future);

    const event = MobileRealtimeEvent(
      type: 'digital_employee_task',
      payload: {
        'task_id': 'task-live-cache-failure',
        'employee_id': 'employee-1',
        'prompt': 'inspect server',
        'status': 'in_progress',
        'message': 'working',
      },
    );
    await container
        .read(digitalEmployeeTaskProvider.notifier)
        .applyRealtimeEvent(event);

    final state = container.read(digitalEmployeeTaskProvider);
    expect(state.hasError, isFalse);
    expect(state.valueOrNull?.taskId, 'task-live-cache-failure');
    expect(state.valueOrNull?.status, 'in_progress');
  });
}
