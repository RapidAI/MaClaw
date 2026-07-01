import 'package:flutter/material.dart';

const appLanguageChinese = 'zh_CN';
const appLanguageEnglish = 'en_US';

class AppPreferences {
  final ThemeMode themeMode;
  final String language;

  const AppPreferences({
    this.themeMode = ThemeMode.system,
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
  return switch (appLanguageFromWire(language)) {
    appLanguageEnglish => 'English',
    _ => '简体中文',
  };
}

String appLanguageFromWire(String? value) {
  return switch ((value ?? '').trim()) {
    appLanguageEnglish => appLanguageEnglish,
    _ => appLanguageChinese,
  };
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
