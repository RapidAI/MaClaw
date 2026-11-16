import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';

import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';

import '../bots/bots_controller.dart' show failureMessage;
import '../bots/widgets.dart' show linkifyMarkdownUrls, openExternalUrl;
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
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('远控只能发送任务消息，不能运行本机命令')));
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
        unawaited(
          _scrollCtrl.animateTo(
            _scrollCtrl.position.maxScrollExtent,
            duration: const Duration(milliseconds: 200),
            curve: Curves.easeOut,
          ),
        );
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
      final file = await api.fetchFile(widget.grant.id, widget.taskPath, path);
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
      final target = '${dir.path}${Platform.pathSeparator}remote-$unique';
      await File(target).writeAsBytes(bytes, flush: true);
      if (!mounted) return;
      await SharePlus.instance.share(ShareParams(files: [XFile(target)]));
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
            Text(
              title,
              style: theme.textTheme.titleMedium,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
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
                  child: Text(detail?.task.pinned == true ? '取消置顶' : '置顶'),
                ),
                const PopupMenuItem(value: 'stop', child: Text('停止任务')),
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
                  FilledButton.tonal(onPressed: _load, child: const Text('重试')),
                ],
              ),
            )
          : Column(
              children: [
                if (detail != null && detail.task.running)
                  Container(
                    width: double.infinity,
                    color: theme.colorScheme.primaryContainer.withValues(
                      alpha: .5,
                    ),
                    padding: const EdgeInsets.symmetric(
                      horizontal: 14,
                      vertical: 8,
                    ),
                    child: Row(
                      children: [
                        SizedBox(
                          width: 12,
                          height: 12,
                          child: CircularProgressIndicator(
                            strokeWidth: 1.5,
                            color: theme.colorScheme.primary,
                          ),
                        ),
                        const SizedBox(width: 8),
                        Expanded(
                          child: Text(
                            detail.task.activePhase.isNotEmpty
                                ? '运行中 · ${detail.task.activePhase}'
                                : '运行中',
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.primary,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                Expanded(
                  child: ListView(
                    controller: _scrollCtrl,
                    padding: const EdgeInsets.all(12),
                    children: [
                      if (detail == null || detail.messages.isEmpty)
                        Padding(
                          padding: const EdgeInsets.symmetric(vertical: 24),
                          child: Center(
                            child: Text(
                              '还没有对话记录',
                              style: theme.textTheme.bodyMedium?.copyWith(
                                color: theme.colorScheme.onSurfaceVariant,
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
                      if (detail != null && detail.artifacts.isNotEmpty) ...[
                        const SizedBox(height: 16),
                        Text('产出物', style: theme.textTheme.titleSmall),
                        const SizedBox(height: 8),
                        for (final artifact in detail.artifacts)
                          Card(
                            child: ListTile(
                              dense: true,
                              leading: const Icon(Icons.description_outlined),
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
                                    child: CircularProgressIndicator(
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
    return message.role == 'user'
        ? _UserBubble(text: message.content)
        : _AssistantTurn(
            message: message,
            onFetch: onFetch,
            downloading: downloading,
          );
  }
}

/// 我的消息 — 右侧气泡。
class _UserBubble extends StatelessWidget {
  final String text;

  const _UserBubble({required this.text});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Align(
      alignment: Alignment.centerRight,
      child: Container(
        margin: const EdgeInsets.symmetric(vertical: 4),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
        constraints: BoxConstraints(
          maxWidth: MediaQuery.of(context).size.width * 0.8,
        ),
        decoration: BoxDecoration(
          color: theme.colorScheme.primary,
          borderRadius: const BorderRadius.only(
            topLeft: Radius.circular(18),
            topRight: Radius.circular(18),
            bottomLeft: Radius.circular(18),
            bottomRight: Radius.circular(4),
          ),
        ),
        child: SelectableText(
          text,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onPrimary,
            height: 1.45,
          ),
        ),
      ),
    );
  }
}

/// 助手回复 — 豆包式：左侧头像 + 无气泡排版，思考过程可折叠，
/// 正文按 Markdown 渲染，产出文件以卡片列出可点击下载。
class _AssistantTurn extends StatelessWidget {
  final RemoteMessage message;
  final Future<void> Function(String path) onFetch;
  final bool downloading;

  const _AssistantTurn({
    required this.message,
    required this.onFetch,
    required this.downloading,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          CircleAvatar(
            radius: 15,
            backgroundColor: theme.colorScheme.primaryContainer,
            child: Icon(
              Icons.smart_toy_outlined,
              size: 17,
              color: theme.colorScheme.onPrimaryContainer,
            ),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (message.reasoning.isNotEmpty) ...[
                  _ReasoningBlock(text: message.reasoning),
                  const SizedBox(height: 8),
                ],
                if (message.content.isNotEmpty)
                  MarkdownBody(
                    data: linkifyMarkdownUrls(message.content),
                    selectable: true,
                    onTapLink: (text, href, title) {
                      if (href != null) openExternalUrl(href);
                    },
                    styleSheet: MarkdownStyleSheet.fromTheme(theme).copyWith(
                      p: theme.textTheme.bodyMedium?.copyWith(height: 1.5),
                      a: theme.textTheme.bodyMedium?.copyWith(
                        color: theme.colorScheme.primary,
                        fontWeight: FontWeight.w600,
                        decoration: TextDecoration.underline,
                      ),
                      code: theme.textTheme.bodySmall?.copyWith(
                        fontFamily: 'monospace',
                        backgroundColor:
                            theme.colorScheme.surfaceContainerHighest,
                      ),
                      codeblockDecoration: BoxDecoration(
                        color: theme.colorScheme.surfaceContainerHighest,
                        borderRadius: BorderRadius.circular(8),
                      ),
                    ),
                  ),
                if (message.resultStatus.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 8),
                    child: _StatusChip(status: message.resultStatus),
                  ),
                if (message.filePaths.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  for (final path in message.filePaths)
                    _FileCard(
                      path: path,
                      downloading: downloading,
                      onTap: () => onFetch(path),
                    ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// 思考过程 — 默认收起，展开看推理细节。
class _ReasoningBlock extends StatelessWidget {
  final String text;

  const _ReasoningBlock({required this.text});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: .55),
        borderRadius: BorderRadius.circular(10),
      ),
      child: ExpansionTile(
        dense: true,
        tilePadding: const EdgeInsets.symmetric(horizontal: 10),
        childrenPadding: const EdgeInsets.fromLTRB(10, 0, 10, 10),
        leading: Icon(
          Icons.psychology_outlined,
          size: 16,
          color: theme.colorScheme.onSurfaceVariant,
        ),
        title: Text(
          '思考过程',
          style: theme.textTheme.labelMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        iconColor: theme.colorScheme.onSurfaceVariant,
        collapsedIconColor: theme.colorScheme.onSurfaceVariant,
        children: [
          SelectableText(
            text,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
              fontStyle: FontStyle.italic,
              height: 1.5,
            ),
          ),
        ],
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  final String status;

  const _StatusChip({required this.status});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final failed = status.contains('失败') || status.contains('error');
    final color = failed
        ? theme.colorScheme.error
        : theme.colorScheme.onSurfaceVariant;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: color.withValues(alpha: .12),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            failed ? Icons.error_outline : Icons.check_circle_outline,
            size: 12,
            color: color,
          ),
          const SizedBox(width: 4),
          Text(
            status,
            style: theme.textTheme.labelSmall?.copyWith(color: color),
          ),
        ],
      ),
    );
  }
}

/// 产出文件卡片 — 图标 + 文件名 + 下载指示。
class _FileCard extends StatelessWidget {
  final String path;
  final bool downloading;
  final VoidCallback onTap;

  const _FileCard({
    required this.path,
    required this.downloading,
    required this.onTap,
  });

  IconData _iconFor(String name) {
    final dot = name.lastIndexOf('.');
    if (dot < 0) return Icons.insert_drive_file_outlined;
    return switch (name.substring(dot + 1).toLowerCase()) {
      'png' ||
      'jpg' ||
      'jpeg' ||
      'gif' ||
      'webp' ||
      'bmp' => Icons.image_outlined,
      'mp4' || 'mov' || 'avi' || 'mkv' || 'webm' => Icons.videocam_outlined,
      'mp3' || 'wav' || 'm4a' || 'flac' => Icons.audio_file_outlined,
      'zip' || 'tar' || 'gz' || '7z' || 'rar' => Icons.folder_zip_outlined,
      'pdf' => Icons.picture_as_pdf_outlined,
      'md' || 'txt' || 'log' => Icons.description_outlined,
      'dart' ||
      'go' ||
      'js' ||
      'ts' ||
      'py' ||
      'java' ||
      'c' ||
      'cpp' ||
      'h' ||
      'rs' ||
      'json' ||
      'yaml' ||
      'yml' ||
      'xml' ||
      'html' ||
      'css' => Icons.code,
      _ => Icons.insert_drive_file_outlined,
    };
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final name = path.split(RegExp(r'[\\/]')).last;
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: InkWell(
        borderRadius: BorderRadius.circular(10),
        onTap: downloading ? null : onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
          decoration: BoxDecoration(
            border: Border.all(color: theme.colorScheme.outlineVariant),
            borderRadius: BorderRadius.circular(10),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(_iconFor(name), size: 18, color: theme.colorScheme.primary),
              const SizedBox(width: 8),
              Flexible(
                child: Text(
                  name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodySmall?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              const SizedBox(width: 10),
              downloading
                  ? const SizedBox(
                      width: 14,
                      height: 14,
                      child: CircularProgressIndicator(strokeWidth: 1.5),
                    )
                  : Icon(
                      Icons.download_outlined,
                      size: 16,
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
            ],
          ),
        ),
      ),
    );
  }
}
