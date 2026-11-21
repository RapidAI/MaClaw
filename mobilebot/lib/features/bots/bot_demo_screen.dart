import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';
import 'package:uuid/uuid.dart';

import '../../core/locale.dart';
import '../../core/platform.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart';
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
    setState(() => _narrations.add(syncTextOr(out['text'], text)));
  });

  Future<void> _toggleMic() async {
    if (_micRecording) {
      setState(() => _micRecording = false);
      final String path;
      try {
        path = await _recorder.stop() ?? '';
      } on Object catch (error) {
        if (mounted) setState(() => _error = failureMessage(error));
        return;
      }
      if (path.isEmpty || !mounted) return;
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
        setState(() => _narrations.add('🎙 ${syncText(out['text'])}'));
      });
      return;
    }
    if (!await _recorder.hasPermission()) {
      if (mounted) {
        setState(() => _error = context.S.demoMicDenied);
      }
      return;
    }
    try {
      final dir = await getTemporaryDirectory();
      final path =
          '${dir.path}${Platform.pathSeparator}narrate-${const Uuid().v4()}.wav';
      await _recorder.start(
        const RecordConfig(encoder: AudioEncoder.wav),
        path: path,
      );
    } on Object catch (error) {
      if (mounted) setState(() => _error = failureMessage(error));
      return;
    }
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
      _demoId = syncText(timeline['id']);
      _events = [
        for (final e in syncList(timeline['events']))
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
    final kind = syncText(e['kind']);
    final action = syncText(e['action']);
    final text = syncText(e['text']);
    final app = syncText(e['app']);
    final window = syncText(e['window']);
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
      appBar: AppBar(
        title: Row(
          children: [
            const BrandMark(size: 26, tile: true),
            const SizedBox(width: AppSpacing.sm + 2),
            Expanded(
              child: Text(
                context.S.demoTitle(widget.bot.title),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
        ),
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(
          AppSpacing.lg,
          AppSpacing.md,
          AppSpacing.lg,
          AppSpacing.xxl,
        ),
        children: [
          if (_error.isNotEmpty)
            Card(
              color: theme.colorScheme.errorContainer,
              child: Padding(
                padding: const EdgeInsets.all(AppSpacing.lg),
                child: Text(
                  _error,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onErrorContainer,
                  ),
                ),
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
        Card(
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Container(
                  width: 40,
                  height: 40,
                  decoration: BoxDecoration(
                    color: theme.colorScheme.primary.withValues(alpha: 0.14),
                    borderRadius: BorderRadius.circular(AppRadius.sm),
                  ),
                  child: Icon(
                    Icons.smart_display_outlined,
                    size: 21,
                    color: theme.colorScheme.primary,
                  ),
                ),
                const SizedBox(width: AppSpacing.md),
                Expanded(
                  child: Text(
                    context.S.demoIntro(widget.bot.title),
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
        SectionHeader(title: context.S.demoTitleLabel, dense: true),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: TextField(
              controller: _titleCtrl,
              decoration: InputDecoration(
                labelText: context.S.demoTitleLabel,
                hintText: context.S.demoTitleHint,
              ),
            ),
          ),
        ),
        const SizedBox(height: AppSpacing.lg),
        FilledButton.icon(
          onPressed: _busy ? null : _start,
          icon: const Icon(Icons.fiber_manual_record, size: 16),
          label: Text(context.S.demoStart),
        ),
      ],
    );
  }

  Widget _buildRecording(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Card(
          color: theme.colorScheme.errorContainer,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(
              AppSpacing.lg,
              AppSpacing.sm,
              AppSpacing.sm,
              AppSpacing.sm,
            ),
            child: Row(
              children: [
                Icon(
                  Icons.fiber_manual_record,
                  color: theme.colorScheme.error,
                  size: 18,
                ),
                const SizedBox(width: AppSpacing.sm),
                Expanded(
                  child: Text(
                    context.S.demoRecording(widget.bot.title),
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.onErrorContainer,
                    ),
                  ),
                ),
                TextButton(
                  onPressed: () => context.push(
                    '/bot/${Uri.encodeComponent(widget.bot.id)}/desktop',
                  ),
                  child: Text(context.S.demoWatch),
                ),
              ],
            ),
          ),
        ),
        SectionHeader(title: context.S.demoNarration, dense: true),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: _narrateCtrl,
                        decoration: InputDecoration(
                          hintText: context.S.demoNarrationHint,
                          isDense: true,
                        ),
                      ),
                    ),
                    if (_asrAvailable && supportsVoiceInput)
                      IconButton(
                        tooltip: _micRecording
                            ? context.S.demoStopRecord
                            : context.S.demoVoiceNarration,
                        icon: Icon(
                          _micRecording ? Icons.stop : Icons.mic,
                          color: _micRecording
                              ? theme.colorScheme.error
                              : null,
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
                  const SizedBox(height: AppSpacing.md),
                  for (final n in _narrations)
                    Padding(
                      padding: const EdgeInsets.only(bottom: AppSpacing.xs),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Icon(
                            Icons.subdirectory_arrow_right,
                            size: 14,
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                          const SizedBox(width: AppSpacing.sm),
                          Expanded(
                            child: Text(n, style: theme.textTheme.bodySmall),
                          ),
                        ],
                      ),
                    ),
                ],
              ],
            ),
          ),
        ),
        const SizedBox(height: AppSpacing.lg),
        FilledButton.tonalIcon(
          onPressed: _busy ? null : _stop,
          icon: const Icon(Icons.stop),
          label: Text(context.S.demoFinish),
        ),
      ],
    );
  }

  Widget _buildTimeline(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SectionHeader(
          title: context.S.demoDone(_events.length),
          dense: true,
        ),
        for (final e in _events)
          Card(
            child: Padding(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(
                    _eventIcon(syncText(e['kind'])),
                    size: 18,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                  const SizedBox(width: AppSpacing.md),
                  Expanded(
                    child: Text(
                      _eventLabel(e),
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
                  if (syncText(e['ref']).isNotEmpty)
                    ClipRRect(
                      borderRadius: BorderRadius.circular(AppRadius.sm),
                      child: _shot(syncText(e['ref'])),
                    ),
                ],
              ),
            ),
          ),
        SectionHeader(title: context.S.demoExtraLabel, dense: true),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: TextField(
              controller: _notesCtrl,
              decoration: InputDecoration(
                labelText: context.S.demoExtraLabel,
                hintText: context.S.demoExtraHint,
              ),
              maxLines: 3,
            ),
          ),
        ),
        const SizedBox(height: AppSpacing.lg),
        FilledButton.icon(
          onPressed: _busy || _timeline.isEmpty ? null : _distill,
          icon: const Icon(Icons.auto_fix_high),
          label: Text(context.S.demoDistill),
        ),
      ],
    );
  }

  Widget _buildSkill(ThemeData theme) {
    final name = syncText(_skill['name']);
    final goal = syncText(_skill['goal']);
    final steps = _skill['steps'] is List ? _skill['steps'] as List : const [];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Card(
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: Row(
              children: [
                Container(
                  width: 40,
                  height: 40,
                  decoration: BoxDecoration(
                    color: theme.colorScheme.primary.withValues(alpha: 0.14),
                    borderRadius: BorderRadius.circular(AppRadius.sm),
                  ),
                  child: Icon(
                    Icons.task_alt,
                    size: 21,
                    color: theme.colorScheme.primary,
                  ),
                ),
                const SizedBox(width: AppSpacing.md),
                Expanded(
                  child: Text(
                    context.S.demoSkillReady,
                    style: theme.textTheme.titleSmall,
                  ),
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: AppSpacing.sm),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (name.isNotEmpty)
                  Text(name, style: theme.textTheme.titleSmall),
                if (goal.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: AppSpacing.xs),
                    child: Text(goal, style: theme.textTheme.bodySmall),
                  ),
                if (steps.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: AppSpacing.sm),
                    child: Text(
                      context.S.demoStepCount(steps.length),
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ),
        const SizedBox(height: AppSpacing.lg),
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
          child: Text(context.S.demoRecordAgain),
        ),
      ],
    );
  }
}
