/// 语音引擎设置 — 系统 TTS / 本地 Kokoro（与桌面同款模型），
/// 系统 ASR / 本地 SenseVoice 兜底，附模型下载与音色选择。
library;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/locale.dart';
import '../../core/providers.dart';
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
      _ttsEngine = e.ttsEngine;
      _asrEngine = e.asrEngine;
      _voice = e.ttsVoice;
    });
  }

  String? get _hubBase => ref.read(sessionProvider).active?.hubUrl;

  Future<void> _downloadTts() async {
    setState(() {
      _stage = '';
      _progress = 0;
      _error = '';
    });
    try {
      await _engine.assets.ensureAll(
        hubBase: _hubBase,
        onProgress: (s, p) {
          if (mounted) {
            setState(() {
              _stage = s;
              _progress = p;
            });
          }
        },
      );
      await _load();
    } on Object catch (e) {
      if (mounted) {
        setState(() {
          _error = '$e';
          _progress = -1;
        });
      }
    }
  }

  Future<void> _downloadAsr() async {
    setState(() {
      _stage = '';
      _progress = 0;
      _error = '';
    });
    try {
      await _engine.assets.ensureAll(
        hubBase: _hubBase,
        onProgress: (s, p) {
          if (mounted) {
            setState(() {
              _stage = s;
              _progress = p;
            });
          }
        },
      );
      await _load();
    } on Object catch (e) {
      if (mounted) {
        setState(() {
          _error = '$e';
          _progress = -1;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final busy = _progress >= 0 && _progress < 1;
    return SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(20, 0, 20, 24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(context.S.voiceTitle, style: theme.textTheme.titleMedium),
          const SizedBox(height: 16),

          Text(context.S.vsTts, style: theme.textTheme.labelLarge),
          const SizedBox(height: 6),
          SegmentedButton<TtsEngine>(
            segments: [
              ButtonSegment(
                value: TtsEngine.system,
                label: Text(context.S.vsTtsSystem),
              ),
              ButtonSegment(value: TtsEngine.kokoro, label: Text('Kokoro')),
            ],
            selected: {_ttsEngine},
            onSelectionChanged: busy
                ? null
                : (v) async {
                    await _engine.setTtsEngine(v.first);
                    setState(() => _ttsEngine = v.first);
                  },
          ),
          const SizedBox(height: 8),
          if (!_ttsReady)
            OutlinedButton.icon(
              onPressed: busy ? null : _downloadTts,
              icon: const Icon(Icons.download, size: 16),
              label: Text(context.S.vsDownloadKokoro),
            )
          else ...[
            Text(context.S.vsVoices, style: theme.textTheme.labelMedium),
            const SizedBox(height: 4),
            DropdownButtonFormField<String>(
              initialValue: _voice,
              isDense: true,
              decoration: const InputDecoration(
                border: OutlineInputBorder(),
                isDense: true,
              ),
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
          const SizedBox(height: 20),

          Text(context.S.vsAsr, style: theme.textTheme.labelLarge),
          const SizedBox(height: 6),
          SegmentedButton<AsrEngine>(
            segments: [
              ButtonSegment(
                value: AsrEngine.system,
                label: Text(context.S.vsAsrSystem),
              ),
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
          const SizedBox(height: 8),
          if (!_asrReady)
            OutlinedButton.icon(
              onPressed: busy ? null : _downloadAsr,
              icon: const Icon(Icons.download, size: 16),
              label: Text(context.S.vsDownloadSenseVoice),
            )
          else
            Text(
              context.S.vsSenseVoiceReady,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),

          if (_progress >= 0 && _progress < 1) ...[
            const SizedBox(height: 16),
            LinearProgressIndicator(value: _progress > 0 ? _progress : null),
            const SizedBox(height: 4),
            Text(_stage, style: theme.textTheme.bodySmall),
          ],
          if (_error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Text(
                _error,
                style: TextStyle(color: theme.colorScheme.error),
              ),
            ),
          const SizedBox(height: 8),
          Text(
            context.S.vsLocalNote,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}
