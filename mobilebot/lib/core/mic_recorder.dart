/// One record: the audio-capture plugin is created only once per process.
///
/// Two `AudioRecorder` instances fighting over `AudioRecord` is why a second
/// mic tap produced no level and no text while the first turn still held the
/// device. Every page gets the same instance so `stop()` on one really
/// releases the microphone for the next.
library;

import 'package:record/record.dart';

AudioRecorder? _shared;

/// The process-wide recorder. Callers must not call `dispose()` on it; the
/// app outlives every page.
AudioRecorder sharedRecorder() => _shared ??= AudioRecorder();
