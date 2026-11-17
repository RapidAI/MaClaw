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

import '../../core/platform.dart';
import '../../core/providers.dart';
import '../../core/speech_text.dart';
import '../../core/voice_engine.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'bot_chat_controller.dart';
import 'bot_models.dart';
import 'bots_controller.dart';
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
    _speechPrefix = _inputCtrl.text;
    setState(() => _listening = true);
    await _speech.listen(
      listenOptions: stt.SpeechListenOptions(localeId: 'zh_CN'),
      onResult: (result) {
        _inputCtrl.text = _speechPrefix.isEmpty
            ? result.recognizedWords
            : '$_speechPrefix ${result.recognizedWords}';
        _inputCtrl.selection = TextSelection.collapsed(
          offset: _inputCtrl.text.length,
        );
      },
    );
  }

  /// 语音助手开关。开：立即进入聆听循环；关：停 ASR+TTS，回到手动输入。
  Future<void> _toggleVoiceMode() async {
    if (_voiceMode) {
      _stopVoiceMode();
      return;
    }
    if (!supportsVoiceInput || !supportsVoiceOutput) return;
    final tts = _tts ??= FlutterTts();
    try {
      await tts.setLanguage('zh-CN');
      tts.setCompletionHandler(_onVoiceTurnDone);
      tts.setErrorHandler((_) => _onVoiceTurnDone());
      // 基线：开启时已有回复不再朗读，只读开启后的新回复。
      _lastSpokenMsgId = _latestAssistantId(ref.read(botChatProvider));
    } catch (_) {
      _toast('语音朗读初始化失败');
      return;
    }
    setState(() => _voiceMode = true);
    _toast('语音助手已开启：说话自动发送，回复自动朗读');
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

  /// 聆听一轮：最终识别结果非空→自动发送；空（静默/超时）→稍后重新聆听。
  Future<void> _voiceListen() async {
    if (!_voiceMode || _listening || _ttsBusy) return;
    final engine = ref.read(voiceEngineProvider);
    await engine.ensurePrefs();
    if (engine.asrEngine == AsrEngine.local) {
      await _recordListen();
      return;
    }
    final ok = await _speech.initialize(
      onError: (_) {
        _listening = false;
        if (mounted) setState(() {});
        _scheduleVoiceRelisten();
      },
      onStatus: (status) {
        if (status != 'done' && status != 'notListening') return;
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
      },
    );
    if (!ok) {
      // 系统识别不可用（如无 GMS 的华为设备）→ 本地 SenseVoice 兜底。
      await _recordListen();
      return;
    }
    _voiceCaptured = '';
    setState(() => _listening = true);
    await _speech.listen(
      listenOptions: stt.SpeechListenOptions(localeId: 'zh_CN'),
      onResult: (result) => _voiceCaptured = result.recognizedWords,
    );
  }

  /// 本地模型缺失时后台拉起下载（GitHub → Hub 镜像），用户不用进设置页。
  void _ensureLocalVoiceModels() {
    final hub = ref.read(sessionProvider).active?.hubUrl;
    unawaited(ref.read(voiceEngineProvider).autoEnsure(hubBase: hub));
  }

  /// 共享采集：record 流式 PCM16@16k + 简易能量 VAD，尾静默 1.2s 或
  /// 20s 封顶收句。keepGoing 变 false 立即收句。失败已 toast，返回 null。
  Future<Uint8List?> _capturePcm({required bool Function() keepGoing}) async {
    final recorder = _recorder ??= AudioRecorder();
    if (!await recorder.hasPermission()) {
      _toast('没有麦克风权限');
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
      _toast('无法打开麦克风录音');
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
    final engine = ref.read(voiceEngineProvider);
    if (!await engine.localAsrReady()) {
      _ensureLocalVoiceModels();
      _stopVoiceMode();
      _toast('语音识别不可用，正在后台下载本地模型，稍后再试');
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
    final engine = ref.read(voiceEngineProvider);
    if (!await engine.localAsrReady()) {
      _ensureLocalVoiceModels();
      _toast('语音识别不可用，正在后台下载本地模型，稍后再试');
      return;
    }
    final prefix = _inputCtrl.text;
    final pcm = await _capturePcm(keepGoing: () => _listening);
    if (pcm == null || !mounted) return;
    if (pcm.length < 32000) {
      _toast('没听清，请再试');
      return;
    }
    final String text;
    try {
      text = (await engine.transcribe(pcm)).trim();
    } on Object {
      _toast('语音识别失败，请重试');
      return;
    }
    if (!RegExp(r'[\w一-鿿]{2,}').hasMatch(text)) {
      _toast('没听清，请再试');
      return;
    }
    setState(() {
      _inputCtrl.text = prefix.isEmpty ? text : '$prefix $text';
      _inputCtrl.selection =
          TextSelection.collapsed(offset: _inputCtrl.text.length);
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
    final ctrl = TextEditingController();
    String? text;
    try {
      text = await showDialog<String>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('给当前任务补充说明'),
          content: TextField(
            controller: ctrl,
            autofocus: true,
            minLines: 1,
            maxLines: 4,
            decoration: const InputDecoration(
              hintText: '例如：只要前 5 条结果',
              border: OutlineInputBorder(),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, ctrl.text),
              child: const Text('追加'),
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
                    : '追加失败'
              : result.accepted
              ? '已补充到当前任务'
              : (result.detail.isNotEmpty ? result.detail : '任务已结束，补充未送达'),
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
    if (_uploadProgress != null || !botHasComputer(widget.bot.desktopMode)) return;
    final choice = await showModalBottomSheet<String>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.photo_camera_outlined),
              title: const Text('拍照'),
              onTap: () => Navigator.of(context).pop('camera'),
            ),
            ListTile(
              leading: const Icon(Icons.photo_library_outlined),
              title: const Text('相册'),
              onTap: () => Navigator.of(context).pop('gallery'),
            ),
            ListTile(
              leading: const Icon(Icons.attach_file),
              title: const Text('文件'),
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
      final line = path.isNotEmpty ? path : '~/素材/${widget.bot.name}/$name';
      final current = _inputCtrl.text;
      _inputCtrl.text = current.isEmpty
          ? '素材已传送到 $line'
          : '$current\n素材已传送到 $line';
      _inputCtrl.selection = TextSelection.collapsed(
        offset: _inputCtrl.text.length,
      );
    } on Object catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('$name 上传失败：$error')));
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
                        ? (_ttsBusy ? '语音播报中…' : '聆听中…')
                        : ((state.sending || state.activeRunId != null)
                              ? '正在处理…'
                              : '在线'),
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
              tooltip: '电脑画面',
              icon: const Icon(Icons.desktop_windows_outlined),
              onPressed: () => context.push(
                '/bot/${Uri.encodeComponent(widget.bot.id)}/desktop',
              ),
            ),
          if (supportsVoiceInput && supportsVoiceOutput)
            IconButton(
              tooltip: _voiceMode ? '关闭语音助手' : '语音助手',
              icon: Icon(
                _voiceMode ? Icons.headset : Icons.headset_outlined,
                color: _voiceMode
                    ? Theme.of(context).colorScheme.primary
                    : null,
              ),
              onPressed: _toggleVoiceMode,
            ),
          IconButton(
            tooltip: '任务状态',
            icon: const Icon(Icons.fact_check_outlined),
            onPressed: () => context.push(
              '/bot/${Uri.encodeComponent(widget.bot.id)}/status',
            ),
          ),
          IconButton(
            tooltip: '设置',
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
                  child: const Text('知道了'),
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
                    '上传 $_uploadingName…',
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
                        '下一条消息将继续该任务线程',
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
                      child: const Text('取消', style: TextStyle(fontSize: 12)),
                    ),
                  ],
                ),
              ),
            ),
          _Composer(
            inputCtrl: _inputCtrl,
            sending: state.sending,
            canSupplement: state.activeRunId != null,
            showAttach: botHasComputer(widget.bot.desktopMode),
            onAttach: botHasComputer(widget.bot.desktopMode) && _uploadProgress == null
                ? _attach
                : null,
            listening: _listening,
            onMic: supportsVoiceInput
                ? (_voiceMode ? _voiceMic : _toggleListening)
                : null,
            onSend: () {
              final text = _inputCtrl.text;
              if (text.trim().isEmpty) return;
              _inputCtrl.clear();
              ref.read(botChatProvider.notifier).send(text);
            },
            onSupplement: _supplement,
            onCancel: () =>
                ref.read(botChatProvider.notifier).cancelActiveRun(),
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
            '和 ${bot.title} 说点什么开始吧',
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
    final bubbleColor = message.failed
        ? theme.colorScheme.errorContainer
        : isUser
        ? theme.colorScheme.primary
        : theme.colorScheme.surfaceContainerHigh;
    final textColor = isUser
        ? theme.colorScheme.onPrimary
        : theme.colorScheme.onSurface;
    final bubble = Container(
      constraints: BoxConstraints(
        maxWidth: MediaQuery.of(context).size.width * 0.78,
      ),
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: bubbleColor,
        borderRadius: BorderRadius.only(
          topLeft: const Radius.circular(16),
          topRight: const Radius.circular(16),
          bottomLeft: Radius.circular(isUser ? 16 : 4),
          bottomRight: Radius.circular(isUser ? 4 : 16),
        ),
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
                    '发送中…',
                    style: TextStyle(fontSize: 11, color: textColor),
                  ),
                ],
              ),
            ),
          if (message.queued && !message.failed)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                _queueTag(message),
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
                    '发送失败',
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
                    child: const Text('重发', style: TextStyle(fontSize: 11)),
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
                label: const Text('按这个安排执行', style: TextStyle(fontSize: 12)),
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
                label: const Text('继续这个任务', style: TextStyle(fontSize: 12)),
                onPressed: () => ref
                    .read(botChatProvider.notifier)
                    .continueTask(message.conversation),
              ),
            ),
          for (final image in message.images) _ReplyImage(image: image),
          for (final file in message.files) _ReplyFileChip(file: file),
          if (message.question.isAnswerable && !message.askResolved)
            _AskCard(message: message),
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
          if (botHasComputer(bot.desktopMode) &&
              (message.handoffUrl.isNotEmpty ||
                  message.attentionReason.isNotEmpty))
            _HandoffBanner(message: message, bot: bot),
        ],
      ),
    );
    if (isUser) {
      return Align(alignment: Alignment.centerRight, child: bubble);
    }
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(top: 2),
          child: BotAvatar(bot: bot, radius: 14),
        ),
        const SizedBox(width: 8),
        Flexible(child: bubble),
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

class _ReplyFileChip extends StatelessWidget {
  final BotReplyFile file;

  const _ReplyFileChip({required this.file});

  Future<void> _open(BuildContext context) async {
    if (file.data.isEmpty) return;
    try {
      final bytes = base64Decode(file.data);
      final dir = await getTemporaryDirectory();
      final safe = file.displayName.replaceAll(RegExp(r'[\\/:*?"<>|]'), '_');
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
        ).showSnackBar(const SnackBar(content: Text('文件解析失败')));
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
          file.displayName,
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
                      ? '需要输入 ${question.secretName}'
                      : '需要你的回答',
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
                      hintText: question.isSecret ? '输入密钥（不会显示在聊天中）' : '输入回答',
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
                  child: const Text('提交'),
                ),
              ],
            ),
          ],
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
                  ? message.attentionReason
                  : 'Bot 把电脑交给你了',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onTertiaryContainer,
              ),
            ),
          ),
          TextButton(
            onPressed: () =>
                context.push('/bot/${Uri.encodeComponent(bot.id)}/desktop'),
            child: const Text('去查看'),
          ),
        ],
      ),
    );
  }
}

class _Composer extends StatelessWidget {
  final TextEditingController inputCtrl;
  final bool sending;
  final bool canSupplement;
  final bool showAttach;
  final VoidCallback? onAttach;
  final bool listening;
  final VoidCallback? onMic;
  final VoidCallback onSend;
  final VoidCallback onSupplement;
  final VoidCallback onCancel;

  const _Composer({
    required this.inputCtrl,
    required this.sending,
    required this.canSupplement,
    required this.showAttach,
    required this.onAttach,
    required this.listening,
    required this.onMic,
    required this.onSend,
    required this.onSupplement,
    required this.onCancel,
  });

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(10, 6, 10, 8),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            Expanded(
              child: Container(
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerHigh,
                  borderRadius: BorderRadius.circular(26),
                ),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: [
                    if (showAttach)
                      IconButton(
                        onPressed: onAttach,
                        visualDensity: VisualDensity.compact,
                        icon: const Icon(Icons.attach_file),
                        tooltip: '传送文件到 Bot 的电脑',
                      ),
                    Expanded(
                      child: TextField(
                        controller: inputCtrl,
                        minLines: 1,
                        maxLines: 5,
                        textInputAction: TextInputAction.newline,
                        decoration: const InputDecoration(
                          hintText: '给 Bot 下达任务…',
                          border: InputBorder.none,
                          contentPadding: EdgeInsets.symmetric(
                            horizontal: 2,
                            vertical: 12,
                          ),
                        ),
                      ),
                    ),
                    if (onMic != null)
                      IconButton(
                        onPressed: onMic,
                        visualDensity: VisualDensity.compact,
                        icon: Icon(
                          listening ? Icons.mic : Icons.mic_none,
                          color: listening ? scheme.error : null,
                        ),
                        tooltip: listening ? '停止语音输入' : '语音输入',
                      ),
                  ],
                ),
              ),
            ),
            const SizedBox(width: 6),
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (canSupplement)
                  IconButton(
                    onPressed: onSupplement,
                    icon: const Icon(Icons.playlist_add),
                    tooltip: '给当前任务补充说明',
                  ),
                // The send button stays usable while a run executes — the
                // Hub queues the message (`queued` reply) behind the turn.
                IconButton.filled(
                  onPressed: sending ? null : onSend,
                  icon: sending
                      ? SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(
                            strokeWidth: 2,
                            color: scheme.onPrimary,
                          ),
                        )
                      : const Icon(Icons.send),
                  tooltip: '发送',
                ),
                if (canSupplement)
                  IconButton(
                    onPressed: onCancel,
                    icon: const Icon(Icons.stop),
                    tooltip: '停止当前任务',
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

String _queueTag(ChatMessage message) {
  // held = the person still holds the desktop keyboard (user_control).
  if (message.queueHeld) return '已暂存（电脑正被你接管中）';
  if (message.queuePosition > 0) return '排队中 · 第 ${message.queuePosition} 位';
  return '排队中，Bot 忙完后处理';
}
