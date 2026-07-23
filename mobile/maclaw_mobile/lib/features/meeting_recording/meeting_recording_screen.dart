import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';

import '../auth/session_controller.dart';
import 'meeting_recording_upload_queue.dart';
import 'meeting_recording_upload.dart';

/// Native long-form recorder launched from the AI assistant.  Recording is kept
/// on-device until Hub confirms every uploaded byte; a later upload queue can
/// reuse the same [MeetingRecordingResult.localPath] after a process restart.
class MeetingRecordingScreen extends ConsumerStatefulWidget {
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
  ConsumerState<MeetingRecordingScreen> createState() =>
      _MeetingRecordingScreenState();
}

class _MeetingRecordingScreenState extends ConsumerState<MeetingRecordingScreen>
    with WidgetsBindingObserver {
  // Keep a small buffer below Hub's 512 MiB PCM-WAV quota (4:39:37).
  static const _maxWavMeetingDuration = Duration(hours: 4, minutes: 39);
  final AudioRecorder _recorder = AudioRecorder();
  Timer? _ticker;
  Timer? _liveUploadTicker;
  StreamSubscription<RecordState>? _recorderStateSubscription;
  StreamSubscription<Amplitude>? _amplitudeSubscription;
  DateTime? _startedAt;
  Duration _elapsedBeforePause = Duration.zero;
  Duration _elapsed = Duration.zero;
  String? _path;
  String _phase = 'ready';
  String? _error;
  bool _recoveringInterruptedRecording = false;
  bool _stoppingOrUploading = false;
  bool _startingRecording = false;
  bool _changingRecordingState = false;
  bool _disposed = false;
  MeetingRecordingUploadQueue? _liveUploadQueue;
  MeetingRecordingUpload? _liveUploadTask;
  Future<void>? _liveUploadSetup;
  Future<void>? _liveUploadInFlight;
  // Keep repaint work bounded to the waveform. Native amplitude events arrive
  // about eleven times per second; rebuilding the whole recording screen for
  // each one is needlessly expensive on lower-end phones.
  final ValueNotifier<List<double>> _waveform = ValueNotifier(
    List<double>.filled(36, 0.035, growable: true),
  );

  bool get _recording => _phase == 'recording';
  bool get _paused => _phase == 'paused';
  bool get _active => mounted && !_disposed;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    // Native platforms emit stop when an audio interruption, device change, or
    // system recorder shutdown ends capture. Treat a successfully materialized
    // file as a completed recording: persist it first, then let the existing
    // restart-safe upload queue resume delivery.
    _recorderStateSubscription = _recorder.onStateChanged().listen(
      (state) {
        // `record` can emit an initial `stop` while Android is showing the
        // first-use microphone permission prompt or bringing up its recorder
        // service.  That is not an interrupted recording.  Only recover a
        // stop after this screen has actually entered an active session.
        // A deliberately paused recorder is not interrupted. Some platform
        // backends can surface a stop while transitioning audio focus, so only
        // auto-recover a capture that the UI still considers active.
        if (_active &&
            !_changingRecordingState &&
            state == RecordState.stop &&
            _recording) {
          unawaited(_recoverInterruptedRecording());
        }
      },
      onError: (Object error, StackTrace stackTrace) {
        if (_active) {
          setState(() => _error = '录音服务异常：$error');
        }
      },
    );
    _amplitudeSubscription =
        _recorder.onAmplitudeChanged(const Duration(milliseconds: 90)).listen(
      (amplitude) {
        if (!_active || !_recording) return;
        final level = _normalizedAmplitude(amplitude.current);
        final next = List<double>.from(_waveform.value, growable: true)
          ..removeAt(0)
          ..add(level);
        _waveform.value = next;
      },
    );
  }

  @override
  void dispose() {
    // `AudioRecorder.dispose()` can cause the platform state stream to emit a
    // final stop event. Mark this State inactive before releasing either
    // resource so that event cannot start an async recovery which later calls
    // setState or Navigator on a deactivated widget tree.
    _disposed = true;
    WidgetsBinding.instance.removeObserver(this);
    _ticker?.cancel();
    _liveUploadTicker?.cancel();
    unawaited(_recorderStateSubscription?.cancel());
    unawaited(_amplitudeSubscription?.cancel());
    _waveform.dispose();
    unawaited(_recorder.dispose());
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // iOS suspends Dart timers in the background while AVAudioRecorder keeps
    // writing through the declared background-audio mode. Reconcile state as
    // soon as the app returns so the elapsed time and interruption UI are
    // accurate even when a platform event was deferred.
    if (state == AppLifecycleState.resumed) {
      unawaited(_reconcileRecordingAfterResume());
    }
  }

  Future<void> _reconcileRecordingAfterResume() async {
    // A user-paused recorder intentionally reports `isRecording == false`.
    // Only an active capture needs reconciliation; otherwise returning from
    // the background would finalize and upload a recording the user paused.
    if (!_active || !_recording) return;
    try {
      if (await _recorder.isRecording()) {
        _startElapsedTicker();
        _startLiveUploadTicker();
        return;
      }
      await _recoverInterruptedRecording();
    } on Object catch (error) {
      if (_active) {
        setState(() => _error = '恢复录音状态失败：$error');
      }
    }
  }

  Future<void> _recoverInterruptedRecording() async {
    if (!_active ||
        _recoveringInterruptedRecording ||
        _stoppingOrUploading ||
        _phase == 'finalizing' ||
        _phase == 'uploading' ||
        _phase == 'done' ||
        _path == null) {
      return;
    }
    _recoveringInterruptedRecording = true;
    _stoppingOrUploading = true;
    _ticker?.cancel();
    _liveUploadTicker?.cancel();
    try {
      // A platform stop may already have finalized the output. Calling stop is
      // still the only cross-platform way for the record package to return its
      // final path, so retain the path captured at start as a fallback.
      final stoppedPath = await _recorder.stop();
      if (!_active) return;
      final path = stoppedPath ?? _path;
      if (path == null || !await File(path).exists()) {
        if (_active) {
          setState(() {
            _phase = 'failed';
            _error = '录音被系统中断，未找到可恢复的音频文件。请重新开始录音。';
          });
        }
        return;
      }
      if (!_active) return;
      _path = path;
      await _liveUploadSetup;
      await _liveUploadInFlight;
      if (_active) {
        setState(() {
          _phase = 'finalizing';
          _error = '录音被系统中断，正在保存并恢复上传。';
        });
      }
      await _upload(File(path));
    } on Object catch (error) {
      if (_active) {
        setState(() {
          _phase = 'failed';
          _error = '录音被系统中断，音频已保留在本机：$error';
        });
      }
    } finally {
      _recoveringInterruptedRecording = false;
      _stoppingOrUploading = false;
    }
  }

  Future<void> _start() async {
    if (!_active ||
        _startingRecording ||
        _stoppingOrUploading ||
        _recording ||
        _paused) {
      return;
    }
    _startingRecording = true;
    try {
      final granted = await _recorder.hasPermission();
      if (!_active) return;
      if (!granted) {
        setState(() => _error = '无法访问麦克风，请在系统设置中允许 MaClaw 使用麦克风。');
        return;
      }
      final dir = await getApplicationDocumentsDirectory();
      if (!_active) return;
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
          androidConfig: AndroidRecordConfig(
            service: AndroidService(
              title: 'MaClaw 正在录制会议',
              content: '点击 MaClaw 返回会议录音',
            ),
            // WAV uses the package's PCM-based recorder, whose live amplitude
            // is derived from audio frames. Keep the physical microphone as
            // the source so device defaults do not flatten its level meter.
            audioSource: AndroidAudioSource.mic,
            manageBluetooth: false,
          ),
          // Keep `record` in control of the shared AVAudioSession. On iOS it
          // configures play-and-record, activates the session, and continues
          // the capture while the app is backgrounded under UIBackgroundModes.
          iosConfig: IosRecordConfig(
            categoryOptions: [IosAudioCategoryOption.allowBluetooth],
          ),
        ),
        path: path,
      );
      if (!_active) {
        await _recorder.stop();
        return;
      }
      _path = path;
      _startedAt = DateTime.now();
      _elapsedBeforePause = Duration.zero;
      _elapsed = Duration.zero;
      _resetWaveform();
      setState(() {
        _phase = 'recording';
        _error = null;
      });
      _startElapsedTicker();
      _liveUploadSetup = _prepareLiveUpload(path);
    } on Object catch (e) {
      if (_active) {
        setState(() => _error = '无法开始录音：$e');
      }
    } finally {
      _startingRecording = false;
    }
  }

  Future<void> _pauseOrResume() async {
    if (_active == false || _stoppingOrUploading || _changingRecordingState) {
      return;
    }
    _changingRecordingState = true;
    try {
      if (_recording) {
        await _recorder.pause();
        if (!_active) return;
        _elapsedBeforePause = _elapsed;
        _ticker?.cancel();
        _liveUploadTicker?.cancel();
        setState(() {
          _phase = 'paused';
          _resetWaveform();
        });
      } else if (_paused) {
        await _recorder.resume();
        if (!_active) return;
        _startedAt = DateTime.now();
        setState(() => _phase = 'recording');
        _startElapsedTicker();
        _startLiveUploadTicker();
      }
    } on Object catch (e) {
      if (_active) {
        setState(() => _error = '无法更新录音状态：$e');
      }
    } finally {
      _changingRecordingState = false;
    }
  }

  Future<void> _stopAndUpload() async {
    if (!_active ||
        _startingRecording ||
        _stoppingOrUploading ||
        _phase == 'finalizing' ||
        _phase == 'uploading' ||
        _phase == 'done') {
      return;
    }
    _stoppingOrUploading = true;
    _ticker?.cancel();
    _liveUploadTicker?.cancel();
    setState(() {
      _phase = 'finalizing';
      _error = null;
    });
    try {
      final output = await _recorder.stop();
      if (!_active) return;
      final path = output ?? _path;
      if (path == null || !await File(path).exists()) {
        throw StateError('录音文件不存在');
      }
      if (!_active) return;
      _path = path;
      await _liveUploadSetup;
      await _liveUploadInFlight;
      await _upload(File(path));
    } on Object catch (e) {
      if (_active) {
        setState(() {
          _phase = 'failed';
          _error = '录音已保存在本机，上传失败：$e';
        });
      }
    } finally {
      _stoppingOrUploading = false;
    }
  }

  Future<void> _upload(File file) async {
    if (!_active) return;
    setState(() => _phase = 'uploading');
    final client = ref.read(apiClientProvider);
    if (client == null) {
      throw StateError('登录已失效，请重新登录后恢复上传。');
    }
    final queue = MeetingRecordingUploadQueue(api: client);
    final liveTask = _liveUploadTask;
    final uploaded = liveTask != null && liveTask.localPath == file.path
        ? await (_liveUploadQueue ?? queue).finalizeLiveUpload(
            liveTask,
            durationSec: _elapsed.inMilliseconds / 1000,
          )
        : await queue.upload(await queue.enqueue(
            localPath: file.path,
            title: widget.title,
            purpose: widget.purpose,
            conversationId: widget.conversationId,
            durationSec: _elapsed.inMilliseconds / 1000,
            processMode: widget.processMode,
            contentType: 'audio/wav',
          ));
    if (uploaded.status != 'processing' && uploaded.status != 'ready') {
      throw StateError(uploaded.message);
    }
    if (!_active) return;
    setState(() => _phase = 'done');
    if (!mounted) return;
    Navigator.pop(
      context,
      MeetingRecordingResult(
        recordingId: uploaded.recordingId,
        duration: _elapsed,
        // Hub takes custody before the queue may delete this duplicate file.
        localPath: uploaded.status == 'ready' ? '' : file.path,
        status: uploaded.status,
        processMode: widget.processMode,
      ),
    );
  }

  String get _time {
    final h = _elapsed.inHours;
    final m = _elapsed.inMinutes.remainder(60).toString().padLeft(2, '0');
    final s = _elapsed.inSeconds.remainder(60).toString().padLeft(2, '0');
    return h > 0 ? '$h:$m:$s' : '$m:$s';
  }

  double _normalizedAmplitude(double dbfs) {
    // record reports dBFS, generally in the [-160, 0] range. The lower floor
    // prevents a flat, invisible idle waveform while keeping speech prominent.
    return ((dbfs.clamp(-60.0, 0.0) + 60) / 60 * .9 + .1).clamp(.1, 1.0);
  }

  void _resetWaveform() {
    _waveform.value = List<double>.filled(36, 0.035, growable: true);
  }

  void _startElapsedTicker() {
    _ticker?.cancel();
    _refreshElapsed();
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) {
      _refreshElapsed();
    });
  }

  Future<void> _prepareLiveUpload(String path) async {
    final client = ref.read(apiClientProvider);
    if (client == null) return;
    final queue = MeetingRecordingUploadQueue(api: client);
    try {
      final task = await queue.enqueue(
        localPath: path,
        title: widget.title,
        purpose: widget.purpose,
        conversationId: widget.conversationId,
        durationSec: 0,
        processMode: widget.processMode,
        contentType: 'audio/wav',
        initialStatus: 'recording',
      );
      // Retain this task even when the Hub is temporarily unavailable. The
      // final upload then reuses it instead of creating a duplicate task.
      _liveUploadQueue = queue;
      _liveUploadTask = task;
      final live = await queue.startLiveUpload(task);
      _liveUploadTask = live;
      if (_active && _recording) {
        setState(() {});
        _startLiveUploadTicker();
      }
    } on Object {
      // Recording must not be interrupted by a temporary Hub/network failure.
      // Keep retrying during this capture when the local task was persisted;
      // the normal stop flow still retains the audio and performs a full retry.
      if (_liveUploadTask != null && _active && _recording) {
        _startLiveUploadTicker();
      }
    }
  }

  void _startLiveUploadTicker() {
    _liveUploadTicker?.cancel();
    _triggerLiveUpload();
    _liveUploadTicker = Timer.periodic(const Duration(seconds: 5), (_) {
      _triggerLiveUpload();
    });
  }

  void _triggerLiveUpload() {
    if (_liveUploadInFlight != null) return;
    _liveUploadInFlight = _uploadAvailableLiveAudio().whenComplete(() {
      _liveUploadInFlight = null;
    });
  }

  Future<void> _uploadAvailableLiveAudio() async {
    final queue = _liveUploadQueue;
    final task = _liveUploadTask;
    if (queue == null || task == null || !_recording) return;
    final uploaded = await queue.uploadLiveChunks(task);
    _liveUploadTask = uploaded;
    if (_active && _recording) setState(() {});
  }

  String get _recordingStateLabel {
    if (_phase == 'uploading') return '正在安全上传录音…';
    if (_paused) return '录音已暂停';
    if (_recording) {
      final message = _liveUploadTask?.message.trim() ?? '';
      return message.isEmpty ? '正在录音，准备预上传' : message;
    }
    return '录音仅会在你点击开始后进行';
  }

  void _refreshElapsed() {
    if (!_active || _startedAt == null || !_recording) return;
    final elapsed =
        _elapsedBeforePause + DateTime.now().difference(_startedAt!);
    if (elapsed != _elapsed) {
      setState(() => _elapsed = elapsed);
    }
    if (elapsed >= _maxWavMeetingDuration) {
      unawaited(_stopAndUpload());
    }
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

  Future<void> _confirmStopAndExit() async {
    if (!_active || _stoppingOrUploading) return;
    final shouldStop = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('结束会议录音？'),
        content: const Text('结束后会先保存本次录音，再完成上传和后续处理。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('继续录音'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: Text(_finishLabel),
          ),
        ],
      ),
    );
    if (shouldStop == true && _active) {
      await _stopAndUpload();
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final busy = _phase == 'finalizing' || _phase == 'uploading';
    final preventExit = _recording || _paused || _recordingActionInProgress;
    return PopScope<Object?>(
      canPop: !preventExit,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop && (_recording || _paused)) {
          unawaited(_confirmStopAndExit());
        }
      },
      child: Scaffold(
        appBar: AppBar(
          title: const Text('会议录音'),
          actions: [
            if (_recording || _paused)
              IconButton(
                tooltip: _paused ? '继续录音' : '暂停录音',
                onPressed: _recordingActionInProgress ? null : _pauseOrResume,
                icon: Icon(_paused ? Icons.play_arrow : Icons.pause),
              ),
          ],
        ),
        body: SafeArea(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  widget.title,
                  style: Theme.of(context)
                      .textTheme
                      .headlineSmall
                      ?.copyWith(fontWeight: FontWeight.w700),
                ),
                if (widget.purpose.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: Text(
                      widget.purpose,
                      style: TextStyle(color: scheme.onSurfaceVariant),
                    ),
                  ),
                const Spacer(),
                Center(
                  child: Container(
                    width: 164,
                    height: 164,
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      color: _recording
                          ? scheme.errorContainer
                          : scheme.primaryContainer,
                    ),
                    child: Icon(
                      _recording ? Icons.mic : Icons.mic_none,
                      size: 68,
                      color: _recording ? scheme.error : scheme.primary,
                    ),
                  ),
                ),
                const SizedBox(height: 20),
                ValueListenableBuilder<List<double>>(
                  valueListenable: _waveform,
                  builder: (context, levels, _) => _MeetingWaveform(
                    levels: levels,
                    active: _recording,
                    color: _recording ? scheme.error : scheme.outlineVariant,
                  ),
                ),
                const SizedBox(height: 16),
                Center(
                  child: Text(
                    _time,
                    style: Theme.of(context).textTheme.displaySmall?.copyWith(
                      fontFeatures: const [FontFeature.tabularFigures()],
                    ),
                  ),
                ),
                const SizedBox(height: 8),
                Center(
                  child: Text(
                    _recordingStateLabel,
                    textAlign: TextAlign.center,
                    style: TextStyle(color: scheme.onSurfaceVariant),
                  ),
                ),
                if (_error != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 18),
                    child: Text(
                      _error!,
                      textAlign: TextAlign.center,
                      style: TextStyle(color: scheme.error),
                    ),
                  ),
                const Spacer(),
                if (_phase == 'ready' || _phase == 'failed')
                  FilledButton.icon(
                    onPressed: _startingRecording || busy ? null : _start,
                    icon: const Icon(Icons.fiber_manual_record),
                    label: const Text('开始录音'),
                  ),
                if (_recording || _paused)
                  LayoutBuilder(
                    builder: (context, constraints) {
                      final useTwoRows = constraints.maxWidth < 420;
                      final pauseButton = OutlinedButton.icon(
                        onPressed:
                            _recordingActionInProgress ? null : _pauseOrResume,
                        icon: Icon(_paused ? Icons.play_arrow : Icons.pause),
                        label: Text(_paused ? '继续' : '暂停'),
                      );
                      final finishButton = FilledButton.icon(
                        onPressed:
                            _recordingActionInProgress ? null : _stopAndUpload,
                        icon: const Icon(Icons.stop),
                        label: Text(
                          _finishLabel,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          softWrap: false,
                        ),
                      );
                      if (useTwoRows) {
                        return Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            pauseButton,
                            const SizedBox(height: 10),
                            finishButton,
                          ],
                        );
                      }
                      return Row(
                        children: [
                          Expanded(child: pauseButton),
                          const SizedBox(width: 12),
                          Expanded(child: finishButton),
                        ],
                      );
                    },
                  ),
                if (busy)
                  const Padding(
                    padding: EdgeInsets.only(top: 16),
                    child: LinearProgressIndicator(),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  bool get _recordingActionInProgress =>
      _startingRecording || _changingRecordingState || _stoppingOrUploading;
}

class _MeetingWaveform extends StatelessWidget {
  final List<double> levels;
  final bool active;
  final Color color;

  const _MeetingWaveform({
    required this.levels,
    required this.active,
    required this.color,
  });

  @override
  Widget build(BuildContext context) {
    return ExcludeSemantics(
      child: SizedBox(
        height: 52,
        child: LayoutBuilder(
          builder: (context, constraints) {
            final barWidth = (constraints.maxWidth / (levels.length * 1.85))
                .clamp(2.0, 5.0)
                .toDouble();
            final gap = (barWidth * .8).clamp(1.0, 3.0).toDouble();
            return Row(
              mainAxisAlignment: MainAxisAlignment.center,
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                for (final level in levels)
                  Padding(
                    padding: EdgeInsets.symmetric(horizontal: gap / 2),
                    child: AnimatedContainer(
                      duration: const Duration(milliseconds: 80),
                      curve: Curves.easeOut,
                      width: barWidth,
                      height: active ? 8 + level * 44 : 4,
                      decoration: BoxDecoration(
                        color: color.withValues(alpha: active ? .9 : .45),
                        borderRadius: BorderRadius.circular(2),
                      ),
                    ),
                  ),
              ],
            );
          },
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
  const MeetingRecordingResult({
    required this.recordingId,
    required this.duration,
    required this.localPath,
    required this.status,
    this.processMode = 'minutes',
  });
}
