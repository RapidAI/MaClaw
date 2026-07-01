import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../storage/mobile_local_store.dart';
export 'app_preferences_model.dart';
import 'app_preferences_model.dart';

final appPreferencesProvider =
    AsyncNotifierProvider<AppPreferencesController, AppPreferences>(
  AppPreferencesController.new,
);

class AppPreferencesController extends AsyncNotifier<AppPreferences> {
  @override
  Future<AppPreferences> build() {
    return ref.watch(mobileLocalStoreProvider).loadAppPreferences();
  }

  Future<void> setThemeMode(ThemeMode mode) async {
    final current = state.valueOrNull ?? await future;
    await _save(current.copyWith(themeMode: mode));
  }

  Future<void> setLanguage(String language) async {
    final current = state.valueOrNull ?? await future;
    await _save(current.copyWith(language: appLanguageFromWire(language)));
  }

  Future<void> _save(AppPreferences preferences) async {
    await ref.read(mobileLocalStoreProvider).saveAppPreferences(preferences);
    state = AsyncData(preferences);
  }
}
