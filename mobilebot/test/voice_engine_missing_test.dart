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
}
