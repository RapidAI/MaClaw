import 'package:flutter/material.dart';

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

/// One task row from `cloud_list_tasks`. [status] is the task's latest turn
/// status (blank before the first send) and [updatedAt] the last transcript
/// activity; both are new server fields an old runtime omits.
class CloudTask {
  final String id;
  final String title;
  final String status;
  final String updatedAt;

  const CloudTask({
    required this.id,
    this.title = '',
    this.status = '',
    this.updatedAt = '',
  });

  factory CloudTask.fromJson(Map json) => CloudTask(
    id: syncText(json['id']),
    title: syncText(json['title']),
    status: syncText(json['status']),
    updatedAt: syncText(json['updated_at']),
  );
}

/// List-row state chip for a cloud task. Mirrors the remote palette so both
/// task lists read the same. `null` = no chip (no turn yet). Takes the
/// scheme directly so pure unit tests can drive it without a widget tree.
({String label, Color color})? cloudTaskBadge(
  AppLocalizations s,
  ColorScheme scheme,
  CloudTask task,
) {
  return switch (task.status) {
    cloudTurnRunning => (label: s.cloudTaskRunning, color: scheme.primary),
    cloudTurnFinished => (
      label: s.cloudTaskFinished,
      color: const Color(0xFF6AA84F),
    ),
    cloudTurnCancelled => (
      label: s.cloudTaskCancelled,
      color: scheme.onSurfaceVariant,
    ),
    cloudTurnInterrupted => (
      label: s.cloudTaskInterrupted,
      color: const Color(0xFFD97B42),
    ),
    _ => null,
  };
}

/// One persisted process row of a turn (`turn_events`): progress lines the
/// round streamed and the sealed thinking text.
class CloudEvent {
  final int seq;
  final String kind;
  final String text;

  const CloudEvent({this.seq = 0, this.kind = '', this.text = ''});

  factory CloudEvent.fromJson(Map json) => CloudEvent(
    seq: syncInt(json['seq']),
    kind: syncText(json['kind']),
    text: syncText(json['text']),
  );
}

/// One transcript row. The runtime stores `role\\ncontent`, not a message
/// object. Tool calls are not in this payload; process rows (thinking,
/// progress) ride in [events], attached by the runtime to the last row of
/// their turn.
class CloudMessage {
  final String role;
  final String content;
  final bool asError;
  final List<String> files;
  final List<CloudEvent> events;
  final String requestId;

  const CloudMessage({
    this.role = '',
    this.content = '',
    this.asError = false,
    this.files = const [],
    this.events = const [],
    this.requestId = '',
  });

  static CloudMessage parseLine(
    String line, {
    bool asError = false,
    List<String> files = const [],
    List<CloudEvent> events = const [],
    String requestId = '',
  }) {
    final split = line.indexOf('\n');
    if (split < 0) {
      return CloudMessage(
        content: line,
        asError: asError,
        files: files,
        events: events,
        requestId: requestId,
      );
    }
    return CloudMessage(
      role: line.substring(0, split),
      content: line.substring(split + 1),
      asError: asError,
      files: files,
      events: events,
      requestId: requestId,
    );
  }

  /// Stable identity for pagination merges. Legacy rows written before the
  /// runtime stamped request ids fall back to their text.
  String get key =>
      requestId.isNotEmpty ? '$requestId|$role' : 'raw|$role|$content';

  Iterable<CloudEvent> get progressRows =>
      events.where((event) => event.kind == 'progress');

  String get thinkingText {
    for (final event in events) {
      if (event.kind == 'thinking' && event.text.isNotEmpty) return event.text;
    }
    return '';
  }
}

/// `cloud_get_task` result. [turnStatus] is blank when the runtime omits it.
/// [messageTotal] is the full row count behind the returned window; an old
/// runtime omits it and the window is then everything.
class CloudTaskDetail {
  final String taskId;
  final List<CloudMessage> messages;
  final String turnStatus;
  final int messageTotal;

  const CloudTaskDetail({
    required this.taskId,
    this.messages = const [],
    this.turnStatus = '',
    this.messageTotal = 0,
  });

  factory CloudTaskDetail.fromJson(Map json) {
    final lines = syncList(json['messages']);
    final flags = syncList(json['as_error']);
    final files = syncList(json['files']);
    final requestIDs = syncList(json['request_ids']);
    final events = syncList(json['events']);
    List<CloudEvent> eventsOf(int index) {
      if (index >= events.length || events[index] is! List) return const [];
      return [
        for (final item in events[index] as List)
          if (item is Map) CloudEvent.fromJson(item),
      ];
    }

    return CloudTaskDetail(
      taskId: syncText(json['task_id']),
      turnStatus: syncText(json['turn_status']),
      messageTotal: json.containsKey('message_total')
          ? syncInt(json['message_total'])
          : lines.length,
      messages: [
        for (var i = 0; i < lines.length; i++)
          CloudMessage.parseLine(
            syncText(lines[i]),
            asError: i < flags.length && flags[i] == true,
            files: i < files.length && files[i] is List
                ? [for (final path in files[i] as List) syncText(path)]
                : const [],
            requestId: i < requestIDs.length
                ? syncText(requestIDs[i])
                : '',
            events: eventsOf(i),
          ),
      ],
    );
  }
}
