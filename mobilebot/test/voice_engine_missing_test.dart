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
    // Callers must therefore check NativeVoice.supported before offering a
    // download, and say the engine is missing instead of "try again later".
    //
    // The host test runner has no Android .so, so supported is false here —
    // exactly the branch the guard takes.
    expect(NativeVoice.supported, isFalse);
  });

  test('an unsupported engine names the ABI and the open failure', () {
    // The mic used to say only "this build has no local engine". A phone
    // running an armeabi-v7a APK (the library ships arm64-v8a and x86_64
    // only) now reads which ABI was asked for and why the open failed.
    final reason = NativeVoice.unsupportedReason;
    expect(reason, isNotEmpty);
    expect(reason, contains('ABI'));
    expect(reason, isNot(contains('\n')));
  });
}
