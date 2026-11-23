/// 语音设置 — 朗读音色，以及一段「语音识别」（系统识别 / 本地 SenseVoice）。
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/locale.dart';
import '../../core/native_voice.dart';
import '../../core/platform.dart';
import '../../core/providers.dart';
import '../../core/theme.dart';
import '../../core/ui_kit.dart';
import '../../core/voice_assets.dart';
import '../../core/voice_engine.dart';

class VoiceSettingsSheet extends ConsumerStatefulWidget {
  const VoiceSettingsSheet({super.key});

  @override
  ConsumerState<VoiceSettingsSheet> createState() => _VoiceSheetState();
}

class _VoiceSheetState extends ConsumerState<VoiceSettingsSheet> {
  bool _ttsReady = false;
  bool _asrReady = false;
  bool _modelsReady = false;
  String _stage = '';
  double _progress = -1;
  String _error = '';
  TtsEngine _ttsEngine = TtsEngine.system;
  AsrEngine _asrEngine = AsrEngine.system;
  String _voice = 'zf_xiaoyi';

  VoiceEngine get _engine => ref.read(voiceEngineProvider);

  static const _voiceNames = {
    'zf_xiaoyi': '晓伊（中文·女）',
    'zf_xiaoxiao': '晓晓（中文·女）',
    'zm_yunxi': '云希（中文·男）',
    'zm_yunyang': '云扬（中文·男）',
    'af_heart': 'Heart（英文·女）',
    'am_adam': 'Adam（英文·男）',
  };

  @override
  void initState() {
    super.initState();
    _load();
    _engine.downloadProgress.addListener(_onEngineProgress);
    // Models are a background job, not a button: a missing set starts here
    // too, in case the launch task has not run yet.
    unawaited(_ensureModels());
  }

  /// Idempotent: the engine dedupes with the launch task, so opening the
  /// sheet never starts a second download of the same files.
  Future<void> _ensureModels() async {
    if (!NativeVoice.usable) return;
    await _engine.autoEnsure(hubBase: _hubBase);
    if (mounted) await _load();
  }

  @override
  void dispose() {
    _engine.downloadProgress.removeListener(_onEngineProgress);
    super.dispose();
  }

  /// Mirror a background download started at app launch.
  void _onEngineProgress() {
    if (!mounted) return;
    final p = _engine.downloadProgress.value;
    var refresh = false;
    setState(() {
      if (p == null) {
        _stage = '';
        _progress = -1;
        _error = '';
        refresh = true; // download finished → refresh readiness flags
      } else if (p.progress < 0) {
        _stage = '';
        _progress = -1;
        _error = p.stage;
      } else {
        _stage = p.stage;
        _progress = p.progress;
      }
    });
    if (refresh) _load();
  }

  Future<void> _load() async {
    final e = _engine;
    await e.ensurePrefs();
    final t = await VoiceAssets.ttsReady();
    final a = await VoiceAssets.asrReady();
    if (!mounted) return;
    setState(() {
      _ttsReady = t;
      _asrReady = a;
      _modelsReady = t && a;
      _ttsEngine = e.ttsEngine;
      _asrEngine = e.asrEngine;
      _voice = e.ttsVoice;
    });
  }

  String? get _hubBase => ref.read(sessionProvider).active?.hubUrl;

  /// One line for a model that is still coming: the download runs in the
  /// background, so the sheet shows progress and nothing to press.
  Widget _modelStatus(
    ThemeData theme, {
    required bool ready,
    required String label,
  }) {
    final scheme = theme.colorScheme;
    return Row(
      children: [
        Icon(
          ready ? Icons.check_circle_outline : Icons.download_outlined,
          size: 14,
          color: ready ? scheme.primary : scheme.onSurfaceVariant,
        ),
        const SizedBox(width: AppSpacing.xs + 2),
        Expanded(
          child: Text(
            label,
            key: ValueKey(ready ? 'vs-model-ready' : 'vs-model-downloading'),
            style: theme.textTheme.bodySmall?.copyWith(
              color: scheme.onSurfaceVariant,
            ),
          ),
        ),
      ],
    );
  }

  /// The local engine is a native library, not a downloadable asset. When it
  /// is absent from the build, say so instead of offering a download that
  /// cannot make the model usable.
  Widget _engineMissingNote(ThemeData theme) {
    final reason = NativeVoice.unsupportedReason.trim();
    final detail = reason.isEmpty
        ? context.S.vsEngineMissing
        : context.S.vsEngineMissingDetail(reason);
    return Text(
      detail,
      key: const ValueKey('vs-engine-missing-detail'),
      style: theme.textTheme.bodySmall?.copyWith(
        color: theme.colorScheme.onSurfaceVariant,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final busy = _progress >= 0 && _progress < 1;
    return SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(
        AppSpacing.lg,
        0,
        AppSpacing.lg,
        AppSpacing.xxl,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(
              AppSpacing.xs,
              AppSpacing.sm,
              AppSpacing.xs,
              0,
            ),
            child: Text(
              context.S.voiceTitle,
              style: theme.textTheme.titleMedium,
            ),
          ),
          if (supportsVoiceOutput) ...[
            SectionHeader(title: context.S.vsTts, dense: true),
            Card(
              child: Padding(
                padding: const EdgeInsets.all(AppSpacing.lg),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
            SegmentedButton<TtsEngine>(
              segments: [
                ButtonSegment(
                  value: TtsEngine.system,
                  label: Text(context.S.vsTtsSystem),
                ),
                if (NativeVoice.usable)
                  ButtonSegment(
                    value: TtsEngine.kokoro,
                    label: Text('Kokoro'),
                  ),
              ],
              selected: {_ttsEngine},
              onSelectionChanged: busy
                  ? null
                  : (v) async {
                      await _engine.setTtsEngine(v.first);
                      setState(() => _ttsEngine = v.first);
                    },
            ),
            const SizedBox(height: AppSpacing.sm),
            if (!NativeVoice.usable)
              _engineMissingNote(theme)
            else if (!_ttsReady)
              _modelStatus(
                theme,
                ready: false,
                label: context.S.vsKokoroDownloading,
              )
            else if (NativeVoice.usable) ...[
              Text(context.S.vsVoices, style: theme.textTheme.labelMedium),
              const SizedBox(height: AppSpacing.xs),
              DropdownButtonFormField<String>(
                initialValue: _voice,
                isDense: true,
                decoration: const InputDecoration(isDense: true),
                items: [
                  for (final id in VoiceAssets.voiceIds)
                    DropdownMenuItem(
                      value: id,
                      child: Text(_voiceNames[id] ?? id),
                    ),
                ],
                onChanged: busy
                    ? null
                    : (v) async {
                        if (v == null) return;
                        await _engine.setTtsVoice(v);
                        setState(() => _voice = v);
                      },
              ),
            ],
                  ],
                ),
              ),
            ),
          ],

          if (supportsVoiceInput) ...[
            SectionHeader(title: context.S.vsAsr),
            Card(
              child: Padding(
                padding: const EdgeInsets.all(AppSpacing.lg),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
            SegmentedButton<AsrEngine>(
              segments: [
                ButtonSegment(
                  value: AsrEngine.system,
                  label: Text(context.S.vsAsrSystem),
                ),
                if (NativeVoice.usable)
                  ButtonSegment(
                    value: AsrEngine.local,
                    label: Text(context.S.vsAsrLocal),
                  ),
              ],
              selected: {_asrEngine},
              onSelectionChanged: busy
                  ? null
                  : (v) async {
                      await _engine.setAsrEngine(v.first);
                      setState(() => _asrEngine = v.first);
                    },
            ),
            const SizedBox(height: AppSpacing.sm),
            if (!NativeVoice.usable)
              // This build has no libmaclaw_voice.so: a download could never
              // fix it, so stop pointing the person at one (papers
              // 2026-11-22 03:05: the assistant kept saying "downloading the
              // local model, try again later" forever).
              _engineMissingNote(theme)
            else if (!_asrReady)
              _modelStatus(
                theme,
                ready: false,
                label: context.S.vsSenseVoiceDownloading,
              )
            else if (NativeVoice.usable)
              Text(
                context.S.vsSenseVoiceReady,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
                  ],
                ),
              ),
            ),
          ],
          // Downloads run in the background; this is the only progress the
          // sheet shows, plus one line when everything is ready.
          if (_progress < 0 && _modelsReady)
            Card(
              child: Padding(
                padding: const EdgeInsets.all(AppSpacing.lg),
                child: _modelStatus(
                  theme,
                  ready: true,
                  label: context.S.vsModelsReady,
                ),
              ),
            ),
          if (_progress >= 0 && _progress < 1)
            Card(
              child: Padding(
                padding: const EdgeInsets.all(AppSpacing.lg),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    LinearProgressIndicator(
                      value: _progress > 0 ? _progress : null,
                      borderRadius: BorderRadius.circular(AppRadius.sm),
                    ),
                    const SizedBox(height: AppSpacing.xs),
                    Text(_stage, style: theme.textTheme.bodySmall),
                  ],
                ),
              ),
            ),
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
          Card(
            child: Padding(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: Text(
                context.S.vsLocalNote,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
