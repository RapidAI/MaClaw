import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:image_picker/image_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';
import 'dart:math' as math;

import 'package:flutter_tts/flutter_tts.dart';
import 'package:record/record.dart';
import 'package:speech_to_text/speech_to_text.dart' as stt;

import '../../components/mermaid/mermaid.dart';
import '../../core/asr_fallback.dart';
import '../../core/native_voice.dart';
import '../../core/platform.dart';
import '../../core/providers.dart';
import '../../core/speech_text.dart';
import '../../core/voice_engine.dart';
import '../../core/chat_ui.dart' show ChatComposer, ChatRole, MessageBubble;
import '../../core/locale.dart';
import '../../core/mic_recorder.dart';
import '../../l10n/gen/app_localizations.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'bot_chat_controller.dart';
import 'bot_models.dart';
import 'bots_controller.dart';
import 'send_trace.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart'
    show BrandMark, LinkifiedText, linkifyMarkdownUrls, openExternalUrl;
import 'widgets.dart';

/// One bot's workspace — the mobile version of the desktop GUI's
/// DesktopBotWorkspace: chat transcript + composer, with the desktop view,
/// work status and settings one tap away.
class BotChatScreen extends ConsumerStatefulWidget {
  final Bot bot;

  const BotChatScreen({super.key, required this.bot});

  @override
  ConsumerState<BotChatScreen> createState() => _BotChatScreenState();
}

class _BotChatScreenState extends ConsumerState<BotChatScreen> {
  final _inputCtrl = TextEditingController();
  final _scrollCtrl = ScrollController();
  final _speech = stt.SpeechToText();
  double? _uploadProgress;
  String _uploadingName = '';
  bool _listening = false;
  AsrEngine _activeAsr = AsrEngine.system;
  bool _recognizing = false;

  /// Live PCM level for the composer meter. Null on the system path.
  final ValueNotifier<double?> _listenLevel = ValueNotifier<double?>(null);

  String _speechPrefix = '';
  int _listenGen = 0;
  Timer? _listenWatchdog;

  /// Composer chrome already cleared, session generation not yet retired.
  /// Same contract as the assistant page: a late result and the silent
  /// watchdog still belong to this generation.
  bool _listenUiReset = false;

  /// 语音助手模式（豆包式）：开启后自动聆听→识别即发送→朗读回复→再聆听。
  FlutterTts? _tts;
  bool _voiceMode = false;
  bool _ttsBusy = false;
  bool _relistenQueued = false;
  String _voiceCaptured = '';
  String _lastSpokenMsgId = '';
  /// 进程内共享的采集器（见 core/mic_recorder.dart），页面只持有引用。
  AudioRecorder? _recorder;

  late final AppLifecycleListener _lifecycle;

  @override
  void initState() {
    super.initState();
    // Pause the journal long-poll and run polling while pocketed — an
    // unseen chat must not hold a 25s connection open. Resuming re-arms
    // both so new lines and the in-flight turn resume instantly.
    _lifecycle = AppLifecycleListener(
      onStateChange: (state) {
        final foreground =
            state == AppLifecycleState.resumed ||
            state == AppLifecycleState.inactive;
        ref.read(botChatProvider.notifier).setForeground(foreground);
        if (!foreground) _stopVoiceMode();
      },
    );
    // 发送键跟随输入框内容：有字就可点。
    _inputCtrl.addListener(_onInputChanged);
  }

  // 输入框重绘只为一件事：有字时发送键可点（见 build 里的 sending）。
  void _onInputChanged() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    // 先作废会话再拆输入框：stop() 的迟到回调不能写已释放的 controller。
    _noteListenCallback();
    _listening = false;
    _recognizing = false;
    _stopVoiceMode();
    _inputCtrl.removeListener(_onInputChanged);
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    _listenLevel.value = null;
    _speech.stop();
    _recorder?.stop();
    _listenLevel.dispose();
    super.dispose();
  }

  void _cancelListenWatchdog() {
    _listenWatchdog?.cancel();
    _listenWatchdog = null;
  }

  /// 作废当前会话：之后到期的看门狗看到代次不符就不再回退。
  /// 音量回调不能走这里——健康识别器会持续打 onRmsChanged，一加代
  /// 看门狗就永远对不上，静默会话不会回退。
  /// 只清聆听态时走 [_markListenUiReset]，不要加代。
  void _noteListenCallback() {
    _listenGen++;
    _listenUiReset = false;
    _cancelListenWatchdog();
  }

  void _markListenUiReset() {
    _listenUiReset = true;
    _listening = false;
    if (mounted) setState(() {});
  }

  void _keepListenAlive() {
    _cancelListenWatchdog();
  }

  /// `listen()` 已返回，但之后没有任何识别/状态/错误/音量回调。
  /// 停系统识别并回退本地一次；本地不可用时由 `_localDictate` toast。
  void _armComposerWatchdog(int gen) {
    _listenWatchdog?.cancel();
    _listenWatchdog = Timer(kListenSilentWatchdog, () {
      if (!mounted ||
          _voiceMode ||
          !listenWatchdogStillOwns(
            armedGen: gen,
            currentGen: _listenGen,
            listening: _listening,
            uiReset: _listenUiReset,
          )) {
        return;
      }
      unawaited(_recoverSilentComposer());
    });
  }

  Future<void> _recoverSilentComposer() async {
    // 先换代：stop() 的迟到终态不能把随后的本地采集清掉。
    _noteListenCallback();
    try {
      await _speech.stop();
    } on Object {
      // 平台会话已经死了。
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
    await _fallbackComposerToLocal();
  }

  /// 语音助手循环里的同样死会话：停掉后回退本地一轮，而不是永远「聆听中」。
  void _armVoiceWatchdog(int gen) {
    _listenWatchdog?.cancel();
    _listenWatchdog = Timer(kListenSilentWatchdog, () {
      if (!mounted ||
          !_voiceMode ||
          !listenWatchdogStillOwns(
            armedGen: gen,
            currentGen: _listenGen,
            listening: _listening,
            uiReset: _listenUiReset,
          )) {
        return;
      }
      unawaited(_recoverSilentVoiceTurn());
    });
  }

  Future<void> _recoverSilentVoiceTurn() async {
    // 先换代：stop() 的迟到终态不能结束随后的本地一轮。
    _noteListenCallback();
    try {
      await _speech.stop();
    } on Object {
      // 平台会话已经死了。
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
    if (!mounted || !_voiceMode) return;
    await _fallbackVoiceToLocal();
  }

  /// 系统识别没起来时留下的具体原因。两条路都不通时必须说出来。
  String _lastSystemListenReason = '';

  /// Engine at work, never a reason why another was skipped: the local
  /// engine taking over is the normal path here, not an error. A failure
  /// with no engine left is a toast.
  String? _listenStatusText() {
    if (!_listening && !_recognizing) return null;
    final S = context.S;
    if (_recognizing) {
      return _activeAsr == AsrEngine.local
          ? S.chatRecognizingLocal
          : S.chatRecognizingSystem;
    }
    return _activeAsr == AsrEngine.local
        ? S.chatListenLocal
        : S.chatListenSystem;
  }

  void _beginListen(AsrEngine engine) {
    _listenUiReset = false;
    setState(() {
      _listening = true;
      _recognizing = false;
      _activeAsr = engine;
    });
  }

  /// On-device speech-to-text: words land straight in the composer so the
  /// person can edit before sending.
  Future<void> _toggleListening() async {
    if (_listening) {
      _noteListenCallback();
      await _speech.stop();
      try {
        await _recorder?.stop(); // 本地听写进行中：提前结束本轮并转写
      } on Object {
        // 未在录音时 stop() 可能抛错——忽略，本轮本来就结束了
      }
      if (mounted) {
        _listenLevel.value = null;
        setState(() => _listening = false);
      }
      return;
    }
    // 代次在 initialize 前捕获：它的 onError/onStatus 会晚于 stop() 到达。
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
        // 只复位界面。加代会让还没到期的看门狗放弃回退。
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
        if (!listenStatusEndsTurn(status)) {
          _keepListenAlive();
          return;
        }
        // 终态先到不retire代次：随后的识别词仍要进输入框。一直没有
        // 结果时，看门狗用同一代次回退本地。
        _markListenUiReset();
      },
          )
          .timeout(kSystemListenProbeTimeout);
    } on Object {
      if (!mounted) return;
      await _fallbackComposerToLocal();
      return;
    }
    if (!mounted) return;
    if (!ok) {
      // 系统识别不可用（无 GMS/华为等）→ 本地 SenseVoice 听写兜底；
      // 模型未下载时明确提示，和助手页同一句。
      await _fallbackComposerToLocal();
      return;
    }
    final started = await _listenSystemDictate();
    if (!started && mounted) await _fallbackComposerToLocal();
  }

  /// 系统听写没起来。本地就绪才采集；否则后台拉模型并明确提示。
  Future<void> _fallbackComposerToLocal() async {
    _activeAsr = AsrEngine.local;
    if (mounted) setState(() {});
    final engine = ref.read(voiceEngineProvider);
    if (!await engine.localAsrReady()) {
      if (!mounted) return;
      _ensureLocalVoiceModels();
      // 本地也不可用时必须带上系统那侧的原因，否则点麦克风看着没反应。
      _toast(
        _lastSystemListenReason.isEmpty
            ? context.S.chatAsrUnavailableDownloading
            : context.S.chatAsrFailedWith(_lastSystemListenReason),
      );
      return;
    }
    await _localDictate();
  }

  /// 系统听写写入输入框。listen 抛错时复位并返回 false，调用方可回退本地。
  ///
  /// 先要端上识别：不设 onDevice 时每次都交给默认（多为云端）识别器，
  /// 没有 GMS 或没网的机型一个词都不回。没有端上模型的机型会直接拒绝，
  /// 第二次保留默认识别器。
  Future<bool> _listenSystemDictate() async {
    if (await _listenSystemDictateWith(onDevice: true)) return true;
    if (!mounted) return false;
    return _listenSystemDictateWith(onDevice: false);
  }

  Future<bool> _listenSystemDictateWith({required bool onDevice}) async {
    _speechPrefix = _inputCtrl.text;
    final fallbackLocaleId = speechLocaleId(Localizations.localeOf(context));
    // 上一轮的界面复位不能算到这一轮。
    _listenUiReset = false;
    // 输入框路径的 initialize 已经占了当前代次；这里复用，避免再加一代
    // 让看门狗和回调对不上。语音助手路径不会进这个函数。
    final gen = _listenGen;
    try {
      // initialize 返回 true 仍可能没有识别器。listen() 在那种机型上
      // 不抛也不回调，开麦前就要退回本地，不能只等看门狗。
      final before = await _probeSystemListen(fallbackLocaleId);
      if (!mounted) return false;
      if (systemListenUnavailable(before.probe)) {
        _lastSystemListenReason = systemListenProbeReason(before.probe);
        _noteListenCallback();
        return false;
      }
      _beginListen(AsrEngine.system);
      final params = SystemListenParams(
        localeId: before.localeId,
        onDevice: onDevice,
      );
      await _speech.listen(
        listenOptions: stt.SpeechListenOptions(
          localeId: params.localeId,
          partialResults: params.partialResults,
          listenFor: params.listenFor,
          pauseFor: params.pauseFor,
          listenMode: stt.ListenMode.dictation,
          onDevice: params.onDevice,
        ),
        onSoundLevelChange: (_) => _keepListenAlive(),
        onResult: (result) {
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
          final text = applyRecognitionHypothesis(
            _speechPrefix,
            result.recognizedWords,
          );
          _inputCtrl.value = TextEditingValue(
            text: text,
            selection: TextSelection.collapsed(offset: text.length),
          );
          // 词已上屏。作废这一代，看门狗不能再回退开第二只麦。
          if (result.recognizedWords.trim().isNotEmpty) _noteListenCallback();
        },
      );
    } on Object catch (e) {
      _lastSystemListenReason = '$e';
      _noteListenCallback();
      if (mounted) setState(() => _listening = false);
      return false;
    }
    if (!mounted) return false;
    final after = await _probeSystemListen(fallbackLocaleId);
    if (!mounted) return false;
    if (systemListenUnavailable(after.probe)) {
      _lastSystemListenReason = systemListenProbeReason(after.probe);
      _noteListenCallback();
      try {
        await _speech.stop();
      } on Object {
        // 平台会话可能根本没起来。
      }
      if (mounted) setState(() => _listening = false);
      return false;
    }
    if (mounted) _armComposerWatchdog(gen);
    return mounted;
  }

  /// 语音助手开关。开：立即进入聆听循环；关：停 ASR+TTS，回到手动输入。
  Future<void> _toggleVoiceMode() async {
    if (_voiceMode) {
      _stopVoiceMode();
      return;
    }
    if (!supportsVoiceInput || !supportsVoiceOutput) return;
    final S = context.S;
    final tts = _tts ??= FlutterTts();
    try {
      await tts.setLanguage('zh-CN');
      tts.setCompletionHandler(_onVoiceTurnDone);
      tts.setErrorHandler((_) => _onVoiceTurnDone());
      // 基线：开启时已有回复不再朗读，只读开启后的新回复。
      _lastSpokenMsgId = _latestAssistantId(ref.read(botChatProvider));
    } catch (_) {
      _toast(S.chatVoiceInitFailed);
      return;
    }
    setState(() => _voiceMode = true);
    _toast(S.chatVoiceAssistantStarted);
    _voiceListen();
  }

  void _stopVoiceMode() {
    if (!_voiceMode) return;
    _noteListenCallback();
    _voiceMode = false;
    _relistenQueued = false;
    _voiceCaptured = '';
    _listening = false;
    _recognizing = false;
    if (mounted) _listenLevel.value = null;
    _ttsBusy = false;
    _speech.stop();
    _tts?.stop();
    _recorder?.stop();
    ref.read(voiceEngineProvider).stopSpeaking();
    if (mounted) setState(() {});
  }

  String _latestAssistantId(ChatState state) {
    for (final m in state.messages.reversed) {
      if (!m.isUser && !m.pending) return m.id;
    }
    return '';
  }

  /// The header's status line: the live step phrase when Hub reports one,
  /// otherwise the generic "working" label; idle when no run is in flight.
  String _headerStatus(ChatState state) {
    if (state.sending || state.activeRunId != null) {
      return state.activity.isNotEmpty ? state.activity : context.S.chatProcessing;
    }
    return context.S.chatOnline;
  }

  /// 一轮聆听的收口：最终识别词非空→自动发送；空（静默/超时）→稍后重新
  /// 聆听。onStatus 终态与 onResult 的 finalResult 都会进来（部分 OEM 识别
  /// 器只发结果不发终态状态），靠 _listening 幂等，先到者生效。
  void _handleVoiceTurnEnd() {
    if (!_listening) return;
    _listening = false;
    final text = _voiceCaptured.trim();
    _voiceCaptured = '';
    // 实时 partial 只是预览。自动发送的是识别词，输入框回到开麦前的草稿，
    // 避免同一句再被手动发出去。
    if (mounted) {
      // 只剥掉本轮识别预览。预览后面手打的字留下来；改过预览本身则整段保留。
      final restored = restoreComposerAfterHypothesis(
        prefix: _speechPrefix,
        recognizedWords: text,
        draft: _inputCtrl.text,
      );
      if (restored != _inputCtrl.text) {
        _inputCtrl.value = TextEditingValue(
          text: restored,
          selection: TextSelection.collapsed(offset: restored.length),
        );
      }
      setState(() {});
    }
    if (!_voiceMode) return;
    if (text.isNotEmpty) {
      ref.read(botChatProvider.notifier).send(text);
    } else {
      _scheduleVoiceRelisten();
    }
  }

  /// 系统路径已经静默失败。本地没就绪时明确提示并退出，不能只停麦。
  Future<void> _fallbackVoiceToLocal() async {
    _activeAsr = AsrEngine.local;
    if (mounted) setState(() {});
    final engine = ref.read(voiceEngineProvider);
    if (await engine.localAsrReady()) {
      await _recordListen();
      return;
    }
    if (!mounted || !_voiceMode) return;
    _ensureLocalVoiceModels();
    _toast(context.S.chatAsrUnavailableDownloading);
    _stopVoiceMode();
  }

  /// 聆听一轮：最终识别结果非空→自动发送；空（静默/超时）→稍后重新聆听。
  Future<void> _voiceListen() async {
    if (!_voiceMode || _listening || _ttsBusy) return;
    final engine = ref.read(voiceEngineProvider);
    await engine.ensurePrefs();
    final localReady = await engine.localAsrReady();
    if (decideAsrFallback(
          systemStarted: false,
          localReady: localReady,
          alreadyFellBack: false,
          engine: engine.asrEngine,
        ) ==
        AsrFallbackChoice.local) {
      await _recordListen();
      return;
    }
    // 模型缺失/校验失败时 localAsrReady 永远 false。不要停掉语音模式
    // 只留一句「稍后再试」——先试系统识别。
    final started = await _startSystemVoiceTurn();
    if (started || _listening) return;
    // alreadyFellBack 表示本地采集也失败过。这里只是系统没起来，
    // 传 true 会把「本地已就绪」也判成稍后再试，下面的本地一轮走不到。
    // 再查一次：开麦时未就绪，等系统 initialize 期间可能已下完。
    final readyNow = await engine.localAsrReady();
    if (decideAsrFallback(
          systemStarted: started,
          localReady: readyNow,
          alreadyFellBack: false,
          engine: engine.asrEngine,
        ) ==
        AsrFallbackChoice.retryLater) {
      // 与助手页同一句：系统没起来且本地不可用时明确提示，再退出语音模式。
      if (mounted) {
        _ensureLocalVoiceModels();
        _toast(context.S.chatAsrUnavailableDownloading);
        _stopVoiceMode();
      }
      return;
    }
    // initialize/listen 没起来（无 GMS 等）且本地模型已就绪 → SenseVoice。
    // 会话中途出错不会走到这里：onError 已把 _listening 复位并排队重听。
    await _fallbackVoiceToLocal();
  }

  /// 系统听写一轮。成功启动返回 true；不可用返回 false，调用方决定回退。
  ///
  /// 会话中途的 onError 只结束本轮并排队重听，不在这里切本地采集——
  /// 麦克风已开，再开 record 会抢麦。
  Future<bool> _startSystemVoiceTurn() async {
    // 代次在 initialize 前捕获：stop() 后的迟到终态不能结束下一轮。
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
        // 麦克风已开，这里切本地会抢麦。先复位，等看门狗回退；
        // 若终态随后把词带回来，由 onResult 上屏并取消看门狗。
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
        if (!listenStatusEndsTurn(status)) {
          _keepListenAlive();
          return;
        }
        // 已有识别词才收口并作废这一代。空终态只复位界面：部分 OEM
        // 先发 notListening，识别词随后才到。这里加代会把词丢掉，
        // 看门狗也对不上，静默会话不再回退。
        if (_voiceCaptured.trim().isNotEmpty) {
          _handleVoiceTurnEnd();
          _noteListenCallback();
          return;
        }
        _markListenUiReset();
      },
          )
          .timeout(kSystemListenProbeTimeout);
    } on Object {
      _noteListenCallback();
      return false;
    }
    if (!ok || !mounted || !_voiceMode) return false;
    _voiceCaptured = '';
    _speechPrefix = _inputCtrl.text;
    final fallbackLocaleId = speechLocaleId(Localizations.localeOf(context));
    _listenUiReset = false;
    try {
      final before = await _probeSystemListen(fallbackLocaleId);
      if (!mounted || !_voiceMode) return false;
      if (systemListenUnavailable(before.probe)) {
        _noteListenCallback();
        return false;
      }
      _beginListen(AsrEngine.system);
      // 语音助手模式（自动发送）也要端上识别优先，回退逻辑同上。
      final params = SystemListenParams(
        localeId: before.localeId,
        onDevice: true,
      );
      await _speech.listen(
        listenOptions: stt.SpeechListenOptions(
          localeId: params.localeId,
          partialResults: params.partialResults,
          listenFor: params.listenFor,
          pauseFor: params.pauseFor,
          listenMode: stt.ListenMode.dictation,
          onDevice: params.onDevice,
        ),
        onSoundLevelChange: (_) => _keepListenAlive(),
        onResult: (result) {
          if (!listenCallbackStillCurrent(
            callbackGen: gen,
            currentGen: _listenGen,
            listening: _listening,
            uiReset: _listenUiReset,
          )) {
            return;
          }
          _keepListenAlive();
          // 终态可能先到并已自动发送。那一次会加代，上面的判定已拦住。
          // 同一代次里界面已复位时，识别词仍要进输入框。
          _voiceCaptured = result.recognizedWords;
          if (mounted) {
            final text = applyRecognitionHypothesis(
              _speechPrefix,
              result.recognizedWords,
            );
            _inputCtrl.value = TextEditingValue(
              text: text,
              selection: TextSelection.collapsed(offset: text.length),
            );
          }
          if (result.finalResult) {
            _handleVoiceTurnEnd();
            // 本轮已收口（发出或判定为空）。看门狗不能再回退开麦。
            _noteListenCallback();
          }
        },
      );
    } on Object {
      _noteListenCallback();
      _listening = false;
      if (mounted) setState(() {});
      return false;
    }
    if (!mounted || !_voiceMode) return false;
    final after = await _probeSystemListen(fallbackLocaleId);
    if (!mounted || !_voiceMode) return false;
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
    if (mounted && _voiceMode) _armVoiceWatchdog(gen);
    return mounted && _voiceMode;
  }

  /// listen 前后各探一次。查询失败按不可用处理，立刻回退而不是空等。
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

  /// 输入框听写的系统路径。成功启动返回 true。
  Future<bool> _startComposerSystemDictate() async {
    // 代次在 initialize 前捕获，和听写回调共用，迟到 stop() 不能清下一轮。
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
        if (!listenStatusEndsTurn(status)) {
          _keepListenAlive();
          return;
        }
        _markListenUiReset();
      },
          )
          .timeout(kSystemListenProbeTimeout);
    } on Object {
      _noteListenCallback();
      return false;
    }
    if (!ok || !mounted) return false;
    return _listenSystemDictate();
  }

  /// 本地模型缺失时后台拉起下载（GitHub → Hub 镜像），用户不用进设置页。
  /// 但本地引擎是安装包里的原生库，不是可下载的资源：这次安装包没有它时，
  /// 再下载也永远「就绪」不了，别让用户一直等一句下载中的提示。
  void _ensureLocalVoiceModels() {
    if (!NativeVoice.usable) return;
    final hub = ref.read(sessionProvider).active?.hubUrl;
    unawaited(ref.read(voiceEngineProvider).autoEnsure(hubBase: hub));
  }

  /// 共享采集：record 流式 PCM16@16k + 简易能量 VAD，尾静默 1.2s 或
  /// 20s 封顶收句。keepGoing 变 false 立即收句。失败已 toast，返回 null。
  Future<Uint8List?> _capturePcm({required bool Function() keepGoing}) async {
    final S = context.S;
    final recorder = _recorder ??= sharedRecorder();
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
      _beginListen(AsrEngine.local);
      _listenLevel.value = 0;
    }
    final buf = BytesBuilder(copy: false);
    var lastLoud = DateTime.now();
    DateTime? lastLevelPublish;
    double? lastRms;
    var levelPending = false;
    try {
      await for (final chunk in stream) {
        if (!keepGoing()) break;
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
        final totalMs = buf.length / 32; // 16kHz * 2B = 32B/ms
        final silenceMs = now.difference(lastLoud).inMilliseconds;
        if ((totalMs > 700 && silenceMs > 1200) || totalMs > 20000) {
          await recorder.stop();
          break;
        }
      }
    } on Object {
      // stream error → treat as end of turn
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
    await recorder.stop(); // cancel-subscription alone leaves the mic held
    if (mounted) {
      _listenLevel.value = null;
      setState(() => _listening = false);
    }
    return buf.takeBytes();
  }

  /// 本地 SenseVoice 聆听：采集 → FFI 转写 → 自动发送。
  Future<void> _recordListen() async {
    final S = context.S;
    final engine = ref.read(voiceEngineProvider);
    if (!await engine.localAsrReady()) {
      _ensureLocalVoiceModels();
      _stopVoiceMode();
      _toast(S.chatAsrUnavailableDownloading);
      return;
    }
    final pcm = await _capturePcm(keepGoing: () => _voiceMode);
    if (pcm == null) {
      // 权限/开麦失败时 _capturePcm 已 toast。这里只退出语音模式，避免空转重听。
      _stopVoiceMode();
      return;
    }
    if (!_voiceMode) return;
    if (pcm.length < 32000) {
      _scheduleVoiceRelisten();
      return;
    }
    String text;
    try {
      text = (await engine.transcribe(pcm)).trim();
    } on Object {
      if (!await engine.localAsrReady()) {
        // 坏模型已丢弃。先试系统识别，不要静默重听同一份失败。
        final started = await _startSystemVoiceTurn();
        if (!started && mounted && _voiceMode) {
          _ensureLocalVoiceModels();
          _toast(S.chatAsrUnavailableDownloading);
          _stopVoiceMode();
        }
        return;
      }
      _scheduleVoiceRelisten();
      return;
    }
    if (!_voiceMode) return;
    // SenseVoice cheerfully transcribes room tone into filler ("嗯", "。")
    // — anything under 2 non-punctuation chars is noise, not a message.
    if (RegExp(r'[\w一-鿿]{2,}').hasMatch(text)) {
      ref.read(botChatProvider.notifier).send(text);
    } else {
      _scheduleVoiceRelisten();
    }
  }

  /// 输入框麦克风的本地听写兜底：系统 ASR 不可用时用 SenseVoice 把话
  /// 写进输入框（可编辑后再发），与系统路径同一前缀拼接语义。
  Future<void> _localDictate() async {
    // 静默回退会先把 _listening 清掉再进来。仍为 true 说明上一轮还占着麦，
    // 不能静默丢掉这次回退。
    if (_listening) {
      _toast(context.S.chatAsrFailed);
      return;
    }
    final S = context.S;
    final engine = ref.read(voiceEngineProvider);
    if (!await engine.localAsrReady()) {
      // 选了本地但模型缺失/校验未过：先试系统识别，不要只 toast 就结束。
      // 已经在系统路径里失败过的调用方不会再进这里。
      if (engine.asrEngine == AsrEngine.local) {
        final started = await _startComposerSystemDictate();
        if (started) return;
      }
      _ensureLocalVoiceModels();
      _toast(S.chatAsrUnavailableDownloading);
      return;
    }
    final prefix = _inputCtrl.text;
    final pcm = await _capturePcm(keepGoing: () => _listening);
    // 权限/开麦失败时 _capturePcm 已 toast。未挂载则页面已走，不必再提示。
    if (pcm == null || !mounted) return;
    if (pcm.length < 32000) {
      _toast(S.chatNotHeard);
      return;
    }
    final String text;
    try {
      text = (await engine.transcribe(pcm)).trim();
    } on Object {
      if (!await engine.localAsrReady()) {
        final started = await _startComposerSystemDictate();
        if (started) return;
        _ensureLocalVoiceModels();
        _toast(S.chatAsrUnavailableDownloading);
        return;
      }
      _toast(S.chatAsrFailed);
      return;
    }
    if (!RegExp(r'[\w一-鿿]{2,}').hasMatch(text)) {
      _toast(S.chatNotHeard);
      return;
    }
    setState(() {
      _inputCtrl.text = prefix.isEmpty ? text : '$prefix $text';
      _inputCtrl.selection = TextSelection.collapsed(
        offset: _inputCtrl.text.length,
      );
    });
  }

  /// 语音模式下输入栏的麦克风=插话键：播报中→打断立即说话；
  /// 聆听中→提前结束这一轮并发送；空闲→开始聆听。
  void _voiceMic() {
    if (_ttsBusy) {
      _tts?.stop();
      ref.read(voiceEngineProvider).stopSpeaking();
      _onVoiceTurnDone();
      return;
    }
    if (_listening) {
      _noteListenCallback();
      _speech.stop();
      // 不在这里清 _listening：本地采集靠它决定是否继续收句。
      // stop() 关闭流后，采集循环自己复位聆听态和电平。
      final recorder = _recorder;
      if (recorder != null) {
        unawaited(recorder.stop().then<void>((_) {}, onError: (_) {}));
      }
      return;
    }
    _voiceListen();
  }

  void _onVoiceTurnDone() {
    _ttsBusy = false;
    if (mounted) setState(() {});
    _scheduleVoiceRelisten();
  }

  void _scheduleVoiceRelisten() {
    if (_relistenQueued || !_voiceMode || _listening || _ttsBusy) return;
    _relistenQueued = true;
    Future.delayed(const Duration(milliseconds: 400), () {
      _relistenQueued = false;
      if (mounted && _voiceMode && !_listening && !_ttsBusy) {
        _voiceListen();
      }
    });
  }

  /// 新落地的助手回复 → 播报。pending 期间不读（还在流式生成）。
  void _onVoiceChatUpdate(ChatState next) {
    if (!_voiceMode) return;
    for (final m in next.messages.reversed) {
      if (m.isUser) continue;
      if (m.pending || m.id == _lastSpokenMsgId) return;
      _lastSpokenMsgId = m.id;
      var text = plainTextForSpeech(m.content);
      if (text.isEmpty && m.question.isAnswerable) {
        text = plainTextForSpeech(m.question.question);
      }
      if (text.isNotEmpty) _speak(text);
      return;
    }
  }

  Future<void> _speak(String text) async {
    final engine = ref.read(voiceEngineProvider);
    if (await engine.useKokoro) {
      _ttsBusy = true;
      if (mounted) setState(() {});
      final err = await engine.speakKokoro(text, onDone: _onVoiceTurnDone);
      if (err == null) return;
      _ttsBusy = false; // Kokoro 失败 → 落到系统 TTS
    }
    final tts = _tts;
    if (tts == null) return;
    _ttsBusy = true;
    if (mounted) setState(() {});
    try {
      await tts.stop();
      await tts.speak(text);
    } catch (_) {
      _ttsBusy = false;
      _scheduleVoiceRelisten();
    }
  }

  void _retryLatestFailed() {
    final failed = ref
        .read(botChatProvider)
        .messages
        .where((m) => m.failed && m.isUser);
    if (failed.isEmpty) return;
    ref.read(botChatProvider.notifier).retry(failed.last);
  }

  Future<void> _copySendTrace(String fallback) async {
    final copied = context.S.chatSendTraceCopied;
    final chat = ref.read(botChatProvider);
    final failed = chat.messages.where((m) => m.failed && m.isUser);
    final messageId = failed.isEmpty ? '' : failed.last.id;
    final fromBuffer = SendTrace.instance.text(messageId: messageId);
    final text = fromBuffer.isNotEmpty
        ? fromBuffer
        : (fallback.isNotEmpty ? fallback : chat.sendTrace);
    if (text.isEmpty) return;
    await Clipboard.setData(ClipboardData(text: text));
    if (!mounted) return;
    _toast(copied);
  }

  void _toast(String msg) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }

  /// Add a supplementary instruction to the run still in flight
  /// (`POST /runs/{id}/supplement`) and toast the outcome Hub reports.
  Future<void> _supplement() async {
    final S = context.S;
    final ctrl = TextEditingController();
    String? text;
    try {
      text = await showDialog<String>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(S.composerSupplement),
          content: TextField(
            controller: ctrl,
            autofocus: true,
            minLines: 1,
            maxLines: 4,
            decoration: InputDecoration(
              hintText: S.chatSupplementHint,
              border: const OutlineInputBorder(),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: Text(S.cancel),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, ctrl.text),
              child: Text(S.chatSupplementAppend),
            ),
          ],
        ),
      );
    } finally {
      ctrl.dispose();
    }
    if (text == null || text.trim().isEmpty || !mounted) return;
    final result = await ref
        .read(botChatProvider.notifier)
        .supplement(text.trim());
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          result == null
              ? ref.read(botChatProvider).error.isNotEmpty
                    ? ref.read(botChatProvider).error
                    : S.chatSupplementFailed
              : result.accepted
              ? S.chatSupplementAccepted
              : (result.detail.isNotEmpty
                    ? result.detail
                    : S.chatSupplementRejected),
        ),
      ),
    );
  }

  /// Pick files and push them to the bot's desktop as material
  /// (`POST /bots/{id}/desktop/files`, chunked). The landing path is left
  /// in the composer so the person can send it with context.
  /// 附件入口 — 拍照 / 相册 / 文件三选一。图片经 image_picker（带压缩），
  /// 其余走通用文件选择器；都落到 Bot 电脑的素材目录。
  Future<void> _attach() async {
    if (_uploadProgress != null || !botHasComputer(widget.bot.desktopMode)) {
      return;
    }
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
      final xfile = await ImagePicker().pickImage(
        source: choice == 'camera' ? ImageSource.camera : ImageSource.gallery,
        // 2048px/85%: enough for vision reads, keeps the upload lean.
        maxWidth: 2048,
        maxHeight: 2048,
        imageQuality: 85,
      );
      if (xfile == null || !mounted) return;
      await _uploadOne(xfile.name, File(xfile.path), null);
      return;
    }
    final picked = await FilePicker.pickFiles();
    if (picked.isEmpty || !mounted) return;
    for (final file in picked) {
      // Path-backed picks stream from disk (no full-file buffer); content
      // URIs without a path still need one in-memory read.
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
      if (disk == null) {
        try {
          bytes = await file.readAsBytes();
        } on Object {
          bytes = null;
        }
        if (bytes == null || bytes.isEmpty) continue;
      }
      await _uploadOne(file.name, disk, bytes);
    }
  }

  Future<void> _uploadOne(String name, File? disk, Uint8List? bytes) async {
    final S = context.S;
    final api = ref.read(botApiProvider);
    if (api == null || !mounted) return;
    setState(() {
      _uploadingName = name;
      _uploadProgress = 0;
    });
    try {
      void onProgress(int sent, int total) {
        if (mounted && total > 0) {
          setState(() => _uploadProgress = sent / total);
        }
      }

      final path = disk != null
          ? await api.uploadMaterialFile(
              widget.bot.id,
              name,
              disk,
              onProgress: onProgress,
            )
          : await api.uploadMaterial(
              widget.bot.id,
              name,
              bytes!,
              onProgress: onProgress,
            );
      if (!mounted) return;
      final line = path.isNotEmpty
          ? path
          : S.chatMaterialFallbackPath(widget.bot.name, name);
      final current = _inputCtrl.text;
      final notice = S.chatMaterialSent(line);
      _inputCtrl.text = current.isEmpty ? notice : '$current\n$notice';
      _inputCtrl.selection = TextSelection.collapsed(
        offset: _inputCtrl.text.length,
      );
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(S.chatUploadFailed(name, error.toString()))),
        );
      }
    } finally {
      if (mounted) {
        setState(() {
          _uploadProgress = null;
          _uploadingName = '';
        });
      }
    }
  }

  void _jumpToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scrollCtrl.hasClients) return;
      // Standard chat convention: only follow the transcript when the
      // person is already near the bottom — someone reading older history
      // must not be yanked down on every long-poll merge.
      final position = _scrollCtrl.position;
      if (position.maxScrollExtent - position.pixels > 400) return;
      _scrollCtrl.animateTo(
        position.maxScrollExtent,
        duration: const Duration(milliseconds: 200),
        curve: Curves.easeOut,
      );
    });
  }

  @override
  Widget build(BuildContext context) {
    // Keep the transcript pinned to the newest message as it grows.
    ref.listen<int>(chatScrollTickProvider, (_, _) => _jumpToBottom());
    ref.listen<ChatState>(
      botChatProvider,
      (_, next) => _onVoiceChatUpdate(next),
    );
    final state = ref.watch(botChatProvider);
    return Scaffold(
      appBar: AppBar(
        titleSpacing: 0,
        title: Row(
          children: [
            const BrandMark(size: 26, tile: true),
            const SizedBox(width: AppSpacing.sm + 2),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    widget.bot.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  Text(
                    _voiceMode
                        ? (_ttsBusy
                              ? context.S.chatVoiceSpeaking
                              : context.S.chatVoiceListening)
                        : _headerStatus(state),
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: (state.sending || state.activeRunId != null)
                          ? Theme.of(context).colorScheme.primary
                          : Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
        actions: [
          if (botHasComputer(widget.bot.desktopMode))
            IconButton(
              tooltip: context.S.botsDesktopTooltip,
              icon: const Icon(Icons.desktop_windows_outlined),
              onPressed: () => context.push(
                '/bot/${Uri.encodeComponent(widget.bot.id)}/desktop',
              ),
            ),
          if (supportsVoiceInput && supportsVoiceOutput)
            IconButton(
              tooltip: _voiceMode
                  ? context.S.chatVoiceAssistantOff
                  : context.S.chatVoiceAssistantOn,
              icon: Icon(
                _voiceMode ? Icons.headset : Icons.headset_outlined,
                color: _voiceMode
                    ? Theme.of(context).colorScheme.primary
                    : null,
              ),
              onPressed: _toggleVoiceMode,
            ),
          IconButton(
            tooltip: context.S.chatStatusTooltip,
            icon: const Icon(Icons.fact_check_outlined),
            onPressed: () => context.push(
              '/bot/${Uri.encodeComponent(widget.bot.id)}/status',
            ),
          ),
          IconButton(
            tooltip: context.S.chatSettingsTooltip,
            icon: const Icon(Icons.settings_outlined),
            onPressed: () => context.push(
              '/bot/${Uri.encodeComponent(widget.bot.id)}/settings',
            ),
          ),
        ],
      ),
      body: Column(
        children: [
          if (state.error.isNotEmpty)
            MaterialBanner(
              content: Text(state.error),
              actions: [
                TextButton(
                  onPressed: () =>
                      ref.read(botChatProvider.notifier).clearError(),
                  child: Text(context.S.chatAcknowledge),
                ),
              ],
            ),
          Expanded(
            child: state.loadingHistory
                ? const Center(child: CircularProgressIndicator())
                : _MessageList(scrollCtrl: _scrollCtrl, bot: widget.bot),
          ),
          if (_uploadProgress != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 4, 16, 0),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    context.S.chatUploading(_uploadingName),
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                  LinearProgressIndicator(value: _uploadProgress),
                ],
              ),
            ),
          if (state.channelError.isNotEmpty)
            _ChannelDownBar(
              message: state.channelError,
              onDismiss: () =>
                  ref.read(botChatProvider.notifier).dismissChannelError(),
              onRetry: _retryLatestFailed,
              onCopyTrace: () => _copySendTrace(state.sendTrace),
            ),
          if (state.continueThread.isNotEmpty)
            Material(
              color: Theme.of(context).colorScheme.secondaryContainer,
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 14,
                  vertical: 4,
                ),
                child: Row(
                  children: [
                    const Icon(Icons.replay, size: 16),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        context.S.chatContinueThreadHint,
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Theme.of(
                            context,
                          ).colorScheme.onSecondaryContainer,
                        ),
                      ),
                    ),
                    TextButton(
                      onPressed: () =>
                          ref.read(botChatProvider.notifier).continueTask(''),
                      child: Text(
                        context.S.cancel,
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ChatComposer(
            controller: _inputCtrl,
            hint: _listening
                ? context.S.chatVoiceListening
                : context.S.chatComposerHint,
            sending: state.sending && _inputCtrl.text.trim().isEmpty,
            onSend: () {
              final text = _inputCtrl.text;
              if (text.trim().isEmpty) return;
              _inputCtrl.clear();
              ref.read(botChatProvider.notifier).send(text);
            },
            textInputAction: TextInputAction.newline,
            onAttach:
                botHasComputer(widget.bot.desktopMode) &&
                    _uploadProgress == null
                ? _attach
                : null,
            attachTooltip: context.S.chatAttachTooltip,
            onMic: supportsVoiceInput
                ? (_voiceMode ? _voiceMic : _toggleListening)
                : null,
            listening: _listening,
            level: _listenLevel,
            listenStatus: _listenStatusText(),
            onSupplement: state.activeRunId != null ? _supplement : null,
            onStop: state.activeRunId != null
                ? () => ref.read(botChatProvider.notifier).cancelActiveRun()
                : null,
          ),
        ],
      ),
    );
  }
}

/// Bumped whenever the transcript should scroll — driven by message count.
// Declared dependencies keep this scoped into the bot route's ProviderScope
// together with botChatProvider instead of mounting at the root container.
final chatScrollTickProvider = Provider<int>((ref) {
  final state = ref.watch(botChatProvider);
  return state.messages.length * 100000 +
      state.messages.fold<int>(0, (acc, m) => acc + m.content.length);
}, dependencies: [botChatProvider]);

class _MessageList extends ConsumerWidget {
  final ScrollController scrollCtrl;
  final Bot bot;

  const _MessageList({required this.scrollCtrl, required this.bot});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final messages = ref.watch(botChatProvider).messages;
    if (messages.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpacing.xxl + 8,
            vertical: AppSpacing.xl,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              BotAvatar(bot: bot, radius: 28),
              const SizedBox(height: AppSpacing.lg + 2),
              Text(
                context.S.chatEmptyGreeting(bot.title),
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.titleMedium?.copyWith(
                  fontSize: 17,
                  height: 1.45,
                  color: Theme.of(context).colorScheme.onSurface,
                ),
              ),
              if (bot.description.isNotEmpty) ...[
                const SizedBox(height: AppSpacing.sm),
                Text(
                  bot.description,
                  textAlign: TextAlign.center,
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
        ),
      );
    }
    return ListView.builder(
      controller: scrollCtrl,
      padding: const EdgeInsets.fromLTRB(
        AppSpacing.lg,
        AppSpacing.md,
        AppSpacing.lg,
        AppSpacing.sm,
      ),
      itemCount: messages.length,
      itemBuilder: (context, index) => _MessageBubble(
        message: messages[index],
        bot: bot,
        // foldPlanConfirmation: a later confirm sentence folds the plan's
        // run-it chip — the arrangement was already approved once.
        planConfirmed: messages
            .skip(index + 1)
            .any((m) => m.isUser && m.content == botConfirmTaskText),
      ),
    );
  }
}

class _MessageBubble extends ConsumerWidget {
  final ChatMessage message;
  final Bot bot;
  final bool planConfirmed;

  const _MessageBubble({
    required this.message,
    required this.bot,
    this.planConfirmed = false,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final isUser = message.isUser;
    final textColor = isUser
        ? theme.colorScheme.onPrimary
        : theme.colorScheme.onSurface;
    final bubbleWidth = MediaQuery.of(context).size.width * 0.82;
    final bubble = MessageBubble(
      role: isUser ? ChatRole.user : ChatRole.assistant,
      color: message.failed ? theme.colorScheme.errorContainer : null,
      maxWidth: bubbleWidth,
      leading: isUser
          ? null
          : Padding(
              padding: const EdgeInsets.only(top: 2),
              child: BotAvatar(bot: bot, radius: 15),
            ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (message.content.isNotEmpty)
            isUser
                ? LinkifiedText(
                    message.content,
                    style: TextStyle(color: textColor, height: 1.4),
                    linkStyle: TextStyle(
                      color: textColor,
                      height: 1.4,
                      fontWeight: FontWeight.w600,
                      decoration: TextDecoration.underline,
                      decorationColor: textColor,
                    ),
                  )
                : MermaidMarkdown(
                    data: linkifyMarkdownUrls(message.content),
                    selectable: true,
                    onTapLink: (text, href, title) {
                      if (href != null) openExternalUrl(href);
                    },
                    styleSheet: MarkdownStyleSheet.fromTheme(theme).copyWith(
                      p: theme.textTheme.bodyMedium?.copyWith(
                        color: textColor,
                        height: 1.45,
                      ),
                      a: theme.textTheme.bodyMedium?.copyWith(
                        color: theme.colorScheme.primary,
                        fontWeight: FontWeight.w600,
                        decoration: TextDecoration.underline,
                      ),
                    ),
                  ),
          if (message.pending && !message.failed)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  SizedBox(
                    width: 12,
                    height: 12,
                    child: CircularProgressIndicator(
                      strokeWidth: 1.5,
                      color: textColor,
                    ),
                  ),
                  const SizedBox(width: 6),
                  Text(
                    context.S.chatSending,
                    style: TextStyle(fontSize: 11, color: textColor),
                  ),
                ],
              ),
            ),
          if (message.queued && !message.failed)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                _queueTag(message, context.S),
                style: TextStyle(fontSize: 11, color: textColor),
              ),
            ),
          if (!isUser &&
              !message.failed &&
              !planConfirmed &&
              message.phase == 'plan' &&
              message.content.isNotEmpty)
            Align(
              alignment: Alignment.centerLeft,
              child: ActionChip(
                avatar: const Icon(Icons.play_arrow, size: 16),
                label: Text(
                  context.S.chatRunPlan,
                  style: const TextStyle(fontSize: 12),
                ),
                onPressed: () => ref
                    .read(botChatProvider.notifier)
                    .send(botConfirmTaskText, phase: BotPhase.execute),
              ),
            ),
          if (!isUser && message.conversation.startsWith('${bot.id}#'))
            Align(
              alignment: Alignment.centerLeft,
              child: ActionChip(
                avatar: const Icon(Icons.replay, size: 16),
                label: Text(
                  context.S.chatContinueTask,
                  style: const TextStyle(fontSize: 12),
                ),
                onPressed: () => ref
                    .read(botChatProvider.notifier)
                    .continueTask(message.conversation),
              ),
            ),
          for (final image in message.images) _ReplyImage(image: image),
          for (final file in message.files) _ReplyFileChip(file: file),
          if (!isUser &&
              message.files.isEmpty &&
              message.localFilePaths.isNotEmpty)
            _LocalFileRow(message: message),
          if (message.question.isAnswerable && !message.askResolved)
            isDesktopHandoffAsk(message)
                ? _TakeoverCard(message: message, bot: bot)
                : _AskCard(message: message),
          if (message.options.isNotEmpty && !message.optionsUsed)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Wrap(
                spacing: 8,
                runSpacing: 6,
                children: [
                  for (final option in message.options)
                    ActionChip(
                      label: Text(option),
                      onPressed: () => ref
                          .read(botChatProvider.notifier)
                          .pickOption(message, option),
                    ),
                ],
              ),
            ),
          // A pending handoff ask already shows the takeover card — the
          // banner would repeat the same reason under it. The banner stays
          // for replies without a question, and for resolved asks.
          if (botHasComputer(bot.desktopMode) &&
              (message.handoffUrl.isNotEmpty ||
                  message.attentionReason.isNotEmpty) &&
              !(message.question.isAnswerable &&
                    !message.askResolved &&
                    isDesktopHandoffAsk(message)))
            _HandoffBanner(message: message, bot: bot),
        ],
      ),
    );
    if (!message.failed) return bubble;
    return Column(
      crossAxisAlignment: isUser
          ? CrossAxisAlignment.end
          : CrossAxisAlignment.start,
      children: [
        bubble,
        _FailedSendLine(message: message, maxWidth: bubbleWidth),
      ],
    );
  }
}

class _ReplyImage extends StatelessWidget {
  final BotReplyImage image;

  const _ReplyImage({required this.image});

  @override
  Widget build(BuildContext context) {
    final bytes = _decodeImage(image.data);
    if (bytes == null) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(8),
        child: GestureDetector(
          onTap: () => showDialog(
            context: context,
            builder: (_) =>
                Dialog(child: InteractiveViewer(child: Image.memory(bytes))),
          ),
          child: Image.memory(bytes, fit: BoxFit.cover),
        ),
      ),
    );
  }

  Uint8List? _decodeImage(String data) {
    try {
      return base64Decode(data);
    } on Object {
      return null;
    }
  }
}

/// Persistent line above the composer when the last send never reached
/// the server. Dismissible, and retry resends the latest failed bubble.
/// It does not block the next send.
class _ChannelDownBar extends StatelessWidget {
  final String message;
  final VoidCallback onDismiss;
  final VoidCallback onRetry;
  final VoidCallback onCopyTrace;

  const _ChannelDownBar({
    required this.message,
    required this.onDismiss,
    required this.onRetry,
    required this.onCopyTrace,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Material(
      color: scheme.errorContainer,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 6, 4, 6),
        child: Row(
          children: [
            Icon(Icons.cloud_off, size: 18, color: scheme.onErrorContainer),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                message,
                key: const ValueKey('channel-down'),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: scheme.onErrorContainer,
                ),
              ),
            ),
            TextButton(
              onPressed: onRetry,
              style: TextButton.styleFrom(
                visualDensity: VisualDensity.compact,
                foregroundColor: scheme.onErrorContainer,
              ),
              child: Text(context.S.retry),
            ),
            IconButton(
              key: const ValueKey('copy-send-trace'),
              onPressed: onCopyTrace,
              visualDensity: VisualDensity.compact,
              icon: Icon(
                Icons.content_copy,
                size: 18,
                color: scheme.onErrorContainer,
              ),
              tooltip: context.S.chatCopySendTrace,
            ),
            IconButton(
              onPressed: onDismiss,
              visualDensity: VisualDensity.compact,
              icon: Icon(Icons.close, size: 18, color: scheme.onErrorContainer),
              tooltip: context.S.chatAcknowledge,
            ),
          ],
        ),
      ),
    );
  }
}

class _FailedSendLine extends ConsumerWidget {
  final ChatMessage message;
  final double maxWidth;

  const _FailedSendLine({required this.message, required this.maxWidth});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final errorColor = Theme.of(context).colorScheme.error;
    final raw = message.failureReason.trim();
    final reason = raw.isEmpty ? '' : userFacingSendFailure(message: raw);
    return ConstrainedBox(
      constraints: BoxConstraints(maxWidth: maxWidth),
      child: Padding(
        padding: const EdgeInsets.only(top: 4),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            _failedSendActions(context, ref, errorColor),
            if (reason.isNotEmpty) _failedSendReason(reason, errorColor),
          ],
        ),
      ),
    );
  }

  Widget _failedSendActions(BuildContext context, WidgetRef ref, Color color) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          context.S.chatSendFailed,
          style: TextStyle(fontSize: 11, color: color),
        ),
        Text(' · ', style: TextStyle(fontSize: 11, color: color)),
        TextButton(
          onPressed: () => ref.read(botChatProvider.notifier).retry(message),
          style: TextButton.styleFrom(
            padding: const EdgeInsets.symmetric(horizontal: 4),
            minimumSize: Size.zero,
            tapTargetSize: MaterialTapTargetSize.shrinkWrap,
          ),
          child: Text(context.S.chatResend, style: const TextStyle(fontSize: 11)),
        ),
        TextButton(
          key: ValueKey('copy-send-trace-${message.id}'),
          onPressed: () async {
            final copiedLabel = context.S.chatSendTraceCopied;
            final text = SendTrace.instance.text(messageId: message.id);
            final fallback = ref.read(botChatProvider).sendTrace;
            final copied = text.isNotEmpty ? text : fallback;
            if (copied.isEmpty) return;
            await Clipboard.setData(ClipboardData(text: copied));
            if (!context.mounted) return;
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(content: Text(copiedLabel)),
            );
          },
          style: TextButton.styleFrom(
            padding: const EdgeInsets.symmetric(horizontal: 4),
            minimumSize: Size.zero,
            tapTargetSize: MaterialTapTargetSize.shrinkWrap,
          ),
          child: Text(
            context.S.chatCopySendTrace,
            style: const TextStyle(fontSize: 11),
          ),
        ),
      ],
    );
  }

  Widget _failedSendReason(String reason, Color color) {
    return Text(
      reason,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      style: TextStyle(fontSize: 11, color: color),
    );
  }
}

/// A reply that delivered files on the computer, before this phone has their
/// bytes. The banner is a chance to retry: the file stays on the desktop and
/// is read on demand, so a failed read is not the end of it.
class _LocalFileRow extends ConsumerWidget {
  final ChatMessage message;

  const _LocalFileRow({required this.message});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final count = message.localFilePaths.length;
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: ActionChip(
        key: const ValueKey('chat-local-file-row'),
        avatar: Icon(Icons.attach_file, size: 18, color: theme.colorScheme.primary),
        label: Text(
          context.S.chatFilesOnDesktop(count),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        onPressed: () =>
            ref.read(botChatProvider.notifier).fetchLocalFiles(message.id),
      ),
    );
  }
}

class _ReplyFileChip extends StatelessWidget {
  final BotReplyFile file;

  const _ReplyFileChip({required this.file});

  Future<void> _open(BuildContext context) async {
    if (file.data.isEmpty) return;
    try {
      final bytes = base64Decode(file.data);
      final safe = file
          .displayName(context.S)
          .replaceAll(RegExp(r'[\\/:*?"<>|]'), '_');
      final dir = await getTemporaryDirectory();
      final path = '${dir.path}${Platform.pathSeparator}$safe';
      await File(path).writeAsBytes(bytes, flush: true);
      if (!context.mounted) return;
      await SharePlus.instance.share(
        ShareParams(
          files: [
            XFile(path, mimeType: file.mime.isNotEmpty ? file.mime : null),
          ],
        ),
      );
    } on Object {
      if (context.mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(context.S.chatFileParseFailed)));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: ActionChip(
        avatar: const Icon(Icons.attach_file, size: 18),
        label: Text(
          file.displayName(context.S),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        onPressed: file.data.isEmpty ? null : () => _open(context),
      ),
    );
  }
}

/// The bot's question — a text field for free answers/secrets, or option
/// chips for a choice. Secrets are sent via secret-fill, not the transcript.
/// A desktop handoff ask (login wall, payment) renders as [_TakeoverCard]
/// instead: the answer happens on the computer screen, not in this chat.
class _AskCard extends ConsumerStatefulWidget {
  final ChatMessage message;

  const _AskCard({required this.message});

  @override
  ConsumerState<_AskCard> createState() => _AskCardState();
}

class _AskCardState extends ConsumerState<_AskCard> {
  final _answerCtrl = TextEditingController();

  @override
  void dispose() {
    _answerCtrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final question = widget.message.question;
    return Container(
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(
                question.isSecret ? Icons.key : Icons.help_outline,
                size: 16,
                color: theme.colorScheme.primary,
              ),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  question.question.isNotEmpty
                      ? question.question
                      : question.isSecret
                      ? context.S.chatSecretRequired(question.secretName)
                      : context.S.chatAnswerRequired,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
            ],
          ),
          if (question.isChoice) ...[
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 6,
              children: [
                for (final option in question.options)
                  ActionChip(
                    label: Text(option),
                    onPressed: () {
                      ref
                          .read(botChatProvider.notifier)
                          .answerQuestion(widget.message, option);
                    },
                  ),
              ],
            ),
          ] else ...[
            const SizedBox(height: 8),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _answerCtrl,
                    obscureText: question.isSecret,
                    decoration: InputDecoration(
                      isDense: true,
                      hintText: question.isSecret
                          ? context.S.chatSecretHint
                          : context.S.chatAnswerHint,
                      border: const OutlineInputBorder(),
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                FilledButton(
                  onPressed: () {
                    final value = _answerCtrl.text.trim();
                    if (value.isEmpty) return;
                    _answerCtrl.clear();
                    ref
                        .read(botChatProvider.notifier)
                        .answerQuestion(widget.message, value);
                  },
                  child: Text(context.S.chatSubmit),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

/// A desktop handoff ask: the bot stopped on a login wall / payment / captcha
/// and the person's step is ON the computer, not in this chat. Shows the wall
/// itself (the screenshot the desktop attached) plus a takeover button that
/// opens the computer view and claims the keyboard; no answer field — a typed
/// reply cannot complete this step.
class _TakeoverCard extends StatelessWidget {
  final ChatMessage message;
  final Bot bot;

  const _TakeoverCard({required this.message, required this.bot});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(
                Icons.desktop_windows,
                size: 16,
                color: theme.colorScheme.primary,
              ),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  message.attentionReason.isNotEmpty
                      ? attentionReasonLabel(message.attentionReason, context.S)
                      : context.S.chatHandoffDefault,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
            ],
          ),
          if (message.images.isNotEmpty)
            _ReplyImage(image: message.images.first),
          const SizedBox(height: 8),
          FilledButton.icon(
            onPressed: () => context.push(
              '/bot/${Uri.encodeComponent(bot.id)}/desktop?takeover=1',
            ),
            icon: const Icon(Icons.touch_app_outlined),
            label: Text(context.S.chatTakeover),
          ),
        ],
      ),
    );
  }
}

/// The bot handed its desktop to the person — jump straight to the screen.
class _HandoffBanner extends StatelessWidget {
  final ChatMessage message;
  final Bot bot;

  const _HandoffBanner({required this.message, required this.bot});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: theme.colorScheme.tertiaryContainer,
        borderRadius: BorderRadius.circular(10),
      ),
      child: Row(
        children: [
          Icon(
            Icons.desktop_windows,
            size: 18,
            color: theme.colorScheme.onTertiaryContainer,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message.attentionReason.isNotEmpty
                  ? attentionReasonLabel(message.attentionReason, context.S)
                  : context.S.chatHandoffDefault,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onTertiaryContainer,
              ),
            ),
          ),
          TextButton(
            onPressed: () =>
                context.push('/bot/${Uri.encodeComponent(bot.id)}/desktop'),
            child: Text(context.S.chatHandoffView),
          ),
        ],
      ),
    );
  }
}

String _queueTag(ChatMessage message, AppLocalizations S) {
  // held = the person still holds the desktop keyboard (user_control).
  if (message.queueHeld) return S.chatQueueHeld;
  if (message.queuePosition > 0) {
    return S.chatQueuedPosition(message.queuePosition);
  }
  return S.chatQueuedBusy;
}
