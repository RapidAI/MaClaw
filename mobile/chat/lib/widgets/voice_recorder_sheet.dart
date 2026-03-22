import 'dart:async';
import 'package:flutter/material.dart';
import 'package:record/record.dart';
import 'package:path_provider/path_provider.dart';

/// Bottom sheet for voice recording with timer display.
class VoiceRecorderSheet extends StatefulWidget {
  final void Function(String path, int durationMs) onRecorded;
  final VoidCallback onCancel;

  const VoiceRecorderSheet({
    super.key,
    required this.onRecorded,
    required this.onCancel,
  });

  @override
  State<VoiceRecorderSheet> createState() => _VoiceRecorderSheetState();
}

class _VoiceRecorderSheetState extends State<VoiceRecorderSheet> {
  final _recorder = AudioRecorder();
  Timer? _timer;
  int _seconds = 0;
  bool _isRecording = false;
  String? _filePath;

  static const _maxDurationSeconds = 120;

  @override
  void dispose() {
    _timer?.cancel();
    _recorder.dispose();
    super.dispose();
  }

  Future<void> _startRecording() async {
    final hasPermission = await _recorder.hasPermission();
    if (!hasPermission) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Microphone permission required')),
        );
      }
      return;
    }

    final dir = await getTemporaryDirectory();
    _filePath =
        '${dir.path}/voice_${DateTime.now().millisecondsSinceEpoch}.m4a';

    await _recorder.start(
      const RecordConfig(encoder: AudioEncoder.aacLc, bitRate: 64000),
      path: _filePath!,
    );

    setState(() {
      _isRecording = true;
      _seconds = 0;
    });

    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (_seconds >= _maxDurationSeconds) {
        _stopRecording();
        return;
      }
      setState(() => _seconds++);
    });
  }

  Future<void> _stopRecording() async {
    _timer?.cancel();
    final path = await _recorder.stop();
    if (!mounted) return;

    if (path != null && _seconds >= 1) {
      widget.onRecorded(path, _seconds * 1000);
    } else {
      // Too short — discard.
      widget.onCancel();
    }
  }

  String _formatTime(int totalSeconds) {
    final m = totalSeconds ~/ 60;
    final s = totalSeconds % 60;
    return '${m.toString().padLeft(2, '0')}:${s.toString().padLeft(2, '0')}';
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            // Waveform indicator
            if (_isRecording)
              Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  const Icon(Icons.fiber_manual_record,
                      color: Colors.red, size: 12),
                  const SizedBox(width: 8),
                  Text(
                    _formatTime(_seconds),
                    style: Theme.of(context)
                        .textTheme
                        .headlineSmall
                        ?.copyWith(fontFeatures: [const FontFeature.tabularFigures()]),
                  ),
                ],
              )
            else
              Text('Tap to start recording',
                  style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 24),
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceEvenly,
              children: [
                TextButton(
                  onPressed: () {
                    if (_isRecording) {
                      _timer?.cancel();
                      _recorder.stop(); // discard
                    }
                    widget.onCancel();
                  },
                  child: const Text('Cancel'),
                ),
                IconButton(
                  iconSize: 56,
                  icon: Icon(
                    _isRecording ? Icons.stop_circle : Icons.fiber_manual_record,
                    color: Colors.red,
                  ),
                  onPressed:
                      _isRecording ? _stopRecording : _startRecording,
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
