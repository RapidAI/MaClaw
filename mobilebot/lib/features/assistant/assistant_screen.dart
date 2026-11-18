import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

import '../../core/chat_ui.dart' show ChatComposer, ChatRole, MessageBubble;
import '../../core/locale.dart';
import '../../core/ui_kit.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_tts/flutter_tts.dart';
import 'package:image_picker/image_picker.dart';
import 'package:record/record.dart';
import 'package:speech_to_text/speech_to_text.dart' as stt;
import 'package:uuid/uuid.dart';

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
    _inputCtrl.dispose();
    _scrollCtrl.dispose();
    _speech.stop();
    _tts?.stop();
    _recorder?.stop();
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
    final query = text.isEmpty ? '请阅读并总结这个附件。' : text;
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

  Future<void> _dictate() async {
    if (!supportsVoiceInput || _listening) return;
    final engine = ref.read(voiceEngineProvider);
    await engine.ensurePrefs();
    var usedSystem = false;
    if (engine.asrEngine == AsrEngine.system) {
      var captured = '';
      final ok = await _speech.initialize(
        onError: (_) {
          if (mounted) setState(() => _listening = false);
        },
        onStatus: (status) {
          if (status != 'done' && status != 'notListening') return;
          if (mounted) setState(() => _listening = false);
          final text = captured.trim();
          if (text.isNotEmpty) _appendInput(text);
        },
      );
      if (ok) {
        usedSystem = true;
        setState(() => _listening = true);
        await _speech.listen(
          listenOptions: stt.SpeechListenOptions(
            localeId: pickAsrLocale(await _speech.locales()),
          ),
          onResult: (r) => captured = r.recognizedWords,
        );
      }
    }
    if (!usedSystem) await _localDictate();
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
    if (mounted) setState(() => _listening = true);
    final buf = BytesBuilder(copy: false);
    var lastLoud = DateTime.now();
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
        if (rms > 400) lastLoud = DateTime.now();
        final totalMs = buf.length / 32;
        final silenceMs = DateTime.now().difference(lastLoud).inMilliseconds;
        if ((totalMs > 700 && silenceMs > 1200) || totalMs > 20000) {
          await recorder.stop();
          break;
        }
      }
    } on Object {
      // stream error → end of turn
    }
    await recorder.stop();
    if (mounted) setState(() => _listening = false);
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
    setState(() => _listening = true);
    final pcm = await _capturePcm();
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
                          _speech.stop();
                          _recorder?.stop();
                          setState(() => _listening = false);
                        }
                      : _dictate)
                : null,
            listening: _listening,
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
            MarkdownBody(data: m.text, selectable: true),
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
