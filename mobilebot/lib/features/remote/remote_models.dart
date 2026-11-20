/// Wire shapes for `/api/remote-control/*`, mirroring
/// `hub/internal/remotecontrol` (Grant/Machine/State views) and
/// `guiapp/remote_control.go` (task snapshots, dir listings, file payloads).
library;

import '../../core/json_safe.dart';
import '../../l10n/gen/app_localizations.dart';

/// One desktop registered to the signed-in account.
class RcMachine {
  final String id;
  final String name;
  final bool online;

  const RcMachine({required this.id, this.name = '', this.online = false});

  String get title => name.isNotEmpty ? name : id;

  factory RcMachine.fromJson(Map json) => RcMachine(
    id: syncText(json['id']),
    name: syncText(json['name']),
    online: json['online'] == true,
  );
}

/// One remote-control grant row (`GrantView` in the Hub service). Bucket
/// meaning on `State`: `grants` are machines the caller may view/operate;
/// `pending` are outgoing invites awaiting the target's reply; `invites`
/// are incoming offers for the caller to accept; `controlled` lists the
/// caller's own machines currently being controlled.
class RcGrant {
  final String id;
  final String machineId;
  final String machineName;
  final String account;
  final String permission;
  final String status;
  final bool sameAccount;
  final bool online;
  final String fromMachine;

  const RcGrant({
    required this.id,
    this.machineId = '',
    this.machineName = '',
    this.account = '',
    this.permission = 'view',
    this.status = 'pending',
    this.sameAccount = false,
    this.online = false,
    this.fromMachine = '',
  });

  bool get isActive => status == 'active';
  bool get canOperate => isActive && permission == 'operate';

  String permissionLabel(AppLocalizations S) =>
      permission == 'operate' ? S.rcOperate : S.rcViewOnly;

  String get title {
    if (machineName.isNotEmpty) return machineName;
    if (account.isNotEmpty) return account;
    return machineId.isNotEmpty ? machineId : id;
  }

  factory RcGrant.fromJson(Map json) => RcGrant(
    id: syncText(json['id']),
    machineId: syncText(json['machine_id']),
    machineName: syncText(json['machine_name']),
    account: syncText(json['account']),
    permission: syncTextOr(json['permission'], 'view'),
    status: syncTextOr(json['status'], 'pending'),
    sameAccount: json['same_account'] == true,
    online: json['online'] == true,
    fromMachine: syncText(json['from_machine']),
  );
}

/// `GET /api/remote-control/state` snapshot.
class RemoteControlState {
  final String machineId;
  final List<RcMachine> machines;
  final List<RcGrant> grants;
  final List<RcGrant> pending;
  final List<RcGrant> invites;
  final List<RcGrant> controlled;

  const RemoteControlState({
    this.machineId = '',
    this.machines = const [],
    this.grants = const [],
    this.pending = const [],
    this.invites = const [],
    this.controlled = const [],
  });

  /// Machines I can drive — the source of the screen's selector. The Hub
  /// buckets grants where the caller is the CONTROLLER under `grants`
  /// (`controlled` is the reverse: who is controlling the caller's own
  /// machines). A mobile session is an account-level controller
  /// (controller_machine_id='') and gets its rows here the same way.
  List<RcGrant> get usableControlled =>
      grants.where((g) => g.isActive).toList();

  factory RemoteControlState.fromJson(Map<String, dynamic> json) {
    List<RcGrant> grantsOf(Object? key) => [
      for (final item in syncList(json[key]))
        if (item is Map) RcGrant.fromJson(item),
    ];
    return RemoteControlState(
      machineId: syncText(json['machine_id']),
      machines: [
        for (final item in syncList(json['machines']))
          if (item is Map) RcMachine.fromJson(item),
      ],
      grants: grantsOf('grants'),
      pending: grantsOf('pending'),
      invites: grantsOf('invites'),
      controlled: grantsOf('controlled'),
    );
  }
}

/// One task on the controlled desktop (`ProjectSearchResult` — the `id` is
/// the project path and doubles as the task key for every follow-up op).
class RemoteTask {
  final String id;
  final String name;
  final String projectPath;
  final String workingDir;
  final String workflowType;
  final String preview;
  final List<String> tags;
  final String createdAt;
  final String lastActivity;
  final bool hasOutput;
  final bool pinned;
  final bool archived;
  final String activePhase;
  final String activeStatus;
  final bool pendingReview;

  const RemoteTask({
    required this.id,
    this.name = '',
    this.projectPath = '',
    this.workingDir = '',
    this.workflowType = '',
    this.preview = '',
    this.tags = const [],
    this.createdAt = '',
    this.lastActivity = '',
    this.hasOutput = false,
    this.pinned = false,
    this.archived = false,
    this.activePhase = '',
    this.activeStatus = '',
    this.pendingReview = false,
  });

  String get title => name.isNotEmpty ? name : id;
  String get path => projectPath.isNotEmpty ? projectPath : id;

  /// Terminal states mirror the desktop panel's `remoteTerminalStatus`
  /// (`completed|cancelled|canceled|failed|interrupted|done`) — anything else
  /// while a workflow snapshot exists reads as still running.
  bool get running {
    if (activePhase.isEmpty && activeStatus.isEmpty) return false;
    final status = activeStatus.toLowerCase();
    return !(status.contains('completed') ||
        status.contains('failed') ||
        status.contains('cancel') ||
        status.contains('interrupted') ||
        status.contains('done'));
  }

  factory RemoteTask.fromJson(Map json) {
    final wf = json['active_workflow'];
    final wfMap = wf is Map ? wf : const <String, dynamic>{};
    return RemoteTask(
      id: syncText(json['id']),
      name: syncText(json['name']),
      projectPath: syncText(json['project_path']),
      workingDir: syncText(json['working_dir']),
      workflowType: syncText(json['workflow_type']),
      preview: syncText(json['preview']),
      tags: [for (final t in syncList(json['tags'])) syncText(t)],
      createdAt: syncText(json['created_at']),
      lastActivity: syncText(json['last_activity']),
      hasOutput: json['has_output'] == true,
      pinned: json['pinned'] == true,
      archived: json['archived'] == true,
      activePhase: syncText(wfMap['phase']),
      activeStatus: syncText(wfMap['status']),
      pendingReview: wfMap['pending_review'] == true,
    );
  }
}

/// One history row inside `get_task` (`ProjectConversationHistoryItem`).
/// [filePaths] are paths on the controlled machine — fetch handles for
/// `fetch_file`, not readable locations on this device.
class RemoteMessage {
  final String role;
  final String content;
  final String reasoning;
  final String resultStatus;
  final List<String> filePaths;

  const RemoteMessage({
    this.role = '',
    this.content = '',
    this.reasoning = '',
    this.resultStatus = '',
    this.filePaths = const [],
  });

  factory RemoteMessage.fromJson(Map json) => RemoteMessage(
    role: syncText(json['role']),
    content: syncText(json['content']),
    reasoning: syncText(json['reasoning_content']),
    resultStatus: syncText(json['result_status']),
    filePaths: [
      for (final p in syncList(json['local_file_paths'])) syncText(p),
    ],
  );
}

/// One produced artifact summary (`ProjectSearchArtifact`).
class RemoteArtifact {
  final String title;
  final String sourceType;
  final String sourceHint;

  /// 产物在受控机上的源路径（desktop 的 source_url）。可能是文件路径，
  /// 也可能是工程检索指针；仅文件路径能经 fetch_file 回传。
  final String sourceUrl;
  final String preview;
  final String updatedAt;

  /// Desktop says this path is an absolute regular file fetch_file will
  /// serve. Missing or false hides the download action.
  final bool downloadable;

  const RemoteArtifact({
    this.title = '',
    this.sourceType = '',
    this.sourceHint = '',
    this.sourceUrl = '',
    this.preview = '',
    this.updatedAt = '',
    this.downloadable = false,
  });

  factory RemoteArtifact.fromJson(Map json) => RemoteArtifact(
    title: syncText(json['title']),
    sourceType: syncText(json['source_type']),
    sourceHint: syncText(json['source_hint']),
    sourceUrl: syncText(json['source_url']),
    preview: syncText(json['preview']),
    updatedAt: syncText(json['updated_at']),
    downloadable: json['downloadable'] == true,
  );
}

/// `get_task` result.
class RemoteTaskDetail {
  final RemoteTask task;
  final List<RemoteMessage> messages;
  final List<RemoteArtifact> artifacts;

  const RemoteTaskDetail({
    required this.task,
    this.messages = const [],
    this.artifacts = const [],
  });

  factory RemoteTaskDetail.fromJson(Map<String, dynamic> json) {
    final task = json['task'];
    return RemoteTaskDetail(
      task: task is Map
          ? RemoteTask.fromJson(task)
          : RemoteTask(id: syncText(json['project_path'])),
      messages: [
        for (final item in syncList(json['messages']))
          if (item is Map) RemoteMessage.fromJson(item),
      ],
      artifacts: [
        for (final item in syncList(json['artifacts']))
          if (item is Map) RemoteArtifact.fromJson(item),
      ],
    );
  }
}

/// One subdirectory row in a `list_dirs` reply.
class RemoteDirEntry {
  final String name;
  final String path;

  const RemoteDirEntry({required this.name, required this.path});

  factory RemoteDirEntry.fromJson(Map json) => RemoteDirEntry(
    name: syncText(json['name']),
    path: syncText(json['path']),
  );
}

/// One `list_dirs` browse page for the new-task working-directory picker.
class RemoteDirList {
  final String path;
  final String parent;
  final String home;
  final String defaultDir;
  final List<String> roots;
  final List<RemoteDirEntry> dirs;
  final bool truncated;

  const RemoteDirList({
    this.path = '',
    this.parent = '',
    this.home = '',
    this.defaultDir = '',
    this.roots = const [],
    this.dirs = const [],
    this.truncated = false,
  });

  factory RemoteDirList.fromJson(Map json) => RemoteDirList(
    path: syncText(json['path']),
    parent: syncText(json['parent']),
    home: syncText(json['home']),
    defaultDir: syncText(json['default_dir']),
    roots: [for (final r in syncList(json['roots'])) syncText(r)],
    dirs: [
      for (final item in syncList(json['dirs']))
        if (item is Map) RemoteDirEntry.fromJson(item),
    ],
    truncated: json['truncated'] == true,
  );
}

/// `fetch_file` result — bytes travel base64 inside the op result.
class RemoteFile {
  final String name;
  final int size;
  final String dataBase64;

  const RemoteFile({this.name = '', this.size = 0, this.dataBase64 = ''});

  factory RemoteFile.fromJson(Map json) => RemoteFile(
    name: syncText(json['name']),
    size: syncInt(json['size']),
    dataBase64: syncText(json['data_base64']),
  );
}
