import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/locale.dart';
import 'package:mobilebot/l10n/gen/app_localizations.dart';
import 'package:mobilebot/l10n/gen/app_localizations_en.dart';
import 'package:mobilebot/l10n/gen/app_localizations_mn.dart';
import 'package:mobilebot/l10n/gen/app_localizations_zh.dart';

void main() {
  group('resolveAppLocale', () {
    test('manual choice wins over the OS locale', () {
      const en = Locale('en');
      expect(
        resolveAppLocale(en, manual: AppLanguage.zhHant),
        const Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant'),
      );
      expect(
        resolveAppLocale(const Locale('zh'), manual: AppLanguage.english),
        const Locale('en'),
      );
      expect(
        resolveAppLocale(en, manual: AppLanguage.mongolian),
        const Locale('mn'),
      );
      expect(
        resolveAppLocale(en, manual: AppLanguage.system),
        const Locale('en'),
      );
    });

    test('OS locale maps onto the four supported locales', () {
      const system = AppLanguage.system;
      expect(
        resolveAppLocale(const Locale('zh'), manual: system),
        const Locale('zh'),
      );
      expect(
        resolveAppLocale(const Locale('zh', 'TW'), manual: system),
        const Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant'),
      );
      expect(
        resolveAppLocale(const Locale('zh', 'HK'), manual: system),
        const Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant'),
      );
      expect(
        resolveAppLocale(
          const Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant'),
          manual: system,
        ),
        const Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant'),
      );
      expect(
        resolveAppLocale(const Locale('en', 'US'), manual: system),
        const Locale('en'),
      );
      expect(
        resolveAppLocale(const Locale('mn'), manual: system),
        const Locale('mn'),
      );
    });

    test('unsupported OS languages fall back to 简体中文', () {
      const system = AppLanguage.system;
      expect(
        resolveAppLocale(const Locale('fr'), manual: system),
        const Locale('zh'),
      );
      expect(
        resolveAppLocale(const Locale('ja'), manual: system),
        const Locale('zh'),
      );
      expect(resolveAppLocale(null, manual: system), const Locale('zh'));
    });
  });

  group('generated localizations', () {
    test('lookup resolves one class per supported locale', () {
      expect(
        lookupAppLocalizations(const Locale('zh')),
        isA<AppLocalizationsZh>(),
      );
      expect(
        lookupAppLocalizations(
          const Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant'),
        ),
        isA<AppLocalizationsZhHant>(),
      );
      expect(
        lookupAppLocalizations(const Locale('en')),
        isA<AppLocalizationsEn>(),
      );
      expect(
        lookupAppLocalizations(const Locale('mn')),
        isA<AppLocalizationsMn>(),
      );
    });

    test('supportedLocales matches the four ARB locales', () {
      final locales = AppLocalizations.supportedLocales
          .map((l) => '${l.languageCode}_${l.scriptCode ?? ''}')
          .toSet();
      expect(locales, {'zh_', 'zh_Hant', 'en_', 'mn_'});
    });
  });
}
