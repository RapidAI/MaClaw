/// Attachments the person picked for one remote-control message.
///
/// A phone cannot hand a file to a desktop path, so a picked file is staged on
/// the controlled machine's Hub material area first and the message then
/// carries its absolute path. This file only owns the local draft state and
/// the upload/progress bookkeeping; the Hub calls live in [RemoteApi].
library;

import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

/// One picked file. [disk] streams from storage; [bytes] covers a content URI
/// with no path. [path] is the absolute path on the controlled machine once
/// the upload finished.
class RemoteOutgoingFile {
  RemoteOutgoingFile({
    required this.name,
    required this.size,
    this.disk,
    this.bytes,
  });

  final String name;
  final int size;
  final File? disk;
  final Uint8List? bytes;

  String path = '';
  double progress = 0;
  bool uploading = false;
  String error = '';

  bool get staged => path.isNotEmpty;
}

/// Result of staging one file on the controlled machine.
class RemoteStagedFile {
  const RemoteStagedFile({this.path = '', this.name = '', this.error = ''});

  final String path;
  final String name;
  final String error;

  bool get ok => path.isNotEmpty && error.isEmpty;
}

double stagedProgress(int sent, int total) {
  if (total <= 0) return 0;
  return (sent / total).clamp(0.0, 1.0);
}

/// Queues files to stage, so a quick multi-pick uploads one after another
/// instead of three at once. Each entry resolves when its own upload ends.
class RemoteUploadQueue {
  Future<void> _tail = Future<void>.value();

  Future<T> add<T>(Future<T> Function() job) {
    final next = _tail.then((_) => job());
    // Keep the chain alive after a failure; the caller still sees the error.
    _tail = next.then((_) {}, onError: (_) {});
    return next;
  }
}

/// Prompt text for a message carrying attachments. The controlled desktop's
/// agent keys off these absolute paths, and the task conversation records
/// them as delivered files so the reply can be downloaded back to the phone.
String messageWithAttachments(String text, List<String> paths) {
  final clean = text.trim();
  final list = [
    for (final path in paths)
      if (path.trim().isNotEmpty) path.trim(),
  ];
  if (list.isEmpty) return clean;
  final buffer = StringBuffer();
  if (clean.isNotEmpty) {
    buffer
      ..writeln(clean)
      ..writeln();
  }
  buffer.writeln(list.length == 1 ? '附件：' : '附件（${list.length} 个）：');
  for (final path in list) {
    buffer.writeln('- $path');
  }
  return buffer.toString().trimRight();
}

/// True when the echo bubble renders as an attachment card, not prose.
bool looksLikeAttachmentMessage(String text) =>
    text.contains('\n- ') && text.contains('附件');

/// Upload id for one attachment. The Hub uses it as the stored file name
/// segment, so it must start with a letter or digit and stay under 64
/// characters: `rc` + microseconds in base 36 + a short name hash.
String remoteAttachmentUploadId(int microseconds, String name) {
  final stamp = microseconds.toRadixString(36);
  final hash = name.codeUnits.fold<int>(7, (acc, c) => (acc * 31 + c) & 0xFFFFFF);
  return 'rc$stamp${hash.toRadixString(36)}';
}

/// Where the controlled machine says a file may go for one task
/// (`get_task.attach`). Empty [taskDir], or a task no bot computer owns, means
/// the phone must not offer an attachment at all.
class RemoteAttachmentTarget {
  const RemoteAttachmentTarget({
    this.taskDir = '',
    this.botId = '',
    this.nodeId = '',
  });

  final String taskDir;
  final String botId;
  final String nodeId;

  bool get canAttach => taskDir.trim().isNotEmpty && botId.trim().isNotEmpty;

  factory RemoteAttachmentTarget.fromJson(Object? raw) {
    if (raw is! Map) return const RemoteAttachmentTarget();
    String text(Object? value) => value == null ? '' : '$value'.trim();
    return RemoteAttachmentTarget(
      taskDir: text(raw['task_dir']),
      botId: text(raw['bot_id']),
      nodeId: text(raw['node_id']),
    );
  }
}
