import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/voice_engine.dart';
import 'package:speech_to_text/speech_to_text.dart';

LocaleName _l(String id) => LocaleName(id, id);

void main() {
  group('pickAsrLocale', () {
    test('prefers mainland zh-CN', () {
      expect(
        pickAsrLocale([_l('en_US'), _l('zh_HK'), _l('zh_CN')]),
        'zh_CN',
      );
    });

    test('falls back to any Chinese locale when zh-CN is missing', () {
      expect(pickAsrLocale([_l('en_US'), _l('zh_TW')]), 'zh_TW');
      expect(pickAsrLocale([_l('zh_SG')]), 'zh_SG');
    });

    test('returns null so the recognizer keeps its own default', () {
      expect(pickAsrLocale([_l('en_US'), _l('ja_JP')]), isNull);
      expect(pickAsrLocale(const []), isNull);
    });
  });
}
