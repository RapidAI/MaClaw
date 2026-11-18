import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/app_update.dart';
import 'package:mobilebot/features/account/update_dialog.dart';
import 'package:mobilebot/l10n/gen/app_localizations.dart';

/// The signature-check branch of the update sheet. The native side of
/// `maclawgo/update` cannot run on a widget-test host, so the updater is
/// faked at the Dart boundary: false = real mismatch, null = undeterminable.
class _FakeUpdater extends AppUpdater {
  _FakeUpdater(this.compatible);

  final bool? compatible;

  @override
  Future<UpdateCheckResult> check(String currentVersion) async =>
      const UpdateCheckResult(
        update: AppUpdate(
          tag: '8.2.0.999',
          version: 'V8.2.0.999',
          downloadUrls: ['https://hub.example.com/MaClawGo.apk'],
        ),
      );

  @override
  Future<File> download(
    AppUpdate update, {
    void Function(double progress)? onProgress,
  }) async {
    // No real I/O — the sheet only hands the path around; the real
    // updater's sha/zip checks are covered by app_update_test.dart. Real
    // file writes would never complete inside the test's fake async zone.
    return File(
      '${Directory.systemTemp.path}'
      '${Platform.pathSeparator}fake-update-${compatible ?? "null"}.apk',
    );
  }

  @override
  Future<bool?> apkSignatureCompatible(File apk) async => compatible;
}

Widget _host(AppUpdater updater) => MaterialApp(
  // The sheet reads strings via context.S — resolve to the zh bundle.
  locale: const Locale('zh'),
  localizationsDelegates: AppLocalizations.localizationsDelegates,
  supportedLocales: AppLocalizations.supportedLocales,
  home: Scaffold(
    body: Builder(
      builder: (context) => Center(
        child: TextButton(
          onPressed: () => showUpdateSheet(
            context,
            prechecked: const AppUpdate(
              tag: '8.2.0.999',
              version: 'V8.2.0.999',
              downloadUrls: ['https://hub.example.com/MaClawGo.apk'],
            ),
            updater: updater,
          ),
          child: const Text('打开更新面板'),
        ),
      ),
    ),
  ),
);

void main() {
  testWidgets('a real signature mismatch blocks the install with guidance', (
    tester,
  ) async {
    await tester.pumpWidget(_host(_FakeUpdater(false)));
    await tester.tap(find.text('打开更新面板'));
    await tester.pumpAndSettle();
    expect(find.text('发现新版本 V8.2.0.999'), findsOneWidget);

    await tester.tap(find.text('下载并安装'));
    await tester.pumpAndSettle();

    expect(find.textContaining('签名不一致'), findsOneWidget);
    expect(find.textContaining('卸载当前版本'), findsOneWidget);
    // The mismatch path must stop before the installer — the sheet offers
    // the browser download page instead (a key change cannot be fixed in
    // place; the person uninstalls and installs from the release page).
    expect(find.text('在浏览器中打开下载页'), findsOneWidget);
    expect(find.text('重试'), findsOneWidget);
  });
}
