import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import 'digital_employee.dart';
import 'digital_employee_prompt.dart';

final digitalEmployeesProvider = AsyncNotifierProvider<
    DigitalEmployeesController, List<DigitalEmployee>>(
  DigitalEmployeesController.new,
);

class DigitalEmployeesController extends AsyncNotifier<List<DigitalEmployee>> {
  @override
  Future<List<DigitalEmployee>> build() async {
    return _load();
  }

  Future<void> refresh() async {
    state = const AsyncLoading();
    state = await AsyncValue.guard(_load);
  }

  Future<List<DigitalEmployee>> _load() async {
    final client = ref.read(apiClientProvider);
    if (client == null) return const [];
    return client.listDigitalEmployees();
  }
}

final digitalEmployeeTaskProvider =
    AsyncNotifierProvider<DigitalEmployeeTaskController, MobileDigitalEmployeeTask?>(
  DigitalEmployeeTaskController.new,
);

final digitalEmployeePromptHistoryProvider = AsyncNotifierProvider<
    DigitalEmployeePromptHistoryController, List<DigitalEmployeePromptEntry>>(
  DigitalEmployeePromptHistoryController.new,
);

class DigitalEmployeePromptHistoryController
    extends AsyncNotifier<List<DigitalEmployeePromptEntry>> {
  @override
  Future<List<DigitalEmployeePromptEntry>> build() {
    return ref.watch(mobileLocalStoreProvider).loadDigitalEmployeePrompts();
  }

  Future<void> record({
    required String employeeId,
    required String prompt,
  }) async {
    final text = prompt.trim();
    if (text.isEmpty) return;
    final current = state.valueOrNull ?? await future;
    final now = DateTime.now().toUtc();
    final entry = DigitalEmployeePromptEntry(
      id: now.microsecondsSinceEpoch.toString(),
      employeeId: employeeId,
      prompt: text,
      createdAt: now,
    );
    final next = [
      entry,
      ...current.where(
        (item) => item.employeeId != employeeId || item.prompt != text,
      ),
    ].take(50).toList();
    await ref.read(mobileLocalStoreProvider).saveDigitalEmployeePrompts(next);
    state = AsyncData(next);
  }
}

class DigitalEmployeeTaskController
    extends AsyncNotifier<MobileDigitalEmployeeTask?> {
  @override
  Future<MobileDigitalEmployeeTask?> build() async => null;

  Future<void> createTask({
    required String employeeId,
    required String prompt,
  }) async {
    final text = prompt.trim();
    if (text.isEmpty) return;
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(StateError('请先登录官方服务。'), StackTrace.current);
      return;
    }
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
      await ref.read(digitalEmployeePromptHistoryProvider.notifier).record(
            employeeId: employeeId,
            prompt: text,
          );
      final task = await client.createDigitalEmployeeTask(
        employeeId: employeeId,
        prompt: text,
      );
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: '数字员工任务已提交',
            body: '任务 ${task.taskId} 状态：${task.status}',
            payload: task.taskId,
          );
      return task;
    });
  }

  Future<void> refreshTask() async {
    final current = state.valueOrNull;
    if (current == null) return;
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(StateError('请先登录官方服务。'), StackTrace.current);
      return;
    }
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
      final task = await client.getDigitalEmployeeTask(current.taskId);
      if (task.status == 'done' || task.status == 'failed') {
        await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
              title: '数字员工任务更新',
              body: '任务 ${task.taskId} 状态：${task.status}',
              payload: task.taskId,
            );
      }
      return task;
    });
  }
}
