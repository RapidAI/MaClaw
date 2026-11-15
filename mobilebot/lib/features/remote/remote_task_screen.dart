import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';

import '../bots/bots_controller.dart' show failureMessage;
import 'remote_models.dart';
import 'remote_screen.dart' show remoteApiProvider, remoteTasksProvider;

/// 远程任务详情 — get_task 轮询（3s，同桌面 watch_task 的进展效果），
/// 消息时间线、产出物下载/分享，按授权档位放开操作动作。
class RemoteTaskScreen extends ConsumerStatefulWidget {
  final RcGrant grant;
  final String taskPath;
  final String taskName;

  const RemoteTaskScreen({
    super.key,
    required this.grant,
    required this.taskPath,
    this.taskName = '',
  });

  @override
  ConsumerState<RemoteTaskScreen> createState() => _RemoteTaskScreenState();
}

class _RemoteTaskScreenState extends ConsumerState<RemoteTaskScreen> {
  final _inputCtrl = TextEditingController();
  final _scrollCtrl = ScrollController();
  Timer? _pollTimer;
  RemoteTaskDetail? _detail;
  String _error = '';
  bool _loading = true;
  bool _sending = false;
  bool _downloading = false;
  bool _pollInFlight = false;

  late final AppLifecycleListener _lifecycle;

  @override
  void initState() {
    super.initState();
    _load();
    _startPoll();
    // Stop polling while the app is backgrounded — an unseen screen must not
    // keep the relay busy, and resuming reloads immediately so the user
    // never stares at a stale snapshot.
    _lifecycle = AppLifecycleListener(
      onStateChange: (state) {
        if (state == AppLifecycleState.resumed) {
          _load();
          _startPoll();
        } else if (state == AppLifecycleState.paused ||
            state == AppLifecycleState.hidden ||
            state == AppLifecycleState.detached) {
          _pollTimer?.cancel();
          _pollTimer = null;
        }
      },
    );
  }

  void _startPoll() {
    // The desktop streams watch_task over Wails events; on mobile a short
    // poll of get_task gives the same moving picture while this screen is
    // open. Cheap enough: one op call every 3s per open detail.
    _pollTimer ??= Timer.periodic(const Duration(seconds: 3), (_) {
      // Skip this tick while a fetch is still in flight; a slow hub must not
      // stack overlapping get_task calls. User actions bypass the guard so
      // their refresh is never deferred behind a pending poll.
      if (!_pollInFlight) _load();
    });
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    _pollTimer?.cancel();
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final api = ref.read(remoteApiProvider);
    if (api == null) return;
    _pollInFlight = true;
    try {
      final detail = await api.getTask(widget.grant.id, widget.taskPath);
      if (!mounted) return;
      setState(() {
        _detail = detail;
        _loading = false;
        _error = '';
      });
    } on Object catch (error) {
      if (!mounted) return;
      // A transient poll failure keeps the last good snapshot on screen —
      // only the first load may show the full-page error.
      if (_detail == null) {
        setState(() {
          _loading = false;
          _error = failureMessage(error);
        });
      }
    } finally {
      _pollInFlight = false;
    }
  }

  Future<void> _send() async {
    final text = _inputCtrl.text.trim();
    if (text.isEmpty || _sending) return;
    if (text.startsWith('/')) {
      // Mirrors the controlled side's remoteControlMessageError — slash
      // commands would run locally on that machine, so reject before the
      // round-trip instead of surfacing the server error.
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('远控只能发送任务消息，不能运行本机命令')),
      );
      return;
    }
    final api = ref.read(remoteApiProvider);
    if (api == null) return;
    setState(() => _sending = true);
    try {
      await api.sendMessage(widget.grant.id, widget.taskPath, text);
      if (!mounted) return;
      _inputCtrl.clear();
      await _load();
      ref.invalidate(remoteTasksProvider(widget.grant.id));
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !_scrollCtrl.hasClients) return;
        unawaited(_scrollCtrl.animateTo(
          _scrollCtrl.position.maxScrollExtent,
          duration: const Duration(milliseconds: 200),
          curve: Curves.easeOut,
        ));
      });
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  Future<void> _rename() async {
    final ctrl = TextEditingController(
      text: _detail?.task.name ?? widget.taskName,
    );
    try {
      final name = await showDialog<String>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('重命名任务'),
          content: TextField(
            controller: ctrl,
            autofocus: true,
            decoration: const InputDecoration(border: OutlineInputBorder()),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, ctrl.text.trim()),
              child: const Text('保存'),
            ),
          ],
        ),
      );
      if (name == null || name.isEmpty || !mounted) return;
      final api = ref.read(remoteApiProvider);
      if (api == null) return;
      await api.renameTask(widget.grant.id, widget.taskPath, name);
      await _load();
      ref.invalidate(remoteTasksProvider(widget.grant.id));
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    } finally {
      ctrl.dispose();
    }
  }

  Future<void> _pin() async {
    final api = ref.read(remoteApiProvider);
    if (api == null || _detail == null) return;
    try {
      await api.pinTask(
        widget.grant.id,
        widget.taskPath,
        !_detail!.task.pinned,
      );
      await _load();
      ref.invalidate(remoteTasksProvider(widget.grant.id));
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    }
  }

  Future<void> _stop() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('停止任务'),
        content: const Text('确定要停止这台机器上正在运行的任务吗？'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('停止'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    final api = ref.read(remoteApiProvider);
    if (api == null) return;
    try {
      await api.stopTask(widget.grant.id, widget.taskPath);
      await _load();
      ref.invalidate(remoteTasksProvider(widget.grant.id));
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(failureMessage(error))));
      }
    }
  }

  /// fetch_file → temp file → system share sheet (download to other apps).
  Future<void> _fetchAndShare(String path) async {
    final api = ref.read(remoteApiProvider);
    if (api == null || _downloading) return;
    setState(() => _downloading = true);
    try {
      final file = await api.fetchFile(
        widget.grant.id,
        widget.taskPath,
        path,
      );
      if (file.dataBase64.isEmpty) {
        throw StateError('远端没有返回文件内容');
      }
      final bytes = base64Decode(file.dataBase64);
      final dir = await getTemporaryDirectory();
      final name = file.name.trim().isNotEmpty
          ? file.name.trim()
          : path.split(RegExp(r'[\\/]')).last;
      final safe = name.replaceAll(RegExp(r'[\\/:*?"<>|]'), '_');
      // A task can produce the same file name across turns — keep each
      // download instead of silently overwriting the previous share.
      final stamp = DateTime.now().millisecondsSinceEpoch;
      final dot = safe.lastIndexOf('.');
      final unique = dot > 0
          ? '${safe.substring(0, dot)}-$stamp${safe.substring(dot)}'
          : '$safe-$stamp';
      final target =
          '${dir.path}${Platform.pathSeparator}remote-$unique';
      await File(target).writeAsBytes(bytes, flush: true);
      if (!mounted) return;
      await SharePlus.instance.share(
        ShareParams(files: [XFile(target)]),
      );
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('下载失败：${failureMessage(error)}')),
        );
      }
    } finally {
      if (mounted) setState(() => _downloading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final detail = _detail;
    final canOperate = widget.grant.canOperate;
    final title = detail?.task.title.isNotEmpty == true
        ? detail!.task.title
        : (widget.taskName.isNotEmpty ? widget.taskName : '远程任务');
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title,
                style: theme.textTheme.titleMedium,
                maxLines: 1,
                overflow: TextOverflow.ellipsis),
            Text(
              '${widget.grant.title} · ${widget.grant.permissionLabel}',
              style: theme.textTheme.labelSmall,
            ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: '刷新',
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
          if (canOperate)
            PopupMenuButton<String>(
              icon: const Icon(Icons.more_vert),
              onSelected: (value) {
                switch (value) {
                  case 'rename':
                    _rename();
                  case 'pin':
                    _pin();
                  case 'stop':
                    _stop();
                }
              },
              itemBuilder: (_) => [
                const PopupMenuItem(value: 'rename', child: Text('重命名')),
                PopupMenuItem(
                  value: 'pin',
                  child: Text(
                    detail?.task.pinned == true ? '取消置顶' : '置顶',
                  ),
                ),
                const PopupMenuItem(
                  value: 'stop',
                  child: Text('停止任务'),
                ),
              ],
            ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error.isNotEmpty
              ? Center(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(_error),
                      const SizedBox(height: 12),
                      FilledButton.tonal(
                        onPressed: _load,
                        child: const Text('重试'),
                      ),
                    ],
                  ),
                )
              : Column(
                  children: [
                    if (detail != null && detail.task.running)
                      Container(
                        width: double.infinity,
                        color: theme.colorScheme.surfaceContainerHighest,
                        padding: const EdgeInsets.symmetric(
                          horizontal: 14,
                          vertical: 6,
                        ),
                        child: Text(
                          '运行中 · ${detail.task.activePhase}',
                          style: theme.textTheme.bodySmall,
                        ),
                      ),
                    Expanded(
                      child: ListView(
                        controller: _scrollCtrl,
                        padding: const EdgeInsets.all(12),
                        children: [
                          if (detail == null || detail.messages.isEmpty)
                            Padding(
                              padding:
                                  const EdgeInsets.symmetric(vertical: 24),
                              child: Center(
                                child: Text(
                                  '还没有对话记录',
                                  style:
                                      theme.textTheme.bodyMedium?.copyWith(
                                    color:
                                        theme.colorScheme.onSurfaceVariant,
                                  ),
                                ),
                              ),
                            )
                          else
                            for (final message in detail.messages)
                              _MessageRow(
                                message: message,
                                onFetch: _fetchAndShare,
                                downloading: _downloading,
                              ),
                          if (detail != null &&
                              detail.artifacts.isNotEmpty) ...[
                            const SizedBox(height: 16),
                            Text('产出物', style: theme.textTheme.titleSmall),
                            const SizedBox(height: 8),
                            for (final artifact in detail.artifacts)
                              Card(
                                child: ListTile(
                                  dense: true,
                                  leading: const Icon(
                                    Icons.description_outlined,
                                  ),
                                  title: Text(
                                    artifact.title.isNotEmpty
                                        ? artifact.title
                                        : '产出物',
                                  ),
                                  subtitle: artifact.preview.isNotEmpty
                                      ? Text(
                                          artifact.preview,
                                          maxLines: 2,
                                          overflow: TextOverflow.ellipsis,
                                        )
                                      : null,
                                ),
                              ),
                          ],
                        ],
                      ),
                    ),
                    if (canOperate)
                      SafeArea(
                        child: Padding(
                          padding: const EdgeInsets.fromLTRB(12, 4, 12, 12),
                          child: Row(
                            children: [
                              Expanded(
                                child: TextField(
                                  controller: _inputCtrl,
                                  minLines: 1,
                                  maxLines: 4,
                                  decoration: const InputDecoration(
                                    hintText: '给这台机器的任务发消息…',
                                    border: OutlineInputBorder(),
                                    isDense: true,
                                  ),
                                  onSubmitted: (_) => _send(),
                                ),
                              ),
                              const SizedBox(width: 8),
                              IconButton.filled(
                                onPressed: _sending ? null : _send,
                                icon: _sending
                                    ? const SizedBox(
                                        width: 16,
                                        height: 16,
                                        child:
                                            CircularProgressIndicator(
                                          strokeWidth: 2,
                                        ),
                                      )
                                    : const Icon(Icons.send, size: 18),
                              ),
                            ],
                          ),
                        ),
                      ),
                  ],
                ),
    );
  }
}

class _MessageRow extends StatelessWidget {
  final RemoteMessage message;
  final Future<void> Function(String path) onFetch;
  final bool downloading;

  const _MessageRow({
    required this.message,
    required this.onFetch,
    required this.downloading,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isUser = message.role == 'user';
    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        margin: const EdgeInsets.symmetric(vertical: 4),
        padding: const EdgeInsets.all(10),
        constraints: BoxConstraints(
          maxWidth: MediaQuery.of(context).size.width * 0.85,
        ),
        decoration: BoxDecoration(
          color: isUser
              ? theme.colorScheme.primaryContainer
              : theme.colorScheme.surfaceContainerLow,
          borderRadius: BorderRadius.circular(12),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (message.reasoning.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: Text(
                  message.reasoning,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                    fontStyle: FontStyle.italic,
                  ),
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            Text(message.content, style: theme.textTheme.bodyMedium),
            if (message.resultStatus.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Text(
                  message.resultStatus,
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
            if (message.filePaths.isNotEmpty) ...[
              const SizedBox(height: 6),
              Wrap(
                spacing: 6,
                runSpacing: 6,
                children: [
                  for (final path in message.filePaths)
                    ActionChip(
                      avatar: downloading
                          ? const SizedBox(
                              width: 12,
                              height: 12,
                              child: CircularProgressIndicator(
                                strokeWidth: 1.5,
                              ),
                            )
                          : const Icon(Icons.download, size: 14),
                      label: Text(
                        path.split(RegExp(r'[\\/]')).last,
                        style: const TextStyle(fontSize: 12),
                      ),
                      onPressed: () => onFetch(path),
                    ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}
