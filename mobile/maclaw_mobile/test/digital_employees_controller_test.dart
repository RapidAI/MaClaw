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
  }) async {
    shown.add((title: title, body: body, payload: payload));
  }
}

void main() {
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

  test('digital employee prompt history redacts secrets without changing task',
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

    const prompt = '请排查服务器，token=raw-secret-token password=prod-password';
    await container.read(digitalEmployeeTaskProvider.notifier).createTask(
      employeeId: 'employee-1',
      prompt: prompt,
      taskType: 'server_maintenance',
      context: const {'source': 'maclaw_mobile'},
    );

    expect(apiClient.createdEmployeeId, 'employee-1');
    expect(apiClient.createdPrompt, prompt);
    expect(apiClient.createdPrompt, contains('raw-secret-token'));
    expect(apiClient.createdPrompt, contains('prod-password'));
    expect(apiClient.createdTaskType, 'server_maintenance');
    expect(apiClient.createdContext, {'source': 'maclaw_mobile'});
    expect(store.lastTask?.prompt, prompt);
    expect(store.prompts, hasLength(1));
    expect(store.prompts.single.prompt, contains('token=[REDACTED_SECRET]'));
    expect(store.prompts.single.prompt, contains('password=[REDACTED_SECRET]'));
    expect(store.prompts.single.prompt, isNot(contains('raw-secret-token')));
    expect(store.prompts.single.prompt, isNot(contains('prod-password')));
  });
}
