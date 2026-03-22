import 'dart:async';
import 'package:flutter/material.dart';
import 'package:record/record.dart';
import 'package:path_provider/path_provider.dart';
import '../../services/api_client.dart';

/// Screen for managing the user's voiceprint enrollments.
class VoiceprintScreen extends StatefulWidget {
  final ApiClient api;
  const VoiceprintScreen({super.key, required this.api});

  @override
  State<VoiceprintScreen> createState() => _VoiceprintScreenState();
}

class _VoiceprintScreenState extends State<VoiceprintScreen> {
  List<Map<String, dynamic>> _voiceprints = [];
  bool _loading = true;
  String? _error;

  // Recording state
  final _recorder = AudioRecorder();
  bool _isRecording = false;
  int _seconds = 0;
  Timer? _timer;

  // Upload state
  bool _uploading = false;

  static const _minSeconds = 3;
  static const _maxSeconds = 30;

  @override
  void initState() {
    super.initState();
    _loadVoiceprints();
  }

  @override
  void dispose() {
    _timer?.cancel();
    _recorder.dispose();
    super.dispose();
  }

  Future<void> _loadVoiceprints() async {
    setState(() { _loading = true; _error = null; });
    try {
      _voiceprints = await widget.api.listMyVoiceprints();
    } on ApiException catch (e) {
      _error = e.message;
    } catch (e) {
      _error = e.toString();
    }
    if (mounted) setState(() => _loading = false);
  }

  Future<void> _startRecording() async {
    final hasPermission = await _recorder.hasPermission();
    if (!hasPermission) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('需要麦克风权限')),
        );
      }
      return;
    }

    final dir = await getTemporaryDirectory();
    final recordPath = '${dir.path}/vp_enroll_${DateTime.now().millisecondsSinceEpoch}.wav';

    await _recorder.start(
      const RecordConfig(
        encoder: AudioEncoder.wav,
        sampleRate: 16000,
        numChannels: 1,
        bitRate: 256000,
      ),
      path: recordPath,
    );

    setState(() { _isRecording = true; _seconds = 0; });

    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (_seconds >= _maxSeconds) {
        _stopAndUpload();
        return;
      }
      setState(() => _seconds++);
    });
  }

  Future<void> _stopAndUpload() async {
    _timer?.cancel();
    final path = await _recorder.stop();
    setState(() => _isRecording = false);

    if (path == null || _seconds < _minSeconds) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('录音太短，至少需要 $_minSeconds 秒')),
        );
      }
      return;
    }

    await _uploadVoiceprint(path);
  }

  Future<void> _cancelRecording() async {
    _timer?.cancel();
    await _recorder.stop();
    setState(() { _isRecording = false; _seconds = 0; });
  }

  Future<void> _uploadVoiceprint(String path) async {
    setState(() => _uploading = true);
    try {
      await widget.api.enrollVoiceprint(path);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('声纹注册成功')),
        );
      }
      await _loadVoiceprints();
    } on ApiException catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('注册失败: ${e.message}')),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('注册失败: $e')),
        );
      }
    }
    if (mounted) setState(() => _uploading = false);
  }

  Future<void> _deleteVoiceprint(String id) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('删除声纹'),
        content: const Text('确定要删除这条声纹记录吗？'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('取消')),
          TextButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('删除')),
        ],
      ),
    );
    if (confirmed != true) return;

    try {
      await widget.api.deleteVoiceprint(id);
      await _loadVoiceprints();
    } on ApiException catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('删除失败: ${e.message}')),
        );
      }
    }
  }

  String _formatTime(int totalSeconds) {
    final m = totalSeconds ~/ 60;
    final s = totalSeconds % 60;
    return '${m.toString().padLeft(2, '0')}:${s.toString().padLeft(2, '0')}';
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('声纹管理')),
      body: Column(
        children: [
          // ── Recording card ──
          Card(
            margin: const EdgeInsets.all(16),
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                children: [
                  Icon(
                    _isRecording ? Icons.mic : Icons.mic_none,
                    size: 48,
                    color: _isRecording ? Colors.red : theme.colorScheme.primary,
                  ),
                  const SizedBox(height: 12),
                  if (_isRecording) ...[
                    Text(
                      _formatTime(_seconds),
                      style: theme.textTheme.headlineMedium?.copyWith(
                        fontFeatures: [const FontFeature.tabularFigures()],
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      _seconds < _minSeconds
                          ? '继续说话... (至少 $_minSeconds 秒)'
                          : '可以停止了',
                      style: theme.textTheme.bodySmall,
                    ),
                    const SizedBox(height: 16),
                    Row(
                      mainAxisAlignment: MainAxisAlignment.center,
                      children: [
                        TextButton(
                          onPressed: _cancelRecording,
                          child: const Text('取消'),
                        ),
                        const SizedBox(width: 24),
                        FilledButton.icon(
                          onPressed: _seconds >= _minSeconds ? _stopAndUpload : null,
                          icon: const Icon(Icons.stop),
                          label: const Text('完成'),
                        ),
                      ],
                    ),
                  ] else if (_uploading) ...[
                    const CircularProgressIndicator(),
                    const SizedBox(height: 12),
                    const Text('正在上传并注册声纹...'),
                  ] else ...[
                    const Text('录制一段语音来注册你的声纹'),
                    const SizedBox(height: 4),
                    Text('建议朗读一段话，$_minSeconds-$_maxSeconds 秒',
                        style: theme.textTheme.bodySmall),
                    const SizedBox(height: 16),
                    FilledButton.icon(
                      onPressed: _startRecording,
                      icon: const Icon(Icons.mic),
                      label: const Text('开始录音'),
                    ),
                  ],
                ],
              ),
            ),
          ),
          // ── Voiceprint list ──
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Row(
              children: [
                Text('已注册声纹', style: theme.textTheme.titleMedium),
                const Spacer(),
                if (!_loading)
                  IconButton(
                    icon: const Icon(Icons.refresh, size: 20),
                    onPressed: _loadVoiceprints,
                  ),
              ],
            ),
          ),
          Expanded(child: _buildList()),
        ],
      ),
    );
  }

  Widget _buildList() {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(_error!, style: const TextStyle(color: Colors.red)),
            const SizedBox(height: 8),
            TextButton(onPressed: _loadVoiceprints, child: const Text('重试')),
          ],
        ),
      );
    }
    if (_voiceprints.isEmpty) {
      return const Center(child: Text('暂无声纹记录'));
    }
    return ListView.builder(
      itemCount: _voiceprints.length,
      padding: const EdgeInsets.symmetric(horizontal: 8),
      itemBuilder: (context, index) {
        final vp = _voiceprints[index];
        return ListTile(
          leading: const Icon(Icons.record_voice_over),
          title: Text(vp['label'] as String? ?? 'default'),
          subtitle: Text('${vp['created_at']}  •  ${vp['dim']}维'),
          trailing: IconButton(
            icon: const Icon(Icons.delete_outline, color: Colors.red),
            onPressed: () => _deleteVoiceprint(vp['id'] as String),
          ),
        );
      },
    );
  }
}
