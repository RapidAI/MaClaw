import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_realtime_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../core/security/mobile_redaction.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import 'digital_employee.dart';
import 'digital_employee_prompt.dart';

final digitalEmployeesProvider =
    AsyncNotifierProvider<DigitalEmployeesController, List<DigitalEmployee>>(
  DigitalEmployeesController.new,
);

/// Last list scope from Hub: `own` (free) or `shared` (paid pool allowed).
final digitalEmployeesScopeProvider = StateProvider<String>((ref) => 'own');

final digitalEmployeesSharedFlagProvider = StateProvider<bool>((ref) => false);

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
    if (client == null) {
      ref.read(digitalEmployeesScopeProvider.notifier).state = 'own';
      ref.read(digitalEmployeesSharedFlagProvider.notifier).state = false;
      return const [];
    }
    final catalog = await client.listDigitalEmployeesCatalog();
    ref.read(digitalEmployeesScopeProvider.notifier).state = catalog.scope;
    ref.read(digitalEmployeesSharedFlagProvider.notifier).state =
        catalog.sharedEmployees;
    // Product rule: only list online digital employees on mobile.
    return filterOnlineDigitalEmployees(catalog.employees);
  }
}

final digitalEmployeeTaskProvider = AsyncNotifierProvider<
    DigitalEmployeeTaskController, MobileDigitalEmployeeTask?>(
  DigitalEmployeeTaskController.new,
);

final digitalEmployeePromptHistoryProvider = AsyncNotifierProvider<
    DigitalEmployeePromptHistoryController, List<DigitalEmployeePromptEntry>>(
  DigitalEmployeePromptHistoryController.new,
);

final digitalEmployeeTaskHistoryProvider = AsyncNotifierProvider<
    DigitalEmployeeTaskHistoryController, List<MobileDigitalEmployeeTask>>(
  DigitalEmployeeTaskHistoryController.new,
);

class DigitalEmployeeTaskHistoryController
    extends AsyncNotifier<List<MobileDigitalEmployeeTask>> {
  @override
  Future<List<MobileDigitalEmployeeTask>> build() {
    return ref.watch(mobileLocalStoreProvider).loadRecentDigitalEmployeeTasks();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(
      () => ref.read(mobileLocalStoreProvider).loadRecentDigitalEmployeeTasks(),
    );
  }
}

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
    final historyText = redactMobileSensitiveText(text);
    final current = state.valueOrNull ?? await future;
    final now = DateTime.now().toUtc();
    final entry = DigitalEmployeePromptEntry(
      id: now.microsecondsSinceEpoch.toString(),
      employeeId: employeeId,
      prompt: historyText,
      createdAt: now,
    );
    final next = [
      entry,
      ...current.where(
        (item) => item.employeeId != employeeId || item.prompt != historyText,
      ),
    ].take(50).toList();
    await ref.read(mobileLocalStoreProvider).saveDigitalEmployeePrompts(next);
    state = AsyncData(next);
  }
}

class DigitalEmployeeTaskController
    extends AsyncNotifier<MobileDigitalEmployeeTask?> {
  Timer? _pollTimer;
  final Set<String> _notifiedFinishedTasks = {};

  @override
  Future<MobileDigitalEmployeeTask?> build() async {
    ref.onDispose(() {
      _pollTimer?.cancel();
      _pollTimer = null;
    });
    final task =
        await ref.watch(mobileLocalStoreProvider).loadLastDigitalEmployeeTask();
    if (task != null) {
      _ensurePolling(task);
    }
    return task;
  }

  Future<void> createTask({
    required String employeeId,
    required String prompt,
    String taskType = 'general',
    Map<String, String> context = const {},
  }) async {
    final text = prompt.trim();
    if (text.isEmpty) return;
    final safeText = redactMobileSensitiveText(text);
    final safeContext = _redactDigitalEmployeeContext(context);
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(StateError('请先登录官方服务。'), StackTrace.current);
      return;
    }
    state = const AsyncLoading();
    final next = await AsyncValue.guard(() async {
      await ref.read(digitalEmployeePromptHistoryProvider.notifier).record(
            employeeId: employeeId,
            prompt: safeText,
          );
      final task = await client.createDigitalEmployeeTask(
        employeeId: employeeId,
        prompt: safeText,
        taskType: taskType,
        context: safeContext,
      );
      await ref
          .read(mobileLocalStoreProvider)
          .saveLastDigitalEmployeeTask(task);
      ref.invalidate(digitalEmployeeTaskHistoryProvider);
      await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
            title: '数字员工任务已提交',
            body: _taskNotificationBody(task),
            payload: mobileDigitalEmployeeTaskNotificationPayload(task.taskId),
          );
      return task;
    });
    state = next;
    final task = next.valueOrNull;
    if (task != null) {
      _ensurePolling(task);
    }
  }

  Future<void> refreshTask({bool silent = false}) async {
    final current = state.valueOrNull;
    if (current == null) return;
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(StateError('请先登录官方服务。'), StackTrace.current);
      return;
    }
    if (!silent) {
      state = const AsyncLoading();
    }
    final next = await AsyncValue.guard(() async {
      final task = await client.getDigitalEmployeeTask(current.taskId);
      await ref
          .read(mobileLocalStoreProvider)
          .saveLastDigitalEmployeeTask(task);
      await ref.read(digitalEmployeeTaskHistoryProvider.notifier).refresh();
      await _notifyTaskFinished(task);
      return task;
    });
    state = next;
    final task = next.valueOrNull;
    if (task != null) {
      _ensurePolling(task);
    }
  }

  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    if (!event.digitalEmployeeTask || event.payload.isEmpty) return;
    final task = MobileDigitalEmployeeTask.fromJson(event.payload);
    if (task.taskId.isEmpty) return;
    await ref.read(mobileLocalStoreProvider).saveLastDigitalEmployeeTask(task);
    await ref.read(digitalEmployeeTaskHistoryProvider.notifier).refresh();
    await _notifyTaskFinished(task);
    state = AsyncData(task);
    _ensurePolling(task);
  }

  void _ensurePolling(MobileDigitalEmployeeTask task) {
    if (_taskFinished(task)) {
      _pollTimer?.cancel();
      _pollTimer = null;
      return;
    }
    if (_pollTimer?.isActive ?? false) {
      return;
    }
    _pollTimer = Timer.periodic(const Duration(seconds: 8), (_) {
      final current = state.valueOrNull;
      if (current == null || _taskFinished(current) || state.isLoading) {
        if (current == null || _taskFinished(current)) {
          _pollTimer?.cancel();
          _pollTimer = null;
        }
        return;
      }
      unawaited(refreshTask(silent: true));
    });
  }

  bool _taskFinished(MobileDigitalEmployeeTask task) {
    return task.status == 'done' || task.status == 'failed';
  }

  Future<void> selectTask(MobileDigitalEmployeeTask task) async {
    await ref.read(mobileLocalStoreProvider).saveLastDigitalEmployeeTask(task);
    await ref.read(digitalEmployeeTaskHistoryProvider.notifier).refresh();
    state = AsyncData(task);
    _ensurePolling(task);
  }

  Future<void> _notifyTaskFinished(MobileDigitalEmployeeTask task) async {
    if (!_taskFinished(task) || task.taskId.isEmpty) return;
    if (!_notifiedFinishedTasks.add(task.taskId)) return;
    await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
          title: task.status == 'failed' ? '数字员工任务失败' : '数字员工任务完成',
          body: _taskNotificationBody(task),
          payload: mobileDigitalEmployeeTaskNotificationPayload(task.taskId),
        );
  }

  String _taskNotificationBody(MobileDigitalEmployeeTask task) {
    final message = redactMobileSensitiveText(task.message.trim());
    if (message.isEmpty) {
      return '任务 ${task.taskId} 状态：${task.status}';
    }
    return '任务 ${task.taskId} 状态：${task.status}，$message';
  }
}

Map<String, String> _redactDigitalEmployeeContext(Map<String, String> context) {
  if (context.isEmpty) return const {};
  return {
    for (final entry in context.entries)
      entry.key: redactMobileSensitiveText(entry.value.trim()),
  };
}
