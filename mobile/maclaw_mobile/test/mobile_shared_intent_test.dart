import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/shared_intents/mobile_shared_intent.dart';

void main() {
  test('classifies shared links for assistant lookup', () {
    final intent = MobileSharedIntent.fromMedia(
      value: 'https://example.com/report',
      typeName: 'url',
    );

    expect(intent.kind, MobileSharedIntentKind.link);
    expect(intent.opensAssistant, isTrue);
    expect(intent.opensDocuments, isFalse);
    expect(intent.assistantPrompt, contains('保留来源引用'));
  });

  test('classifies shared images for document import', () {
    final intent = MobileSharedIntent.fromMedia(
      value: '/tmp/capture.png',
      typeName: 'image',
      mimeType: 'image/png',
    );

    expect(intent.kind, MobileSharedIntentKind.image);
    expect(intent.opensDocuments, isTrue);
    expect(intent.opensAssistant, isFalse);
  });
}
