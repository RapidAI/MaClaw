import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart' show ScrollDirection;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';
import 'package:open_filex/open_filex.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';

import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';

import '../../components/mermaid/mermaid.dart';
import '../bots/bots_controller.dart' show failureMessage;
import '../../core/chat_ui.dart' show ChatComposer, ChatRole, MessageBubble;
import '../../core/dictation.dart';
import '../../core/locale.dart';
import '../../core/native_voice.dart';
import '../../core/platform.dart' show supportsVoiceInput;
import '../../core/providers.dart' show sessionProvider;
import '../../core/voice_engine.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart'
    show AppBadge, EmptyState, linkifyMarkdownUrls, openExternalUrl;
import '../bots/widgets.dart' show runStatusColor;
import 'activity_rows.dart';
import 'history_rows.dart';
import 'remote_api.dart';
import 'remote_attachments.dart';
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

  /// 真机里输入栏在页面内部（导航栏在下面，本来就是对的）。
  /// 只有测试直接渲染这个页面时不套底部导航，才需要 `inlineComposer: false`
  /// 先用底部的占位条补回导航栏高度。
  final bool inlineComposer;

  const RemoteTaskScreen({
    super.key,
    required this.grant,
    required this.taskPath,
    this.taskName = '',
    this.inlineComposer = true,
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

  /// 已点发送、历史还没对上的用户消息。立刻在对话里回显，避免等 get_task。
  final List<PendingUserEcho> _pendingUser = [];

  /// 附件：选好但还没发出（或正在上传）的文件。
  final List<RemoteOutgoingFile> _files = [];
  final RemoteUploadQueue _uploads = RemoteUploadQueue();
  bool _uploading = false;
  final int _fileGen = 0;

  /// 语音输入：系统识别 → 本地 SenseVoice，逻辑在 ComposerDictation。
  ComposerDictation? _dictation;

  /// 上次对账时历史里已有的用户消息条数。没有文本基线时才用它跳过旧行。
  int _ackedUserCount = 0;

  /// 上次对账看到的用户行原文。窗口滑动后按这段对齐，只消费新增尾部。
  final List<String> _ackedUserTexts = [];
  bool _downloading = false;
  bool _pollInFlight = false;
  bool _loadingEarlier = false;
  String _earlierError = '';
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
    _dictation = ComposerDictation(
      controller: _inputCtrl,
      engine: ref.read(voiceEngineProvider),
      toast: _toast,
      localeId: () => speechLocaleId(Localizations.localeOf(context)),
      onLocalModelNeeded: () {
        final hub = ref.read(sessionProvider).active?.hubUrl;
        unawaited(ref.read(voiceEngineProvider).autoEnsure(hubBase: hub));
      },
      // Evaluated per use: autoEnsure may resetProbe and re-probe after this
      // page opened, and a captured bool would hide a recovered engine.
      nativeVoiceSupported: () => NativeVoice.usable,
      voiceInputSupported: supportsVoiceInput,
      messages: () {
        final S = context.S;
        return DictationMessages(
          missingEngine: S.vsEngineMissing,
          downloading: S.chatAsrUnavailableDownloading,
          notHeard: S.chatNotHeard,
          transcribeFailed: S.chatAsrFailed,
          transcribeFailedWith: S.chatAsrFailedWith,
          micPermission: S.chatMicPermissionDenied,
          micOpenFailed: S.chatMicOpenFailed,
        );
      },
    )..onStateChanged = () {
        if (mounted) setState(() {});
      };
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
    // 先停听写再拆输入框：迟到的识别结果不能写已释放的 controller。
    _dictation?.dispose();
    _dictation = null;
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final api = ref.read(remoteApiProvider);
    if (api == null) return;
    _pollInFlight = true;
    try {
      // A later page stays loaded. Polling the default 40 would drop it.
      final loaded = _detail?.messages.length ?? 0;
      final detail = await api.getTask(
        widget.grant.id,
        widget.taskPath,
        historyLimit: loaded > 40 ? loaded : 0,
      );
      if (!mounted) return;
      setState(() {
        _detail = _keepEarlierPage(_detail, detail);
        _loading = false;
        _error = '';
        _ackedUserCount = reconcilePendingUserEchoes(
          _pendingUser,
          detail.messages,
          knownUserCount: _ackedUserCount,
          ackedUserTexts: _ackedUserTexts,
        );
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

  /// A poll that returns the same window must not drop rows the phone already
  /// loaded above that window. A shorter reply is treated as a new tail.
  RemoteTaskDetail _keepEarlierPage(
    RemoteTaskDetail? previous,
    RemoteTaskDetail next,
  ) {
    final old = previous?.messages ?? const <RemoteMessage>[];
    if (old.length <= next.messages.length) return next;
    if (next.messages.isEmpty) {
      return previous!.copyWith(
        task: next.task,
        artifacts: next.artifacts,
        messageTotal: next.messageTotal,
      );
    }
    final head = old.length - next.messages.length;
    for (var i = 0; i < next.messages.length; i++) {
      if (old[head + i].content != next.messages[i].content ||
          old[head + i].role != next.messages[i].role) {
        return next;
      }
    }
    return previous!.copyWith(
      task: next.task,
      messages: old,
      artifacts: next.artifacts,
      messageTotal: next.messageTotal,
    );
  }

  /// Rows in [page] that sit strictly before [shown]. Equal content at the
  /// join is the overlap; anything else is prepended as returned.
  List<RemoteMessage> _earlierThan(
    List<RemoteMessage> shown,
    List<RemoteMessage> page,
  ) {
    if (shown.isEmpty) return page;
    if (page.length <= shown.length) return const [];
    final head = page.length - shown.length;
    for (var i = 0; i < shown.length; i++) {
      if (page[head + i].content != shown[i].content ||
          page[head + i].role != shown[i].role) {
        return page;
      }
    }
    return page.sublist(0, head);
  }

  /// Asks for twice the rows already on screen, capped at 400, and prepends
  /// only the part the phone did not have. A failure leaves the transcript.
  Future<void> _loadEarlier() async {
    final current = _detail;
    final api = ref.read(remoteApiProvider);
    if (current == null || api == null || _loadingEarlier) return;
    final shown = current.messages.length;
    if (shown >= current.messageTotal) return;
    var limit = shown * 2;
    if (limit < 1) limit = 40;
    if (limit > 400) limit = 400;
    setState(() {
      _loadingEarlier = true;
      _earlierError = '';
    });
    try {
      final page = await api.getTask(
        widget.grant.id,
        widget.taskPath,
        historyLimit: limit,
      );
      if (!mounted) return;
      final earlier = _earlierThan(current.messages, page.messages);
      setState(() {
        _detail = current.withEarlier(earlier).copyWith(
          task: page.task,
          artifacts: page.artifacts,
          messageTotal: page.messageTotal,
        );
        _loadingEarlier = false;
      });
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        _loadingEarlier = false;
        _earlierError = failureMessage(error);
      });
    }
  }

  void _toast(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  /// 附件入口 — 拍照 / 相册 / 文件。文件先传到被控机器，再随消息发出。
  Future<void> _attach() async {
    if (_uploading || _sending) return;
    final S = context.S;
    final choice = await showModalBottomSheet<String>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.photo_camera_outlined),
              title: Text(S.chatAttachCamera),
              onTap: () => Navigator.of(context).pop('camera'),
            ),
            ListTile(
              leading: const Icon(Icons.photo_library_outlined),
              title: Text(S.chatAttachGallery),
              onTap: () => Navigator.of(context).pop('gallery'),
            ),
            ListTile(
              leading: const Icon(Icons.attach_file),
              title: Text(S.chatAttachFile),
              onTap: () => Navigator.of(context).pop('file'),
            ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
    if (choice == null || !mounted) return;
    if (choice == 'camera' || choice == 'gallery') {
      final picked = await ImagePicker().pickImage(
        source: choice == 'camera' ? ImageSource.camera : ImageSource.gallery,
        maxWidth: 2048,
        maxHeight: 2048,
        imageQuality: 85,
      );
      if (picked == null || !mounted) return;
      final disk = File(picked.path);
      final size = await disk.length().catchError((_) => 0);
      if (size <= 0) return;
      await _stageFile(
        RemoteOutgoingFile(name: picked.name, size: size, disk: disk),
      );
      return;
    }
    final picked = await FilePicker.pickFiles();
    if (picked.isEmpty || !mounted) return;
    for (final file in picked) {
      File? disk;
      final localPath = file.path;
      if (localPath != null) {
        final candidate = File(localPath);
        try {
          disk = await candidate.length() > 0 ? candidate : null;
        } on Object {
          disk = null;
        }
      }
      Uint8List? bytes;
      var size = 0;
      if (disk == null) {
        try {
          bytes = await file.readAsBytes();
        } on Object {
          bytes = null;
        }
        if (bytes == null || bytes.isEmpty) continue;
        size = bytes.length;
      }
      await _stageFile(
        RemoteOutgoingFile(
          name: file.name,
          size: size,
          disk: disk,
          bytes: bytes,
        ),
      );
    }
  }

  /// 上传一个文件到被控机器对应任务的 `attachments/` 目录，然后挂到待发列表。
  ///
  /// 走远控的 `attach` 分片接口（Hub 收下后在任务目录里写文件），不走 Bot
  /// 素材目录：任务里的 agent 能直接读到，回包里的本地文件也按这个路径
  /// 记成投递文件，手机才能再下载回来。上传串行执行。
  Future<void> _stageFile(RemoteOutgoingFile file) async {
    if (!mounted) return;
    final sizeLimit = remoteAttachmentMaxBytes;
    if (file.size > sizeLimit) {
      _toast(context.S.rtAttachTooLarge(file.name, sizeLimit >> 20));
      return;
    }
    final api = ref.read(remoteApiProvider);
    if (api == null) {
      _toast(context.S.rtAttachDesktopUnknown);
      return;
    }
    final target = _detail?.attach;
    if (target == null || !target.canAttach) {
      _toast(context.S.rtAttachNoComputer);
      return;
    }
    final gen = _fileGen;
    setState(() {
      file.uploading = true;
      _files.add(file);
      _uploading = true;
    });
    final staged = await _uploads.add(
      () => api.uploadAttachment(
        widget.grant.id,
        widget.taskPath,
        target,
        file,
        onProgress: (sent, total) {
          if (!mounted || gen != _fileGen) return;
          setState(() => file.progress = stagedProgress(sent, total));
        },
      ),
    );
    if (!mounted || gen != _fileGen) return;
    setState(() {
      file.uploading = false;
      file.path = staged.ok ? staged.path : '';
      file.error = staged.ok ? '' : staged.error;
      _uploading = _files.any((f) => f.uploading);
    });
    if (!staged.ok) {
      _toast(context.S.rtAttachFailed(file.name, staged.error));
      return;
    }
    // 不往输入框里写路径文案：气泡会把原文当附件清单解析，多一行说明
    // 就得多一条路径，对不上。附件只显示在附件条上，发送时拼进消息。
  }

  void _removeFile(RemoteOutgoingFile file) {
    setState(() => _files.remove(file));
  }

  Future<void> _toggleDictation() async {
    final dictation = _dictation;
    if (dictation == null) return;
    await dictation.toggle();
  }

  Future<void> _send() async {
    final text = _inputCtrl.text.trim();
    if (_sending) return;
    if (_uploading) {
      _toast(context.S.rtAttachUploading);
      return;
    }
    final stagedPaths = [
      for (final file in _files)
        if (file.staged) file.path,
    ];
    if (text.isEmpty && stagedPaths.isEmpty) return;
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
    final echo = PendingUserEcho(
      text: messageWithAttachments(text, stagedPaths),
      sentAt: DateTime.now(),
    );
    setState(() {
      _pendingUser.add(echo);
      _files.removeWhere((file) => file.staged);
    });
    _stickToEnd = true;
    _revealTail();
    await _dispatchEcho(echo, clearInput: true);
  }

  Future<void> _retryEcho(PendingUserEcho echo) async {
    if (!canRetryPendingEcho(echo)) return;
    await _dispatchEcho(echo, clearInput: false);
  }

  /// 发出一条待确认回显。成功去掉“发送中”；失败标红并保留重试。
  Future<void> _dispatchEcho(
    PendingUserEcho echo, {
    required bool clearInput,
  }) async {
    if (_sending) return;
    final api = ref.read(remoteApiProvider);
    if (api == null) {
      if (!mounted) return;
      setState(() => markEchoFailed(_pendingUser, echo, context.S.chatSendFailed));
      return;
    }
    setState(() {
      beginPendingRetry(echo);
      _sending = true;
    });
    _stickToEnd = true;
    _revealTail();
    try {
      await api.sendMessage(widget.grant.id, widget.taskPath, echo.text);
      if (!mounted) return;
      setState(() => markEchoSent(_pendingUser, echo));
      if (clearInput) _inputCtrl.clear();
      await _load();
      ref.invalidate(remoteTasksProvider(widget.grant.id));
      // Sending is a request to watch the reply, even if the reader had
      // parked higher in the history.
      _stickToEnd = true;
      _revealTail();
    } on Object catch (error) {
      if (!mounted) return;
      final message = failureMessage(error);
      setState(() => markEchoFailed(_pendingUser, echo, message));
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(message)));
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
            decoration: const InputDecoration(),
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
      // The count row is laid out with the first history page. A jump before
      // that row has a size leaves the tail a few dozen pixels short.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !_stickToEnd || !_scrollCtrl.hasClients) return;
        final again = _scrollCtrl.position;
        if (!again.hasContentDimensions) return;
        final grown = again.maxScrollExtent;
        if (grown <= again.pixels + 1) return;
        _pinning++;
        try {
          again.jumpTo(grown);
        } finally {
          _pinning--;
        }
      });
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
    final task = detail?.task;
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Flexible(
                  child: Text(
                    title,
                    style: theme.textTheme.titleMedium,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (task != null) ...[
                  const SizedBox(width: AppSpacing.sm - 2),
                  Flexible(
                    child: _KindChip(
                      label: task.isCoding
                          ? context.S.rcKindCoding
                          : context.S.rcKindChat,
                      color: task.isCoding
                          ? theme.colorScheme.tertiary
                          : theme.colorScheme.primary,
                    ),
                  ),
                ],
              ],
            ),
            Text(
              '${widget.grant.title} · ${widget.grant.permissionLabel(context.S)}',
              style: theme.textTheme.labelSmall,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
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
          ? EmptyState(
              icon: Icons.cloud_off_rounded,
              title: _error,
              action: FilledButton.tonalIcon(
                onPressed: _load,
                icon: const Icon(Icons.refresh, size: 18),
                label: Text(context.S.retry),
              ),
            )
          : Column(
              children: [
                if (!widget.inlineComposer) const _BottomNavPlaceholder(),
                if (detail != null && detail.task.running)
                  Container(
                    width: double.infinity,
                    margin: const EdgeInsets.fromLTRB(
                      AppSpacing.lg,
                      AppSpacing.sm,
                      AppSpacing.lg,
                      0,
                    ),
                    decoration: BoxDecoration(
                      color: theme.colorScheme.primaryContainer.withValues(
                        alpha: .55,
                      ),
                      borderRadius: BorderRadius.circular(AppRadius.md),
                    ),
                    padding: const EdgeInsets.symmetric(
                      horizontal: AppSpacing.md + 2,
                      vertical: AppSpacing.sm,
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
                        const SizedBox(width: AppSpacing.sm),
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
                      padding: const EdgeInsets.all(AppSpacing.md),
                      children: [
                        if (_degraded)
                          Padding(
                            padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                            child: Text(
                              context.S.rtTimelineDegraded,
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: theme.colorScheme.error,
                              ),
                            ),
                          ),
                        if (detail != null && detail.messages.isNotEmpty)
                          Padding(
                            padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                            child: Text(
                              context.S.rtShownCount(
                                detail.messages.length,
                                detail.messageTotal,
                              ),
                              key: const ValueKey('remote-task-shown-count'),
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: theme.colorScheme.onSurfaceVariant,
                              ),
                            ),
                          ),
                        if (detail != null &&
                            detail.messages.length < detail.messageTotal)
                          Padding(
                            padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                            child: Align(
                              alignment: Alignment.centerLeft,
                              child: TextButton.icon(
                                key: const ValueKey('remote-task-load-earlier'),
                                onPressed: _loadingEarlier ? null : _loadEarlier,
                                icon: _loadingEarlier
                                    ? SizedBox(
                                        width: 14,
                                        height: 14,
                                        child: CircularProgressIndicator(
                                          strokeWidth: 1.5,
                                          color: theme.colorScheme.primary,
                                        ),
                                      )
                                    : const Icon(Icons.unfold_less, size: 16),
                                label: Text(context.S.rtLoadEarlier),
                              ),
                            ),
                          ),
                        if (_earlierError.isNotEmpty)
                          Padding(
                            padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                            child: Text(
                              _earlierError,
                              key: const ValueKey('remote-task-earlier-error'),
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
                        for (final echo in _pendingUser)
                          _UserBubble(
                            text: echo.text,
                            sending: echo.sending,
                            failed: echo.failed,
                            error: echo.error,
                            onRetry: echo.failed ? () => _retryEcho(echo) : null,
                          ),
                        if ((detail == null || detail.messages.isEmpty) &&
                            _pendingUser.isEmpty &&
                            !_liveHasCopy())
                          Padding(
                            padding: const EdgeInsets.symmetric(vertical: AppSpacing.xxl),
                            child: EmptyState(
                              icon: Icons.forum_outlined,
                              title: context.S.rtNoTranscript,
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
                    hint: _dictation?.listening == true
                        ? context.S.chatVoiceListening
                        : context.S.rtComposerHint,
                    sending: _sending,
                    onSend: _send,
                    onSubmitted: (_) => _send(),
                    onAttach: _attach,
                    attachTooltip: context.S.rtAttachTooltip,
                    attachments: [
                      for (final file in _files) _AttachmentChip(
                        file: file,
                        onRemove: () => _removeFile(file),
                      ),
                    ],
                    busy: _uploading,
                    onMic: supportsVoiceInput ? _toggleDictation : null,
                    listening: _dictation?.listening ?? false,
                    level: _dictation?.level,
                    listenStatus: _dictation?.statusText(
                      listenSystem: context.S.chatListenSystem,
                      listenLocal: context.S.chatListenLocal,
                      recognizingSystem: context.S.chatRecognizingSystem,
                      recognizingLocal: context.S.chatRecognizingLocal,
                    ),
                  ),
              ],
            ),
    );
  }
}

/// 测试直接渲染任务页时的底部占位：高度与五栏导航一致，让输入栏和
/// 对话区的高度与真机对齐。
class _BottomNavPlaceholder extends StatelessWidget {
  const _BottomNavPlaceholder();

  @override
  Widget build(BuildContext context) {
    final media = MediaQuery.maybeOf(context);
    final padding = media?.padding.bottom ?? 0;
    return SizedBox(
      key: const ValueKey('bottom-nav-placeholder'),
      height: 80 + padding,
    );
  }
}

/// 附件条上的一项：文件名、上传进度或错误，删得掉的叉。
class _AttachmentChip extends StatelessWidget {
  final RemoteOutgoingFile file;
  final VoidCallback onRemove;

  const _AttachmentChip({required this.file, required this.onRemove});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final failed = file.error.isNotEmpty;
    return Container(
      constraints: const BoxConstraints(maxWidth: 240),
      padding: const EdgeInsets.fromLTRB(AppSpacing.sm, 2, 2, 2),
      decoration: BoxDecoration(
        color: failed
            ? scheme.errorContainer.withValues(alpha: 0.5)
            : scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(AppRadius.sm + 4),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            failed ? Icons.error_outline : Icons.attach_file,
            size: 14,
            color: failed ? scheme.error : scheme.onSurfaceVariant,
          ),
          const SizedBox(width: 4),
          Flexible(
            child: Text(
              file.uploading
                  ? context.S.rtAttachUploadingName(
                      file.name,
                      (file.progress * 100).round(),
                    )
                  : failed
                  ? context.S.rtAttachFailedShort(file.name)
                  : file.name,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodySmall?.copyWith(
                color: failed ? scheme.error : scheme.onSurface,
              ),
            ),
          ),
          IconButton(
            onPressed: onRemove,
            visualDensity: VisualDensity.compact,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(minWidth: 26, minHeight: 26),
            icon: Icon(
              Icons.close,
              size: 14,
              color: scheme.onSurfaceVariant,
            ),
            tooltip: context.S.rtAttachRemove,
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
    final body = shownBody;
    final folded = foldActivityRows(
      state,
      labels: ActivityRowLabels(
        confirmInvalid: S.rtConfirmInvalid,
        confirmRequired: S.rtConfirmRequired,
        failures: S.rtFailures(state.trialFailures),
      ),
      suppressThinking: suppressThinking,
      historyTexts: historyTexts,
      recovery: recovery,
    );
    final rows = folded;
    if (rows.isEmpty && body.isEmpty && filePaths.isEmpty && !syncing) {
      return const SizedBox.shrink();
    }
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.md),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 30,
            height: 30,
            margin: const EdgeInsets.only(top: AppSpacing.xs / 2),
            decoration: BoxDecoration(
              color: theme.colorScheme.primaryContainer,
              borderRadius: BorderRadius.circular(AppRadius.sm),
            ),
            child: Icon(
              Icons.auto_awesome_rounded,
              size: 16,
              color: theme.colorScheme.primary,
            ),
          ),
          const SizedBox(width: AppSpacing.md - 2),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (rows.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                    child: _ActivityTimeline(
                      rows: rows,
                      roundLive: !state.responded,
                    ),
                  ),
                if (body.isNotEmpty)
                  MermaidMarkdown(
                    data: linkifyMarkdownUrls(body),
                    selectable: true,
                    onTapLink: (text, href, title) {
                      if (href != null) openExternalUrl(href);
                    },
                    styleSheet: _taskMarkdownStyle(theme),
                  ),
                if (syncing)
                  Padding(
                    padding: const EdgeInsets.only(top: AppSpacing.xs),
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
          padding: const EdgeInsets.fromLTRB(
            AppSpacing.md,
            AppSpacing.xs,
            AppSpacing.md,
            AppSpacing.xs,
          ),
          children: [
            Text(context.S.rtArtifacts, style: theme.textTheme.titleSmall),
            const SizedBox(height: AppSpacing.xs),
            for (final artifact in artifacts)
              Card(
                child: ListTile(
                  dense: true,
                  // downloadable 由受控机标记：绝对路径的普通文件，且已记在
                  // 对话投递列表里。没有这个标记时不提供打开/下载。
                  enabled: artifact.downloadable && artifact.sourceUrl.isNotEmpty,
                  onTap: artifact.downloadable && artifact.sourceUrl.isNotEmpty
                      ? () => onOpen(artifact.sourceUrl)
                      : null,
                  leading: Icon(
                    Icons.description_outlined,
                    color: theme.colorScheme.primary,
                  ),
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
                  trailing: !artifact.downloadable || artifact.sourceUrl.isEmpty
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

/// Live process list, laid out like the desktop process timeline: a
/// collapsible summary line, then thought, tool, banner and note rows in
/// arrival order. Open while the turn runs, foldable at any time.
class _ActivityTimeline extends StatefulWidget {
  final List<ActivityRow> rows;

  /// The round has not responded yet — its newest thought stays open.
  final bool roundLive;

  const _ActivityTimeline({required this.rows, required this.roundLive});

  @override
  State<_ActivityTimeline> createState() => _ActivityTimelineState();
}

class _ActivityTimelineState extends State<_ActivityTimeline> {
  bool? _open;

  /// A new arrival re-opens a list the person had folded for the previous
  /// rows — the desktop folds on the live→idle edge, not mid-turn.
  int _seenRows = -1;

  @override
  void initState() {
    super.initState();
    _seenRows = widget.rows.length;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final typed = [
      for (final row in widget.rows)
        if (!row.isSummary) row,
    ];
    final roundLive = widget.roundLive;
    final summary = widget.rows.firstWhere(
      (row) => row.isSummary,
      orElse: () => const ActivityRow(kind: ActivityRowKind.summary, seq: -1),
    );
    if (typed.isEmpty) return const SizedBox.shrink();
    if (_seenRows != widget.rows.length) {
      _seenRows = widget.rows.length;
      _open = true;
    }
    final open = _open ?? true;
    final parts = <String>[];
    if (summary.thinkingCount > 0) {
      parts.add(context.S.rtActivityThoughts(summary.thinkingCount));
    }
    if (summary.toolCount > 0) {
      parts.add(context.S.rtActivityTools(summary.toolCount));
    }
    if (summary.noteCount > 0) {
      parts.add(context.S.rtActivityNotes(summary.noteCount));
    }
    return Container(
      decoration: BoxDecoration(
        color: scheme.surface,
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
        borderRadius: BorderRadius.circular(AppRadius.md),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          InkWell(
            key: const ValueKey('live-activity-summary'),
            onTap: () => setState(() => _open = !open),
            borderRadius: BorderRadius.circular(AppRadius.md),
            child: Padding(
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpacing.sm,
                vertical: AppSpacing.sm,
              ),
              child: Row(
                children: [
                  Icon(
                    Icons.account_tree_outlined,
                    size: 14,
                    color: scheme.onSurfaceVariant,
                  ),
                  const SizedBox(width: AppSpacing.xs + 2),
                  Expanded(
                    child: Text(
                      parts.isEmpty
                          ? context.S.rtActivityRunning
                          : parts.join(' · '),
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.labelMedium?.copyWith(
                        color: scheme.onSurface,
                      ),
                    ),
                  ),
                  Icon(
                    open ? Icons.expand_less : Icons.expand_more,
                    size: 18,
                    color: scheme.onSurfaceVariant,
                  ),
                ],
              ),
            ),
          ),
          if (open)
            Padding(
              padding: const EdgeInsets.fromLTRB(
                AppSpacing.sm,
                0,
                AppSpacing.sm,
                AppSpacing.sm,
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Divider(
                    height: 1,
                    thickness: 0.5,
                    color: scheme.outlineVariant.withValues(alpha: 0.7),
                  ),
                  const SizedBox(height: AppSpacing.xs + 2),
                  for (final row in typed)
                    Padding(
                      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                      child: switch (row.kind) {
                        ActivityRowKind.thinking => _ActivityThinkingRow(
                            text: row.text,
                            live: roundLive,
                          ),
                        ActivityRowKind.tool =>
                          row.isNamedTool ? _ToolChip(row: row) : _Banner(row: row),
                        ActivityRowKind.status => _Banner(row: row),
                        ActivityRowKind.note => _NoteLine(
                            text: row.text,
                            asError: row.asError,
                          ),
                        ActivityRowKind.summary => const SizedBox.shrink(),
                      },
                    ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

/// Desktop phase words (CODING_AGENT_PHASE_LABELS). An unknown token stays as
/// the reducer sent it so nothing is silently renamed.
String _phaseLabel(BuildContext context, String phase) {
  switch (phase.trim().toLowerCase()) {
    case '':
      return '';
    case 'starting':
      return context.S.rtPhaseStarting;
    case 'running':
      return context.S.rtPhaseRunning;
    case 'completed':
      return context.S.rtPhaseCompleted;
    case 'failed':
      return context.S.rtPhaseFailed;
    case 'retrying':
      return context.S.rtPhaseRetrying;
    case 'skipped':
      return context.S.rtPhaseSkipped;
    case 'result':
      return context.S.rtPhaseResult;
    case 'waiting_writer':
      return context.S.rtPhaseWaitingWriter;
    case 'verifying_ui':
      return context.S.rtPhaseVerifyingUi;
    case 'unknown':
      return context.S.rtPhaseUnknown;
    default:
      return phase;
  }
}

/// Icon for a tool kind, matching the desktop CodingAgentProcessIcon set.
IconData _toolIcon(ToolKind kind) {
  switch (kind) {
    case ToolKind.run:
      return Icons.terminal_rounded;
    case ToolKind.read:
      return Icons.description_outlined;
    case ToolKind.write:
      return Icons.note_add_outlined;
    case ToolKind.edit:
      return Icons.edit_outlined;
    case ToolKind.find:
      return Icons.folder_open_outlined;
    case ToolKind.search:
      return Icons.search_rounded;
    case ToolKind.explore:
      return Icons.travel_explore_rounded;
    case ToolKind.todo:
      return Icons.checklist_rounded;
    case ToolKind.agent:
      return Icons.smart_toy_outlined;
    case ToolKind.other:
      return Icons.build_outlined;
  }
}

/// Status of one tool call, colored like the desktop outcome marks. Success
/// reuses the run-status green so the same outcome has one color app-wide.
({Color color, String label}) _toolStateTone(
  BuildContext context,
  ToolState state,
  String phase,
) {
  final scheme = Theme.of(context).colorScheme;
  switch (state) {
    case ToolState.running:
      return (color: scheme.primary, label: context.S.rtToolRunning);
    case ToolState.done:
      return (
        color: runStatusColor('done'),
        label: context.S.rtToolDone,
      );
    case ToolState.failed:
      return (color: scheme.error, label: context.S.rtToolFailed);
    case ToolState.unknown:
      return (
        color: scheme.onSurfaceVariant,
        label: _phaseLabel(context, phase),
      );
  }
}

/// One stored tool call or note inside a history turn. Same geometry as the
/// live [_ToolChip] so a call reads the same before and after the round ends.
class _HistoryRowView extends StatelessWidget {
  final HistoryRow row;

  const _HistoryRowView({required this.row});

  @override
  Widget build(BuildContext context) {
    final tool = row.tool;
    if (tool == null) {
      return _NoteLine(text: row.text, asError: row.asError);
    }
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final failed = tool.state == ToolState.failed;
    final tone = failed ? scheme.error : scheme.onSurfaceVariant;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(top: 1),
          child: Icon(
            _toolIcon(historyToolKind(tool.name)),
            size: 14,
            color: tone,
          ),
        ),
        const SizedBox(width: AppSpacing.sm),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                tool.name,
                key: const ValueKey('history-tool-name'),
                style: TextStyle(
                  fontFamily: 'monospace',
                  fontFamilyFallback: const ['Menlo', 'Consolas', 'Roboto Mono'],
                  fontSize: 12,
                  fontWeight: FontWeight.w600,
                  color: failed ? scheme.error : scheme.onSurface,
                  height: 1.3,
                ),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
              if (tool.target.trim().isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 1),
                  child: Text(
                    tool.target.trim(),
                    key: const ValueKey('history-tool-target'),
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                      height: 1.35,
                    ),
                    maxLines: 3,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
            ],
          ),
        ),
        if (tool.state != ToolState.unknown) ...[
          const SizedBox(width: AppSpacing.xs + 2),
          Padding(
            padding: const EdgeInsets.only(top: 1),
            child: AppBadge(
              label: failed ? context.S.rtToolFailed : context.S.rtToolDone,
              color: tone,
            ),
          ),
        ],
      ],
    );
  }
}

/// A tool call: kind icon, tool name in the code face, muted target, and a
/// tinted state pill on the right — the mobile form of the desktop tool row.
class _ToolChip extends StatelessWidget {
  final ActivityRow row;

  const _ToolChip({required this.row});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final tone = _toolStateTone(context, row.toolState, row.phase);
    final codeStyle = TextStyle(
      fontFamily: 'monospace',
      fontFamilyFallback: const ['Menlo', 'Consolas', 'Roboto Mono'],
      fontSize: 12,
      fontWeight: FontWeight.w600,
      color: scheme.onSurface,
      height: 1.3,
    );
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(top: 1),
          child: Icon(_toolIcon(row.toolKind), size: 14, color: tone.color),
        ),
        const SizedBox(width: AppSpacing.sm),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                row.toolName,
                style: codeStyle,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
              if (row.toolTarget.trim().isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 1),
                  child: Text(
                    row.toolTarget.trim(),
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                      height: 1.35,
                    ),
                    maxLines: 3,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
            ],
          ),
        ),
        if (tone.label.isNotEmpty) ...[
          const SizedBox(width: AppSpacing.xs + 2),
          Padding(
            padding: const EdgeInsets.only(top: 1),
            child: AppBadge(label: tone.label, color: tone.color),
          ),
        ],
      ],
    );
  }
}

/// Short left-rail banner: task-status changes and notes too small for a row
/// of their own.
class _Banner extends StatelessWidget {
  final ActivityRow row;

  const _Banner({required this.row});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final phase = _phaseLabel(context, row.phase);
    final failed = row.phase.trim().toLowerCase() == 'failed';
    final color = failed ? scheme.error : scheme.onSurfaceVariant;
    final title = row.title.isNotEmpty ? row.title : row.text;
    return Padding(
      padding: const EdgeInsets.only(left: 2),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (phase.isNotEmpty)
            Text(
              phase,
              style: theme.textTheme.labelSmall?.copyWith(
                color: color,
                fontWeight: FontWeight.w600,
              ),
            ),
          Text(
            title,
            style: theme.textTheme.bodySmall?.copyWith(
              color: failed ? scheme.error : scheme.onSurface,
              height: 1.4,
            ),
          ),
        ],
      ),
    );
  }
}

/// One thinking block inside the timeline. The live round shows the thought
/// body while it streams (the desktop keeps the newest thought open); a
/// closed round folds it to the header line.
class _ActivityThinkingRow extends StatelessWidget {
  final String text;
  final bool live;

  const _ActivityThinkingRow({required this.text, this.live = false});

  @override
  Widget build(BuildContext context) {
    return _ReasoningBlock(
      text: text,
      streaming: live,
      initiallyExpanded: live,
    );
  }
}

/// One live-note / reasoning line rendered as markdown — the bot's streaming
/// text carries formatting (steps, code ticks, lists) and plain Text showed
/// it raw. Compact spacing + optional muted/error tone.
class _NoteLine extends StatelessWidget {
  final String text;
  final bool muted;
  final bool asError;

  const _NoteLine({
    required this.text,
    this.muted = false,
    this.asError = false,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final base = asError
        ? theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.error,
            height: 1.45,
          )
        : muted
        ? theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
            height: 1.5,
          )
        : theme.textTheme.bodyMedium?.copyWith(height: 1.45);
    return MermaidMarkdown(
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
      borderRadius: BorderRadius.circular(AppRadius.sm),
    ),
  );
}

/// 标题区类型芯片。宽度受限时省略，不把 AppBar 撑出屏幕。
class _KindChip extends StatelessWidget {
  final String label;
  final Color color;

  const _KindChip({required this.label, required this.color});

  @override
  Widget build(BuildContext context) {
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 96),
      child: Container(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.sm,
          vertical: 2,
        ),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        child: Text(
          label,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          softWrap: false,
          style: TextStyle(
            fontSize: 11,
            color: color,
            fontWeight: FontWeight.w600,
          ),
        ),
      ),
    );
  }
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

/// 已发出、尚未被历史对上的用户消息。
class PendingUserEcho {
  final String text;
  final DateTime sentAt;
  bool sending;
  bool failed;
  String error;

  PendingUserEcho({
    required this.text,
    required this.sentAt,
    this.sending = true,
    this.failed = false,
    this.error = '',
  });
}

/// 只有失败且当前不在发送中的回显可以重试。
bool canRetryPendingEcho(PendingUserEcho echo) => echo.failed && !echo.sending;

/// 重试前清掉失败标记，重新进入发送中。
void beginPendingRetry(PendingUserEcho echo) {
  echo.sending = true;
  echo.failed = false;
  echo.error = '';
}

/// 请求已返回成功：去掉“发送中”，等历史对上后再移除气泡。
void markEchoSent(List<PendingUserEcho> pending, PendingUserEcho echo) {
  if (!pending.contains(echo)) return;
  echo.sending = false;
  echo.failed = false;
  echo.error = '';
}

/// 请求失败：标红并保留错误文案，供气泡上的重试入口使用。
void markEchoFailed(
  List<PendingUserEcho> pending,
  PendingUserEcho echo,
  String message,
) {
  if (!pending.contains(echo)) return;
  echo.sending = false;
  echo.failed = true;
  echo.error = message;
}

/// 历史里角色为 user 的消息条数。
int userMessageCount(List<RemoteMessage> messages) {
  var count = 0;
  for (final message in messages) {
    if (message.role == 'user') count++;
  }
  return count;
}

/// get_task 只带最近若干条。用户行变少说明窗口滑走了旧行，不能再按绝对序号跳过。
const remoteHistoryUserWindow = 40;

/// 历史对上本地回显后，时间差超过这个窗口就不当成同一次发送。
const pendingEchoMatchWindow = Duration(minutes: 2);

/// 桌面端给远控用户行加的 〔远控〕/〔…〕 前缀。对账前剥掉，避免和本地原文对不上。
final _remoteOriginMarker = RegExp(r'^〔[^〕]*〕');

String remoteEchoText(String raw) {
  final text = raw.trim();
  final match = _remoteOriginMarker.firstMatch(text);
  if (match == null) return text;
  return text.substring(match.end).trim();
}

/// 用户行没有独立时间戳。行在历史末尾、或相对上次已确认条数是新增行时，
/// 视为落在发送之后的合理范围内。没有文本基线时才走这条；有基线时按对齐结果。
bool historyEchoArrivedAfterSend({
  required int index,
  required int userCount,
  required int knownUserCount,
  required bool windowSlid,
}) {
  if (userCount <= 0 || index < 0 || index >= userCount) return false;
  if (index == userCount - 1) return true;
  if (windowSlid || knownUserCount <= 0) return false;
  return index >= knownUserCount;
}

bool pendingEchoMatchesHistoryText(PendingUserEcho echo, String historyText) {
  final left = remoteEchoText(echo.text);
  final right = remoteEchoText(historyText);
  return left.isNotEmpty && left == right;
}

/// 上次看到的用户行与这次窗口的最长后缀/前缀重叠。重叠之后才是新落地的行。
/// 窗口从前面滑走旧行时，绝对序号对不上，只能靠这段重叠找新增尾部。
int firstUnackedUserIndex(List<String> previous, List<String> users) {
  final maxOverlap = previous.length < users.length
      ? previous.length
      : users.length;
  for (var overlap = maxOverlap; overlap >= 0; overlap--) {
    var matches = true;
    for (var i = 0; i < overlap; i++) {
      if (previous[previous.length - overlap + i] != users[i]) {
        matches = false;
        break;
      }
    }
    if (matches) return overlap;
  }
  return 0;
}

/// 按顺序、按条数把历史里已落地的用户行配给待确认回显。
/// 连续相同文本各占一条，更早的同文消息不会误删还没对上的回显。
/// [ackedUserTexts] 有内容时按文本对齐，只消费新增尾部；调用后写回这次看到的用户行。
/// 没有基线时仍用 [knownUserCount]。返回这次看到的用户行数。
int reconcilePendingUserEchoes(
  List<PendingUserEcho> pending,
  List<RemoteMessage> messages, {
  int knownUserCount = 0,
  List<String>? ackedUserTexts,
  DateTime? now,
}) {
  final users = <String>[];
  for (final message in messages) {
    if (message.role != 'user') continue;
    final text = remoteEchoText(message.content);
    if (text.isEmpty) continue;
    users.add(text);
  }
  final nextKnown = users.length;
  final baseline = ackedUserTexts ?? const <String>[];
  final aligned = baseline.isNotEmpty;
  final windowSlid =
      !aligned &&
      (knownUserCount > nextKnown || nextKnown >= remoteHistoryUserWindow);
  var cursor = 0;
  if (aligned) {
    cursor = firstUnackedUserIndex(baseline, users);
  } else if (!windowSlid && knownUserCount > 0 && knownUserCount < nextKnown) {
    cursor = knownUserCount;
  }
  if (pending.isNotEmpty) {
    final clock = now ?? DateTime.now();
    pending.removeWhere((echo) {
      if (echo.failed || echo.sending) return false;
      if (clock.difference(echo.sentAt) > pendingEchoMatchWindow) return false;
      for (var index = cursor; index < users.length; index++) {
        final eligible =
            aligned ||
            historyEchoArrivedAfterSend(
              index: index,
              userCount: users.length,
              knownUserCount: knownUserCount,
              windowSlid: windowSlid,
            );
        if (!eligible) continue;
        if (!pendingEchoMatchesHistoryText(echo, users[index])) continue;
        cursor = index + 1;
        return true;
      }
      return false;
    });
  }
  // 还有发送中的回显时不要前移基线，否则这次已经落地的行下次对不上。
  if (ackedUserTexts != null && !pending.any((echo) => echo.sending)) {
    ackedUserTexts
      ..clear()
      ..addAll(users);
  }
  return nextKnown;
}


/// 我的消息 — 右侧气泡。发送中在气泡下方给小号进度；失败标红并保留重试。
class _UserBubble extends StatelessWidget {
  final String text;
  final bool sending;
  final bool failed;
  final String error;
  final VoidCallback? onRetry;

  const _UserBubble({
    required this.text,
    this.sending = false,
    this.failed = false,
    this.error = '',
    this.onRetry,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Align(
      alignment: Alignment.centerRight,
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxWidth: MediaQuery.of(context).size.width * 0.78,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            MessageBubble(
              role: ChatRole.user,
              maxWidth: MediaQuery.of(context).size.width * 0.78,
              margin: const EdgeInsets.only(top: AppSpacing.xs),
              child: SelectableText(text, style: const TextStyle(height: 1.45)),
            ),
            if (sending)
              Padding(
                key: const ValueKey('pending-echo-sending'),
                padding: const EdgeInsets.only(
                  bottom: AppSpacing.xs,
                  right: AppSpacing.xs,
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    SizedBox(
                      width: 12,
                      height: 12,
                      child: CircularProgressIndicator(
                        strokeWidth: 1.5,
                        color: scheme.onSurfaceVariant,
                      ),
                    ),
                    const SizedBox(width: AppSpacing.sm - 2),
                    Text(
                      context.S.chatSending,
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
            if (failed)
              Padding(
                key: const ValueKey('pending-echo-failed'),
                padding: const EdgeInsets.only(
                  bottom: AppSpacing.xs,
                  right: AppSpacing.xs,
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: [
                    Text(
                      error.isEmpty ? context.S.chatSendFailed : error,
                      textAlign: TextAlign.end,
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: scheme.error,
                      ),
                    ),
                    TextButton(
                      onPressed: onRetry,
                      style: TextButton.styleFrom(
                        padding: const EdgeInsets.symmetric(horizontal: AppSpacing.sm),
                        minimumSize: Size.zero,
                        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      ),
                      child: Text(
                        context.S.chatResend,
                        style: TextStyle(fontSize: 11, color: scheme.error),
                      ),
                    ),
                  ],
                ),
              ),
          ],
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
    // A stored tool call keeps its own row, the way the desktop transcript
    // lists it; the prose stays the answer body.
    final display = historyDisplayOf(message.historyEntry);
    return MessageBubble(
      role: ChatRole.assistant,
      maxWidth: MediaQuery.of(context).size.width * 0.84,
      margin: const EdgeInsets.symmetric(vertical: AppSpacing.sm),
      leading: Container(
        width: 30,
        height: 30,
        decoration: BoxDecoration(
          color: theme.colorScheme.primaryContainer,
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        child: Icon(
          Icons.auto_awesome_rounded,
          size: 16,
          color: theme.colorScheme.primary,
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (message.reasoning.isNotEmpty) ...[
            _ReasoningBlock(text: message.reasoning),
            const SizedBox(height: AppSpacing.sm),
          ],
          for (final row in display.toolRows)
            Padding(
              padding: const EdgeInsets.only(bottom: AppSpacing.sm),
              child: _HistoryRowView(row: row),
            ),
          if (display.answer.isNotEmpty)
            MermaidMarkdown(
              data: linkifyMarkdownUrls(display.answer),
              selectable: true,
              onTapLink: (text, href, title) {
                if (href != null) openExternalUrl(href);
              },
              styleSheet: _taskMarkdownStyle(theme),
            ),
          if (message.resultStatus.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: AppSpacing.sm),
              child: _StatusChip(status: message.resultStatus),
            ),
          if (message.filePaths.isNotEmpty) ...[
            const SizedBox(height: AppSpacing.sm),
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
    final scheme = theme.colorScheme;
    return Container(
      decoration: BoxDecoration(
        color: scheme.surface,
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
        borderRadius: BorderRadius.circular(AppRadius.sm),
      ),
      child: ExpansionTile(
        dense: true,
        initiallyExpanded: initiallyExpanded,
        tilePadding: const EdgeInsets.symmetric(horizontal: AppSpacing.md),
        childrenPadding: const EdgeInsets.fromLTRB(
          AppSpacing.md,
          0,
          AppSpacing.md,
          AppSpacing.sm,
        ),
        leading: Icon(
          Icons.psychology_outlined,
          size: 16,
          color: scheme.onSurfaceVariant,
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
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.sm,
        vertical: 3,
      ),
      decoration: BoxDecoration(
        color: color.withValues(alpha: .12),
        borderRadius: BorderRadius.circular(AppRadius.sm),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            failed ? Icons.error_outline : Icons.check_circle_outline,
            size: 12,
            color: color,
          ),
          const SizedBox(width: AppSpacing.xs),
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
      padding: const EdgeInsets.only(bottom: AppSpacing.sm - 2),
      child: InkWell(
        borderRadius: BorderRadius.circular(AppRadius.sm),
        onTap: downloading ? null : onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpacing.md - 2,
            vertical: AppSpacing.sm,
          ),
          decoration: BoxDecoration(
            color: theme.colorScheme.surface,
            border: Border.all(
              color: theme.colorScheme.outlineVariant.withValues(alpha: 0.7),
            ),
            borderRadius: BorderRadius.circular(AppRadius.sm),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(_iconFor(name), size: 18, color: theme.colorScheme.primary),
              const SizedBox(width: AppSpacing.sm),
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
              const SizedBox(width: AppSpacing.xs),
              downloading
                  ? const Padding(
                      padding: EdgeInsets.symmetric(horizontal: AppSpacing.md),
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
                        const SizedBox(width: AppSpacing.xs / 2),
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
