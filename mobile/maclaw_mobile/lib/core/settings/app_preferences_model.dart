import 'package:flutter/material.dart';

import '../../l10n/app_locale.dart';

export '../../l10n/app_locale.dart'
    show appLanguageSystem, appLanguageChinese, appLanguageEnglish;

class AppPreferences {
  final ThemeMode themeMode;
  /// Stored preference: `system` | `zh_CN` | `en_US`.
  final String language;

  const AppPreferences({
    this.themeMode = ThemeMode.system,
    // Default Chinese UI for first run; users can pick System / English.
    this.language = appLanguageChinese,
  });

  factory AppPreferences.fromJson(Map<String, dynamic> json) {
    return AppPreferences(
      themeMode: themeModeFromWire(json['theme_mode'] as String?),
      language: appLanguageFromWire(json['language'] as String?),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'theme_mode': themeModeWireValue(themeMode),
      'language': language,
    };
  }

  AppPreferences copyWith({
    ThemeMode? themeMode,
    String? language,
  }) {
    return AppPreferences(
      themeMode: themeMode ?? this.themeMode,
      language: language ?? this.language,
    );
  }
}

String appLanguageLabel(String language) {
  return switch (appUiLanguagePreferenceFromWire(language)) {
    appLanguageSystem => '跟随系统 / System',
    appLanguageEnglish => 'English',
    _ => '简体中文',
  };
}

/// Preference wire value (system / zh_CN / en_US). Use [resolveAppUiLanguage]
/// for effective Chinese vs English UI.
String appLanguageFromWire(String? value) {
  return appUiLanguagePreferenceFromWire(value);
}

ThemeMode themeModeFromWire(String? value) {
  return switch ((value ?? '').trim()) {
    'light' => ThemeMode.light,
    'dark' => ThemeMode.dark,
    _ => ThemeMode.system,
  };
}

String themeModeWireValue(ThemeMode mode) {
  return switch (mode) {
    ThemeMode.light => 'light',
    ThemeMode.dark => 'dark',
    ThemeMode.system => 'system',
  };
}
