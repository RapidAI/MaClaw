import 'package:flutter/material.dart';

/// Supported UI languages: Chinese UI vs English UI for all other languages.
const appLanguageSystem = 'system';
const appLanguageChinese = 'zh_CN';
const appLanguageEnglish = 'en_US';

/// Resolve preference wire value to a concrete UI language code (`zh_CN` / `en_US`).
///
/// Rules:
/// - explicit English → English
/// - explicit Chinese → Chinese
/// - system / unknown → Chinese if platform language is `zh*`, otherwise English
String resolveAppUiLanguage({
  required String preferenceLanguage,
  Locale? platformLocale,
}) {
  final pref = preferenceLanguage.trim();
  if (pref == appLanguageEnglish || pref.toLowerCase().startsWith('en')) {
    return appLanguageEnglish;
  }
  if (pref == appLanguageChinese ||
      pref.toLowerCase() == 'zh' ||
      pref.toLowerCase().startsWith('zh_') ||
      pref.toLowerCase().startsWith('zh-')) {
    return appLanguageChinese;
  }
  // system or any other language preference → platform, then en/zh rule
  final platform = platformLocale ??
      WidgetsBinding.instance.platformDispatcher.locale;
  final code = platform.languageCode.toLowerCase();
  if (code == 'zh') {
    return appLanguageChinese;
  }
  return appLanguageEnglish;
}

Locale resolveAppLocale({
  required String preferenceLanguage,
  Locale? platformLocale,
}) {
  final ui = resolveAppUiLanguage(
    preferenceLanguage: preferenceLanguage,
    platformLocale: platformLocale,
  );
  return ui == appLanguageEnglish
      ? const Locale('en', 'US')
      : const Locale('zh', 'CN');
}

bool isChineseUiLanguage(String language) {
  return resolveAppUiLanguage(preferenceLanguage: language) ==
      appLanguageChinese;
}

/// Normalize stored preference: system | zh_CN | en_US
String appUiLanguagePreferenceFromWire(String? value) {
  final raw = (value ?? '').trim();
  if (raw.isEmpty) return appLanguageSystem;
  final lower = raw.toLowerCase();
  if (lower == 'system' || lower == 'auto') return appLanguageSystem;
  if (lower.startsWith('en')) return appLanguageEnglish;
  if (lower == 'zh' ||
      lower.startsWith('zh_') ||
      lower.startsWith('zh-') ||
      raw == appLanguageChinese) {
    return appLanguageChinese;
  }
  // Unknown language codes → English UI (still store as English preference).
  return appLanguageEnglish;
}
