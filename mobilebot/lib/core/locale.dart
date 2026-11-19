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
final initialLanguageProvider = Provider<AppLanguage>((_) => AppLanguage.system);

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
Locale? localeFor(AppLanguage language) => switch (language) {
  AppLanguage.system => null,
  AppLanguage.zhHans => const Locale('zh'),
  AppLanguage.zhHant => const Locale.fromSubtags(languageCode: 'zh', scriptCode: 'Hant'),
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
