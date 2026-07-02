import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/settings/app_preferences_model.dart';

void main() {
  test('app preferences round trip theme and language', () {
    const preferences = AppPreferences(
      themeMode: ThemeMode.dark,
      language: appLanguageEnglish,
    );

    final restored = AppPreferences.fromJson(preferences.toJson());

    expect(restored.themeMode, ThemeMode.dark);
    expect(restored.language, appLanguageEnglish);
    expect(appLanguageLabel(restored.language), 'English');
  });

  test('app preferences default to system theme and Chinese speech', () {
    final preferences = AppPreferences.fromJson({
      'theme_mode': 'unknown',
      'language': 'fr_FR',
    });

    expect(preferences.themeMode, ThemeMode.system);
    expect(preferences.language, appLanguageChinese);
    expect(appLanguageLabel(preferences.language), '简体中文');
    expect(themeModeWireValue(preferences.themeMode), 'system');
  });
}
