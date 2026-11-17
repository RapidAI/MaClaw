import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart' show ScrollDirection;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';

import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';

import '../bots/bots_controller.dart' show failureMessage;
import '../bots/widgets.dart' show linkifyMarkdownUrls, openExternalUrl;
import 'remote_api.dart';
import 'remote_models.dart';
import 'remote_screen.dart' show remoteApiProvider, remoteTasksProvider;
import 'viewer_reducer.dart';

/// 远程任务详情。先 watch_task（stream_proto 2）再订阅账号 SSE。
/// 回复里的 cursor 是水位；SSE 出现新缺口时再问一次 watch_task，
/// 因为账号流的补帧没有 live。这次回复若还没盖住洞里已经看到的光标，
/// 再问一次；水位没往前走就停。补读还没结束时又开的洞，等这次读完再问。
/// SSE 打不开时才退回 3 秒 get_task。
/// 消息时间线、产出物下载/分享，按授权档位放开操作动作。
/// 编码运行时恢复只展示，不另发操作。
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
  Timer? _slowTimer;
  RemoteTaskDetail? _detail;
  String _error = '';
  bool _loading = true;
  bool _sending = false;
  bool _downloading = false;
  bool _pollInFlight = false;
  bool _paused = false;
  bool _degraded = false;
  bool _rewatching = false;
  bool _gapFollow = false;
  bool _gapEdgeDeferred = false;
  bool _stickToEnd = true;
  int _pinning = 0;
  String _revealStamp = '';
  int _gapSeen = 0;
  int _fillGen = 0;
  int _streamGen = 0;
  ViewerReduceState _live = emptyViewerState();
  String _recovery = '';
  AccountEventStream? _account;
  StreamSubscription<Map<String, dynamic>>? _liveSub;

  late final AppLifecycleListener _lifecycle;

  @override
  void initState() {
    super.initState();
    _load();
    _openStream();
    // Stop the relay while the app is backgrounded. Resuming opens the
    // stream again and only polls if that open fails.
    _lifecycle = AppLifecycleListener(
      onStateChange: (state) {
        if (state == AppLifecycleState.resumed) {
          _paused = false;
          _load();
          _openStream();
        } else if (state == AppLifecycleState.paused ||
            state == AppLifecycleState.hidden ||
            state == AppLifecycleState.detached) {
          _paused = true;
          _stopRefresh();
          unawaited(_closeStream());
        }
      },
    );
  }

  Future<void> _openStream() async {
    final gen = ++_streamGen;
    _fillGen++;
    _rewatching = false;
    _gapEdgeDeferred = false;
    _gapSeen = 0;
    _gapFollow = false;
    await _closeStream(keepGeneration: true);
    if (!mounted || _paused || gen != _streamGen) return;
    final api = ref.read(remoteApiProvider);
    if (api == null) {
      _startPoll();
      return;
    }
    try {
      final reply = await api.watchTask(widget.grant.id, widget.taskPath);
      if (!mounted || _paused || gen != _streamGen) return;
      final proto = reply['stream_proto'];
      if (proto == 1 || proto == '1') {
        setState(() => _degraded = true);
      }
      final seeded = seedViewerFromWatchReply(reply, _live);
      if (!mounted || _paused || gen != _streamGen) return;
      if (!identical(seeded, _live)) {
        setState(() => _live = seeded);
      }
      final opened = await api.openAccountStream(
        widget.grant.id,
        widget.taskPath,
        afterCursor: seeded.last,
      );
      if (!mounted || _paused || gen != _streamGen) {
        await opened.close();
        return;
      }
      _startSlowRefresh();
      _account = opened;
      _liveSub = opened.events.listen(
        (event) {
          if (!mounted || _paused || gen != _streamGen) return;
          final prev = _live;
          final next = reduceViewerEvent(prev, event);
          final recovery = _recoveryText(event);
          final openedGap = next.gap && !prev.gap;
          if (next.gap) {
            final cursor = _eventCursor(event);
            if (cursor > _gapSeen) _gapSeen = cursor;
          }
          if (openedGap) _gapFollow = true;
          if (!identical(next, prev) || recovery.isNotEmpty) {
            setState(() {
              _live = next;
              if (recovery.isNotEmpty) _recovery = recovery;
            });
          }
          if (openedGap) unawaited(_fillViewerGap(gen));
        },
        onError: (_) {
          if (mounted && !_paused) _startPoll();
        },
        onDone: () {
          if (mounted && !_paused && gen == _streamGen) _startPoll();
        },
      );
    } catch (_) {
      if (mounted && !_paused && gen == _streamGen) _startPoll();
    }
  }

  /// A new gap means this SSE missed a cursor. The account broker's resync
  /// frame has no live body, so the turn has to be read again from watch_task.
  /// One ask per rising edge: later gapped frames of the same hole only raise
  /// [_gapSeen]. If that read moves the watermark but stops short of a cursor
  /// already dropped, ask once more. A reply that does not move it stops.
  /// A hole that opens while this read is still in flight cannot start a
  /// second one; it is asked once this read finishes, if it is still owed.
  Future<void> _fillViewerGap(int gen) async {
    if (_degraded || _paused || !mounted || gen != _streamGen) return;
    if (_rewatching) {
      _gapEdgeDeferred = true;
      return;
    }
    final api = ref.read(remoteApiProvider);
    if (api == null) return;
    _rewatching = true;
    final fillGen = ++_fillGen;
    _gapEdgeDeferred = false;
    try {
      final advanced = await _readWatch(api, gen);
      if (gen == _streamGen && !advanced) {
        _gapFollow = false;
      } else if (gen == _streamGen &&
          (_gapSeen == 0 || _live.last >= _gapSeen)) {
        _gapSeen = 0;
        _gapFollow = false;
      } else if (gen == _streamGen && _gapFollow) {
        _gapFollow = false;
        final again = await _readWatch(api, gen);
        if (again &&
            gen == _streamGen &&
            (_gapSeen == 0 || _live.last >= _gapSeen)) {
          _gapSeen = 0;
          _gapFollow = false;
        }
      }
    } catch (_) {
      // A failed read leaves the gap set. The next time it clears and
      // opens again is the next ask, so a down hub cannot spin.
    } finally {
      if (fillGen == _fillGen) _rewatching = false;
    }
    if (fillGen != _fillGen ||
        gen != _streamGen ||
        !mounted ||
        _paused ||
        _degraded ||
        !_gapEdgeDeferred) {
      return;
    }
    _gapEdgeDeferred = false;
    if (_live.gap || _gapSeen > _live.last) {
      await _fillViewerGap(gen);
    }
  }

  /// Applies one watch_task onto the open round. True when its watermark moved.
  Future<bool> _readWatch(RemoteControlApi api, int gen) async {
    final before = _live.last;
    final reply = await api.watchTask(widget.grant.id, widget.taskPath);
    if (!mounted || _paused || gen != _streamGen) return false;
    final seeded = seedViewerFromWatchReply(reply, _live);
    if (!mounted || _paused || gen != _streamGen) return false;
    if (!identical(seeded, _live)) {
      setState(() => _live = seeded);
    }
    return seeded.last > before;
  }

  int _eventCursor(Map<String, dynamic> event) {
    final value = event['cursor'];
    if (value is num && value > 0) return value.toInt();
    return 0;
  }

  Future<void> _closeStream({bool keepGeneration = false}) async {
    if (!keepGeneration) _streamGen++;
    final sub = _liveSub;
    _liveSub = null;
    final account = _account;
    _account = null;
    await sub?.cancel();
    if (account != null) await account.close();
  }

  String _recoveryText(Map<String, dynamic> event) {
    final terminal = event['terminal'];
    if (terminal is! Map) return '';
    final recovery = terminal['coding_runtime_recovery'];
    if (recovery is Map) {
      final summary = recovery['summary'];
      return summary is String ? summary : '';
    }
    return recovery is String ? recovery : '';
  }

  void _stopRefresh() {
    _pollTimer?.cancel();
    _pollTimer = null;
    _slowTimer?.cancel();
    _slowTimer = null;
  }

  void _startPoll() {
    // SSE did not open. A 3s get_task is the only moving picture.
    _slowTimer?.cancel();
    _slowTimer = null;
    _pollTimer ??= Timer.periodic(const Duration(seconds: 3), (_) {
      if (!_pollInFlight) _load();
    });
  }

  void _startSlowRefresh() {
    // The stream carries the live round. A 15s get_task only refreshes
    // workflow state and file paths, not a second copy of the tokens.
    _pollTimer?.cancel();
    _pollTimer = null;
    _slowTimer ??= Timer.periodic(const Duration(seconds: 15), (_) {
      if (!_pollInFlight) _load();
    });
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    _stopRefresh();
    unawaited(_closeStream());
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
      // Sending is a request to watch the reply, even if the reader had
      // parked higher in the history.
      _stickToEnd = true;
      _revealTail();
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

  /// User drag and the settle after a fling. Content growth and our own
  /// tail jump do not count: those used to clear the follow latch before
  /// the jump ran, so a streaming reply stayed above the fold.
  bool _onScrollNotice(ScrollNotification notice) {
    if (_pinning > 0) return false;
    final dragged =
        notice is ScrollUpdateNotification && notice.dragDetails != null;
    final settled =
        notice is UserScrollNotification &&
        notice.direction == ScrollDirection.idle;
    if (!dragged && !settled) return false;
    final metrics = notice.metrics;
    _stickToEnd = metrics.maxScrollExtent - metrics.pixels < 160;
    return false;
  }

  String _shownLiveBody() {
    final live = viewerShownAnswer(_live.body, _live.visible);
    final messages = _detail?.messages;
    if (live.isEmpty || messages == null || messages.isEmpty) return live;
    if (messages.last.role != 'assistant') return live;
    final history = messages.last.content;
    if (_historyIsCappedPrefix(history, live)) return live;
    final prefix = _trimmedHistoryPrefix(history);
    if (prefix == null || prefix.startsWith(live) || live.startsWith(prefix)) {
      return live;
    }
    if (!_trimmedTailIsThisRound()) return live;
    return _joinedAnswerSlices(prefix, live);
  }

  /// The trimmed row is this round when the user line above it is the
  /// round's prompt. A later prompt keeps that older answer as its own row.
  bool _trimmedTailIsThisRound() {
    final messages = _detail?.messages;
    if (messages == null || messages.length < 2) return false;
    final prompt = _roundPrompt(_live.displayText);
    if (prompt.isEmpty) return false;
    for (var i = messages.length - 2; i >= 0; i--) {
      if (messages[i].role != 'user') continue;
      return messages[i].content.trim() == prompt;
    }
    return false;
  }

  /// The finished answer already sits in the transcript. Keeping the live
  /// copy would show it twice. A get_task row cut at 4000 runes is not that
  /// answer — the stream still has the rest.
  bool _transcriptOwnsLive() {
    if (!_live.responded) return false;
    final body = _shownLiveBody();
    if (body.isEmpty) return false;
    final messages = _detail?.messages;
    if (messages == null || messages.isEmpty) return false;
    final tail = messages.last;
    if (tail.role != 'assistant') return false;
    final content = tail.content.trim();
    if (content.isEmpty || _historyIsCappedPrefix(content, body)) return false;
    final n = body.length < 60 ? body.length : 60;
    return content.startsWith(body.substring(0, n));
  }

  bool _omitCappedHistoryTail(RemoteMessage message, bool isTail) {
    if (!isTail || message.role != 'assistant') return false;
    return _historyIsCappedPrefix(message.content, _shownLiveBody());
  }

  List<String> _cappedTailFiles() {
    final messages = _detail?.messages;
    if (messages == null || messages.isEmpty) return const [];
    final tail = messages.last;
    if (!_omitCappedHistoryTail(tail, true)) return const [];
    return tail.filePaths;
  }

  void _revealTail() {
    if (!_stickToEnd) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_stickToEnd || !_scrollCtrl.hasClients) return;
      final position = _scrollCtrl.position;
      if (!position.hasContentDimensions) return;
      final max = position.maxScrollExtent;
      if (max <= position.pixels + 1) return;
      _pinning++;
      try {
        position.jumpTo(max);
      } finally {
        _pinning--;
      }
    });
  }

  bool _liveHasCopy() {
    if (_live.body.trim().isNotEmpty || _live.visible.trim().isNotEmpty) {
      return true;
    }
    if (_live.thinking.any((block) => block.text.isNotEmpty)) return true;
    if (_live.timeline.isNotEmpty ||
        _live.cards.isNotEmpty ||
        _live.notices.isNotEmpty) {
      return true;
    }
    if (_recovery.isNotEmpty) return true;
    return _live.gap || _rewatching;
  }

  @override
  Widget build(BuildContext context) {
    final shownBody = _shownLiveBody();
    final revealStamp =
        '${_live.last}:${shownBody.length}:${_live.gap}:$_rewatching:${_detail?.messages.length ?? -1}';
    if (revealStamp != _revealStamp) {
      _revealStamp = revealStamp;
      _revealTail();
    }
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
                  child: NotificationListener<ScrollNotification>(
                    onNotification: _onScrollNotice,
                    child: ListView(
                      key: const ValueKey('remote-task-transcript'),
                      controller: _scrollCtrl,
                      padding: const EdgeInsets.all(12),
                      children: [
                        if (_degraded)
                          Padding(
                            padding: const EdgeInsets.only(bottom: 8),
                            child: Text(
                              '时间线已降级',
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: theme.colorScheme.error,
                              ),
                            ),
                          ),
                        if (detail != null)
                          for (var i = 0; i < detail.messages.length; i++)
                            if (!_omitCappedHistoryTail(
                              detail.messages[i],
                              i == detail.messages.length - 1,
                            ))
                              _MessageRow(
                                message: detail.messages[i],
                                onFetch: _fetchAndShare,
                                downloading: _downloading,
                              ),
                        if ((detail == null || detail.messages.isEmpty) &&
                            !_liveHasCopy())
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
                          ),
                        if (!_transcriptOwnsLive())
                          _LiveRound(
                            state: _live,
                            shownBody: shownBody,
                            recovery: _recovery,
                            syncing: _live.gap || _rewatching,
                            filePaths: _cappedTailFiles(),
                            downloading: _downloading,
                            onFetch: _fetchAndShare,
                          ),
                      ],
                    ),
                  ),
                ),
                if (detail != null && detail.artifacts.isNotEmpty)
                  _ArtifactStrip(artifacts: detail.artifacts),
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

class _LiveRound extends StatelessWidget {
  final ViewerReduceState state;
  final String shownBody;
  final String recovery;
  final bool syncing;
  final List<String> filePaths;
  final bool downloading;
  final Future<void> Function(String path) onFetch;

  const _LiveRound({
    required this.state,
    required this.shownBody,
    required this.recovery,
    required this.syncing,
    this.filePaths = const [],
    this.downloading = false,
    required this.onFetch,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final notes = <String>[];
    for (final block in state.thinking) {
      if (block.text.isEmpty) continue;
      notes.add(block.done ? block.text : '思考中 ${block.text}');
    }
    final body = shownBody;
    for (final row in state.timeline) {
      final title = row.title.isNotEmpty ? row.title : row.text;
      final line = row.phase.isNotEmpty ? '${row.phase} · $title' : title;
      if (line.isNotEmpty) notes.add(line);
    }
    for (final card in state.cards) {
      if (card.expired) {
        notes.add('确认已失效');
      } else if (card.text.isNotEmpty) {
        notes.add(card.text);
      } else if (card.kind == 'confirmation') {
        notes.add('需要确认');
      }
    }
    for (final note in state.notices) {
      if (note.text.isNotEmpty) notes.add(note.text);
    }
    if (state.trialStatus.isNotEmpty) notes.add(state.trialStatus);
    if (state.trialFailures > 0) notes.add('失败 ${state.trialFailures}');
    if (recovery.isNotEmpty) notes.add(recovery);
    if (notes.isEmpty && body.isEmpty && filePaths.isEmpty && !syncing) {
      return const SizedBox.shrink();
    }
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
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
                for (final line in notes)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 4),
                    child: Text(line, style: theme.textTheme.bodyMedium),
                  ),
                if (body.isNotEmpty)
                  MarkdownBody(
                    data: linkifyMarkdownUrls(body),
                    selectable: true,
                    onTapLink: (text, href, title) {
                      if (href != null) openExternalUrl(href);
                    },
                    styleSheet: _taskMarkdownStyle(theme),
                  ),
                if (syncing)
                  Padding(
                    padding: const EdgeInsets.only(top: 4),
                    child: Text(
                      '正在补齐中断的输出',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                for (final path in filePaths)
                  _FileCard(
                    path: path,
                    downloading: downloading,
                    onTap: () => onFetch(path),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// body is the draft. visible is the terminal. A longer terminal replaces
/// the draft when the draft is its prefix or its suffix. The snapshot keeps
/// only the newest slice, and that slice is the suffix.
String viewerShownAnswer(String draft, String terminal) {
  final d = draft.trim();
  final t = terminal.trim();
  if (d.isEmpty) return t;
  if (t.length <= d.length) return d;
  if (t.startsWith(d) || t.endsWith(d)) return t;
  return d;
}

/// get_task keeps the first 4000 runes and appends "…" (trimRemoteControlText).
const _historyTextCap = 4000;

const _answerSliceOverlapMin = 16;

/// Longest prefix of [tail] that is a suffix of [prefix].
int _sharedEdge(String prefix, String tail) {
  if (prefix.isEmpty || tail.isEmpty) return 0;
  final s = '$tail\u0000$prefix';
  final pi = List<int>.filled(s.length, 0);
  var k = 0;
  for (var i = 1; i < s.length; i++) {
    while (k > 0 && s.codeUnitAt(i) != s.codeUnitAt(k)) {
      k = pi[k - 1];
    }
    if (s.codeUnitAt(i) == s.codeUnitAt(k)) k++;
    pi[i] = k;
  }
  final n = pi[s.length - 1];
  return n >= _answerSliceOverlapMin ? n : 0;
}

String _joinedAnswerSlices(String prefix, String tail) {
  final overlap = _sharedEdge(prefix, tail);
  if (overlap > 0) return prefix + tail.substring(overlap);
  return '$prefix\n\n…\n\n$tail';
}

String? _trimmedHistoryPrefix(String history) {
  final h = history.trim();
  if (!h.endsWith('…') || h.length < _historyTextCap + 1) return null;
  return h.substring(0, h.length - 1);
}

String _roundPrompt(String raw) {
  final text = raw.trim();
  final match = RegExp(r'^〔[^〕]*〕').firstMatch(text);
  if (match == null) return text;
  return text.substring(match.end).trim();
}

bool _historyIsCappedPrefix(String history, String live) {
  final prefix = _trimmedHistoryPrefix(history);
  final l = live.trim();
  if (prefix == null || l.length <= prefix.length) return false;
  return l.startsWith(prefix);
}

class _ArtifactStrip extends StatelessWidget {
  final List<RemoteArtifact> artifacts;

  const _ArtifactStrip({required this.artifacts});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      color: theme.colorScheme.surfaceContainerLow,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxHeight: 132),
        child: ListView(
          shrinkWrap: true,
          padding: const EdgeInsets.fromLTRB(12, 4, 12, 4),
          children: [
            Text('产出物', style: theme.textTheme.titleSmall),
            const SizedBox(height: 4),
            for (final artifact in artifacts)
              Card(
                child: ListTile(
                  dense: true,
                  leading: const Icon(Icons.description_outlined),
                  title: Text(
                    artifact.title.isNotEmpty ? artifact.title : '产出物',
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
        ),
      ),
    );
  }
}

MarkdownStyleSheet _taskMarkdownStyle(ThemeData theme) {
  return MarkdownStyleSheet.fromTheme(theme).copyWith(
    p: theme.textTheme.bodyMedium?.copyWith(height: 1.5),
    a: theme.textTheme.bodyMedium?.copyWith(
      color: theme.colorScheme.primary,
      fontWeight: FontWeight.w600,
      decoration: TextDecoration.underline,
    ),
    code: theme.textTheme.bodySmall?.copyWith(
      fontFamily: 'monospace',
      backgroundColor: theme.colorScheme.surfaceContainerHighest,
    ),
    codeblockDecoration: BoxDecoration(
      color: theme.colorScheme.surfaceContainerHighest,
      borderRadius: BorderRadius.circular(8),
    ),
  );
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
                    styleSheet: _taskMarkdownStyle(theme),
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
