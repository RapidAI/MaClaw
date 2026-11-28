import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/voice_worker.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  tearDown(() async {
    await VoiceWorker.shutdown();
  });

  test('voice worker stays up across calls', () async {
    final first = await VoiceWorker.debugGeneration();
    final second = await VoiceWorker.debugGeneration();
    expect(first, 1);
    expect(second, 2);

    await VoiceWorker.shutdown();
    final restarted = await VoiceWorker.debugGeneration();
    expect(restarted, 1);
  });

  test('two calls share one isolate', () async {
    final both = await Future.wait([
      VoiceWorker.debugGeneration(),
      VoiceWorker.debugGeneration(),
    ]);
    expect(both.toSet(), {1, 2});
  });
}
