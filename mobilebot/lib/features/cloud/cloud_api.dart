import '../../core/api_client.dart';
import 'cloud_models.dart';

/// Account-level cloud agents. Instances use `/api/v1/cloud-agents`.
/// Tasks and transcripts use a control grant plus `cloud_*` remote ops.
/// A phone has no desktop row. Do not invent `X-Machine-Id`: Hub treats a
/// non-empty id as a logged-in computer and rejects anything else. An empty
/// id is the account-level identity other mobile grants already use. Opening
/// a cloud grant still fails until Hub accepts that empty id.
class CloudApi {
  CloudApi(this._client);

  final ApiClient _client;

  /// Explicit machine identity, when the caller already knows which computer
  /// is signed in (a machine row selected in remote control). Empty means
  /// "this account": Hub then labels the grant 「移动设备」 instead of asking a
  /// phone for a desktop it does not have.
  String machineId = '';

  Map<String, dynamic>? get _machineHeaders {
    final id = machineId.trim().isNotEmpty
        ? machineId.trim()
        : _client.account.machineId.trim();
    if (id.isEmpty) return null;
    return {'X-Machine-Id': id};
  }

  Future<CloudInstanceList> listInstances() async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/cloud-agents',
        options: _client.opts(headers: _machineHeaders),
      ),
    );
    return CloudInstanceList.fromJson(data);
  }

  Future<CloudInstance> createInstance({
    required String name,
    String kind = 'session',
  }) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/v1/cloud-agents',
        data: {'name': name.trim(), 'kind': kind},
        options: _client.opts(headers: _machineHeaders),
      ),
    );
    return CloudInstance.fromJson(data);
  }

  Future<CloudInstance> stopInstance(String id) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/v1/cloud-agents/${Uri.encodeComponent(id)}/stop',
        data: const <String, dynamic>{},
        options: _client.opts(headers: _machineHeaders),
      ),
    );
    return CloudInstance.fromJson(data);
  }

  Future<CloudInstance> resumeInstance(String id) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/v1/cloud-agents/${Uri.encodeComponent(id)}/resume',
        data: const <String, dynamic>{},
        options: _client.opts(headers: _machineHeaders),
      ),
    );
    return CloudInstance.fromJson(data);
  }

  Future<String> openGrant(String instanceId) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/v1/cloud-agents/${Uri.encodeComponent(instanceId)}/control-grant',
        data: const <String, dynamic>{},
        options: _client.opts(headers: _machineHeaders),
      ),
    );
    return (data['grant_id'] ?? '').toString();
  }

  Future<Map<String, dynamic>> _op(
    String grantId,
    String op, [
    Map<String, dynamic> body = const {},
  ]) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/remote-control/grants/${Uri.encodeComponent(grantId)}/op',
        data: {'op': op, 'body': body},
        options: _client.opts(headers: _machineHeaders),
      ),
    );
    final result = data['result'];
    return result is Map ? Map<String, dynamic>.from(result) : const {};
  }

  Future<List<CloudTask>> listTasks(String grantId) async {
    final result = await _op(grantId, 'cloud_list_tasks');
    return [
      for (final item
          in result['tasks'] is List ? result['tasks'] as List : const [])
        if (item is Map) CloudTask.fromJson(item),
    ];
  }

  Future<CloudTask> createTask(String grantId, {required String title}) async {
    final result = await _op(grantId, 'cloud_create_task', {
      'title': title.trim(),
    });
    final id = (result['task_id'] ?? '').toString();
    return CloudTask(id: id, title: title.trim());
  }

  /// [historyLimit] windows the transcript tail. 0 means the runtime default
  /// (40 rows); a poll passes the already-loaded count so a wider window is
  /// not dropped back to the default page.
  Future<CloudTaskDetail> getTask(
    String grantId,
    String taskId, {
    int historyLimit = 0,
  }) async {
    final result = await _op(grantId, 'cloud_get_task', {
      'task_id': taskId,
      if (historyLimit > 0) 'history_limit': historyLimit,
    });
    return CloudTaskDetail.fromJson(result);
  }

  Future<void> sendMessage(
    String grantId,
    String taskId,
    String text, {
    String clientMessageId = '',
  }) async {
    await _op(grantId, 'cloud_send_message', {
      'task_id': taskId,
      'text': text,
      if (clientMessageId.isNotEmpty) 'client_message_id': clientMessageId,
    });
  }

  Future<void> stopTask(String grantId, String taskId) async {
    await _op(grantId, 'cloud_stop_task', {'task_id': taskId});
  }

  /// Deletes the task and its whole transcript on the runtime. A running
  /// turn is cancelled first server-side.
  Future<void> deleteTask(String grantId, String taskId) async {
    await _op(grantId, 'cloud_delete_task', {'task_id': taskId});
  }

  Future<void> renameTask(
    String grantId,
    String taskId,
    String title,
  ) async {
    await _op(grantId, 'cloud_rename_task', {
      'task_id': taskId,
      'title': title.trim(),
    });
  }
}
