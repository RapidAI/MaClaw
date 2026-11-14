import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';
import 'package:uuid/uuid.dart';

import 'bot_models.dart';
import 'bots_controller.dart';

/// 演示录制 — 桌面 GUI 演示功能（demo/start|narrate|stop|distill）的手机版：
/// 在 Bot 的独立桌面上录操作过程，加旁白（打字或语音），结束后把
/// 时间线提炼成可复用技能。仅 desktop_mode=isolated 的 Bot 可用，
/// Hub 会在不满足时返回明确的错误提示。
class BotDemoScreen extends ConsumerStatefulWidget {
  final Bot bot;

  const BotDemoScreen({super.key, required this.bot});

  @override
  ConsumerState<BotDemoScreen> createState() => _BotDemoScreenState();
}

enum _DemoPhase { idle, recording, stopped, distilled }

class _BotDemoScreenState extends ConsumerState<BotDemoScreen> {
  final _titleCtrl = TextEditingController();
  final _narrateCtrl = TextEditingController();
  final _notesCtrl = TextEditingController();
  final _recorder = AudioRecorder();
  final _shotCache = <String, Uint8List>{};
  final _shotFutures = <String, Future<Uint8List?>>{};

  _DemoPhase _phase = _DemoPhase.idle;
  bool _busy = false;
  bool _micRecording = false;
  bool _asrAvailable = false;
  String _error = '';
  final _narrations = <String>[];
  Map<String, dynamic> _timeline = const {};
  List<Map<String, dynamic>> _events = const [];
  String _demoId = '';
  Map<String, dynamic> _skill = const {};

  @override
  void dispose() {
    _titleCtrl.dispose();
    _narrateCtrl.dispose();
    _notesCtrl.dispose();
    _recorder.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() body) async {
    setState(() {
      _busy = true;
      _error = '';
    });
    try {
      await body();
    } on Object catch (error) {
      if (mounted) setState(() => _error = failureMessage(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _start() => _run(() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    final out = await api.demoStart(widget.bot.id, _titleCtrl.text.trim());
    if (!mounted) return;
    setState(() {
      _phase = _DemoPhase.recording;
      _asrAvailable = out['asr_available'] == true;
    });
  });

  Future<void> _narrateText() => _run(() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    final text = _narrateCtrl.text.trim();
    if (text.isEmpty) return;
    final out = await api.demoNarrate(widget.bot.id, text: text);
    if (!mounted) return;
    _narrateCtrl.clear();
    setState(
      () => _narrations.add((out['text'] as String? ?? text).toString()),
    );
  });

  Future<void> _toggleMic() async {
    if (_micRecording) {
      setState(() => _micRecording = false);
      final path = await _recorder.stop();
      if (path == null || path.isEmpty || !mounted) return;
      await _run(() async {
        final api = ref.read(botApiProvider);
        if (api == null) return;
        final file = File(path);
        final bytes = await file.readAsBytes();
        await file.delete().onError((_, _) => file); // temp clip — best effort
        final out = await api.demoNarrate(
          widget.bot.id,
          audioBase64: base64Encode(bytes),
          mime: 'audio/wav',
        );
        if (!mounted) return;
        setState(
          () => _narrations.add(
            '🎙 ${(out['text'] as String? ?? '').toString()}',
          ),
        );
      });
      return;
    }
    if (!await _recorder.hasPermission()) return;
    final dir = await getTemporaryDirectory();
    final path =
        '${dir.path}${Platform.pathSeparator}narrate-${const Uuid().v4()}.wav';
    await _recorder.start(
      const RecordConfig(encoder: AudioEncoder.wav),
      path: path,
    );
    if (mounted) setState(() => _micRecording = true);
  }

  Future<void> _stop() => _run(() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    final out = await api.demoStop(widget.bot.id);
    if (!mounted) return;
    final timeline = out['timeline'] is Map
        ? Map<String, dynamic>.from(out['timeline'] as Map)
        : const <String, dynamic>{};
    setState(() {
      _phase = _DemoPhase.stopped;
      _timeline = timeline;
      _demoId = (timeline['id'] as String? ?? '').toString();
      _events = [
        for (final e in (timeline['events'] as List? ?? const []))
          if (e is Map) Map<String, dynamic>.from(e),
      ];
    });
  });

  Future<void> _distill() => _run(() async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    final out = await api.demoDistill(
      widget.bot.id,
      _timeline,
      notes: _notesCtrl.text.trim(),
    );
    if (!mounted) return;
    setState(() {
      _phase = _DemoPhase.distilled;
      _skill = out;
    });
  });

  IconData _eventIcon(String kind) => switch (kind) {
    'screenshot' => Icons.screenshot_outlined,
    'input' => Icons.mouse_outlined,
    'focus' => Icons.window_outlined,
    'narration' => Icons.record_voice_over_outlined,
    _ => Icons.circle_outlined,
  };

  String _eventLabel(Map<String, dynamic> e) {
    final kind = (e['kind'] as String? ?? '').toString();
    final action = (e['action'] as String? ?? '').toString();
    final text = (e['text'] as String? ?? '').toString();
    final app = (e['app'] as String? ?? '').toString();
    final window = (e['window'] as String? ?? '').toString();
    return [
      if (action.isNotEmpty) action,
      if (text.isNotEmpty) text,
      if (app.isNotEmpty) app,
      if (window.isNotEmpty && window != app) window,
      if (kind.isNotEmpty && action.isEmpty && text.isEmpty) kind,
    ].join(' · ');
  }

  Widget _shot(String shotRef) {
    final cached = _shotCache[shotRef];
    if (cached != null) {
      return Image.memory(
        cached,
        height: 72,
        fit: BoxFit.cover,
        errorBuilder: (_, _, _) => const SizedBox.shrink(),
      );
    }
    final future = _shotFutures.putIfAbsent(shotRef, () {
      final api = ref.read(botApiProvider);
      if (api == null) return Future<Uint8List?>.value();
      return api
          .demoShot(widget.bot.id, _demoId, shotRef)
          .then((s) => s.data.isEmpty ? null : base64Decode(s.data));
    });
    return FutureBuilder<Uint8List?>(
      future: future,
      builder: (context, snap) {
        final data = snap.data;
        if (data == null) {
          return const SizedBox(
            width: 72,
            height: 72,
            child: Center(
              child: SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(strokeWidth: 1.5),
              ),
            ),
          );
        }
        _shotCache[shotRef] = data;
        return Image.memory(data, height: 72, fit: BoxFit.cover);
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: Text('${widget.bot.title} · 演示录制')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (_error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Text(
                _error,
                style: TextStyle(color: theme.colorScheme.error),
              ),
            ),
          switch (_phase) {
            _DemoPhase.idle => _buildIdle(theme),
            _DemoPhase.recording => _buildRecording(theme),
            _DemoPhase.stopped => _buildTimeline(theme),
            _DemoPhase.distilled => _buildSkill(theme),
          },
        ],
      ),
    );
  }

  Widget _buildIdle(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Icon(
          Icons.smart_display_outlined,
          size: 56,
          color: theme.colorScheme.primary,
        ),
        const SizedBox(height: 12),
        Text(
          '录一段在 ${widget.bot.title} 电脑上的操作过程，加上旁白说明，结束后可以提炼成它能反复使用的技能。\n\n需要这个 Bot 使用独立电脑（desktop_mode=isolated）。',
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 20),
        TextField(
          controller: _titleCtrl,
          decoration: const InputDecoration(
            labelText: '演示标题',
            hintText: '如：在 Excel 里生成周报',
            border: OutlineInputBorder(),
          ),
        ),
        const SizedBox(height: 12),
        FilledButton.icon(
          onPressed: _busy ? null : _start,
          icon: const Icon(Icons.fiber_manual_record, size: 16),
          label: const Text('开始录制'),
        ),
      ],
    );
  }

  Widget _buildRecording(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Container(
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: theme.colorScheme.errorContainer,
            borderRadius: BorderRadius.circular(10),
          ),
          child: Row(
            children: [
              Icon(
                Icons.fiber_manual_record,
                color: theme.colorScheme.error,
                size: 18,
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  '录制中 — 去 ${widget.bot.title} 的电脑上操作',
                  style: TextStyle(color: theme.colorScheme.onErrorContainer),
                ),
              ),
              TextButton(
                onPressed: () => context.push(
                  '/bot/${Uri.encodeComponent(widget.bot.id)}/desktop',
                ),
                child: const Text('看画面'),
              ),
            ],
          ),
        ),
        const SizedBox(height: 16),
        Text('旁白', style: theme.textTheme.titleSmall),
        const SizedBox(height: 8),
        Row(
          children: [
            Expanded(
              child: TextField(
                controller: _narrateCtrl,
                decoration: const InputDecoration(
                  hintText: '这一步在做什么…',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
              ),
            ),
            if (_asrAvailable)
              IconButton(
                tooltip: _micRecording ? '停止录音' : '语音旁白',
                icon: Icon(
                  _micRecording ? Icons.stop : Icons.mic,
                  color: _micRecording ? theme.colorScheme.error : null,
                ),
                onPressed: _busy ? null : _toggleMic,
              ),
            IconButton.filled(
              onPressed: _busy ? null : _narrateText,
              icon: const Icon(Icons.add_comment, size: 18),
            ),
          ],
        ),
        if (_narrations.isNotEmpty) ...[
          const SizedBox(height: 12),
          for (final n in _narrations)
            Padding(
              padding: const EdgeInsets.only(bottom: 4),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(
                    Icons.subdirectory_arrow_right,
                    size: 14,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                  const SizedBox(width: 6),
                  Expanded(child: Text(n, style: theme.textTheme.bodySmall)),
                ],
              ),
            ),
        ],
        const SizedBox(height: 20),
        FilledButton.tonalIcon(
          onPressed: _busy ? null : _stop,
          icon: const Icon(Icons.stop),
          label: const Text('结束录制'),
        ),
      ],
    );
  }

  Widget _buildTimeline(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text('录制完成 · ${_events.length} 个事件', style: theme.textTheme.titleSmall),
        const SizedBox(height: 8),
        for (final e in _events)
          Card(
            child: Padding(
              padding: const EdgeInsets.all(10),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(
                    _eventIcon((e['kind'] as String? ?? '')),
                    size: 18,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Text(
                      _eventLabel(e),
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
                  if ((e['ref'] as String? ?? '').isNotEmpty)
                    ClipRRect(
                      borderRadius: BorderRadius.circular(6),
                      child: _shot((e['ref'] as String).toString()),
                    ),
                ],
              ),
            ),
          ),
        const SizedBox(height: 16),
        TextField(
          controller: _notesCtrl,
          decoration: const InputDecoration(
            labelText: '补充说明（可选）',
            hintText: '给提炼模型的额外提示，如注意事项',
            border: OutlineInputBorder(),
          ),
          maxLines: 3,
        ),
        const SizedBox(height: 12),
        FilledButton.icon(
          onPressed: _busy || _timeline.isEmpty ? null : _distill,
          icon: const Icon(Icons.auto_fix_high),
          label: const Text('提炼为技能'),
        ),
      ],
    );
  }

  Widget _buildSkill(ThemeData theme) {
    final name = (_skill['name'] as String? ?? '').toString();
    final goal = (_skill['goal'] as String? ?? '').toString();
    final steps = _skill['steps'] is List ? _skill['steps'] as List : const [];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Icon(Icons.task_alt, size: 56, color: theme.colorScheme.primary),
        const SizedBox(height: 12),
        Text(
          '技能已提炼',
          style: theme.textTheme.titleMedium,
          textAlign: TextAlign.center,
        ),
        const SizedBox(height: 12),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(14),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (name.isNotEmpty)
                  Text(name, style: theme.textTheme.titleSmall),
                if (goal.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 4),
                    child: Text(goal, style: theme.textTheme.bodySmall),
                  ),
                if (steps.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 8),
                    child: Text(
                      '${steps.length} 个步骤',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 16),
        FilledButton.tonal(
          onPressed: () => setState(() {
            _phase = _DemoPhase.idle;
            _narrations.clear();
            _timeline = const {};
            _events = const [];
            _demoId = '';
            _skill = const {};
            _titleCtrl.clear();
            _notesCtrl.clear();
          }),
          child: const Text('再录一段'),
        ),
      ],
    );
  }
}
