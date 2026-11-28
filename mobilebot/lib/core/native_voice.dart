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
/// synthesize/transcribe are heavyweight and must run on the resident voice
/// worker, not the UI isolate. The Go side serializes on its own mutex.
/// Each isolate looks the symbols up for itself; the worker keeps that
/// lookup, the UI isolate only probes.
class NativeVoice {
  NativeVoice._();

  static DynamicLibrary? _lib;
  static bool _openTried = false;

  /// Why [supported] is false on Android, for the settings sheet and the mic
  /// toast. Empty on a supported ABI or on a platform without the library.
  static String lastOpenError = '';

  /// What [probe] found: the library opened and every export the engine calls
  /// is present. A non-empty value starting with `missing export` means the
  /// file loaded but was built without one of them (an old or partial build),
  /// which an open alone cannot see.
  static String _probeError = '';
  static bool _probed = false;

  /// Symbols the Dart side resolves. A library missing any of them opens fine
  /// and then throws on first use, which used to read as "no local engine".
  static const _requiredExports = [
    'VoiceLastError',
    'VoiceFree',
    'VoiceLoadTTS',
    'VoiceUnloadTTS',
    'VoiceTTS',
    'VoiceLoadASR',
    'VoiceUnloadASR',
    'VoiceASR',
  ];

  /// ABIs without a bundled .so (armeabi-v7a) throw ArgumentError on open —
  /// probe once and report unsupported instead of crashing in the isolate.
  static DynamicLibrary? _tryOpen() {
    if (_lib != null) return _lib;
    if (_openTried) return null;
    _openTried = true;
    try {
      return _lib = DynamicLibrary.open('libmaclaw_voice.so');
    } on Object catch (e) {
      lastOpenError = '$e';
      return null;
    }
  }

  /// Resolves every export once, on whatever thread calls first. Idempotent,
  /// so a failed launch can be retried (`probe()` again) without a restart.
  /// Non-Android builds have no bundled library — report that instead of
  /// paying for a dlopen that can only fail and pollute [unsupportedReason].
  static String probe() {
    if (_probed) return _probeError;
    _probed = true;
    if (!Platform.isAndroid || isOhos) {
      _probeError = 'no bundled voice library on this platform';
      return _probeError;
    }
    final lib = _tryOpen();
    if (lib == null) {
      final message = lastOpenError.trim();
      // The c-shared build also links libc++_shared.so, which a plain APK
      // does not carry unless the build script copied it in. A .so installed
      // later into the app's lib dir makes the next open work, so retry once
      // and say plainly what is missing when it still fails.
      final missing = RegExp(r'library "([^"]+)" not found').firstMatch(message);
      if (missing != null) {
        _openTried = false;
        _lib = null;
        if (_tryOpen() == null) {
          _probeError =
              'dlopen failed: ${missing.group(1)} not found '
              '(bundled runtime library missing from the APK)';
          return _probeError;
        }
      } else {
        _probeError = message.isEmpty ? 'library not found' : message;
        return _probeError;
      }
    }
    final opened = _lib;
    if (opened == null) {
      _probeError = 'library not found';
      return _probeError;
    }
    for (final name in _requiredExports) {
      try {
        opened.lookup(name);
      } on Object catch (e) {
        _probeError = 'missing export $name: $e';
        return _probeError;
      }
    }
    _probeError = '';
    lastOpenError = '';
    return '';
  }

  /// Drops a failed probe so the next [probe] opens again. Currently only
  /// tests call this: `autoEnsure` no longer resets the probe on every call
  /// because the .so lives inside the APK and cannot be swapped without a
  /// process restart. A future explicit retry path (e.g. a replaced .so on
  /// a sideloaded build) can call this first.
  static void resetProbe() {
    _probed = false;
    _probeError = '';
    _openTried = false;
    _lib = null;
    lastOpenError = '';
  }

  /// Whether the engine can actually run: platform, open, and every export.
  static bool get usable => Platform.isAndroid && !isOhos && probe().isEmpty;

  /// One line naming the ABI and the failure. The mic used to answer "this
  /// build has no local engine" with no cause, so a sideloaded armeabi-v7a
  /// APK (no .so for that ABI) looked like a broken build.
  static String get unsupportedReason {
    probe();
    final abi = Platform.isAndroid ? _androidAbi() : Platform.operatingSystem;
    // One line: the reason is painted in a small note, not a log.
    final err = _probeError
        .trim()
        .replaceAll(RegExp(r'\s+'), ' ');
    return err.isEmpty ? 'ABI $abi' : 'ABI $abi: $err';
  }

  /// `ro.product.cpu.abi`-level guess from the Dart VM: the APK ships
  /// arm64-v8a and x86_64 only, so anything else has no library to open.
  static String _androidAbi() {
    if (Platform.version.contains('arm64')) return 'arm64-v8a';
    if (Platform.version.contains('arm')) return 'armeabi-v7a';
    if (Platform.version.contains('x64')) return 'x86_64';
    return 'unknown';
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
