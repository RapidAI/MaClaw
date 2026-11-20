import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart' show ScrollDirection;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';

import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';

import '../bots/bots_controller.dart' show failureMessage;
import '../../core/chat_ui.dart' show ChatComposer, ChatRole, MessageBubble;
import '../../core/locale.dart';
import '../../core/ui_kit.dart' show linkifyMarkdownUrls, openExternalUrl;
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

  ///  事件到达后锁存—— 会把 state.responded 清掉，
  /// 没有这个锁存，历史已接管本轮后 live 轮会整体复活，与历史的完整
  /// 思考/正文同屏重复。新 round（或 requestId 变化）时清掉。
  bool _respondedLatch = false;

  /// 一旦转录的尾行拥有本轮正文，锁存整轮归属，live 轮彻底退场。
  bool _ownRoundLatch = false;
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
        // 快照换轮（requestId 变化）时，上一轮的归属/响应锁存必须跟着退场。
        if (seeded.requestId.isNotEmpty &&
            seeded.requestId != _live.requestId) {
          _respondedLatch = false;
          _ownRoundLatch = false;
        }
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
          final eventName = event['event']?.toString() ?? '';
          if (eventName == 'response') _respondedLatch = true;
          if (eventName == 'round_started' ||
              next.requestId != prev.requestId) {
            _respondedLatch = false;
            _ownRoundLatch = false;
          }
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
      if (seeded.requestId.isNotEmpty && seeded.requestId != _live.requestId) {
        _respondedLatch = false;
        _ownRoundLatch = false;
      }
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
      ).showSnackBar(SnackBar(content: Text(context.S.rtLocalCmdRejected)));
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
          title: Text(context.S.rtRename),
          content: TextField(
            controller: ctrl,
            autofocus: true,
            decoration: const InputDecoration(border: OutlineInputBorder()),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: Text(context.S.cancel),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, ctrl.text.trim()),
              child: Text(context.S.save),
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
        title: Text(context.S.rtStopTaskTitle),
        content: Text(context.S.rtStopTaskBody),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: Text(context.S.cancel),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(context.S.stop),
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

  /// fetch_file → temp file (unique per download) — shared by the 打开 and
  /// 分享 actions. Returns the written path, or null after a reported error.
  Future<String?> _fetchToFile(String path) async {
    final S = context.S;
    final api = ref.read(remoteApiProvider);
    if (api == null || _downloading) return null;
    setState(() => _downloading = true);
    try {
      final file = await api.fetchFile(widget.grant.id, widget.taskPath, path);
      if (file.dataBase64.isEmpty) {
        throw StateError(S.rtNoFileContent);
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
      return target;
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(context.S.rtDownloadFailed(failureMessage(error))),
          ),
        );
      }
      return null;
    } finally {
      if (mounted) setState(() => _downloading = false);
    }
  }

  /// fetch_file → temp file → system share sheet (download to other apps).
  Future<void> _fetchAndShare(String path) async {
    final target = await _fetchToFile(path);
    if (target == null || !mounted) return;
    await SharePlus.instance.share(ShareParams(files: [XFile(target)]));
  }

  /// fetch_file → temp file → the system default app for this file type.
  Future<void> _fetchAndOpen(String path) async {
    final target = await _fetchToFile(path);
    if (target == null || !mounted) return;
    final OpenResult result;
    try {
      result = await OpenFilex.open(target);
    } on Object {
      // Plugin unavailable (or the platform threw) — never crash the task
      // screen over an open that cannot proceed.
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(context.S.rtNoAppToOpen)));
      return;
    }
    if (!mounted) return;
    if (result.type != ResultType.done) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            result.message.isNotEmpty
                ? result.message
                : context.S.rtNoAppToOpen,
          ),
        ),
      );
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
  /// answer — the stream still has the rest. Ownership is judged by content
  /// overlap, not an exact prefix: one side containing the other, or a shared
  /// leading run of [_ownershipPrefixMin] runes, is the same answer.
  bool _transcriptOwnsLive() {
    if (_ownRoundLatch) return true;
    if (!_live.responded && !_respondedLatch) return false;
    final body = _shownLiveBody();
    final messages = _detail?.messages;
    if (messages == null || messages.isEmpty) return false;
    final tail = messages.last;
    if (tail.role != 'assistant') return false;
    final content = tail.content.trim();
    if (body.isNotEmpty) {
      if (content.isEmpty || _historyIsCappedPrefix(content, body)) {
        return false;
      }
      var owned = content.contains(body) || body.contains(content);
      if (!owned) {
        var i = 0;
        final n = content.length < body.length ? content.length : body.length;
        while (i < n && content.codeUnitAt(i) == body.codeUnitAt(i)) {
          i++;
        }
        owned = i >= _ownershipPrefixMin;
      }
      if (owned) _ownRoundLatch = true;
      return owned;
    }
    // 正文为空的轮（仅文件/思考）：尾行之后最近一条 user 行以前还有
    // assistant 输出时，尾行即本轮，历史即拥有。
    if (_assistantAfterLastUser(messages)) {
      _ownRoundLatch = true;
      return true;
    }
    return false;
  }

  /// At least one assistant row sits after the last user prompt — so the
  /// tail row belongs to the latest round even when its body is empty.
  bool _assistantAfterLastUser(List<RemoteMessage> messages) {
    for (var i = messages.length - 1; i >= 0; i--) {
      final role = messages[i].role;
      if (role == 'assistant') return true;
      if (role == 'user') return false;
    }
    return false;
  }

  /// Normalised texts already shown by the history tail row — live notes or
  /// thinking blocks that restate one of these are the same copy, not new
  /// output. Multi-line history rows contribute each trimmed line too, so a
  /// note echoing one line of the row is still recognised as a duplicate.
  Set<String> _historyTailTexts() {
    final messages = _detail?.messages;
    if (messages == null || messages.isEmpty) return const {};
    final tail = messages.last;
    if (tail.role != 'assistant') return const {};
    final texts = <String>{};
    for (final raw in [tail.content, tail.reasoning]) {
      final trimmed = raw.trim();
      if (trimmed.isEmpty) continue;
      texts.add(trimmed);
      for (final line in trimmed.split('\n')) {
        final l = line.trim();
        if (l.isNotEmpty) texts.add(l);
      }
    }
    return texts;
  }

  /// Live thinking restates the history reasoning when both trims match, or
  /// the shorter one is a [_thinkingPrefixMin]-rune-plus prefix of the other.
  bool _thinkingOwnedByHistory(String thinking) {
    final messages = _detail?.messages;
    if (thinking.isEmpty || messages == null || messages.isEmpty) {
      return false;
    }
    final tail = messages.last;
    if (tail.role != 'assistant') return false;
    final reasoning = tail.reasoning.trim();
    if (reasoning.isEmpty) return false;
    if (reasoning == thinking) return true;
    final shorter = reasoning.length < thinking.length
        ? reasoning
        : thinking;
    if (shorter.length < _thinkingPrefixMin) return false;
    return reasoning.startsWith(thinking) || thinking.startsWith(reasoning);
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
        : (widget.taskName.isNotEmpty
              ? widget.taskName
              : context.S.rtDefaultTaskName);
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
              '${widget.grant.title} · ${widget.grant.permissionLabel(context.S)}',
              style: theme.textTheme.labelSmall,
            ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: context.S.refresh,
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
                PopupMenuItem(value: 'rename', child: Text(context.S.rtRename)),
                PopupMenuItem(
                  value: 'pin',
                  child: Text(
                    detail?.task.pinned == true
                        ? context.S.rtUnpin
                        : context.S.rtPin,
                  ),
                ),
                PopupMenuItem(
                  value: 'stop',
                  child: Text(context.S.rtStopTaskTitle),
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
                    child: Text(context.S.retry),
                  ),
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
                                ? context.S.rtRunningWith(
                                    detail.task.activePhase,
                                  )
                                : context.S.rcStateRunning,
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
                              context.S.rtTimelineDegraded,
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
                                onOpen: _fetchAndOpen,
                                downloading: _downloading,
                              ),
                        if ((detail == null || detail.messages.isEmpty) &&
                            !_liveHasCopy())
                          Padding(
                            padding: const EdgeInsets.symmetric(vertical: 24),
                            child: Center(
                              child: Text(
                                context.S.rtNoTranscript,
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
                            suppressThinking: _thinkingOwnedByHistory([
                              for (final block in _live.thinking)
                                if (block.text.trim().isNotEmpty) block.text,
                            ].join('\n\n')),
                            historyTexts: _historyTailTexts(),
                            filePaths: _cappedTailFiles(),
                            downloading: _downloading,
                            onFetch: _fetchAndShare,
                            onOpen: _fetchAndOpen,
                          ),
                      ],
                    ),
                  ),
                ),
                if (detail != null && detail.artifacts.isNotEmpty)
                  _ArtifactStrip(
                    artifacts: detail.artifacts,
                    onFetch: _fetchAndShare,
                    onOpen: _fetchAndOpen,
                    downloading: _downloading,
                  ),
                if (canOperate)
                  ChatComposer(
                    controller: _inputCtrl,
                    hint: context.S.rtComposerHint,
                    sending: _sending,
                    onSend: _send,
                    onSubmitted: (_) => _send(),
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

  /// The history tail already shows this thinking text — rendering the live
  /// block again would double it.
  final bool suppressThinking;

  /// Texts the history tail row already displays; live note lines that
  /// duplicate one are dropped.
  final Set<String> historyTexts;
  final List<String> filePaths;
  final bool downloading;
  final Future<void> Function(String path) onFetch;
  final Future<void> Function(String path) onOpen;

  const _LiveRound({
    required this.state,
    required this.shownBody,
    required this.recovery,
    required this.syncing,
    this.suppressThinking = false,
    this.historyTexts = const {},
    this.filePaths = const [],
    this.downloading = false,
    required this.onFetch,
    required this.onOpen,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final S = context.S;
    final notes = <String>[];
    final thinking = suppressThinking
        ? ''
        : [
            for (final block in state.thinking)
              if (block.text.trim().isNotEmpty) block.text,
          ].join('\n\n');
    final body = shownBody;
    final seen = <String>{};
    void addNote(String line) {
      final trimmed = line.trim();
      if (trimmed.isEmpty ||
          historyTexts.contains(trimmed) ||
          !seen.add(trimmed)) {
        return;
      }
      notes.add(line);
    }

    for (final row in state.timeline) {
      final title = row.title.isNotEmpty ? row.title : row.text;
      final line = row.phase.isNotEmpty ? '${row.phase} · $title' : title;
      addNote(line);
    }
    for (final card in state.cards) {
      if (card.expired) {
        addNote(S.rtConfirmInvalid);
      } else if (card.text.isNotEmpty) {
        addNote(card.text);
      } else if (card.kind == 'confirmation') {
        addNote(S.rtConfirmRequired);
      }
    }
    for (final note in state.notices) {
      if (note.text.isNotEmpty) addNote(note.text);
    }
    if (state.trialStatus.isNotEmpty) addNote(state.trialStatus);
    if (state.trialFailures > 0) {
      addNote(S.rtFailures(state.trialFailures));
    }
    if (recovery.isNotEmpty) addNote(recovery);
    if (thinking.isEmpty &&
        notes.isEmpty &&
        body.isEmpty &&
        filePaths.isEmpty &&
        !syncing) {
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
                if (thinking.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 4),
                    child: _ReasoningBlock(
                      text: thinking,
                      streaming: true,
                      initiallyExpanded: true,
                    ),
                  ),
                for (final line in notes)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 4),
                    child: _NoteLine(text: line),
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
                      context.S.rtResuming,
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
                    onOpen: () => onOpen(path),
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

/// Leading runes history and live must share before the transcript owns the
/// live round — long enough that two different answers cannot collide.
const _ownershipPrefixMin = 40;

/// Shorter side must be at least this long before a prefix match counts as
/// the same thinking text.
const _thinkingPrefixMin = 20;

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
  final Future<void> Function(String path) onFetch;
  final Future<void> Function(String path) onOpen;
  final bool downloading;

  const _ArtifactStrip({
    required this.artifacts,
    required this.onFetch,
    required this.onOpen,
    this.downloading = false,
  });

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
            Text(context.S.rtArtifacts, style: theme.textTheme.titleSmall),
            const SizedBox(height: 4),
            for (final artifact in artifacts)
              Card(
                child: ListTile(
                  dense: true,
                  // sourceUrl 是产物在受控机上的路径——fetch_file 只回传
                  // 任务记录里投递过的文件，路径没被投递时服务端会如实报错。
                  enabled: artifact.sourceUrl.isNotEmpty,
                  onTap: artifact.sourceUrl.isEmpty
                      ? null
                      : () => onOpen(artifact.sourceUrl),
                  leading: const Icon(Icons.description_outlined),
                  title: Text(
                    artifact.title.isNotEmpty
                        ? artifact.title
                        : context.S.rtArtifacts,
                  ),
                  subtitle: artifact.preview.isNotEmpty
                      ? Text(
                          artifact.preview,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                        )
                      : null,
                  trailing: artifact.sourceUrl.isEmpty
                      ? null
                      : IconButton(
                          tooltip: context.S.rtDownload,
                          icon: downloading
                              ? const SizedBox(
                                  width: 16,
                                  height: 16,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                  ),
                                )
                              : const Icon(Icons.download_outlined),
                          onPressed: downloading
                              ? null
                              : () => onFetch(artifact.sourceUrl),
                        ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// One live-note / reasoning line rendered as markdown — the bot's streaming
/// text carries formatting (steps, code ticks, lists) and plain Text showed
/// it raw. Compact spacing + optional muted/error tone.
class _NoteLine extends StatelessWidget {
  final String text;
  final bool muted;

  const _NoteLine({required this.text, this.muted = false});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final base = muted
        ? theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
            height: 1.5,
          )
        : theme.textTheme.bodyMedium?.copyWith(height: 1.45);
    return MarkdownBody(
      // 推理/笔记里的链接要可点（桌面端同行为），文本可选中以便复制。
      data: linkifyMarkdownUrls(text),
      selectable: true,
      onTapLink: (text, href, title) {
        if (href != null) openExternalUrl(href);
      },
      styleSheet: _taskMarkdownStyle(theme).copyWith(
        p: base,
        a: theme.textTheme.bodyMedium?.copyWith(
          color: theme.colorScheme.primary,
          fontWeight: FontWeight.w600,
          decoration: TextDecoration.underline,
        ),
        h1: theme.textTheme.titleSmall,
        h2: theme.textTheme.titleSmall,
        h3: theme.textTheme.titleSmall,
        blockSpacing: 4,
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
  final Future<void> Function(String path) onOpen;
  final bool downloading;

  const _MessageRow({
    required this.message,
    required this.onFetch,
    required this.onOpen,
    required this.downloading,
  });

  @override
  Widget build(BuildContext context) {
    return message.role == 'user'
        ? _UserBubble(text: message.content)
        : _AssistantTurn(
            message: message,
            onFetch: onFetch,
            onOpen: onOpen,
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
    return MessageBubble(
      role: ChatRole.user,
      maxWidth: MediaQuery.of(context).size.width * 0.78,
      margin: const EdgeInsets.symmetric(vertical: 4),
      child: SelectableText(text, style: const TextStyle(height: 1.45)),
    );
  }
}

/// 助手回复 — 豆包式：左侧头像 + 无气泡排版，思考过程可折叠，
/// 正文按 Markdown 渲染，产出文件以卡片列出可点击下载。
class _AssistantTurn extends StatelessWidget {
  final RemoteMessage message;
  final Future<void> Function(String path) onFetch;
  final Future<void> Function(String path) onOpen;
  final bool downloading;

  const _AssistantTurn({
    required this.message,
    required this.onFetch,
    required this.onOpen,
    required this.downloading,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return MessageBubble(
      role: ChatRole.assistant,
      maxWidth: MediaQuery.of(context).size.width * 0.84,
      margin: const EdgeInsets.symmetric(vertical: 6),
      leading: CircleAvatar(
        radius: 15,
        backgroundColor: theme.colorScheme.primaryContainer,
        child: Icon(
          Icons.smart_toy_outlined,
          size: 17,
          color: theme.colorScheme.onPrimaryContainer,
        ),
      ),
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
                onOpen: () => onOpen(path),
              ),
          ],
        ],
      ),
    );
  }
}

/// 思考过程 — 默认收起，展开看推理细节。
class _ReasoningBlock extends StatelessWidget {
  final String text;

  /// live 轮流式阶段为 true：标题走「思考中」，正文随 delta 增长。
  final bool streaming;

  /// live 观察态默认展开（正文随流增长可见）；历史里保持折叠。
  final bool initiallyExpanded;

  const _ReasoningBlock({
    required this.text,
    this.streaming = false,
    this.initiallyExpanded = false,
  });

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
        initiallyExpanded: initiallyExpanded,
        tilePadding: const EdgeInsets.symmetric(horizontal: 10),
        childrenPadding: const EdgeInsets.fromLTRB(10, 0, 10, 10),
        leading: Icon(
          Icons.psychology_outlined,
          size: 16,
          color: theme.colorScheme.onSurfaceVariant,
        ),
        title: Text(
          streaming ? context.S.rtThinkingStreaming : context.S.rtReasoning,
          style: theme.textTheme.labelMedium?.copyWith(
            color: streaming
                ? theme.colorScheme.primary
                : theme.colorScheme.onSurfaceVariant,
          ),
        ),
        iconColor: theme.colorScheme.onSurfaceVariant,
        collapsedIconColor: theme.colorScheme.onSurfaceVariant,
        children: [_NoteLine(text: text, muted: true)],
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

/// 产出文件卡片 — 图标 + 文件名 + 打开/下载指示。点卡片分享，
/// 右侧「打开」用系统默认应用查看。
class _FileCard extends StatelessWidget {
  final String path;
  final bool downloading;
  final VoidCallback onTap;
  final VoidCallback onOpen;

  const _FileCard({
    required this.path,
    required this.downloading,
    required this.onTap,
    required this.onOpen,
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
              const SizedBox(width: 4),
              downloading
                  ? const Padding(
                      padding: EdgeInsets.symmetric(horizontal: 12),
                      child: SizedBox(
                        width: 14,
                        height: 14,
                        child: CircularProgressIndicator(strokeWidth: 1.5),
                      ),
                    )
                  : Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        IconButton(
                          tooltip: context.S.rtOpen,
                          visualDensity: VisualDensity.compact,
                          padding: EdgeInsets.zero,
                          constraints: const BoxConstraints(
                            minWidth: 30,
                            minHeight: 30,
                          ),
                          icon: const Icon(Icons.open_in_new, size: 16),
                          color: theme.colorScheme.primary,
                          onPressed: onOpen,
                        ),
                        const SizedBox(width: 2),
                        Icon(
                          Icons.download_outlined,
                          size: 16,
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ],
                    ),
            ],
          ),
        ),
      ),
    );
  }
}
