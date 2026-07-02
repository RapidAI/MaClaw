import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_realtime_client.dart';
import 'package:maclaw_mobile/core/notifications/mobile_notification_service.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employees_controller.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee_prompt.dart';

class _FakeMobileLocalStore extends MobileLocalStore {
  MobileDigitalEmployeeTask? lastTask;

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
    return const [];
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
    expect(notifications.shown.single.payload, 'task-remote-1');
  });
}
