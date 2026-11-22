import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/native_voice.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('the local voice engine is a native library, not a downloadable asset',
      () {
    // SupportsVoiceInput is true on Android, so the mic is shown. The local
    // engine behind it comes from libmaclaw_voice.so in the APK: when that
    // library is absent (a build that skipped tool/build_voice.cmd), every
    // local model download would still leave asrReady() false forever.
    // Callers must therefore check NativeVoice.usable before offering a
    // download, and say the engine is missing instead of "try again later".
    //
    // The host test runner is not Android, so usable is false here —
    // exactly the branch the guard takes.
    expect(NativeVoice.usable, isFalse);
  });

  test('probe reports unusable on a platform without the library', () {
    // usable short-circuits on non-Android, but probe() itself must also
    // refuse: unsupportedReason and any future caller rely on probe() never
    // claiming a library that cannot exist on this platform.
    expect(NativeVoice.probe(), isNotEmpty);
    expect(NativeVoice.usable, isFalse);
  });

  test('resetProbe keeps a missing engine missing', () {
    // The retry path (pull-to-refresh, reopening the voice sheet) must not
    // turn a genuinely absent library into a usable one.
    NativeVoice.resetProbe();
    expect(NativeVoice.usable, isFalse);
    expect(NativeVoice.probe(), isNotEmpty);
  });

  test('an unsupported engine names the ABI and the failure', () {
    // The mic used to say only "this build has no local engine". A phone
    // running an armeabi-v7a APK (the library ships arm64-v8a and x86_64
    // only) now reads which ABI was asked for and why the open failed —
    // and on platforms with no bundled library at all, a plain reason
    // instead of a dlopen error for a file that was never shipped.
    final reason = NativeVoice.unsupportedReason;
    expect(reason, isNotEmpty);
    expect(reason, contains('ABI'));
    expect(reason, isNot(contains('\n')));
    expect(reason, isNot(contains('dlopen')));
  });
}
