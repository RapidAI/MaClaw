/// dart:ffi bindings for `libmaclaw_voice.so` — the desktop's own Kokoro
/// TTS and SenseVoice ASR compiled from `corelib/tts` + `corelib/asr` with
/// `go build -buildmode=c-shared` (see `native/voiceffi/main.go` and
/// `tool/build_voice.cmd`). Inference runs on the phone via the same NEON
/// arm64 kernels the desktop uses.
library;

import 'dart:ffi';
import 'dart:io';
import 'dart:typed_data';

import 'package:ffi/ffi.dart';

import 'platform.dart';

typedef _StrFnC = Pointer<Char> Function();
typedef _LoadTtsC =
    Pointer<Char> Function(Pointer<Char>, Pointer<Char>, Pointer<Char>);
typedef _LoadOneC = Pointer<Char> Function(Pointer<Char>);
typedef _TtsC = Pointer<Char> Function(Pointer<Char>, Double, Pointer<Int32>);
typedef _AsrC = Pointer<Char> Function(Pointer<Char>, Int32);
typedef _FreeC = Void Function(Pointer<Void>);
typedef _VoidC = Void Function();

typedef _StrFnD = Pointer<Char> Function();
typedef _LoadTtsD =
    Pointer<Char> Function(Pointer<Char>, Pointer<Char>, Pointer<Char>);
typedef _LoadOneD = Pointer<Char> Function(Pointer<Char>);
typedef _TtsD = Pointer<Char> Function(Pointer<Char>, double, Pointer<Int32>);
typedef _AsrD = Pointer<Char> Function(Pointer<Char>, int);
typedef _FreeD = void Function(Pointer<Void>);
typedef _VoidD = void Function();

/// Lazily opened handle to the voice library. Every call is synchronous —
/// synthesize/transcribe are heavyweight and must run off the UI isolate
/// (`Isolate.run`). The Go side serializes on its own mutex, so concurrent
/// isolates are safe.
class NativeVoice {
  NativeVoice._();

  static bool get supported =>
      Platform.isAndroid && !isOhos && _tryOpen() != null;

  static DynamicLibrary? _lib;
  static bool _openTried = false;

  /// ABIs without a bundled .so (armeabi-v7a) throw ArgumentError on open —
  /// probe once and report unsupported instead of crashing in the isolate.
  static DynamicLibrary? _tryOpen() {
    if (_lib != null) return _lib;
    if (_openTried) return null;
    _openTried = true;
    try {
      return _lib = DynamicLibrary.open('libmaclaw_voice.so');
    } on Object {
      return null;
    }
  }

  static DynamicLibrary get _dylib => _tryOpen()!;

  static String? _goString(Pointer<Char> p) {
    if (p == nullptr) return null;
    final s = p.cast<Utf8>().toDartString();
    _dylib.lookupFunction<_FreeC, _FreeD>('VoiceFree')(p.cast());
    return s;
  }

  /// `VoiceLoadTTS(model, voiceDir, voiceId)` — nil pointer means OK.
  static String? loadTts(String model, String voiceDir, String voiceId) {
    final fn = _dylib.lookupFunction<_LoadTtsC, _LoadTtsD>('VoiceLoadTTS');
    final m = model.toNativeUtf8();
    final d = voiceDir.toNativeUtf8();
    final v = voiceId.toNativeUtf8();
    try {
      return _goString(fn(m.cast<Char>(), d.cast<Char>(), v.cast<Char>()));
    } finally {
      malloc.free(m);
      malloc.free(d);
      malloc.free(v);
    }
  }

  /// `VoiceTTS(text, speed, &len)` → WAV bytes, or throws with VoiceLastError.
  static Uint8List tts(String text, {double speed = 1.0}) {
    final fn = _dylib.lookupFunction<_TtsC, _TtsD>('VoiceTTS');
    final t = text.toNativeUtf8();
    final len = calloc<Int32>();
    try {
      final out = fn(t.cast<Char>(), speed, len);
      if (out == nullptr) {
        throw StateError(lastError() ?? 'kokoro synthesize failed');
      }
      final n = len.value;
      final bytes = Uint8List.fromList(
        out.cast<Uint8>().asTypedList(n).toList(),
      );
      _dylib.lookupFunction<_FreeC, _FreeD>('VoiceFree')(out.cast());
      return bytes;
    } finally {
      malloc.free(t);
      calloc.free(len);
    }
  }

  /// `VoiceLoadASR(model)` — nil pointer means OK.
  static String? loadAsr(String model) {
    final fn = _dylib.lookupFunction<_LoadOneC, _LoadOneD>('VoiceLoadASR');
    final m = model.toNativeUtf8();
    try {
      return _goString(fn(m.cast<Char>()));
    } finally {
      malloc.free(m);
    }
  }

  /// `VoiceASR(pcm16@16k mono, len)` → transcript, or throws.
  static String asr(Uint8List pcm16) {
    final fn = _dylib.lookupFunction<_AsrC, _AsrD>('VoiceASR');
    final buf = malloc<Char>(pcm16.length);
    buf.cast<Uint8>().asTypedList(pcm16.length).setAll(0, pcm16);
    try {
      final out = fn(buf, pcm16.length);
      if (out == nullptr) {
        throw StateError(lastError() ?? 'sensevoice transcribe failed');
      }
      final s = out.cast<Utf8>().toDartString();
      _dylib.lookupFunction<_FreeC, _FreeD>('VoiceFree')(out.cast());
      return s;
    } finally {
      malloc.free(buf);
    }
  }

  static String? lastError() =>
      _goString(_dylib.lookupFunction<_StrFnC, _StrFnD>('VoiceLastError')());

  static void unloadTts() =>
      _dylib.lookupFunction<_VoidC, _VoidD>('VoiceUnloadTTS')();
  static void unloadAsr() =>
      _dylib.lookupFunction<_VoidC, _VoidD>('VoiceUnloadASR')();
}
