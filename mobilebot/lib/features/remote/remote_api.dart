import '../../core/api_client.dart';
import 'remote_models.dart';

/// Thin client over `/api/remote-control/*` — the same surface the desktop
/// GUI drives through `App.RemoteControlOp` → `remote_control.exec`. Every op
/// goes through `POST /grants/{id}/op`; the Hub relays it to the controlled
/// machine and answers `{result: ...}`.
class RemoteControlApi {
  RemoteControlApi(this._client);

  final ApiClient _client;

  Future<RemoteControlState> state() async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/remote-control/state',
        options: _client.opts(),
      ),
    );
    return RemoteControlState.fromJson(data);
  }

  Future<void> _grantAction(
    String grantId,
    String action, {
    String bindMachineId = '',
  }) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/remote-control/grants/$grantId/$action',
        queryParameters: {
          if (bindMachineId.isNotEmpty) 'machine_id': bindMachineId,
        },
        options: _client.opts(),
      ),
    );
  }

  /// Accepting an incoming invite binds one of my desktops as the controlled
  /// target — a phone has no machine of its own, so [bindMachineId] names
  /// which registered desktop answers.
  Future<void> acceptGrant(String grantId, {String bindMachineId = ''}) =>
      _grantAction(grantId, 'accept', bindMachineId: bindMachineId);
  Future<void> rejectGrant(String grantId) => _grantAction(grantId, 'reject');
  Future<void> ignoreGrant(String grantId) => _grantAction(grantId, 'ignore');
  Future<void> revokeGrant(String grantId) => _grantAction(grantId, 'revoke');

  /// Invite an account to let one of my machines be controlled, or let me
  /// control — the Hub decides direction by which machine holds the grant.
  /// `permission` is `view` or `operate`.
  Future<void> invite(String account, String permission) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/remote-control/invites',
        data: {'account': account.trim(), 'permission': permission},
        options: _client.opts(),
      ),
    );
  }

  /// Pair two machines of my own account (`same_account` grant — no approval
  /// round-trip on the other side).
  Future<void> sameAccount(String targetMachineId, String permission) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/remote-control/same-account',
        data: {'target_machine_id': targetMachineId, 'permission': permission},
        options: _client.opts(),
      ),
    );
  }

  Future<Map<String, dynamic>> _op(
    String grantId,
    String op, [
    Map<String, dynamic> body = const {},
  ]) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/remote-control/grants/$grantId/op',
        data: {'op': op, 'body': body},
        options: _client.opts(),
      ),
    );
    final result = data['result'];
    return result is Map ? Map<String, dynamic>.from(result) : const {};
  }

  // -- view-tier ops -------------------------------------------------------

  Future<List<RemoteTask>> listTasks(String grantId) async {
    final result = await _op(grantId, 'list_tasks');
    return [
      for (final item
          in result['tasks'] is List ? result['tasks'] as List : const [])
        if (item is Map) RemoteTask.fromJson(item),
    ];
  }

  Future<RemoteTaskDetail> getTask(String grantId, String projectPath) async {
    final result = await _op(grantId, 'get_task', {
      'project_path': projectPath,
    });
    return RemoteTaskDetail.fromJson(result);
  }

  /// Reads one produced file off the controlled desktop. The Hub allows only
  /// paths the task transcript recorded as delivered attachments.
  Future<RemoteFile> fetchFile(
    String grantId,
    String projectPath,
    String path,
  ) async {
    final result = await _op(grantId, 'fetch_file', {
      'project_path': projectPath,
      'path': path,
    });
    return RemoteFile.fromJson(result);
  }

  // -- operate-tier ops -----------------------------------------------------

  Future<RemoteTask> createTask(
    String grantId, {
    required String name,
    String mode = '',
    String workDir = '',
  }) async {
    final result = await _op(grantId, 'create_task', {
      'name': name.trim(),
      if (mode.isNotEmpty) 'mode': mode,
      if (workDir.trim().isNotEmpty) 'work_dir': workDir.trim(),
    });
    final task = result['task'];
    return task is Map
        ? RemoteTask.fromJson(task)
        : RemoteTask(id: '', name: name.trim());
  }

  Future<void> sendMessage(
    String grantId,
    String projectPath,
    String text,
  ) async {
    await _op(grantId, 'send_message', {
      'project_path': projectPath,
      'text': text,
    });
  }

  Future<void> stopTask(String grantId, String projectPath) async {
    await _op(grantId, 'stop_task', {'project_path': projectPath});
  }

  Future<String> renameTask(
    String grantId,
    String projectPath,
    String name,
  ) async {
    final result = await _op(grantId, 'rename_task', {
      'project_path': projectPath,
      'name': name,
    });
    return result['name'] is String ? result['name'] as String : name;
  }

  Future<void> pinTask(String grantId, String projectPath, bool pinned) async {
    await _op(grantId, 'pin_task', {
      'project_path': projectPath,
      'pinned': pinned,
    });
  }

  /// One browse page of the controlled machine's directories for the
  /// new-task work-dir picker. An empty [path] opens its default dir.
  Future<RemoteDirList> listDirs(String grantId, String path) async {
    final result = await _op(grantId, 'list_dirs', {'path': path});
    final list = result.isEmpty ? const <String, dynamic>{} : result;
    return RemoteDirList.fromJson(list);
  }
}
