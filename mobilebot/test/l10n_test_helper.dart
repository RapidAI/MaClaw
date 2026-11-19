import 'package:flutter/material.dart';
import 'package:mobilebot/l10n/gen/app_localizations.dart';

/// Wraps [child] in a MaterialApp pinned to 简体中文 — the zh template mirrors
/// the original hardcoded strings, so text finders keep passing unchanged.
Widget l10nApp({required Widget child}) => MaterialApp(
  locale: const Locale('zh'),
  localizationsDelegates: AppLocalizations.localizationsDelegates,
  supportedLocales: AppLocalizations.supportedLocales,
  home: child,
);
