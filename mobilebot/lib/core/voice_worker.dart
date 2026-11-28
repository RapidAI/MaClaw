/// Resident isolate for Kokoro and SenseVoice.
///
/// Each utterance used to pay `Isolate.run`: a fresh isolate plus a fresh
/// dlopen, then the isolate was thrown away. The Go engine lives in the
/// process, behind one mutex, so a second isolate cannot run a second
/// inference anyway. This isolate stays up. The UI thread only waits on a
/// port, and the next turn reuses the open library.
library;

import 'dart:async';
import 'dart:isolate';
import 'dart:typed_data';

import 'native_voice.dart';

class _VoiceCall {
  _VoiceCall(
    this.op,
    this.reply, {
    this.a = '',
    this.b = '',
    this.c = '',
    this.speed = 1,
    this.pcm,
  });

  final String op;
  final SendPort reply;
  final String a;
  final String b;
  final String c;
  final double speed;
  final Uint8List? pcm;
}

/// Entry point for [VoiceWorker]. Top-level so the spawn does not capture
/// a VoiceEngine (the engine holds an AudioPlayer, which cannot be sent).
void voiceWorkerMain(SendPort parent) {
  final inbox = ReceivePort();
  parent.send(inbox.sendPort);
  var generation = 0;
  inbox.listen((message) {
    final call = message as _VoiceCall;
    try {
      switch (call.op) {
        case 'stop':
          Isolate.exit(call.reply, const ['ret', null]);
        case 'gen':
          generation++;
          call.reply.send(['ret', generation]);
        case 'open':
          final err = NativeVoice.probe();
          call.reply.send(['ret', err.isEmpty ? null : err]);
        case 'loadTts':
          call.reply.send([
            'ret',
            NativeVoice.loadTts(call.a, call.b, call.c),
          ]);
        case 'tts':
          call.reply.send([
            'ret',
            NativeVoice.tts(call.a, speed: call.speed),
          ]);
        case 'loadAsr':
          call.reply.send(['ret', NativeVoice.loadAsr(call.a)]);
        case 'asr':
          final pcm = call.pcm;
          if (pcm == null) {
            call.reply.send(['throw', 'Bad state: empty pcm']);
            return;
          }
          call.reply.send(['ret', NativeVoice.asr(pcm)]);
        default:
          call.reply.send(['throw', 'Bad state: unknown voice op ${call.op}']);
      }
    } catch (e) {
      // toString() keeps "Bad state: …" so callers that interpolate the
      // error see the same text Isolate.run used to rethrow.
      call.reply.send(['throw', e.toString()]);
    }
  });
}

/// One background isolate for every native voice call.
class VoiceWorker {
  VoiceWorker._();

  static Future<SendPort>? _starting;
  static Isolate? _isolate;
  static int _epoch = 0;

  /// Spawn the isolate and open the library off the UI thread. A second
  /// call reuses the same isolate. Failure is ignored: the next inference
  /// reports the real error.
  static Future<void> warm() async {
    try {
      await _roundTrip('open');
    } on Object {
      // probe errors come back as a return value; a dead spawn lands here.
    }
  }

  static Future<String?> loadTts(String model, String voiceDir, String voice) async {
    final value = await _roundTrip(
      'loadTts',
      a: model,
      b: voiceDir,
      c: voice,
    );
    if (value == null) return null;
    if (value is String) return value;
    throw StateError('voice load failed');
  }

  static Future<Uint8List> tts(String text, {double speed = 1}) async {
    final value = await _roundTrip('tts', a: text, speed: speed);
    if (value is Uint8List) return value;
    throw StateError('kokoro synthesize failed');
  }

  static Future<String?> loadAsr(String model) async {
    final value = await _roundTrip('loadAsr', a: model);
    if (value == null) return null;
    if (value is String) return value;
    throw StateError('voice load failed');
  }

  static Future<String> asr(Uint8List pcm) async {
    final value = await _roundTrip('asr', pcm: pcm);
    if (value is String) return value;
    throw StateError('sensevoice transcribe failed');
  }

  /// How many `gen` calls this isolate has served. Tests use it to prove
  /// the next call did not spawn a replacement.
  static Future<int> debugGeneration() async {
    final value = await _roundTrip('gen');
    if (value is int) return value;
    throw StateError('voice worker generation missing');
  }

  /// Stop the isolate. Tests call this so a live ReceivePort does not keep
  /// the VM up. Not for use while a synthesis or transcription is in flight:
  /// killing the isolate inside the Go call would leave the voice mutex held.
  static Future<void> shutdown() async {
    final starting = _starting;
    final isolate = _isolate;
    _epoch++;
    _starting = null;
    _isolate = null;
    if (starting == null || isolate == null) {
      isolate?.kill(priority: Isolate.immediate);
      return;
    }
    final reply = ReceivePort();
    try {
      final port = await starting;
      port.send(_VoiceCall('stop', reply.sendPort));
      await reply.first.timeout(const Duration(seconds: 2));
    } on Object {
      isolate.kill(priority: Isolate.immediate);
    } finally {
      reply.close();
    }
  }

  static Future<Object?> _roundTrip(
    String op, {
    String a = '',
    String b = '',
    String c = '',
    double speed = 1,
    Uint8List? pcm,
  }) async {
    final port = await _port();
    final reply = ReceivePort();
    try {
      port.send(
        _VoiceCall(op, reply.sendPort, a: a, b: b, c: c, speed: speed, pcm: pcm),
      );
      final msg = await reply.first;
      if (msg is! List || msg.length != 2 || msg[0] is! String) {
        throw StateError('voice worker reply was not a result');
      }
      if (msg[0] == 'throw') {
        final err = msg[1];
        throw err ?? StateError('voice worker failed');
      }
      return msg[1];
    } finally {
      reply.close();
    }
  }

  static Future<SendPort> _port() {
    final started = _starting;
    if (started != null) return started;
    final gate = Completer<SendPort>();
    final epoch = _epoch;
    _starting = gate.future;
    unawaited(_spawn(gate, epoch));
    return gate.future;
  }

  static Future<void> _spawn(Completer<SendPort> gate, int epoch) async {
    final init = ReceivePort();
    final exitPort = ReceivePort();
    final startup = Completer<SendPort>();
    try {
      final isolate = await Isolate.spawn(
        voiceWorkerMain,
        init.sendPort,
        debugName: 'maclaw-voice',
        onExit: exitPort.sendPort,
      );
      init.listen((message) {
        if (message is SendPort && !startup.isCompleted) {
          startup.complete(message);
        }
      });
      // Stays open until the isolate exits. A later death drops the cached
      // port so the next call can spawn again; shutdown bumps [_epoch] first
      // so this listener cannot clear the replacement.
      exitPort.listen((_) {
        exitPort.close();
        if (!startup.isCompleted) {
          startup.completeError(StateError('voice worker exited'));
        }
        if (_epoch == epoch) {
          _starting = null;
          _isolate = null;
        }
      });
      if (_epoch != epoch) {
        isolate.kill(priority: Isolate.immediate);
        if (!gate.isCompleted) {
          gate.completeError(StateError('voice worker stopped'));
        }
        return;
      }
      _isolate = isolate;
      final send = await startup.future;
      if (_epoch != epoch) {
        isolate.kill(priority: Isolate.immediate);
        if (!gate.isCompleted) {
          gate.completeError(StateError('voice worker stopped'));
        }
        return;
      }
      if (!gate.isCompleted) gate.complete(send);
    } catch (e, st) {
      if (_epoch == epoch) {
        _starting = null;
        _isolate = null;
      }
      if (!gate.isCompleted) gate.completeError(e, st);
    } finally {
      init.close();
    }
  }
}
