import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/locale.dart';
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
    });

    test('empty Android locale list still names a concrete id', () {
      expect(pickAsrLocale(const []), 'zh_CN');
      expect(
        pickAsrLocale(const [], fallbackLocaleId: speechLocaleId(const Locale('en'))),
        'en_US',
      );
      expect(
        pickAsrLocale(
          const [],
          fallbackLocaleId: speechLocaleId(
            const Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant'),
          ),
        ),
        'zh_TW',
      );
    });
  });
}
