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

  test('app preferences default to system theme; unknown language is English UI',
      () {
    final preferences = AppPreferences.fromJson({
      'theme_mode': 'unknown',
      'language': 'fr_FR',
    });

    expect(preferences.themeMode, ThemeMode.system);
    // Non-Chinese languages use English UI.
    expect(preferences.language, appLanguageEnglish);
    expect(appLanguageLabel(preferences.language), 'English');
    expect(themeModeWireValue(preferences.themeMode), 'system');
  });

  test('system language preference is preserved', () {
    final preferences = AppPreferences.fromJson({
      'language': 'system',
    });
    expect(preferences.language, appLanguageSystem);
    expect(appLanguageLabel(preferences.language), contains('System'));
  });
}
