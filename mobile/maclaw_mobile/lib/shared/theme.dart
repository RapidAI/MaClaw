import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// Steel-blue workbench palette for MaClaw Mobile.
///
/// Keeps navigation, selection, and emphasis on restrained blue-gray;
/// success stays low-saturation green; red is reserved for true danger.
abstract final class MaClawColors {
  static const Color brand = Color(0xFF2D6B9F);
  static const Color brandDeep = Color(0xFF1E5F8F);
  static const Color brandBright = Color(0xFF8BC7F4);

  static const Color success = Color(0xFF317C6E);
  static const Color successBright = Color(0xFF8FD4C2);

  static const Color lightScaffold = Color(0xFFF3F6F8);
  static const Color lightSurface = Color(0xFFF7F9FB);
  static const Color lightCard = Color(0xFFFFFFFF);
  static const Color lightBorder = Color(0xFFD8E1EA);
  static const Color lightInk = Color(0xFF15202B);
  static const Color lightMuted = Color(0xFF5B6B7C);

  // Dark hierarchy: scaffold (lowest) → surface → card (elevated panel).
  // Muted text kept ≥ ~4.5:1 on scaffold for WCAG AA body readability.
  static const Color darkScaffold = Color(0xFF0A0E14);
  static const Color darkSurface = Color(0xFF10171F);
  static const Color darkCard = Color(0xFF161E28);
  static const Color darkElevated = Color(0xFF1B2430);
  static const Color darkBorder = Color(0xFF2A3645);
  static const Color darkInk = Color(0xFFEDF2F7);
  static const Color darkMuted = Color(0xFFB4C0CD);
  static const Color darkInputFill = Color(0xFF0D131A);

  static const double radiusSm = 8;
  static const double radiusMd = 12;
  static const double radiusLg = 16;
  static const double spaceXs = 4;
  static const double spaceSm = 8;
  static const double spaceMd = 12;
  static const double spaceLg = 16;
  static const double spaceXl = 20;
  static const double spaceXxl = 28;
}

ThemeData buildMaClawTheme(Brightness brightness) {
  final dark = brightness == Brightness.dark;
  final scheme = ColorScheme.fromSeed(
    seedColor: MaClawColors.brand,
    brightness: brightness,
  ).copyWith(
    primary: dark ? MaClawColors.brandBright : MaClawColors.brandDeep,
    onPrimary: dark ? const Color(0xFF00344F) : Colors.white,
    primaryContainer:
        dark ? const Color(0xFF17344A) : const Color(0xFFD6EAF8),
    onPrimaryContainer:
        dark ? const Color(0xFFD8ECFA) : const Color(0xFF0B3A5C),
    secondary: dark ? MaClawColors.successBright : MaClawColors.success,
    onSecondary: dark ? const Color(0xFF00382F) : Colors.white,
    secondaryContainer:
        dark ? const Color(0xFF163830) : const Color(0xFFD5EFE8),
    onSecondaryContainer:
        dark ? const Color(0xFFC6EDE2) : const Color(0xFF0F3F36),
    tertiary: dark ? const Color(0xFFD4B88A) : const Color(0xFF7A5C2E),
    onTertiary: dark ? const Color(0xFF3A2A0E) : Colors.white,
    tertiaryContainer:
        dark ? const Color(0xFF3A2F1C) : const Color(0xFFF3E6CF),
    onTertiaryContainer:
        dark ? const Color(0xFFF0DFC0) : const Color(0xFF3F2E12),
    surface: dark ? MaClawColors.darkSurface : MaClawColors.lightSurface,
    onSurface: dark ? MaClawColors.darkInk : MaClawColors.lightInk,
    onSurfaceVariant: dark ? MaClawColors.darkMuted : MaClawColors.lightMuted,
    surfaceContainerLowest:
        dark ? MaClawColors.darkScaffold : MaClawColors.lightScaffold,
    surfaceContainerLow:
        dark ? const Color(0xFF121920) : const Color(0xFFF0F4F7),
    surfaceContainer:
        dark ? MaClawColors.darkElevated : const Color(0xFFEAF0F5),
    surfaceContainerHigh:
        dark ? const Color(0xFF202A36) : const Color(0xFFE4EBF1),
    surfaceContainerHighest:
        dark ? const Color(0xFF253140) : const Color(0xFFDCE4EC),
    outline: dark ? const Color(0xFF455668) : const Color(0xFFB7C4D1),
    outlineVariant:
        dark ? MaClawColors.darkBorder : MaClawColors.lightBorder,
    error: dark ? const Color(0xFFFFB4AB) : const Color(0xFFBA1A1A),
    onError: dark ? const Color(0xFF690005) : Colors.white,
    errorContainer:
        dark ? const Color(0xFF4A1814) : const Color(0xFFFDECEC),
    onErrorContainer:
        dark ? const Color(0xFFFFDAD6) : const Color(0xFF5F1410),
  );

  final scaffold =
      dark ? MaClawColors.darkScaffold : MaClawColors.lightScaffold;
  final cardColor = dark ? MaClawColors.darkCard : MaClawColors.lightCard;
  final borderColor =
      dark ? MaClawColors.darkBorder : MaClawColors.lightBorder;

  final textTheme = _buildTextTheme(brightness, scheme);

  return ThemeData(
    useMaterial3: true,
    brightness: brightness,
    colorScheme: scheme,
    scaffoldBackgroundColor: scaffold,
    canvasColor: scaffold,
    fontFamily: 'Roboto',
    visualDensity: VisualDensity.standard,
    materialTapTargetSize: MaterialTapTargetSize.padded,
    splashFactory: InkRipple.splashFactory,
    textTheme: textTheme,
    primaryTextTheme: textTheme,
    dividerTheme: DividerThemeData(
      color: borderColor,
      thickness: 1,
      space: 1,
    ),
    cardTheme: CardThemeData(
      elevation: 0,
      shadowColor: Colors.transparent,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(MaClawColors.radiusMd),
        side: BorderSide(color: borderColor),
      ),
      color: cardColor,
      margin: EdgeInsets.zero,
      clipBehavior: Clip.antiAlias,
    ),
    appBarTheme: AppBarTheme(
      elevation: 0,
      scrolledUnderElevation: 0,
      centerTitle: false,
      backgroundColor: scaffold,
      foregroundColor: scheme.onSurface,
      surfaceTintColor: Colors.transparent,
      titleTextStyle: textTheme.titleLarge?.copyWith(
        fontWeight: FontWeight.w600,
        color: scheme.onSurface,
      ),
      systemOverlayStyle:
          dark ? SystemUiOverlayStyle.light : SystemUiOverlayStyle.dark,
    ),
    navigationBarTheme: NavigationBarThemeData(
      height: 68,
      elevation: 0,
      backgroundColor: cardColor,
      surfaceTintColor: Colors.transparent,
      shadowColor: Colors.transparent,
      indicatorColor: scheme.primaryContainer,
      labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
      iconTheme: WidgetStateProperty.resolveWith((states) {
        final selected = states.contains(WidgetState.selected);
        return IconThemeData(
          size: 22,
          color: selected ? scheme.primary : scheme.onSurfaceVariant,
        );
      }),
      labelTextStyle: WidgetStateProperty.resolveWith((states) {
        final selected = states.contains(WidgetState.selected);
        return textTheme.labelMedium?.copyWith(
          fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
          color: selected ? scheme.primary : scheme.onSurfaceVariant,
          letterSpacing: 0.1,
        );
      }),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: dark ? MaClawColors.darkInputFill : Colors.white,
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: BorderSide(color: borderColor),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: BorderSide(color: borderColor),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: BorderSide(color: scheme.primary, width: 1.5),
      ),
      errorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: BorderSide(color: scheme.error),
      ),
      focusedErrorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: BorderSide(color: scheme.error, width: 1.5),
      ),
      labelStyle: textTheme.bodyMedium?.copyWith(
        color: scheme.onSurfaceVariant,
      ),
      hintStyle: textTheme.bodyMedium?.copyWith(
        color: scheme.onSurfaceVariant.withValues(alpha: 0.85),
      ),
      helperStyle: textTheme.bodySmall?.copyWith(
        color: scheme.onSurfaceVariant,
      ),
      prefixIconColor: scheme.onSurfaceVariant,
      suffixIconColor: scheme.onSurfaceVariant,
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        elevation: 0,
        minimumSize: const Size(48, 44),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(10),
        ),
        textStyle: textTheme.labelLarge?.copyWith(fontWeight: FontWeight.w600),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: const Size(48, 44),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        side: BorderSide(color: borderColor),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(10),
        ),
        foregroundColor: scheme.onSurface,
        textStyle: textTheme.labelLarge?.copyWith(fontWeight: FontWeight.w600),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        minimumSize: const Size(40, 40),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        foregroundColor: scheme.primary,
        textStyle: textTheme.labelLarge?.copyWith(fontWeight: FontWeight.w600),
      ),
    ),
    iconButtonTheme: IconButtonThemeData(
      style: IconButton.styleFrom(
        minimumSize: const Size(44, 44),
        foregroundColor: scheme.onSurfaceVariant,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(10),
        ),
      ),
    ),
    chipTheme: ChipThemeData(
      backgroundColor:
          dark ? MaClawColors.darkElevated : const Color(0xFFEEF3F7),
      selectedColor: scheme.primaryContainer,
      disabledColor: scheme.surfaceContainerHighest,
      secondarySelectedColor: scheme.primaryContainer,
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      labelPadding: const EdgeInsets.symmetric(horizontal: 4),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(20),
        side: BorderSide(color: borderColor),
      ),
      side: BorderSide(color: borderColor),
      labelStyle: textTheme.labelMedium?.copyWith(
        color: scheme.onSurface,
        fontWeight: FontWeight.w500,
      ),
      secondaryLabelStyle: textTheme.labelMedium?.copyWith(
        color: scheme.onPrimaryContainer,
        fontWeight: FontWeight.w600,
      ),
      iconTheme: IconThemeData(size: 16, color: scheme.primary),
      showCheckmark: false,
      elevation: 0,
      pressElevation: 0,
    ),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      elevation: 2,
      backgroundColor: dark ? const Color(0xFF243140) : const Color(0xFF1B2834),
      contentTextStyle: textTheme.bodyMedium?.copyWith(
        color: Colors.white,
        height: 1.35,
      ),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(10),
      ),
      insetPadding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
      actionTextColor: MaClawColors.brandBright,
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: cardColor,
      surfaceTintColor: Colors.transparent,
      elevation: 2,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(MaClawColors.radiusLg),
      ),
      titleTextStyle: textTheme.titleLarge?.copyWith(
        fontWeight: FontWeight.w600,
        color: scheme.onSurface,
      ),
      contentTextStyle: textTheme.bodyMedium?.copyWith(
        color: scheme.onSurfaceVariant,
        height: 1.45,
      ),
    ),
    bottomSheetTheme: BottomSheetThemeData(
      backgroundColor: cardColor,
      surfaceTintColor: Colors.transparent,
      elevation: 2,
      showDragHandle: true,
      dragHandleColor: scheme.outlineVariant,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(
          top: Radius.circular(MaClawColors.radiusLg),
        ),
      ),
      clipBehavior: Clip.antiAlias,
    ),
    listTileTheme: ListTileThemeData(
      iconColor: scheme.onSurfaceVariant,
      contentPadding: const EdgeInsets.symmetric(horizontal: 4),
      dense: false,
      titleTextStyle: textTheme.titleSmall?.copyWith(
        color: scheme.onSurface,
        fontWeight: FontWeight.w600,
      ),
      subtitleTextStyle: textTheme.bodySmall?.copyWith(
        color: scheme.onSurfaceVariant,
        height: 1.35,
      ),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(10),
      ),
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(
      color: scheme.primary,
      linearTrackColor: scheme.surfaceContainerHighest,
      circularTrackColor: scheme.surfaceContainerHighest,
    ),
    floatingActionButtonTheme: FloatingActionButtonThemeData(
      backgroundColor: scheme.primary,
      foregroundColor: scheme.onPrimary,
      elevation: 1,
      focusElevation: 2,
      hoverElevation: 2,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(14),
      ),
    ),
    badgeTheme: BadgeThemeData(
      backgroundColor: scheme.primary,
      textColor: scheme.onPrimary,
      smallSize: 8,
      largeSize: 16,
    ),
    tooltipTheme: TooltipThemeData(
      waitDuration: const Duration(milliseconds: 400),
      decoration: BoxDecoration(
        color: dark ? const Color(0xFF243140) : const Color(0xFF1B2834),
        borderRadius: BorderRadius.circular(8),
      ),
      textStyle: textTheme.bodySmall?.copyWith(color: Colors.white),
    ),
    pageTransitionsTheme: const PageTransitionsTheme(
      builders: {
        TargetPlatform.android: FadeUpwardsPageTransitionsBuilder(),
        TargetPlatform.iOS: CupertinoPageTransitionsBuilder(),
        TargetPlatform.windows: FadeUpwardsPageTransitionsBuilder(),
        TargetPlatform.macOS: CupertinoPageTransitionsBuilder(),
        TargetPlatform.linux: FadeUpwardsPageTransitionsBuilder(),
      },
    ),
  );
}

TextTheme _buildTextTheme(Brightness brightness, ColorScheme scheme) {
  final base = brightness == Brightness.dark
      ? Typography.material2021(platform: TargetPlatform.android).white
      : Typography.material2021(platform: TargetPlatform.android).black;

  TextStyle tune(
    TextStyle? style, {
    FontWeight weight = FontWeight.w400,
    double? letterSpacing,
    double height = 1.35,
  }) {
    return (style ?? const TextStyle()).copyWith(
      fontWeight: weight,
      letterSpacing: letterSpacing,
      height: height,
      color: scheme.onSurface,
    );
  }

  return base.copyWith(
    displaySmall: tune(
      base.displaySmall,
      weight: FontWeight.w600,
      letterSpacing: -0.02,
      height: 1.2,
    ),
    headlineLarge: tune(
      base.headlineLarge,
      weight: FontWeight.w600,
      letterSpacing: -0.02,
      height: 1.22,
    ),
    headlineMedium: tune(
      base.headlineMedium,
      weight: FontWeight.w600,
      letterSpacing: -0.015,
      height: 1.25,
    ),
    headlineSmall: tune(
      base.headlineSmall,
      weight: FontWeight.w600,
      letterSpacing: -0.01,
      height: 1.28,
    ),
    titleLarge: tune(
      base.titleLarge,
      weight: FontWeight.w600,
      height: 1.3,
    ),
    titleMedium: tune(
      base.titleMedium,
      weight: FontWeight.w600,
      height: 1.3,
    ),
    titleSmall: tune(
      base.titleSmall,
      weight: FontWeight.w600,
      height: 1.3,
    ),
    bodyLarge: tune(base.bodyLarge, height: 1.45),
    bodyMedium: tune(base.bodyMedium, height: 1.45),
    bodySmall: tune(
      base.bodySmall,
      height: 1.4,
    ).copyWith(color: scheme.onSurfaceVariant),
    labelLarge: tune(base.labelLarge, weight: FontWeight.w600, height: 1.2),
    labelMedium: tune(base.labelMedium, weight: FontWeight.w500, height: 1.2),
    labelSmall: tune(
      base.labelSmall,
      weight: FontWeight.w500,
      height: 1.2,
    ).copyWith(color: scheme.onSurfaceVariant),
  );
}
