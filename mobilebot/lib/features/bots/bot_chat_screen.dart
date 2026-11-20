import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:image_picker/image_picker.dart';
import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';
import 'dart:math' as math;

import 'package:flutter_tts/flutter_tts.dart';
import 'package:record/record.dart';
import 'package:speech_to_text/speech_to_text.dart' as stt;

import '../../core/asr_fallback.dart';
import '../../core/platform.dart';
import '../../core/providers.dart';
import '../../core/speech_text.dart';
import '../../core/voice_engine.dart';
import '../../core/chat_ui.dart' show ChatComposer, ChatRole, MessageBubble;
import '../../core/locale.dart';
import '../../l10n/gen/app_localizations.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'bot_chat_controller.dart';
import 'bot_models.dart';
import 'bots_controller.dart';
import '../../core/ui_kit.dart'
    show LinkifiedText, linkifyMarkdownUrls, openExternalUrl;
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
  String _speechPrefix = '';

  /// 语音助手模式（豆包式）：开启后自动聆听→识别即发送→朗读回复→再聆听。
  FlutterTts? _tts;
  bool _voiceMode = false;
  bool _ttsBusy = false;
  bool _relistenQueued = false;
  String _voiceCaptured = '';
  String _lastSpokenMsgId = '';
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
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    _stopVoiceMode();
    _speech.stop();
    _recorder?.dispose();
    super.dispose();
  }

  /// On-device speech-to-text: words land straight in the composer so the
  /// person can edit before sending.
  Future<void> _toggleListening() async {
    if (_listening) {
      await _speech.stop();
      try {
        await _recorder?.stop(); // 本地听写进行中：提前结束本轮并转写
      } on Object {
        // 未在录音时 stop() 可能抛错——忽略，本轮本来就结束了
      }
      if (mounted) setState(() => _listening = false);
      return;
    }
    final ok = await _speech.initialize(
      onError: (_) {
        if (mounted) setState(() => _listening = false);
      },
      onStatus: (status) {
        if (mounted && (status == 'done' || status == 'notListening')) {
          setState(() => _listening = false);
        }
      },
    );
    if (!mounted) return;
    if (!ok) {
      // 系统识别不可用（无 GMS/华为等）→ 本地 SenseVoice 听写兜底；
      // 模型未下载时 _localDictate 会后台拉起下载并提示。
      unawaited(_localDictate());
      return;
    }
    final started = await _listenSystemDictate();
    if (!started && mounted) unawaited(_localDictate());
  }

  /// 系统听写写入输入框。listen 抛错时复位并返回 false，调用方可回退本地。
  Future<bool> _listenSystemDictate() async {
    _speechPrefix = _inputCtrl.text;
    final fallbackLocaleId = speechLocaleId(Localizations.localeOf(context));
    setState(() => _listening = true);
    try {
      // speech_to_text 7.x：启动失败抛 ListenFailedException，成功则无返回值。
      final locales = await _speech.locales();
      if (!mounted) return false;
      await _speech.listen(
        listenOptions: stt.SpeechListenOptions(
          localeId: pickAsrLocale(
            locales,
            fallbackLocaleId: fallbackLocaleId,
          ),
        ),
        onResult: (result) {
          _inputCtrl.text = _speechPrefix.isEmpty
              ? result.recognizedWords
              : '$_speechPrefix ${result.recognizedWords}';
          _inputCtrl.selection = TextSelection.collapsed(
            offset: _inputCtrl.text.length,
          );
        },
      );
    } on Object {
      if (mounted) setState(() => _listening = false);
      return false;
    }
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
    _voiceMode = false;
    _relistenQueued = false;
    _voiceCaptured = '';
    _listening = false;
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

  /// 一轮聆听的收口：最终识别词非空→自动发送；空（静默/超时）→稍后重新
  /// 聆听。onStatus 终态与 onResult 的 finalResult 都会进来（部分 OEM 识别
  /// 器只发结果不发终态状态），靠 _listening 幂等，先到者生效。
  void _handleVoiceTurnEnd() {
    if (!_listening) return;
    _listening = false;
    if (mounted) setState(() {});
    if (!_voiceMode) return;
    final text = _voiceCaptured.trim();
    _voiceCaptured = '';
    if (text.isNotEmpty) {
      ref.read(botChatProvider.notifier).send(text);
    } else {
      _scheduleVoiceRelisten();
    }
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
    if (decideAsrFallback(
          systemStarted: started,
          localReady: localReady,
          alreadyFellBack: true,
          engine: engine.asrEngine,
        ) ==
        AsrFallbackChoice.retryLater) {
      return;
    }
    // initialize/listen 没起来（无 GMS 等）且本地模型已就绪 → SenseVoice。
    // 会话中途出错不会走到这里：onError 已把 _listening 复位并排队重听。
    await _recordListen();
  }

  /// 系统听写一轮。成功启动返回 true；不可用返回 false，调用方决定回退。
  ///
  /// 会话中途的 onError 只结束本轮并排队重听，不在这里切本地采集——
  /// 麦克风已开，再开 record 会抢麦。
  Future<bool> _startSystemVoiceTurn() async {
    final ok = await _speech.initialize(
      onError: (_) {
        _listening = false;
        if (mounted) setState(() {});
        _scheduleVoiceRelisten();
      },
      onStatus: (status) {
        if (status != 'done' && status != 'notListening') return;
        _handleVoiceTurnEnd();
      },
    );
    if (!ok || !mounted || !_voiceMode) return false;
    _voiceCaptured = '';
    final fallbackLocaleId = speechLocaleId(Localizations.localeOf(context));
    setState(() => _listening = true);
    try {
      final locales = await _speech.locales();
      if (!mounted) return false;
      await _speech.listen(
        listenOptions: stt.SpeechListenOptions(
          localeId: pickAsrLocale(
            locales,
            fallbackLocaleId: fallbackLocaleId,
          ),
        ),
        onResult: (result) {
          _voiceCaptured = result.recognizedWords;
          if (result.finalResult) _handleVoiceTurnEnd();
        },
      );
    } on Object {
      _listening = false;
      if (mounted) setState(() {});
      return false;
    }
    return mounted && _voiceMode;
  }

  /// 输入框听写的系统路径。成功启动返回 true。
  Future<bool> _startComposerSystemDictate() async {
    final ok = await _speech.initialize(
      onError: (_) {
        if (mounted) setState(() => _listening = false);
      },
      onStatus: (status) {
        if (mounted && (status == 'done' || status == 'notListening')) {
          setState(() => _listening = false);
        }
      },
    );
    if (!ok || !mounted) return false;
    return _listenSystemDictate();
  }

  /// 本地模型缺失时后台拉起下载（GitHub → Hub 镜像），用户不用进设置页。
  void _ensureLocalVoiceModels() {
    final hub = ref.read(sessionProvider).active?.hubUrl;
    unawaited(ref.read(voiceEngineProvider).autoEnsure(hubBase: hub));
  }

  /// 共享采集：record 流式 PCM16@16k + 简易能量 VAD，尾静默 1.2s 或
  /// 20s 封顶收句。keepGoing 变 false 立即收句。失败已 toast，返回 null。
  Future<Uint8List?> _capturePcm({required bool Function() keepGoing}) async {
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
    if (mounted) setState(() => _listening = true);
    final buf = BytesBuilder(copy: false);
    var lastLoud = DateTime.now();
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
        if (rms > 400) lastLoud = DateTime.now();
        final totalMs = buf.length / 32; // 16kHz * 2B = 32B/ms
        final silenceMs = DateTime.now().difference(lastLoud).inMilliseconds;
        if ((totalMs > 700 && silenceMs > 1200) || totalMs > 20000) {
          await recorder.stop();
          break;
        }
      }
    } on Object {
      // stream error → treat as end of turn
    }
    await recorder.stop(); // cancel-subscription alone leaves the mic held
    if (mounted) setState(() => _listening = false);
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
    if (_listening) return;
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
      _speech.stop();
      _recorder?.stop(); // local-ASR path: end the turn early
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
            BotAvatar(bot: widget.bot, radius: 17),
            const SizedBox(width: 10),
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
                        : ((state.sending || state.activeRunId != null)
                              ? context.S.chatProcessing
                              : context.S.chatOnline),
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
            hint: context.S.chatComposerHint,
            sending: state.sending,
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
          padding: const EdgeInsets.all(32),
          child: Text(
            context.S.chatEmptyGreeting(bot.title),
            style: TextStyle(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ),
      );
    }
    return ListView.builder(
      controller: scrollCtrl,
      padding: const EdgeInsets.fromLTRB(12, 12, 12, 8),
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
    return MessageBubble(
      role: isUser ? ChatRole.user : ChatRole.assistant,
      color: message.failed ? theme.colorScheme.errorContainer : null,
      maxWidth: MediaQuery.of(context).size.width * 0.78,
      leading: isUser
          ? null
          : Padding(
              padding: const EdgeInsets.only(top: 2),
              child: BotAvatar(bot: bot, radius: 14),
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
                : MarkdownBody(
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
          if (message.pending)
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
          if (message.failed)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    context.S.chatSendFailed,
                    style: TextStyle(
                      fontSize: 11,
                      color: theme.colorScheme.error,
                    ),
                  ),
                  TextButton(
                    onPressed: () =>
                        ref.read(botChatProvider.notifier).retry(message),
                    style: TextButton.styleFrom(
                      padding: const EdgeInsets.symmetric(horizontal: 8),
                      minimumSize: Size.zero,
                      tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                    ),
                    child: Text(
                      context.S.chatResend,
                      style: const TextStyle(fontSize: 11),
                    ),
                  ),
                ],
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
