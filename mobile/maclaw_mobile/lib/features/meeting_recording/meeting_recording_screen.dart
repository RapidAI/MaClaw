import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';

import 'meeting_recording_upload_queue.dart';

/// Native long-form recorder launched from the AI assistant.  Recording is kept
/// on-device until Hub confirms every uploaded byte; a later upload queue can
/// reuse the same [MeetingRecordingResult.localPath] after a process restart.
class MeetingRecordingScreen extends StatefulWidget {
  final String title;
  final String purpose;
  final String conversationId;
  final String processMode;

  const MeetingRecordingScreen({
    super.key,
    required this.title,
    this.purpose = '',
    this.conversationId = '',
    this.processMode = 'minutes',
  });

  @override
  State<MeetingRecordingScreen> createState() => _MeetingRecordingScreenState();
}

class _MeetingRecordingScreenState extends State<MeetingRecordingScreen> {
  // Keep a small buffer below Hub's 512 MiB PCM-WAV quota (4:39:37).
  static const _maxWavMeetingDuration = Duration(hours: 4, minutes: 39);
  final AudioRecorder _recorder = AudioRecorder();
  Timer? _ticker;
  StreamSubscription<RecordState>? _recorderStateSubscription;
  DateTime? _startedAt;
  Duration _elapsedBeforePause = Duration.zero;
  Duration _elapsed = Duration.zero;
  String? _path;
  String _phase = 'ready';
  String? _error;
  bool _recoveringInterruptedRecording = false;

  bool get _recording => _phase == 'recording';
  bool get _paused => _phase == 'paused';

  @override
  void initState() {
    super.initState();
    // Native platforms emit stop when an audio interruption, device change, or
    // system recorder shutdown ends capture. Treat a successfully materialized
    // file as a completed recording: persist it first, then let the existing
    // restart-safe upload queue resume delivery.
    _recorderStateSubscription = _recorder.onStateChanged().listen((state) {
      if (state == RecordState.stop) {
        unawaited(_recoverInterruptedRecording());
      }
    });
  }

  @override
  void dispose() {
    _ticker?.cancel();
    unawaited(_recorderStateSubscription?.cancel());
    unawaited(_recorder.dispose());
    super.dispose();
  }

  Future<void> _recoverInterruptedRecording() async {
    if (_recoveringInterruptedRecording ||
        _phase == 'finalizing' ||
        _phase == 'uploading' ||
        _phase == 'done' ||
        _path == null) {
      return;
    }
    _recoveringInterruptedRecording = true;
    _ticker?.cancel();
    try {
      // A platform stop may already have finalized the output. Calling stop is
      // still the only cross-platform way for the record package to return its
      // final path, so retain the path captured at start as a fallback.
      final stoppedPath = await _recorder.stop();
      final path = stoppedPath ?? _path;
      if (path == null || !await File(path).exists()) {
        if (mounted) {
          setState(() {
            _phase = 'failed';
            _error = '录音被系统中断，未找到可恢复的音频文件。请重新开始录音。';
          });
        }
        return;
      }
      _path = path;
      if (mounted) {
        setState(() {
          _phase = 'finalizing';
          _error = '录音被系统中断，正在保存并恢复上传。';
        });
      }
      await _upload(File(path));
    } on Object catch (error) {
      if (mounted) {
        setState(() {
          _phase = 'failed';
          _error = '录音被系统中断，音频已保留在本机：$error';
        });
      }
    } finally {
      _recoveringInterruptedRecording = false;
    }
  }

  Future<void> _start() async {
    try {
      final granted = await _recorder.hasPermission();
      if (!granted) {
        setState(() => _error = '无法访问麦克风，请在系统设置中允许 MaClaw 使用麦克风。');
        return;
      }
      final dir = await getApplicationDocumentsDirectory();
      final safeTitle = widget.title
          .replaceAll(RegExp(r'[^a-zA-Z0-9_\-\u4e00-\u9fff]+'), '_');
      final path =
          '${dir.path}${Platform.pathSeparator}meeting_${DateTime.now().millisecondsSinceEpoch}_$safeTitle.wav';
      await _recorder.start(
        const RecordConfig(
          // Standard PCM WAV is consumed directly by CoreLib ASR. This keeps
          // the mobile meeting path independent of FFmpeg and external codecs.
          encoder: AudioEncoder.wav,
          sampleRate: 16000,
          numChannels: 1,
          autoGain: true,
          echoCancel: true,
          noiseSuppress: true,
          androidConfig: const AndroidRecordConfig(
            service: AndroidService(
              title: 'MaClaw 正在录制会议',
              content: '点击 MaClaw 返回会议录音',
            ),
          ),
        ),
        path: path,
      );
      _path = path;
      _startedAt = DateTime.now();
      _elapsedBeforePause = Duration.zero;
      _elapsed = Duration.zero;
      _startElapsedTicker();
      setState(() {
        _phase = 'recording';
        _error = null;
      });
    } on Object catch (e) {
      setState(() => _error = '无法开始录音：$e');
    }
  }

  Future<void> _pauseOrResume() async {
    try {
      if (_recording) {
        await _recorder.pause();
        _elapsedBeforePause = _elapsed;
        _ticker?.cancel();
        setState(() => _phase = 'paused');
      } else if (_paused) {
        await _recorder.resume();
        _startedAt = DateTime.now();
        _startElapsedTicker();
        setState(() => _phase = 'recording');
      }
    } on Object catch (e) {
      setState(() => _error = '无法更新录音状态：$e');
    }
  }

  Future<void> _stopAndUpload() async {
    if (_phase == 'finalizing' || _phase == 'uploading' || _phase == 'done') {
      return;
    }
    _ticker?.cancel();
    setState(() {
      _phase = 'finalizing';
      _error = null;
    });
    try {
      final output = await _recorder.stop();
      final path = output ?? _path;
      if (path == null || !await File(path).exists())
        throw StateError('录音文件不存在');
      _path = path;
      await _upload(File(path));
    } on Object catch (e) {
      if (mounted)
        setState(() {
          _phase = 'failed';
          _error = '录音已保存在本机，上传失败：$e';
        });
    }
  }

  Future<void> _upload(File file) async {
    setState(() => _phase = 'uploading');
    final queue = MeetingRecordingUploadQueue();
    final task = await queue.enqueue(
      localPath: file.path,
      title: widget.title,
      purpose: widget.purpose,
      conversationId: widget.conversationId,
      durationSec: _elapsed.inMilliseconds / 1000,
      processMode: widget.processMode,
      contentType: 'audio/wav',
    );
    final uploaded = await queue.upload(task);
    if (uploaded.status != 'processing' && uploaded.status != 'ready') {
      throw StateError(uploaded.message);
    }
    if (!mounted) return;
    setState(() => _phase = 'done');
    Navigator.of(context).pop(MeetingRecordingResult(
      recordingId: uploaded.recordingId,
      duration: _elapsed,
      // Hub takes custody before the queue may delete this duplicate file.
      localPath: uploaded.status == 'ready' ? '' : file.path,
      status: uploaded.status,
      processMode: widget.processMode,
    ));
  }

  String get _time {
    final h = _elapsed.inHours;
    final m = _elapsed.inMinutes.remainder(60).toString().padLeft(2, '0');
    final s = _elapsed.inSeconds.remainder(60).toString().padLeft(2, '0');
    return h > 0 ? '$h:$m:$s' : '$m:$s';
  }

  void _startElapsedTicker() {
    _ticker?.cancel();
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted || _startedAt == null || !_recording) return;
      final elapsed =
          _elapsedBeforePause + DateTime.now().difference(_startedAt!);
      setState(() => _elapsed = elapsed);
      if (elapsed >= _maxWavMeetingDuration) {
        unawaited(_stopAndUpload());
      }
    });
  }

  String get _finishLabel {
    switch (widget.processMode) {
      case 'keep':
        return '结束并归档音频';
      case 'transcript':
        return '结束并生成逐字稿';
      case 'minutes':
      default:
        return '结束并生成纪要';
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final busy = _phase == 'finalizing' || _phase == 'uploading';
    return Scaffold(
      appBar: AppBar(title: const Text('会议录音')),
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child:
              Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            Text(widget.title,
                style: Theme.of(context)
                    .textTheme
                    .headlineSmall
                    ?.copyWith(fontWeight: FontWeight.w700)),
            if (widget.purpose.isNotEmpty)
              Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Text(widget.purpose,
                      style: TextStyle(color: scheme.onSurfaceVariant))),
            const Spacer(),
            Center(
                child: Container(
                    width: 164,
                    height: 164,
                    decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: _recording
                            ? scheme.errorContainer
                            : scheme.primaryContainer),
                    child: Icon(_recording ? Icons.mic : Icons.mic_none,
                        size: 68,
                        color: _recording ? scheme.error : scheme.primary))),
            const SizedBox(height: 20),
            Center(
                child: Text(_time,
                    style: Theme.of(context).textTheme.displaySmall?.copyWith(
                        fontFeatures: const [FontFeature.tabularFigures()]))),
            const SizedBox(height: 8),
            Center(
                child: Text(
                    _phase == 'uploading'
                        ? '正在安全上传录音…'
                        : _paused
                            ? '录音已暂停'
                            : _recording
                                ? '正在录音'
                                : '录音仅会在你点击开始后进行',
                    style: TextStyle(color: scheme.onSurfaceVariant))),
            if (_error != null)
              Padding(
                  padding: const EdgeInsets.only(top: 18),
                  child: Text(_error!,
                      textAlign: TextAlign.center,
                      style: TextStyle(color: scheme.error))),
            const Spacer(),
            if (_phase == 'ready' || _phase == 'failed')
              FilledButton.icon(
                  onPressed: busy ? null : _start,
                  icon: const Icon(Icons.fiber_manual_record),
                  label: const Text('开始录音')),
            if (_recording || _paused)
              Row(children: [
                Expanded(
                    child: OutlinedButton.icon(
                        onPressed: _pauseOrResume,
                        icon: Icon(_paused ? Icons.play_arrow : Icons.pause),
                        label: Text(_paused ? '继续' : '暂停'))),
                const SizedBox(width: 12),
                Expanded(
                    child: FilledButton.icon(
                        onPressed: _stopAndUpload,
                        icon: const Icon(Icons.stop),
                        label: Text(_finishLabel)))
              ]),
            if (busy)
              const Padding(
                  padding: EdgeInsets.only(top: 16),
                  child: LinearProgressIndicator()),
          ]),
        ),
      ),
    );
  }
}

class MeetingRecordingResult {
  final String recordingId;
  final Duration duration;
  final String localPath;
  final String status;
  final String processMode;
  const MeetingRecordingResult(
      {required this.recordingId,
      required this.duration,
      required this.localPath,
      required this.status,
      this.processMode = 'minutes'});
}
