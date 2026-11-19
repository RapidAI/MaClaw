import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'app.dart';
import 'core/locale.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  // Phone-first portrait app; the bot desktop screen temporarily switches to
  // landscape (see bot_desktop_screen.dart) and restores this on exit.
  SystemChrome.setPreferredOrientations([DeviceOrientation.portraitUp]);
  // Saved manual language must be known before the first frame — inject it
  // as the initial value of the language provider.
  final prefs = await SharedPreferences.getInstance();
  final saved = prefs.getString('mobilebot.locale');
  final initial = AppLanguage.values.asNameMap()[saved] ?? AppLanguage.system;
  runApp(
    ProviderScope(
      overrides: [initialLanguageProvider.overrideWithValue(initial)],
      child: const MaclawGoApp(),
    ),
  );
}
