import '../../core/json_safe.dart';
import '../../l10n/gen/app_localizations.dart';

/// Hub cloud-agent instance statuses. Unknown values stay as raw text so a
/// new server status is not invented into one of these.
const cloudStatusProvisioning = 'provisioning';
const cloudStatusIdle = 'idle';
const cloudStatusRunning = 'running';
const cloudStatusStopped = 'stopped';
const cloudStatusProvisionFailed = 'provision_failed';

/// One account-level cloud instance. Not a remote-control machine grant.
class CloudInstance {
  final String id;
  final String name;
  final String kind;
  final String status;
  final String runtimeStatus;

  const CloudInstance({
    required this.id,
    this.name = '',
    this.kind = '',
    this.status = '',
    this.runtimeStatus = '',
  });

  factory CloudInstance.fromJson(Map json) => CloudInstance(
    id: syncText(json['id']),
    name: syncText(json['name']),
    kind: syncText(json['kind']),
    status: syncText(json['status']),
    runtimeStatus: syncText(json['runtime_status']),
  );

  bool get canStop =>
      status == cloudStatusIdle ||
      status == cloudStatusRunning ||
      status == cloudStatusProvisioning;

  // Hub Resume rejects anything that is not stopped. A failed provision
  // retries on the next cloud op, not on this button.
  bool get canResume => status == cloudStatusStopped;

  String statusLabel(AppLocalizations s) => switch (status) {
    cloudStatusProvisioning => s.cloudStatusProvisioning,
    cloudStatusIdle => s.cloudStatusIdle,
    cloudStatusRunning => s.cloudStatusRunning,
    cloudStatusStopped => s.cloudStatusStopped,
    cloudStatusProvisionFailed => s.cloudStatusProvisionFailed,
    _ => status,
  };
}

/// Transcript turn statuses written by the cloud runtime. Anything else stays
/// as the raw string so a new server value is not relabeled.
const cloudTurnRunning = 'running';
const cloudTurnFinished = 'finished';
const cloudTurnCancelled = 'cancelled';
const cloudTurnInterrupted = 'interrupted';

String cloudTurnLabel(AppLocalizations s, String status) => switch (status) {
  '' => '',
  cloudTurnRunning => s.cloudTurnRunning,
  cloudTurnFinished => s.cloudTurnFinished,
  cloudTurnCancelled => s.cloudTurnCancelled,
  cloudTurnInterrupted => s.cloudTurnInterrupted,
  _ => status,
};

class CloudInstanceList {
  final List<CloudInstance> instances;
  final bool enabled;

  const CloudInstanceList({this.instances = const [], this.enabled = false});

  factory CloudInstanceList.fromJson(Map json) => CloudInstanceList(
    instances: [
      for (final item in syncList(json['instances']))
        if (item is Map) CloudInstance.fromJson(item),
    ],
    enabled: json['enabled'] == true,
  );
}

/// One task row from `cloud_list_tasks`. Title is the only label the runtime
/// returns; a missing title stays blank rather than inventing one.
class CloudTask {
  final String id;
  final String title;

  const CloudTask({required this.id, this.title = ''});

  factory CloudTask.fromJson(Map json) => CloudTask(
    id: syncText(json['id']),
    title: syncText(json['title']),
  );
}

/// One transcript row. The runtime stores `role\\ncontent`, not a message
/// object. Tool calls are not in this payload, so they stay empty.
class CloudMessage {
  final String role;
  final String content;
  final bool asError;
  final List<String> files;

  const CloudMessage({
    this.role = '',
    this.content = '',
    this.asError = false,
    this.files = const [],
  });

  static CloudMessage parseLine(
    String line, {
    bool asError = false,
    List<String> files = const [],
  }) {
    final split = line.indexOf('\n');
    if (split < 0) {
      return CloudMessage(content: line, asError: asError, files: files);
    }
    return CloudMessage(
      role: line.substring(0, split),
      content: line.substring(split + 1),
      asError: asError,
      files: files,
    );
  }
}

/// `cloud_get_task` result. [turnStatus] is blank when the runtime omits it.
class CloudTaskDetail {
  final String taskId;
  final List<CloudMessage> messages;
  final String turnStatus;

  const CloudTaskDetail({
    required this.taskId,
    this.messages = const [],
    this.turnStatus = '',
  });

  factory CloudTaskDetail.fromJson(Map json) {
    final lines = syncList(json['messages']);
    final flags = syncList(json['as_error']);
    final files = syncList(json['files']);
    return CloudTaskDetail(
      taskId: syncText(json['task_id']),
      turnStatus: syncText(json['turn_status']),
      messages: [
        for (var i = 0; i < lines.length; i++)
          CloudMessage.parseLine(
            syncText(lines[i]),
            asError: i < flags.length && flags[i] == true,
            files: i < files.length && files[i] is List
                ? [for (final path in files[i] as List) syncText(path)]
                : const [],
          ),
      ],
    );
  }
}
