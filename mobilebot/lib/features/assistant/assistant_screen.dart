import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

import '../../core/chat_ui.dart' show ChatComposer, ChatRole, MessageBubble;
import '../../core/locale.dart';
import '../../core/ui_kit.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_tts/flutter_tts.dart';
import 'package:image_picker/image_picker.dart';
import 'package:record/record.dart';
import 'package:speech_to_text/speech_to_text.dart' as stt;
import 'package:uuid/uuid.dart';

import '../../components/mermaid/mermaid.dart';
import '../../core/asr_fallback.dart';
import '../../core/platform.dart';
import '../../core/providers.dart';
import '../../core/speech_text.dart';
import '../../core/voice_engine.dart';
import 'assistant_api.dart';

class _Msg {
  final String id;
  final bool isUser;
  String text;
  bool pending;
  bool failed = false;

  /// Bound document this user turn carried (displayed as a chip).
  final String attachment;

  _Msg({
    required this.id,
    required this.isUser,
    this.text = '',
    this.pending = false,
    this.attachment = '',
  });
}

/// 助手 — 免建 Bot 的直接问答（豆包式入口）。后端走 Hub
/// `/api/mobile/agent/jobs` 的整包服务端 agent：文本/图片/文档附件、
/// 语音输入（系统 ASR → 本地 SenseVoice 兜底），回复默认语音播报。
class AssistantScreen extends ConsumerStatefulWidget {
  const AssistantScreen({super.key});

  @override
  ConsumerState<AssistantScreen> createState() => _AssistantScreenState();
}

class _AssistantScreenState extends ConsumerState<AssistantScreen> {
  static const _uuid = Uuid();
  static const _pollInterval = Duration(milliseconds: 1600);
  static const _pollDeadline = Duration(minutes: 6, seconds: 30);

  final _inputCtrl = TextEditingController();
  final _scrollCtrl = ScrollController();
  final _speech = stt.SpeechToText();
  FlutterTts? _tts;
  AudioRecorder? _recorder;

  final List<_Msg> _messages = [];
  final Set<String> _spoken = {};
  bool _busy = false;
  bool _listening = false;

  /// 当前实际生效的识别引擎。回退时改成 local，并保留原因行。
  AsrEngine _activeAsr = AsrEngine.system;
  bool _asrFellBack = false;
  bool _recognizing = false;

  /// Live PCM level for the composer meter. Null means the current path has
  /// no samples (system recognition) — the composer then pulses instead.
  final ValueNotifier<double?> _listenLevel = ValueNotifier<double?>(null);

  /// Bumped on every listen start/stop so a late watchdog cannot fall back
  /// after the user already cancelled or a callback arrived.
  int _listenGen = 0;
  Timer? _listenWatchdog;

  /// Set when the composer drops the listening chrome *before* the session
  /// generation is retired. speech_to_text 7.4 can deliver `notListening`
  /// and the final hypothesis only after that. The watchdog and result
  /// callback still own this generation until [_noteListenCallback].
  bool _listenUiReset = false;

  /// 回复自动朗读 — 默认开启（产品要求），右上可关。
  bool _readOut = true;

  /// 已上传待绑定的文档（下一句话一起发给 agent）。
  String _docDraftId = '';
  String _docName = '';
  bool _docUploading = false;

  @override
  void initState() {
    super.initState();
    if (supportsVoiceOutput) {
      _tts = FlutterTts();
      unawaited(_tts!.setLanguage('zh-CN').then((_) {}).catchError((_) {}));
    }
  }

  @override
  void dispose() {
    // 先作废会话再拆输入框：stop() 的迟到回调不能写已释放的 controller。
    _noteListenCallback();
    _listening = false;
    _recognizing = false;
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    _listenLevel.value = null;
    _speech.stop();
    _tts?.stop();
    _recorder?.stop();
    _listenLevel.dispose();
    ref.read(voiceEngineProvider).stopSpeaking();
    super.dispose();
  }

  void _toast(String msg) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }

  void _scrollToEnd() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scrollCtrl.hasClients) {
        _scrollCtrl.jumpTo(_scrollCtrl.position.maxScrollExtent);
      }
    });
  }

  List<Map<String, String>> _history() => [
    for (final m in _messages)
      if (!m.pending && !m.failed && m.text.trim().isNotEmpty)
        {'role': m.isUser ? 'user' : 'assistant', 'content': m.text},
  ].reversed.take(20).toList().reversed.toList();

  Future<void> _send() async {
    final S = context.S;
    final api = ref.read(assistantApiProvider);
    final text = _inputCtrl.text.trim();
    if (api == null || _busy) return;
    if (text.isEmpty && _docDraftId.isEmpty) return;
    final query = text.isEmpty ? S.assistantAttachQuery : text;
    final docId = _docDraftId;
    final docName = _docName;
    _inputCtrl.clear();
    setState(() {
      _busy = true;
      _docDraftId = '';
      _docName = '';
      _messages.add(
        _Msg(id: _uuid.v4(), isUser: true, text: query, attachment: docName),
      );
      _messages.add(_Msg(id: _uuid.v4(), isUser: false, pending: true));
    });
    _scrollToEnd();
    final pending = _messages.last;
    try {
      var job = await api.createJob(
        query: query,
        messages: _history(),
        documentId: docId,
      );
      final deadline = DateTime.now().add(_pollDeadline);
      while (job.isActive && DateTime.now().isBefore(deadline)) {
        await Future<void>.delayed(_pollInterval);
        if (!mounted) return;
        job = await api.getJob(job.jobId);
      }
      if (!mounted) return;
      setState(() {
        pending.pending = false;
        if (job.isReady && job.answer.trim().isNotEmpty) {
          pending.text = job.answer;
        } else {
          pending.failed = true;
          pending.text = job.isFailed
              ? (job.message.isNotEmpty ? job.message : S.asstAnswerFailed)
              : S.asstAnswerTimeout;
        }
      });
      if (!pending.failed) _speakOnce(pending);
    } on Object catch (error) {
      if (!mounted) return;
      setState(() {
        pending.pending = false;
        pending.failed = true;
        pending.text = error.toString();
      });
    } finally {
      if (mounted) {
        setState(() => _busy = false);
        _scrollToEnd();
      }
    }
  }

  void _retry(_Msg failed) {
    final i = _messages.indexOf(failed);
    String query = '';
    for (var j = i - 1; j >= 0; j--) {
      if (_messages[j].isUser) {
        query = _messages[j].text;
        break;
      }
    }
    if (query.isEmpty || _busy) return;
    setState(() => _messages.remove(failed));
    _inputCtrl.text = query;
    _send();
  }

  // ---- 附件：拍照/相册/文件 → 上传 → 绑定到下一轮提问 ----

  Future<void> _attach() async {
    if (_busy || _docUploading) return;
    final choice = await showModalBottomSheet<String>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.photo_camera_outlined),
              title: Text(context.S.chatAttachCamera),
              onTap: () => Navigator.of(context).pop('camera'),
            ),
            ListTile(
              leading: const Icon(Icons.photo_library_outlined),
              title: Text(context.S.chatAttachGallery),
              onTap: () => Navigator.of(context).pop('gallery'),
            ),
            ListTile(
              leading: const Icon(Icons.attach_file),
              title: Text(context.S.chatAttachFile),
              onTap: () => Navigator.of(context).pop('file'),
            ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
    if (choice == null || !mounted) return;
    String? path;
    String? name;
    if (choice == 'camera' || choice == 'gallery') {
      final xfile = await ImagePicker().pickImage(
        source: choice == 'camera' ? ImageSource.camera : ImageSource.gallery,
        maxWidth: 2048,
        maxHeight: 2048,
        imageQuality: 85,
      );
      if (xfile == null) return;
      path = xfile.path;
      name = xfile.name;
    } else {
      final picked = await FilePicker.pickFiles();
      if (picked.isEmpty) return;
      path = picked.first.path;
      name = picked.first.name;
    }
    if (path == null || !mounted) return;
    await _uploadAndBind(path, name);
  }

  Future<void> _uploadAndBind(String path, String name) async {
    final S = context.S;
    final api = ref.read(assistantApiProvider);
    if (api == null) return;
    setState(() {
      _docUploading = true;
      _docName = name;
    });
    try {
      var task = await api.uploadDocument(path);
      final deadline = DateTime.now().add(const Duration(minutes: 3));
      while (task.taskId.isNotEmpty &&
          !task.isReady &&
          !task.isFailed &&
          DateTime.now().isBefore(deadline)) {
        await Future<void>.delayed(const Duration(milliseconds: 1200));
        if (!mounted) return;
        task = await api.getUploadTask(task.taskId);
      }
      if (!mounted) return;
      if (task.isReady && task.draftId.isNotEmpty) {
        setState(() => _docDraftId = task.draftId);
        _toast(S.asstAttached(name));
      } else {
        _toast(task.isFailed ? S.asstAttachFailed : S.asstAttachTimeout);
      }
    } on Object catch (error) {
      _toast(S.asstAttachUploadFailed(error.toString()));
    } finally {
      if (mounted) setState(() => _docUploading = false);
    }
  }

  // ---- 语音输入：系统 ASR → 本地 SenseVoice 兜底（同 Bot 聊天） ----

  String? _listenStatusText() {
    if (!_listening && !_recognizing && !_asrFellBack) return null;
    final S = context.S;
    if (_asrFellBack && _activeAsr == AsrEngine.local) {
      return S.chatListenFallbackLocal;
    }
    if (_recognizing) {
      return _activeAsr == AsrEngine.local
          ? S.chatRecognizingLocal
          : S.chatRecognizingSystem;
    }
    return _activeAsr == AsrEngine.local
        ? S.chatListenLocal
        : S.chatListenSystem;
  }

  void _beginListen(AsrEngine engine, {bool fellBack = false}) {
    _listenUiReset = false;
    setState(() {
      _listening = true;
      _recognizing = false;
      _activeAsr = engine;
      if (fellBack) _asrFellBack = true;
      if (!fellBack && engine == AsrEngine.system) _asrFellBack = false;
    });
  }

  Future<void> _dictate() async {
    if (!supportsVoiceInput || _listening) return;
    _asrFellBack = false;
    final engine = ref.read(voiceEngineProvider);
    await engine.ensurePrefs();
    final localReady = await engine.localAsrReady();
    final choice = decideAsrFallback(
      systemStarted: false,
      localReady: localReady,
      alreadyFellBack: false,
      engine: engine.asrEngine,
    );
    if (choice != AsrFallbackChoice.local) {
      // 选了本地但模型缺失/校验未过时，localAsrReady 一直是 false。
      // 先试系统识别，别只 toast「稍后再试」就结束。
      final started = await _startSystemDictate();
      if (started) return;
      // alreadyFellBack 表示本地采集也失败过。这里只是系统没起来，
      // 传 true 会把「本地已就绪」也判成稍后再试，下面的本地听写走不到。
      // 再查一次：点麦克风时未就绪，等系统 initialize 期间可能已下完。
      // 用这次结果做判定，不能只传给回退函数——旧的 false 会提前返回。
      final readyNow = await engine.localAsrReady();
      if (decideAsrFallback(
            systemStarted: started,
            localReady: readyNow,
            alreadyFellBack: false,
            engine: engine.asrEngine,
          ) ==
          AsrFallbackChoice.retryLater) {
        // 系统没起来、本地也不可用：和 Bot 页一样明确提示，不要静默结束。
        await _fallbackToLocalDictate(localReady: false);
        return;
      }
      // 系统也起不来且本地模型已就绪：继续本地听写。
    }
    await _fallbackToLocalDictate(
      localReady: await engine.localAsrReady(),
    );
  }

  void _cancelListenWatchdog() {
    _listenWatchdog?.cancel();
    _listenWatchdog = null;
  }

  /// 作废当前会话：之后到期的看门狗看到代次不符就不再回退。
  /// 音量回调不能走这里——健康识别器会持续打 onRmsChanged，一加代
  /// 看门狗就永远对不上，静默会话不会回退。
  /// 只清聆听态时走 [_markListenUiReset]，不要加代：终态和识别结果
  /// 经常晚于界面复位，加代会把它们和看门狗一起丢掉。
  void _noteListenCallback() {
    _listenGen++;
    _listenUiReset = false;
    _cancelListenWatchdog();
  }

  void _markListenUiReset() {
    _listenUiReset = true;
    if (mounted) setState(() => _listening = false);
  }

  void _keepListenAlive() {
    _cancelListenWatchdog();
  }

  /// `listen()` 正常返回后若平台一个回调都不发，输入栏会永远停在「聆听中」。
  /// 超时则停系统识别、复位，并回退本地 SenseVoice 一次。
  void _armListenWatchdog(int gen) {
    _listenWatchdog?.cancel();
    _listenWatchdog = Timer(kListenSilentWatchdog, () {
      if (!mounted ||
          !listenWatchdogStillOwns(
            armedGen: gen,
            currentGen: _listenGen,
            listening: _listening,
            uiReset: _listenUiReset,
          )) {
        return;
      }
      unawaited(_recoverSilentListen());
    });
  }

  Future<void> _recoverSilentListen() async {
    // 先换代：stop() 的迟到终态不能把随后的本地采集清掉。
    _noteListenCallback();
    try {
      await _speech.stop();
    } on Object {
      // 平台会话已经死了——停不掉也不影响回退。
    }
    try {
      await _recorder?.stop();
    } on Object {
      // 系统路径没开本地采集，或会话已经停了。
    }
    if (mounted) {
      _listenLevel.value = null;
      setState(() => _listening = false);
    }
    if (!mounted) return;
    // 与 Bot 输入框同一判定：本地模型没就绪就提示下载，不要空转再开麦。
    final engine = ref.read(voiceEngineProvider);
    await _fallbackToLocalDictate(
      localReady: await engine.localAsrReady(),
    );
  }

  /// 系统路径已经失败。本地就绪才采集；否则后台拉模型并明确提示。
  Future<void> _fallbackToLocalDictate({required bool localReady}) async {
    _asrFellBack = true;
    _activeAsr = AsrEngine.local;
    if (mounted) setState(() {});
    if (!localReady) {
      final hub = ref.read(sessionProvider).active?.hubUrl;
      unawaited(ref.read(voiceEngineProvider).autoEnsure(hubBase: hub));
      _toast(context.S.chatAsrUnavailableDownloading);
      return;
    }
    await _localDictate();
  }

  /// 系统听写。成功启动返回 true；不可用或启动失败返回 false，调用方回退本地。
  ///
  /// 识别词直接进输入框（同 Bot 聊天）：部分 OEM 只发结果不发终态，等
  /// 'done' 再上屏会让输入栏卡在「聆听中」而一个字都不落。
  /// 会话中途的 onError 只结束聆听态，不在这里回退——麦克风已开，再切
  /// 本地采集会抢麦。完全没有回调则由看门狗回退一次。
  Future<bool> _startSystemDictate() async {
    // 代次在 initialize 前捕获：它的 onError/onStatus 会晚于 stop() 到达，
    // 必须能和下一轮区分开。
    final gen = ++_listenGen;
    final bool ok;
    try {
      ok = await _speech
          .initialize(
      onError: (_) {
        if (!listenCallbackStillCurrent(
          callbackGen: gen,
          currentGen: _listenGen,
          listening: _listening,
          uiReset: _listenUiReset,
        )) {
          return;
        }
        // 平台报错说明会话已死，但终态/结果可能还在通道里。先只复位
        // 界面，等看门狗用同一代次回退；这里加代会让看门狗对不上。
        _markListenUiReset();
      },
      onStatus: (status) {
        if (!listenCallbackStillCurrent(
          callbackGen: gen,
          currentGen: _listenGen,
          listening: _listening,
          uiReset: _listenUiReset,
        )) {
          return;
        }
        // 中间态只续命。一加代，后面的 partial 会被当成过期回调丢掉。
        if (!listenStatusEndsTurn(status)) {
          _keepListenAlive();
          return;
        }
        // 终态先到：界面复位，但代次留给随后的 onResult。看门狗仍拥有
        // 这一代；若结果一直不来，到期后回退本地，而不是永远停在聆听中。
        _markListenUiReset();
      },
          )
          .timeout(kSystemListenProbeTimeout);
    } on Object {
      // recognizerNotAvailable / 通道挂起：没有识别器，立刻回退。
      _noteListenCallback();
      return false;
    }
    if (!ok || !mounted) return false;
    final prefix = _inputCtrl.text;
    final fallbackLocaleId = speechLocaleId(Localizations.localeOf(context));
    // 上一轮的界面复位不能算到这一轮。否则迟到终态会被当成当前会话。
    _listenUiReset = false;
    try {
      // speech_to_text 7.x：启动失败只在 MethodChannel 异常时抛。
      // initialize 已返回 true 时，空语言列表或 lastError 仍表示没有识别器。
      // 这两种情况 listen() 不抛也不回调，必须在开麦前退回本地。
      final before = await _probeSystemListen(fallbackLocaleId);
      if (!mounted) return false;
      if (systemListenUnavailable(before.probe)) {
        _noteListenCallback();
        return false;
      }
      _beginListen(AsrEngine.system);
      final params = SystemListenParams(localeId: before.localeId);
      await _speech.listen(
        listenOptions: stt.SpeechListenOptions(
          localeId: params.localeId,
          partialResults: params.partialResults,
          listenFor: params.listenFor,
          pauseFor: params.pauseFor,
          listenMode: stt.ListenMode.dictation,
        ),
        onSoundLevelChange: (_) => _keepListenAlive(),
        onResult: (r) {
          if (!listenCallbackStillCurrent(
            callbackGen: gen,
            currentGen: _listenGen,
            listening: _listening,
            uiReset: _listenUiReset,
          )) {
            return;
          }
          _keepListenAlive();
          if (!mounted) return;
          final text = applyRecognitionHypothesis(prefix, r.recognizedWords);
          _inputCtrl.value = TextEditingValue(
            text: text,
            selection: TextSelection.collapsed(offset: text.length),
          );
          // 词已上屏。作废这一代，看门狗不能再回退开第二只麦。
          if (r.recognizedWords.trim().isNotEmpty) _noteListenCallback();
        },
      );
    } on Object {
      // ListenFailedException / 通道错误：识别器没起来。复位后再回退，
      // 避免输入栏停在「聆听中」。
      _noteListenCallback();
      if (mounted) setState(() => _listening = false);
      return false;
    }
    if (!mounted) return false;
    // listen 正常返回仍可能是空识别器：插件丢掉了平台 bool，并在
    // speechRecognizer 为 null 时也 success(true)。再探一次；已经报错、
    // 没权限或语言列表变空就立刻回退，不要只等看门狗。
    final after = await _probeSystemListen(fallbackLocaleId);
    if (!mounted) return false;
    if (systemListenUnavailable(after.probe)) {
      _noteListenCallback();
      try {
        await _speech.stop();
      } on Object {
        // 平台会话可能根本没起来。
      }
      if (mounted) setState(() => _listening = false);
      return false;
    }
    // 不要用 isListening 判断：它要等平台 status 回调，成功启动时也可能
    // 仍是 false，误判会和系统识别抢麦。语言列表非空且没有错误时，仍由
    // 看门狗兜住「一个回调都不来」的机型。
    if (mounted) _armListenWatchdog(gen);
    return mounted;
  }

  /// listen 前后各探一次。语言列表查询失败按空列表处理——那就是
  /// 「没有可用识别器」，不能当成健康会话。
  Future<({SystemListenProbe probe, String? localeId})> _probeSystemListen(
    String fallbackLocaleId,
  ) async {
    var granted = false;
    var timedOut = false;
    try {
      granted = await _speech.hasPermission.timeout(kSystemListenProbeTimeout);
    } on TimeoutException {
      timedOut = true;
    } on Object {
      granted = false;
    }
    var locales = const <stt.LocaleName>[];
    if (!timedOut) {
      try {
        locales = await _speech.locales().timeout(kSystemListenProbeTimeout);
      } on TimeoutException {
        timedOut = true;
      } on Object {
        locales = const [];
      }
    }
    final err = _speech.lastError;
    return (
      probe: SystemListenProbe(
        permissionGranted: timedOut || granted,
        localeCount: locales.length,
        permanentError: err?.permanent ?? false,
        errorCode: err?.errorMsg ?? '',
        timedOut: timedOut,
      ),
      localeId: pickAsrLocale(locales, fallbackLocaleId: fallbackLocaleId),
    );
  }

  Future<Uint8List?> _capturePcm() async {
    final S = context.S;
    final recorder = _recorder ??= AudioRecorder();
    if (!await recorder.hasPermission()) {
      _toast(S.chatMicPermissionDenied);
      return null;
    }
    late final Stream<Uint8List> stream;
    try {
      stream = await recorder.startStream(
        const RecordConfig(
          encoder: AudioEncoder.pcm16bits,
          sampleRate: 16000,
          numChannels: 1,
        ),
      );
    } on Object {
      _toast(S.chatMicOpenFailed);
      return null;
    }
    if (mounted) {
      _beginListen(AsrEngine.local, fellBack: _asrFellBack);
      _listenLevel.value = 0;
    }
    final buf = BytesBuilder(copy: false);
    var lastLoud = DateTime.now();
    DateTime? lastLevelPublish;
    double? lastRms;
    var levelPending = false;
    try {
      await for (final chunk in stream) {
        if (!_listening) break;
        buf.add(chunk);
        var sum = 0.0;
        final n = chunk.length ~/ 2;
        for (var i = 0; i < n; i++) {
          final s = (chunk[i * 2] | (chunk[i * 2 + 1] << 8)).toSigned(16);
          sum += s * s;
        }
        final rms = n > 0 ? math.sqrt(sum / n) : 0.0;
        lastRms = rms;
        final now = DateTime.now();
        // ~32ms chunks; publish at kListenLevelThrottle so the meter does
        // not rebuild every chunk. A skipped chunk stays pending and is
        // flushed once when the stream ends.
        final publish = shouldPublishListenLevel(
          now: now,
          lastPublish: lastLevelPublish,
        );
        levelPending = !publish;
        if (mounted && publish) {
          _listenLevel.value = normalizeListenLevel(rms);
          lastLevelPublish = now;
        }
        if (rms > 400) lastLoud = now;
        final totalMs = buf.length / 32;
        final silenceMs = now.difference(lastLoud).inMilliseconds;
        if ((totalMs > 700 && silenceMs > 1200) || totalMs > 20000) {
          await recorder.stop();
          break;
        }
      }
    } on Object {
      // stream error → end of turn
    }
    // Stop, stream end, and stream error all leave here. A chunk skipped
    // by the throttle is still the last value and must be published once
    // before the meter is cleared below. Not a second clock check: the
    // gap may already have elapsed by the time we get here.
    if (mounted && levelPending && lastRms != null) {
      _listenLevel.value = normalizeListenLevel(lastRms);
      // One frame, not the whole throttle window: clearing in this turn
      // notifies twice before paint, so the last level never shows. A full
      // 70ms hold would delay stop and the transcript.
      await Future<void>.delayed(kListenLevelFlushHold);
    }
    await recorder.stop();
    if (mounted) {
      _listenLevel.value = null;
      setState(() => _listening = false);
    }
    return buf.takeBytes();
  }

  Future<void> _localDictate() async {
    final S = context.S;
    final engine = ref.read(voiceEngineProvider);
    if (!await engine.localAsrReady()) {
      final hub = ref.read(sessionProvider).active?.hubUrl;
      unawaited(engine.autoEnsure(hubBase: hub));
      _toast(S.chatAsrUnavailableDownloading);
      return;
    }
    final pcm = await _capturePcm();
    // 权限/开麦失败时 _capturePcm 已 toast。未挂载则页面已走，不必再提示。
    if (pcm == null || !mounted) return;
    if (pcm.length < 32000) {
      _toast(S.chatNotHeard);
      return;
    }
    try {
      final text = (await engine.transcribe(pcm)).trim();
      if (RegExp(r'[\w一-鿿]{2,}').hasMatch(text)) {
        _appendInput(text);
      } else {
        _toast(S.chatNotHeard);
      }
    } on Object {
      if (!await engine.localAsrReady()) {
        // 权重校验/加载失败：文件已丢弃。选了本地才再试系统，和 Bot 输入框
        // 同一条件。系统路径失败后进来的调用方不再开麦。
        if (engine.asrEngine == AsrEngine.local) {
          final started = await _startSystemDictate();
          if (started) return;
        }
        final hub = ref.read(sessionProvider).active?.hubUrl;
        unawaited(engine.autoEnsure(hubBase: hub));
        _toast(S.chatAsrUnavailableDownloading);
        return;
      }
      _toast(S.chatAsrFailed);
    }
  }

  void _appendInput(String text) {
    setState(() {
      _inputCtrl.text = _inputCtrl.text.isEmpty
          ? text
          : '${_inputCtrl.text} $text';
      _inputCtrl.selection = TextSelection.collapsed(
        offset: _inputCtrl.text.length,
      );
    });
  }

  // ---- 语音播报：默认开，Kokoro → 系统 TTS 兜底 ----

  Future<void> _speakOnce(_Msg m) async {
    if (!_readOut || !supportsVoiceOutput || _spoken.contains(m.id)) return;
    _spoken.add(m.id);
    final text = plainTextForSpeech(m.text);
    if (text.isEmpty) return;
    final engine = ref.read(voiceEngineProvider);
    if (await engine.useKokoro) {
      final err = await engine.speakKokoro(text, onDone: () {});
      if (err == null) return;
    }
    try {
      await _tts?.stop();
      await _tts?.speak(text);
    } on Object {
      // TTS is best-effort — the answer is already on screen.
    }
  }

  void _newChat() {
    if (_busy) return;
    setState(() {
      _messages.clear();
      _spoken.clear();
      _docDraftId = '';
      _docName = '';
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Text(context.S.tabAssistant),
        actions: [
          if (supportsVoiceOutput)
            IconButton(
              tooltip: _readOut
                  ? context.S.asstReadOutDisable
                  : context.S.asstReadOutEnable,
              icon: Icon(
                _readOut ? Icons.volume_up : Icons.volume_off_outlined,
              ),
              onPressed: () {
                setState(() => _readOut = !_readOut);
                if (!_readOut) {
                  _tts?.stop();
                  ref.read(voiceEngineProvider).stopSpeaking();
                }
              },
            ),
          IconButton(
            tooltip: context.S.asstNewChat,
            icon: const Icon(Icons.add_comment_outlined),
            onPressed: _busy ? null : _newChat,
          ),
        ],
      ),
      body: Column(
        children: [
          Expanded(
            child: _messages.isEmpty
                ? _EmptyState(
                    onAsk: (q) {
                      _inputCtrl.text = q;
                      _send();
                    },
                  )
                : ListView.builder(
                    controller: _scrollCtrl,
                    padding: const EdgeInsets.fromLTRB(12, 12, 12, 8),
                    itemCount: _messages.length,
                    itemBuilder: (context, i) => _bubble(theme, _messages[i]),
                  ),
          ),
          if (_docName.isNotEmpty || _docUploading)
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 4),
              child: Align(
                alignment: Alignment.centerLeft,
                child: InputChip(
                  avatar: _docUploading
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.attach_file, size: 18),
                  label: Text(_docName),
                  onDeleted: _docUploading
                      ? null
                      : () => setState(() {
                          _docDraftId = '';
                          _docName = '';
                        }),
                ),
              ),
            ),
          ChatComposer(
            controller: _inputCtrl,
            hint: _listening
                ? context.S.chatVoiceListening
                : context.S.asstComposerHint,
            sending: _busy,
            onSend: _send,
            onSubmitted: (_) => _send(),
            onAttach: _busy || _docUploading ? null : _attach,
            attachTooltip: context.S.asstAttachTooltip,
            onMic: supportsVoiceInput
                ? (_listening
                      ? () {
                          _noteListenCallback();
                          _listenLevel.value = null;
                          _speech.stop();
                          _recorder?.stop();
                          setState(() => _listening = false);
                        }
                      : _dictate)
                : null,
            listening: _listening,
            level: _listenLevel,
            listenStatus: _listenStatusText(),
          ),
        ],
      ),
    );
  }

  Widget _bubble(ThemeData theme, _Msg m) {
    final scheme = theme.colorScheme;
    return MessageBubble(
      role: m.isUser ? ChatRole.user : ChatRole.assistant,
      maxWidth: MediaQuery.of(context).size.width * 0.78,
      leading: m.isUser
          ? null
          : Container(
              width: 28,
              height: 28,
              margin: const EdgeInsets.only(top: 2),
              decoration: BoxDecoration(
                color: scheme.primaryContainer,
                shape: BoxShape.circle,
              ),
              child: Icon(Icons.auto_awesome, size: 15, color: scheme.primary),
            ),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
      margin: const EdgeInsets.symmetric(vertical: 4),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (m.attachment.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 4),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Icon(Icons.attach_file, size: 14),
                  const SizedBox(width: 4),
                  Flexible(
                    child: Text(
                      m.attachment,
                      style: theme.textTheme.bodySmall,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
            ),
          if (m.pending)
            const SizedBox(
              width: 18,
              height: 18,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          else if (m.isUser)
            Text(m.text)
          else ...[
            MermaidMarkdown(data: m.text, selectable: true),
            if (m.failed)
              TextButton.icon(
                onPressed: () => _retry(m),
                icon: const Icon(Icons.refresh, size: 16),
                label: Text(context.S.retry),
              ),
          ],
        ],
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  final void Function(String) onAsk;

  const _EmptyState({required this.onAsk});

  @override
  Widget build(BuildContext context) {
    final suggestions = [
      context.S.asstSuggestWeekly,
      context.S.asstSuggestExplain,
      context.S.asstSuggestSummarize,
    ];
    return ListView(
      padding: const EdgeInsets.all(24),
      children: [
        const SizedBox(height: 40),
        EmptyState(
          icon: Icons.auto_awesome,
          title: context.S.asstEmptyTitle,
          hint: context.S.asstEmptyHint,
          action: Wrap(
            spacing: 8,
            runSpacing: 8,
            alignment: WrapAlignment.center,
            children: [
              for (final s in suggestions)
                OutlinedButton(onPressed: () => onAsk(s), child: Text(s)),
            ],
          ),
        ),
      ],
    );
  }
}
