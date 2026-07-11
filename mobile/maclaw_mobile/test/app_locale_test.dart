import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/settings/app_preferences_model.dart';
import 'package:maclaw_mobile/l10n/app_locale.dart';
import 'package:maclaw_mobile/l10n/app_strings.dart';

void main() {
  test('system preference follows platform Chinese vs English UI rule', () {
    expect(
      resolveAppUiLanguage(
        preferenceLanguage: appLanguageSystem,
        platformLocale: const Locale('zh', 'CN'),
      ),
      appLanguageChinese,
    );
    expect(
      resolveAppUiLanguage(
        preferenceLanguage: appLanguageSystem,
        platformLocale: const Locale('en', 'US'),
      ),
      appLanguageEnglish,
    );
    expect(
      resolveAppUiLanguage(
        preferenceLanguage: appLanguageSystem,
        platformLocale: const Locale('ja', 'JP'),
      ),
      appLanguageEnglish,
    );
  });

  test('explicit preferences force Chinese or English UI', () {
    expect(
      resolveAppUiLanguage(
        preferenceLanguage: appLanguageChinese,
        platformLocale: const Locale('en', 'US'),
      ),
      appLanguageChinese,
    );
    expect(
      resolveAppUiLanguage(
        preferenceLanguage: appLanguageEnglish,
        platformLocale: const Locale('zh', 'CN'),
      ),
      appLanguageEnglish,
    );
  });

  test('AppStrings switches tab labels by language', () {
    final zh = AppStrings.forLanguage(appLanguageChinese);
    final en = AppStrings.forLanguage(appLanguageEnglish);
    expect(zh.tabAssistant, 'AI助手');
    expect(en.tabAssistant, 'Assistant');
    expect(zh.tabDocuments, '文档');
    expect(en.tabDocuments, 'Docs');
    expect(zh.tabTasks, '后台');
    expect(en.tabTasks, 'Tasks');
    expect(zh.tabEmployees, '数字员工');
    expect(en.tabEmployees, 'Employees');
    expect(zh.tabAccount, '我的');
    expect(en.tabAccount, 'Me');
  });

  test('wire preference normalizes system/zh/en', () {
    expect(appUiLanguagePreferenceFromWire(null), appLanguageSystem);
    expect(appUiLanguagePreferenceFromWire('auto'), appLanguageSystem);
    expect(appUiLanguagePreferenceFromWire('zh-CN'), appLanguageChinese);
    expect(appUiLanguagePreferenceFromWire('en-GB'), appLanguageEnglish);
    expect(appUiLanguagePreferenceFromWire('de_DE'), appLanguageEnglish);
  });
}
