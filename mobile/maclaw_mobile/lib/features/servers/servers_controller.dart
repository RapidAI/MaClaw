import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_realtime_client.dart';
import '../../core/notifications/mobile_notification_service.dart';
import '../../core/security/mobile_redaction.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import '../tasks/mobile_jobs_provider.dart';
import 'server_command.dart';
import 'server_profile.dart';

final serverProfilesProvider =
    AsyncNotifierProvider<ServerProfilesController, List<ServerProfile>>(
  ServerProfilesController.new,
);

final sshAnalysisProvider =
    AsyncNotifierProvider<SSHAnalysisController, MobileSSHAnalysis?>(
  SSHAnalysisController.new,
);

final serverCommandsProvider =
    AsyncNotifierProvider<ServerCommandsController, List<ServerCommandEntry>>(
  ServerCommandsController.new,
);

final backendSshSessionsProvider = AsyncNotifierProvider<
    BackendSshSessionsController, Map<String, MobileBackendSSHSession>>(
  BackendSshSessionsController.new,
);

final backendSshTasksProvider = AsyncNotifierProvider<BackendSshTasksController,
    Map<String, List<MobileBackendSSHTask>>>(
  BackendSshTasksController.new,
);

final backendSshFileOperationsProvider = AsyncNotifierProvider<
    BackendSshFileOperationsController,
    Map<String, List<MobileBackendSSHFileOperation>>>(
  BackendSshFileOperationsController.new,
);

class ServerProfilesController extends AsyncNotifier<List<ServerProfile>> {
  @override
  Future<List<ServerProfile>> build() async {
    final local =
        await ref.watch(mobileLocalStoreProvider).loadServerProfiles();
    return _refreshFromHub(local);
  }

  Future<void> clearCachedProfile(String id) async {
    final current = state.valueOrNull ?? await future;
    final next = current.where((item) => item.id != id).toList();
    await ref.read(mobileLocalStoreProvider).saveServerProfiles(next);
    state = AsyncData(next);
  }

  Future<List<ServerProfile>> refreshFromHub() async {
    final current = state.valueOrNull ?? await future;
    final next = await _refreshFromHub(current);
    state = AsyncData(next);
    return next;
  }

  Future<List<ServerProfile>> _refreshFromHub(
    List<ServerProfile> local,
  ) async {
    try {
      await ref.read(sessionControllerProvider.future);
      final client = ref.read(apiClientProvider);
      if (client == null) return local;
      // Push local profiles so Hub AI assistant / hub_exec can see them.
      final validLocal = local.where((p) => p.isValid).toList();
      if (validLocal.isNotEmpty) {
        try {
          await client.upsertServerProfiles(validLocal);
        } on Object {
          // Offline or older Hub — keep local list usable.
        }
      }
      final remote = await client.listServerProfiles();
      if (remote.isEmpty) return local;
      final merged = _mergeServerProfiles(local, remote);
      await ref.read(mobileLocalStoreProvider).saveServerProfiles(merged);
      return merged;
    } catch (_) {
      return local;
    }
  }
}

List<ServerProfile> _mergeServerProfiles(
  List<ServerProfile> local,
  List<ServerProfile> remote,
) {
  final byId = <String, ServerProfile>{};
  for (final profile in local) {
    if (profile.id.trim().isNotEmpty) {
      byId[profile.id] = profile;
    }
  }
  for (final profile in remote) {
    if (profile.id.trim().isNotEmpty && profile.isValid) {
      byId[profile.id] = profile;
    }
  }
  return byId.values.toList();
}

class BackendSshSessionsController
    extends AsyncNotifier<Map<String, MobileBackendSSHSession>> {
  final _notifiedAbnormalSessionIds = <String>{};

  @override
  Future<Map<String, MobileBackendSSHSession>> build() async {
    try {
      final client = ref.watch(apiClientProvider);
      if (client == null) return const {};
      final sessions = await client.listBackendSSHSessions();
      return {
        for (final session in sessions)
          if (session.sessionId.trim().isNotEmpty) session.sessionId: session,
      };
    } catch (_) {
      return const {};
    }
  }

  Future<void> refresh() async {
    state = const AsyncLoading();
    state = await AsyncValue.guard(build);
  }

  Future<void> put(MobileBackendSSHSession session) async {
    if (session.sessionId.trim().isEmpty) return;
    final current = state.valueOrNull ?? await future;
    state = AsyncData({
      ...current,
      session.sessionId: session,
    });
  }

  Future<MobileBackendSSHSession> createSession({
    required String serverProfileId,
    String execMode = '',
  }) async {
    final profileId = serverProfileId.trim();
    if (profileId.isEmpty) {
      throw ArgumentError('serverProfileId is required');
    }
    final session = await _requireApiClient().createBackendSSHSession(
      serverProfileId: profileId,
      execMode: execMode,
    );
    await put(session);
    return session;
  }

  Future<MobileBackendSSHSession> attachSession(String sessionId) async {
    final normalized = sessionId.trim();
    if (normalized.isEmpty) {
      throw ArgumentError('sessionId is required');
    }
    final session = await _requireApiClient().attachBackendSSHSession(
      normalized,
    );
    await put(session);
    return session;
  }

  Future<MobileBackendSSHSession> reconnectSession(String sessionId) async {
    final normalized = sessionId.trim();
    if (normalized.isEmpty) {
      throw ArgumentError('sessionId is required');
    }
    final session = await _requireApiClient().reconnectBackendSSHSession(
      normalized,
    );
    await put(session);
    return session;
  }

  Future<MobileBackendSSHSession> interruptSession(String sessionId) async {
    final normalized = sessionId.trim();
    if (normalized.isEmpty) {
      throw ArgumentError('sessionId is required');
    }
    final session = await _requireApiClient().interruptBackendSSHSession(
      normalized,
    );
    await put(session);
    return session;
  }

  Future<MobileBackendSSHSessionInputResult> sendInput({
    required String sessionId,
    required String input,
    bool raw = false,
  }) async {
    final normalized = sessionId.trim();
    if (normalized.isEmpty) {
      throw ArgumentError('sessionId is required');
    }
    if (!raw && input.trim().isEmpty) {
      throw ArgumentError('sessionId and input are required');
    }
    if (raw && input.isEmpty) {
      throw ArgumentError('raw input is required');
    }
    return _requireApiClient().sendBackendSSHSessionInput(
      sessionId: normalized,
      input: input,
      raw: raw,
    );
  }

  Future<void> closeSession(String sessionId) async {
    final normalized = sessionId.trim();
    if (normalized.isEmpty) {
      throw ArgumentError('sessionId is required');
    }
    await _requireApiClient().closeBackendSSHSession(normalized);
    final current = state.valueOrNull ?? await future;
    final existing = current[normalized];
    if (existing == null) return;
    final hub = existing.isHubExec;
    state = AsyncData({
      ...current,
      normalized: _sessionWithControlState(
        existing,
        status: hub ? 'closed' : 'close_requested',
        state: hub ? 'hub_closed' : 'closing',
        message: hub
            ? 'Hub SSH live connection and interactive shell closed.'
            : 'Close request queued for GUI/agent backend SSH handling.',
      ),
    });
  }

  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    if (!event.sshSession) return;
    final current = state.valueOrNull ?? await future;
    final incoming = MobileBackendSSHSession.fromJson(event.payload);
    final existing = current[incoming.sessionId];
    final incomingOutputSeq = incoming.outputSeq;
    if (existing != null &&
        event.payload.containsKey('output_seq') &&
        incomingOutputSeq > 0 &&
        existing.outputSeq > incomingOutputSeq) {
      return;
    }
    final session = existing == null
        ? incoming
        : _mergeBackendSSHRealtimeSession(
            existing: existing,
            incoming: incoming,
            payload: event.payload,
          );
    await put(session);
    await _notifyBackendSSHAbnormalState(session);
  }

  Future<void> _notifyBackendSSHAbnormalState(
    MobileBackendSSHSession session,
  ) async {
    final sessionId = session.sessionId.trim();
    if (sessionId.isEmpty) return;
    if (session.connected) {
      _notifiedAbnormalSessionIds.remove(sessionId);
      return;
    }
    if (!_backendSSHSessionAbnormal(session)) return;
    if (!_notifiedAbnormalSessionIds.add(sessionId)) return;
    final status = [
      if (session.status.trim().isNotEmpty) session.status.trim(),
      if (session.state.trim().isNotEmpty &&
          session.state.trim() != session.status.trim())
        session.state.trim(),
    ].join(' / ');
    final detail = redactMobileSensitiveText(session.message.trim());
    final profile = session.serverProfileId.trim();
    await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
          title: 'SSH 后台会话异常',
          body: [
            if (profile.isNotEmpty) '服务器档案 $profile',
            if (status.isNotEmpty) '状态 $status',
            if (detail.isNotEmpty) detail,
          ].join(' · '),
          payload: profile.isEmpty
              ? null
              : mobileServerProfileNotificationPayload(profile),
        );
  }

  ApiClient _requireApiClient() {
    final client = ref.read(apiClientProvider);
    if (client == null) {
      throw StateError('请先登录官方服务。');
    }
    return client;
  }
}

MobileBackendSSHSession _sessionWithControlState(
  MobileBackendSSHSession session, {
  required String status,
  required String state,
  required String message,
}) {
  final now = DateTime.now().toUtc();
  return MobileBackendSSHSession(
    sessionId: session.sessionId,
    serverProfileId: session.serverProfileId,
    backendSessionId: session.backendSessionId,
    execMode: session.execMode,
    status: status,
    state: state,
    message: message,
    recentOutput: session.recentOutput,
    outputChunk: session.outputChunk,
    outputSeq: session.outputSeq,
    pendingInputCount: session.pendingInputCount,
    claimedBy: session.claimedBy,
    createdAt: session.createdAt,
    updatedAt: now,
    lastActivityAt: now,
  );
}

bool _backendSSHSessionAbnormal(MobileBackendSSHSession session) {
  final status = session.status.trim().toLowerCase();
  final state = session.state.trim().toLowerCase();
  const abnormalValues = {
    'failed',
    'error',
    'disconnect',
    'disconnected',
    'connect_failed',
    'input_failed',
    'interrupt_failed',
    'reconnect_failed',
    'agent_unavailable',
    'ssh_manager_unavailable',
    'profile_not_found',
    'config_error',
  };
  return abnormalValues.contains(status) ||
      abnormalValues.contains(state) ||
      status.endsWith('_failed') ||
      state.endsWith('_failed');
}

MobileBackendSSHSession _mergeBackendSSHRealtimeSession({
  required MobileBackendSSHSession existing,
  required MobileBackendSSHSession incoming,
  required Map<String, dynamic> payload,
}) {
  bool hasAny(Iterable<String> keys) => keys.any(payload.containsKey);

  return MobileBackendSSHSession(
    sessionId: incoming.sessionId,
    serverProfileId: hasAny(['server_profile_id', 'profile_id'])
        ? incoming.serverProfileId
        : existing.serverProfileId,
    backendSessionId: payload.containsKey('backend_session_id')
        ? incoming.backendSessionId
        : existing.backendSessionId,
    execMode: payload.containsKey('exec_mode')
        ? incoming.execMode
        : existing.execMode,
    status: payload.containsKey('status') ? incoming.status : existing.status,
    state: payload.containsKey('state') ? incoming.state : existing.state,
    message: hasAny(['message', 'error']) ? incoming.message : existing.message,
    recentOutput: hasAny(['recent_output', 'output'])
        ? incoming.recentOutput
        : existing.recentOutput,
    outputChunk: payload.containsKey('output_chunk')
        ? incoming.outputChunk
        : existing.outputChunk,
    outputSeq: payload.containsKey('output_seq')
        ? incoming.outputSeq
        : existing.outputSeq,
    pendingInputCount: payload.containsKey('pending_input_count')
        ? incoming.pendingInputCount
        : existing.pendingInputCount,
    claimedBy: payload.containsKey('claimed_by')
        ? incoming.claimedBy
        : existing.claimedBy,
    createdAt: payload.containsKey('created_at')
        ? incoming.createdAt
        : existing.createdAt,
    updatedAt: payload.containsKey('updated_at')
        ? incoming.updatedAt
        : existing.updatedAt,
    lastActivityAt: hasAny(['last_activity_at', 'updated_at'])
        ? incoming.lastActivityAt
        : existing.lastActivityAt,
  );
}

class ServerCommandsController extends AsyncNotifier<List<ServerCommandEntry>> {
  @override
  Future<List<ServerCommandEntry>> build() {
    return ref.watch(mobileLocalStoreProvider).loadServerCommands();
  }

  Future<void> record(String command, {bool favorite = false}) async {
    final text = command.trim();
    if (text.isEmpty) return;
    final current = state.valueOrNull ?? await future;
    ServerCommandEntry? existing;
    for (final item in current) {
      if (item.command == text) {
        existing = item;
        break;
      }
    }
    final now = DateTime.now().toUtc();
    final entry = existing == null
        ? ServerCommandEntry(
            id: now.microsecondsSinceEpoch.toString(),
            command: text,
            label: _labelFor(text),
            favorite: favorite,
            createdAt: now,
            lastUsedAt: now,
          )
        : existing.copyWith(
            label: _labelFor(text),
            favorite: existing.favorite || favorite,
            lastUsedAt: now,
          );
    final next = [
      entry,
      ...current.where((item) => item.id != entry.id),
    ].take(80).toList();
    await _save(next);
  }

  Future<void> toggleFavorite(String id) async {
    final current = state.valueOrNull ?? await future;
    final next = [
      for (final item in current)
        if (item.id == id) item.copyWith(favorite: !item.favorite) else item,
    ];
    await _save(next);
  }

  Future<void> remove(String id) async {
    final current = state.valueOrNull ?? await future;
    await _save(current.where((item) => item.id != id).toList());
  }

  Future<void> _save(List<ServerCommandEntry> next) async {
    await ref.read(mobileLocalStoreProvider).saveServerCommands(next);
    state = AsyncData(next);
  }

  String _labelFor(String command) {
    final redacted = redactMobileSensitiveText(command);
    final first = redacted.split(RegExp(r'\s+')).take(3).join(' ');
    return first.length > 64 ? '${first.substring(0, 64)}...' : first;
  }
}

class BackendSshTasksController
    extends AsyncNotifier<Map<String, List<MobileBackendSSHTask>>> {
  final _notifiedTerminalTaskIds = <String>{};

  @override
  Future<Map<String, List<MobileBackendSSHTask>>> build() async => const {};

  Future<List<MobileBackendSSHTask>> refreshForSession(String sessionId) async {
    final normalized = sessionId.trim();
    if (normalized.isEmpty) return const [];
    final client = _requireApiClient();
    final tasks = await client.listBackendSSHBackgroundTasks(normalized);
    await _put(normalized, tasks);
    return tasks;
  }

  Future<MobileBackendSSHTask> startBackgroundTask({
    required String sessionId,
    required String command,
    int? tailLines,
  }) async {
    final normalized = sessionId.trim();
    final text = command.trim();
    if (normalized.isEmpty || text.isEmpty) {
      throw ArgumentError('sessionId and command are required');
    }
    final client = _requireApiClient();
    final task = await client.startBackendSSHBackgroundTask(
      sessionId: normalized,
      command: text,
      tailLines: tailLines,
    );
    await _upsert(normalized, task);
    return task;
  }

  Future<MobileBackendSSHTask> checkTask({
    required String sessionId,
    required String taskId,
  }) async {
    final normalized = sessionId.trim();
    final normalizedTaskId = taskId.trim();
    if (normalized.isEmpty || normalizedTaskId.isEmpty) {
      throw ArgumentError('sessionId and taskId are required');
    }
    final task = await _requireApiClient().getBackendSSHBackgroundTask(
      sessionId: normalized,
      taskId: normalizedTaskId,
    );
    await _upsert(normalized, task);
    return task;
  }

  Future<MobileBackendSSHTask> waitTask({
    required String sessionId,
    required String taskId,
    int? timeoutSeconds,
    int? tailLines,
  }) async {
    final normalized = sessionId.trim();
    final normalizedTaskId = taskId.trim();
    if (normalized.isEmpty || normalizedTaskId.isEmpty) {
      throw ArgumentError('sessionId and taskId are required');
    }
    final task = await _requireApiClient().waitBackendSSHBackgroundTask(
      sessionId: normalized,
      taskId: normalizedTaskId,
      timeoutSeconds: timeoutSeconds,
      tailLines: tailLines,
    );
    await _upsert(normalized, task);
    return task;
  }

  Future<MobileBackendSSHTask> killTask({
    required String sessionId,
    required String taskId,
  }) async {
    final normalized = sessionId.trim();
    final normalizedTaskId = taskId.trim();
    if (normalized.isEmpty || normalizedTaskId.isEmpty) {
      throw ArgumentError('sessionId and taskId are required');
    }
    final task = await _requireApiClient().killBackendSSHBackgroundTask(
      sessionId: normalized,
      taskId: normalizedTaskId,
    );
    await _upsert(normalized, task);
    return task;
  }

  Future<MobileBackendSSHFileOperation> requestFileOperation({
    required String sessionId,
    required String action,
    String localPath = '',
    String remotePath = '',
  }) async {
    final normalized = sessionId.trim();
    final normalizedAction = action.trim();
    if (normalized.isEmpty || normalizedAction.isEmpty) {
      throw ArgumentError('sessionId and action are required');
    }
    final operation = await _requireApiClient().requestBackendSSHFileOperation(
      sessionId: normalized,
      action: normalizedAction,
      localPath: localPath.trim(),
      remotePath: remotePath.trim(),
    );
    await ref.read(backendSshFileOperationsProvider.notifier).put(operation);
    return operation;
  }

  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    if (!event.sshTask || event.payload.isEmpty) return;
    final task = MobileBackendSSHTask.fromJson(event.payload);
    final sessionId = task.sessionId.trim();
    if (sessionId.isEmpty || task.taskId.trim().isEmpty) return;
    await _upsert(sessionId, task);
  }

  Future<void> _notifyTerminalTask(MobileBackendSSHTask task) async {
    final taskId = task.taskId.trim();
    if (taskId.isEmpty) return;
    final status = task.status.trim().toLowerCase();
    final terminal = status.contains('ready') ||
        status.contains('complete') ||
        status.contains('success') ||
        status.contains('fail') ||
        status.contains('error') ||
        status.contains('cancel');
    if (!terminal) return;
    if (!_notifiedTerminalTaskIds.add(taskId)) return;
    final failed =
        status.contains('fail') || status.contains('error');
    final cancelled = status.contains('cancel');
    final cmd = task.command.trim();
    final clip = cmd.length > 48 ? '${cmd.substring(0, 48)}…' : cmd;
    await ref.read(mobileNotificationServiceProvider).showTaskCompleted(
          title: failed
              ? 'SSH 任务失败'
              : cancelled
                  ? 'SSH 任务已取消'
                  : 'SSH 任务完成',
          body: [
            if (clip.isNotEmpty) clip,
            if (task.message.trim().isNotEmpty) task.message.trim(),
            if (task.exitCode != null) 'exit ${task.exitCode}',
          ].join(' · '),
          payload: mobileSshTaskNotificationPayload(taskId),
        );
  }

  ApiClient _requireApiClient() {
    final client = ref.read(apiClientProvider);
    if (client == null) {
      throw StateError('请先登录官方服务。');
    }
    return client;
  }

  Future<void> _put(
    String sessionId,
    List<MobileBackendSSHTask> tasks,
  ) async {
    final current = state.valueOrNull ?? await future;
    state = AsyncData({...current, sessionId: tasks});
  }

  Future<void> _upsert(String sessionId, MobileBackendSSHTask task) async {
    final current = state.valueOrNull ?? await future;
    final tasks = current[sessionId] ?? const <MobileBackendSSHTask>[];
    final taskId = task.taskId.trim();
    MobileBackendSSHTask? existing;
    for (final item in tasks) {
      if (item.taskId == taskId) {
        existing = item;
        break;
      }
    }
    final nextTask = existing == null
        ? task
        : _mergeBackendSSHTask(existing: existing, incoming: task);
    final next = [
      if (taskId.isNotEmpty) nextTask,
      for (final item in tasks)
        if (item.taskId != taskId) item,
    ];
    state = AsyncData({...current, sessionId: next});
    ref.invalidate(mobileJobsProvider);
    await _notifyTerminalTask(nextTask);
  }
}

class BackendSshFileOperationsController
    extends AsyncNotifier<Map<String, List<MobileBackendSSHFileOperation>>> {
  final _notifiedTerminalOps = <String>{};
  final _lastProgressNotifyAt = <String, DateTime>{};

  @override
  Future<Map<String, List<MobileBackendSSHFileOperation>>> build() async =>
      const {};

  Future<void> put(MobileBackendSSHFileOperation operation) async {
    final sessionId = operation.sessionId.trim();
    final operationId = operation.operationId.trim();
    if (sessionId.isEmpty || operationId.isEmpty) return;
    final current = state.valueOrNull ?? await future;
    final operations = current[sessionId] ?? const [];
    final next = [
      operation,
      for (final item in operations)
        if (item.operationId.trim() != operationId) item,
    ].take(20).toList();
    state = AsyncData({...current, sessionId: next});
    ref.invalidate(mobileJobsProvider);
    await _notifyFileOpProgress(operation);
    await _notifyTerminalFileOp(operation);
  }

  Future<void> applyRealtimeEvent(MobileRealtimeEvent event) async {
    if (!event.sshFileOperation || event.payload.isEmpty) return;
    await put(MobileBackendSSHFileOperation.fromJson(event.payload));
  }

  /// Throttled tray progress for running hub_exec downloads (parses "a/b bytes").
  Future<void> _notifyFileOpProgress(MobileBackendSSHFileOperation op) async {
    final id = op.operationId.trim();
    if (id.isEmpty) return;
    final status = op.status.trim().toLowerCase();
    final running = status.contains('run') ||
        status.contains('progress') ||
        status.contains('pending') ||
        status.contains('stream');
    if (!running) return;
    final action = op.action.trim().toLowerCase();
    if (action.isNotEmpty &&
        !action.contains('download') &&
        !action.contains('read') &&
        !action.contains('pull')) {
      // Prefer download-like ops for tray progress noise control.
      if (!op.message.toLowerCase().contains('download') &&
          !op.message.contains('bytes')) {
        return;
      }
    }
    final now = DateTime.now();
    final last = _lastProgressNotifyAt[id];
    if (last != null && now.difference(last) < const Duration(milliseconds: 900)) {
      return;
    }
    _lastProgressNotifyAt[id] = now;
    final parsed = _parseBytesProgress(op.message);
    final path = op.remotePath.trim().isNotEmpty
        ? op.remotePath.trim()
        : op.localPath.trim();
    final body = [
      if (path.isNotEmpty) path,
      if (op.message.trim().isNotEmpty) op.message.trim(),
      if (op.bytesTransferred > 0) '${op.bytesTransferred} 字节',
    ].where((s) => s.isNotEmpty).join(' · ');
    await ref.read(mobileNotificationServiceProvider).showTaskProgress(
          notificationId: mobileSshFileNotificationId(id),
          title: 'SSH 文件传输中',
          body: body.isEmpty ? action : body,
          progress: parsed?.$1 ?? op.bytesTransferred,
          max: parsed?.$2 ?? 0,
          indeterminate: parsed == null && op.bytesTransferred <= 0,
          payload: mobileSshFileNotificationPayload(id),
        );
  }

  Future<void> _notifyTerminalFileOp(MobileBackendSSHFileOperation op) async {
    final id = op.operationId.trim();
    if (id.isEmpty) return;
    final status = op.status.trim().toLowerCase();
    final terminal = status.contains('ready') ||
        status.contains('complete') ||
        status.contains('success') ||
        status.contains('fail') ||
        status.contains('error');
    if (!terminal) return;
    if (!_notifiedTerminalOps.add(id)) return;
    final notify = ref.read(mobileNotificationServiceProvider);
    final trayId = mobileSshFileNotificationId(id);
    await notify.cancelNotification(trayId);
    _lastProgressNotifyAt.remove(id);
    final failed = status.contains('fail') || status.contains('error');
    final action = op.action.trim().isEmpty ? 'file' : op.action.trim();
    final path = op.remotePath.trim().isNotEmpty
        ? op.remotePath.trim()
        : op.localPath.trim();
    await notify.showTaskCompleted(
      title: failed ? 'SSH 文件操作失败' : 'SSH 文件操作完成',
      body: [
        action,
        if (path.isNotEmpty) path,
        if (op.bytesTransferred > 0) '${op.bytesTransferred} 字节',
        if (op.downloadUrl.trim().isNotEmpty) '可下载',
        if (op.message.trim().isNotEmpty && failed) op.message.trim(),
      ].join(' · '),
      payload: mobileSshFileNotificationPayload(id),
      notificationId: trayId,
    );
  }
}

/// Parse Hub progress messages like "12345/67890 bytes" or "a/b bytes".
(int, int)? _parseBytesProgress(String message) {
  final m = RegExp(r'(\d+)\s*/\s*(\d+)\s*bytes?', caseSensitive: false)
      .firstMatch(message);
  if (m == null) return null;
  final a = int.tryParse(m.group(1) ?? '');
  final b = int.tryParse(m.group(2) ?? '');
  if (a == null || b == null || b <= 0) return null;
  return (a.clamp(0, b), b);
}

MobileBackendSSHTask _mergeBackendSSHTask({
  required MobileBackendSSHTask existing,
  required MobileBackendSSHTask incoming,
}) {
  return MobileBackendSSHTask(
    taskId: incoming.taskId.isNotEmpty ? incoming.taskId : existing.taskId,
    sessionId:
        incoming.sessionId.isNotEmpty ? incoming.sessionId : existing.sessionId,
    backendSessionId: incoming.backendSessionId.isNotEmpty
        ? incoming.backendSessionId
        : existing.backendSessionId,
    command: incoming.command.isNotEmpty ? incoming.command : existing.command,
    status: incoming.status.isNotEmpty ? incoming.status : existing.status,
    message: incoming.message.isNotEmpty ? incoming.message : existing.message,
    logTail: incoming.logTail.isNotEmpty ? incoming.logTail : existing.logTail,
    exitCode: incoming.exitCode ?? existing.exitCode,
    claimedBy:
        incoming.claimedBy.isNotEmpty ? incoming.claimedBy : existing.claimedBy,
    createdAt: incoming.createdAt ?? existing.createdAt,
    updatedAt: incoming.updatedAt ?? existing.updatedAt,
  );
}

class SSHAnalysisController extends AsyncNotifier<MobileSSHAnalysis?> {
  @override
  Future<MobileSSHAnalysis?> build() async => null;

  Future<void> analyze(String output, {String? backendSessionId}) async {
    final text = output.trim();
    if (text.isEmpty) return;
    await ref.read(sessionControllerProvider.future);
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(StateError('请先登录官方服务。'), StackTrace.current);
      return;
    }
    final redacted = redactMobileSensitiveText(text);
    final sessionId = redactMobileSensitiveText(backendSessionId?.trim() ?? '');
    state = const AsyncLoading();
    state = await AsyncValue.guard(
      () => client.analyzeSSHOutput(
        redacted,
        backendSessionId: sessionId.isEmpty ? null : sessionId,
      ),
    );
  }
}
