import 'package:flutter/material.dart';

/// Latin/digit typeface bundled with the app (assets/fonts, see pubspec).
/// CJK glyphs fall back to the platform font automatically, so Chinese text
/// keeps the system rendering while numbers/latin/branding get Inter.
const appFontFamily = 'Inter';

/// Spacing scale — reach for these instead of inventing literals.
abstract final class AppSpacing {
  static const double xs = 4;
  static const double sm = 8;
  static const double md = 12;
  static const double lg = 16;
  static const double xl = 20;
  static const double xxl = 24;
}

/// Corner-radius scale used across cards, sheets and containers.
abstract final class AppRadius {
  static const double sm = 10;
  static const double md = 14;
  static const double lg = 18;
  static const double xl = 24;
  static const BorderRadius pill = BorderRadius.all(Radius.circular(999));
}

/// Central Material 3 theme — one source for light & dark so every screen
/// shares the same surfaces, radii and component styling.
class AppTheme {
  static const seedColor = Color(0xFF5B8DEF);

  /// The mobile bot reads like a mobile AI workspace, not a settings app:
  /// a warm off-white canvas, white sheets floating on it, and one deep
  /// indigo for every action. Both lightness levels share the same hue
  /// family so a screenshot of either is recognisably the same product.
  static const canvasLight = Color(0xFFF6F7FB);
  static const canvasDark = Color(0xFF0E1116);
  static const sheetDark = Color(0xFF171B22);

  static ThemeData light() => _base(Brightness.light);
  static ThemeData dark() => _base(Brightness.dark);

  static ThemeData _base(Brightness brightness) {
    final isDark = brightness == Brightness.dark;
    final scheme = ColorScheme.fromSeed(
      seedColor: seedColor,
      brightness: brightness,
    ).copyWith(
      primary: isDark ? const Color(0xFF9DB7F5) : const Color(0xFF2F5BD8),
      onPrimary: isDark ? const Color(0xFF0C1630) : Colors.white,
      surface: isDark ? canvasDark : canvasLight,
    );
    // Light cards are white sheets on the tinted canvas; dark cards lift a
    // step above the near-black canvas instead.
    final cardColor = isDark ? sheetDark : Colors.white;
    final outline = scheme.outlineVariant;
    // Borders are hairlines: the sheet's own contrast separates cards, so a
    // heavy outline would only add noise.
    final hairline = outline.withValues(alpha: isDark ? 0.5 : 0.9);

    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      scaffoldBackgroundColor: isDark ? canvasDark : canvasLight,
      fontFamily: appFontFamily,
      visualDensity: VisualDensity.adaptivePlatformDensity,
      textTheme: _textTheme(scheme),
      pageTransitionsTheme: const PageTransitionsTheme(
        builders: {
          TargetPlatform.android: ZoomPageTransitionsBuilder(),
          TargetPlatform.iOS: CupertinoPageTransitionsBuilder(),
          TargetPlatform.macOS: CupertinoPageTransitionsBuilder(),
          TargetPlatform.windows: ZoomPageTransitionsBuilder(),
        },
      ),
      appBarTheme: AppBarTheme(
        centerTitle: false,
        // The canvas colour, not surface: a translucent surface tint would
        // grey the header once content scrolls under it.
        backgroundColor: isDark ? sheetDark : Colors.white,
        surfaceTintColor: isDark ? sheetDark : Colors.white,
        scrolledUnderElevation: 0,
        elevation: 0,
        titleSpacing: AppSpacing.lg,
        titleTextStyle: TextStyle(
          fontSize: 20,
          fontWeight: FontWeight.w700,
          letterSpacing: -0.4,
          color: scheme.onSurface,
          fontFamily: appFontFamily,
        ),
      ),
      cardTheme: CardThemeData(
        elevation: 0,
        margin: const EdgeInsets.symmetric(vertical: 5),
        clipBehavior: Clip.antiAlias,
        color: cardColor,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.xl),
          side: BorderSide(color: hairline),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          minimumSize: const Size(64, 48),
          padding: const EdgeInsets.symmetric(horizontal: AppSpacing.xl),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(AppRadius.md),
          ),
          textStyle: const TextStyle(
            fontSize: 15,
            fontWeight: FontWeight.w600,
            fontFamily: appFontFamily,
          ),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          minimumSize: const Size(64, 48),
          padding: const EdgeInsets.symmetric(horizontal: AppSpacing.lg),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(AppRadius.md),
          ),
          side: BorderSide(color: outline.withValues(alpha: 0.8)),
          textStyle: const TextStyle(
            fontSize: 15,
            fontWeight: FontWeight.w600,
            fontFamily: appFontFamily,
          ),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          textStyle: const TextStyle(
            fontSize: 14.5,
            fontWeight: FontWeight.w600,
            fontFamily: appFontFamily,
          ),
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: isDark
            ? scheme.surfaceContainerHigh
            : scheme.surfaceContainerLow,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.lg,
          vertical: 14,
        ),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadius.md),
          borderSide: BorderSide(color: outline.withValues(alpha: 0.6)),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadius.md),
          borderSide: BorderSide(color: outline.withValues(alpha: 0.6)),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadius.md),
          borderSide: BorderSide(color: scheme.primary, width: 1.8),
        ),
        errorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadius.md),
          borderSide: BorderSide(color: scheme.error),
        ),
        focusedErrorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadius.md),
          borderSide: BorderSide(color: scheme.error, width: 1.8),
        ),
      ),
      listTileTheme: ListTileThemeData(
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.md),
        ),
      ),
      chipTheme: ChipThemeData(
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        side: BorderSide(color: outline.withValues(alpha: 0.6)),
        labelStyle: TextStyle(
          fontSize: 12.5,
          fontWeight: FontWeight.w500,
          fontFamily: appFontFamily,
        ),
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.md - 2,
          vertical: 2,
        ),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: cardColor,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.xl),
        ),
      ),
      bottomNavigationBarTheme: BottomNavigationBarThemeData(
        backgroundColor: isDark ? sheetDark : Colors.white,
      ),
      floatingActionButtonTheme: FloatingActionButtonThemeData(
        backgroundColor: scheme.primary,
        foregroundColor: scheme.onPrimary,
        elevation: 2,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.lg),
        ),
        extendedTextStyle: const TextStyle(
          fontSize: 15,
          fontWeight: FontWeight.w600,
          fontFamily: appFontFamily,
        ),
      ),
      bottomSheetTheme: const BottomSheetThemeData(
        showDragHandle: true,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(28)),
        ),
      ),
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.md),
        ),
      ),
      tabBarTheme: TabBarThemeData(
        labelColor: scheme.primary,
        unselectedLabelColor: scheme.onSurfaceVariant,
        labelStyle: const TextStyle(
          fontSize: 14.5,
          fontWeight: FontWeight.w600,
          fontFamily: appFontFamily,
        ),
        unselectedLabelStyle: const TextStyle(
          fontSize: 14.5,
          fontWeight: FontWeight.w500,
          fontFamily: appFontFamily,
        ),
        dividerColor: outline.withValues(alpha: 0.4),
        indicatorSize: TabBarIndicatorSize.label,
      ),
      segmentedButtonTheme: SegmentedButtonThemeData(
        style: SegmentedButton.styleFrom(
          textStyle: const TextStyle(
            fontSize: 13.5,
            fontWeight: FontWeight.w600,
            fontFamily: appFontFamily,
          ),
          side: BorderSide(color: outline.withValues(alpha: 0.6)),
        ),
      ),
      navigationBarTheme: NavigationBarThemeData(
        height: 68,
        backgroundColor: isDark ? sheetDark : Colors.white,
        surfaceTintColor: isDark ? sheetDark : Colors.white,
        elevation: 0,
        labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
        labelTextStyle: WidgetStateProperty.resolveWith((states) {
          final selected = states.contains(WidgetState.selected);
          return TextStyle(
            fontSize: 11,
            height: 1.2,
            fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
            color: selected ? scheme.primary : scheme.onSurfaceVariant,
            fontFamily: appFontFamily,
          );
        }),
        iconTheme: WidgetStateProperty.resolveWith((states) {
          final selected = states.contains(WidgetState.selected);
          return IconThemeData(
            size: 23,
            color: selected ? scheme.primary : scheme.onSurfaceVariant,
          );
        }),
      ),
      dividerTheme: DividerThemeData(
        color: outline.withValues(alpha: 0.5),
        thickness: 1,
        space: 1,
      ),
      tooltipTheme: TooltipThemeData(
        decoration: BoxDecoration(
          color: scheme.inverseSurface,
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
      ),
      progressIndicatorTheme: const ProgressIndicatorThemeData(
        linearMinHeight: 6,
      ),
    );
  }

  /// The app's type ramp: tighter tracking on titles, comfortable line
  /// heights on body, and small caps-like labels for meta text.
  static TextTheme _textTheme(ColorScheme scheme) {
    final onSurface = scheme.onSurface;
    final onVariant = scheme.onSurfaceVariant;
    return TextTheme(
      displaySmall: TextStyle(
        fontSize: 32,
        fontWeight: FontWeight.w700,
        letterSpacing: -0.8,
        height: 1.2,
        color: onSurface,
        fontFamily: appFontFamily,
      ),
      headlineSmall: TextStyle(
        fontSize: 24,
        fontWeight: FontWeight.w700,
        letterSpacing: -0.4,
        color: onSurface,
        fontFamily: appFontFamily,
      ),
      titleLarge: TextStyle(
        fontSize: 20,
        fontWeight: FontWeight.w600,
        letterSpacing: -0.3,
        color: onSurface,
        fontFamily: appFontFamily,
      ),
      titleMedium: TextStyle(
        fontSize: 16,
        fontWeight: FontWeight.w600,
        letterSpacing: -0.1,
        height: 1.3,
        color: onSurface,
        fontFamily: appFontFamily,
      ),
      titleSmall: TextStyle(
        fontSize: 14,
        fontWeight: FontWeight.w600,
        height: 1.3,
        color: onSurface,
        fontFamily: appFontFamily,
      ),
      bodyLarge: TextStyle(
        fontSize: 15,
        height: 1.45,
        color: onSurface,
        fontFamily: appFontFamily,
      ),
      bodyMedium: TextStyle(
        fontSize: 14,
        height: 1.45,
        color: onSurface,
        fontFamily: appFontFamily,
      ),
      bodySmall: TextStyle(
        fontSize: 12.5,
        height: 1.35,
        color: onVariant,
        fontFamily: appFontFamily,
      ),
      labelLarge: TextStyle(
        fontSize: 14,
        fontWeight: FontWeight.w600,
        letterSpacing: 0.1,
        color: onSurface,
        fontFamily: appFontFamily,
      ),
      labelMedium: TextStyle(
        fontSize: 11.5,
        fontWeight: FontWeight.w500,
        letterSpacing: 0.3,
        color: onVariant,
        fontFamily: appFontFamily,
      ),
      labelSmall: TextStyle(
        fontSize: 10.5,
        fontWeight: FontWeight.w500,
        letterSpacing: 0.3,
        color: onVariant,
        fontFamily: appFontFamily,
      ),
    );
  }
}
