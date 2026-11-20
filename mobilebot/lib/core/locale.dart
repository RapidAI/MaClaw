import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../l10n/gen/app_localizations.dart';

/// The app speaks 简体中文 / 繁體中文 / English / Монгол. [AppLanguage.system]
/// follows the OS; anything else pins one locale (persisted in
/// shared_preferences, loaded in main()).
enum AppLanguage { system, zhHans, zhHant, english, mongolian }

/// Loaded from disk in main() and injected as an override so the first frame
/// already carries the saved choice.
final initialLanguageProvider = Provider<AppLanguage>(
  (_) => AppLanguage.system,
);

class AppLanguageController extends Notifier<AppLanguage> {
  static const _prefsKey = 'mobilebot.locale';

  @override
  AppLanguage build() => ref.watch(initialLanguageProvider);

  Future<void> set(AppLanguage language) async {
    state = language;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_prefsKey, language.name);
  }
}

final appLanguageProvider =
    NotifierProvider<AppLanguageController, AppLanguage>(
      AppLanguageController.new,
    );

/// Null means "follow the OS" — MaterialApp then resolves via
/// [resolveAppLocale].
/// `language_COUNTRY` id Android recognizers accept. `Locale.toLanguageTag()`
/// (`zh-Hans-CN`) is rejected by some OEM `listen` intents.
String speechLocaleId(Locale locale) {
  final language = locale.languageCode.toLowerCase();
  final country = (locale.countryCode ?? '').toUpperCase();
  final script = (locale.scriptCode ?? '').toLowerCase();
  switch (language) {
    case 'en':
      return 'en_US';
    case 'mn':
      return 'mn_MN';
    case 'zh':
      final hant =
          script == 'hant' ||
          country == 'TW' ||
          country == 'HK' ||
          country == 'MO';
      return hant ? 'zh_TW' : 'zh_CN';
    default:
      return 'zh_CN';
  }
}

Locale? localeFor(AppLanguage language) => switch (language) {
  AppLanguage.system => null,
  AppLanguage.zhHans => const Locale('zh'),
  AppLanguage.zhHant => const Locale.fromSubtags(
    languageCode: 'zh',
    scriptCode: 'Hant',
  ),
  AppLanguage.english => const Locale('en'),
  AppLanguage.mongolian => const Locale('mn'),
};

/// Manual overrides win; otherwise map the OS locale onto the four supported
/// ones. Any OTHER system language falls back to 简体中文 so existing users
/// never see a surprise language.
Locale resolveAppLocale(Locale? deviceLocale, {required AppLanguage manual}) {
  final override = localeFor(manual);
  if (override != null) return override;
  final locale = deviceLocale ?? const Locale('zh');
  switch (locale.languageCode) {
    case 'en':
      return const Locale('en');
    case 'mn':
      return const Locale('mn');
    case 'zh':
      final scriptHant =
          locale.scriptCode == 'Hant' ||
          locale.countryCode == 'TW' ||
          locale.countryCode == 'HK' ||
          locale.countryCode == 'MO';
      return scriptHant
          ? const Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant')
          : const Locale('zh');
    default:
      return const Locale('zh');
  }
}

/// Option labels are locale-native names — identical in every translation.
String languageDisplayName(AppLanguage language) => switch (language) {
  AppLanguage.system => '跟随系统',
  AppLanguage.zhHans => '简体中文',
  AppLanguage.zhHant => '繁體中文',
  AppLanguage.english => 'English',
  AppLanguage.mongolian => 'Монгол хэл',
};

/// Shorthand so call sites read `context.S.someKey` instead of the long
/// `AppLocalizations.of(context).someKey`.
extension L10n on BuildContext {
  AppLocalizations get S => AppLocalizations.of(this);
}

/// Locale snapshot kept current by the app shell (`MaclawGoApp`'s
/// `localeResolutionCallback`). Non-widget code — controllers, API and error
/// mappers, pure helpers like `plainTextForSpeech` — has no BuildContext, so
/// its localized fallback text resolves through [sharedL10n]. Defaults to zh
/// (the product's base language), which keeps pure-function unit tests and
/// pre-first-frame behavior byte-identical to the old hardcoded zh strings.
Locale _sharedLocale = const Locale('zh');

void updateSharedL10nLocale(Locale locale) => _sharedLocale = locale;

AppLocalizations get sharedL10n => lookupAppLocalizations(_sharedLocale);
