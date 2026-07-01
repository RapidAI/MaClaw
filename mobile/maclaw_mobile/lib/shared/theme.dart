import 'package:flutter/material.dart';

ThemeData buildMaClawTheme(Brightness brightness) {
  final dark = brightness == Brightness.dark;
  final scheme = ColorScheme.fromSeed(
    seedColor: const Color(0xFF2D6B9F),
    brightness: brightness,
  ).copyWith(
    primary: dark ? const Color(0xFF8BC7F4) : const Color(0xFF1E5F8F),
    secondary: dark ? const Color(0xFF8FD4C2) : const Color(0xFF317C6E),
    surface: dark ? const Color(0xFF111820) : const Color(0xFFF7F9FB),
    error: dark ? const Color(0xFFFFB4AB) : const Color(0xFFBA1A1A),
  );

  return ThemeData(
    useMaterial3: true,
    brightness: brightness,
    colorScheme: scheme,
    scaffoldBackgroundColor:
        dark ? const Color(0xFF0C1117) : const Color(0xFFF3F6F8),
    fontFamily: 'Roboto',
    cardTheme: CardTheme(
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: BorderSide(
          color: dark ? const Color(0xFF253241) : const Color(0xFFD8E1EA),
        ),
      ),
      color: dark ? const Color(0xFF111820) : Colors.white,
      margin: EdgeInsets.zero,
    ),
    inputDecorationTheme: InputDecorationTheme(
      border: OutlineInputBorder(borderRadius: BorderRadius.circular(10)),
      filled: true,
    ),
  );
}

