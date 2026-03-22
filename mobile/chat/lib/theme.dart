import 'package:flutter/material.dart';

/// App-wide theme definitions for light and dark modes.
class AppTheme {
  static const _seed = Color(0xFF0F6FFF);

  static final light = ThemeData(
    colorScheme: ColorScheme.fromSeed(seedColor: _seed),
    useMaterial3: true,
    appBarTheme: const AppBarTheme(centerTitle: false, elevation: 0),
    listTileTheme: const ListTileThemeData(
      contentPadding: EdgeInsets.symmetric(horizontal: 16),
    ),
  );

  static final dark = ThemeData(
    colorScheme: ColorScheme.fromSeed(
      seedColor: _seed,
      brightness: Brightness.dark,
    ),
    useMaterial3: true,
    appBarTheme: const AppBarTheme(centerTitle: false, elevation: 0),
    listTileTheme: const ListTileThemeData(
      contentPadding: EdgeInsets.symmetric(horizontal: 16),
    ),
  );
}
