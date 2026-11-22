import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/core/account_store.dart';
import 'package:mobilebot/core/providers.dart';
import 'package:mobilebot/features/account/about_sheet.dart';
import 'package:mobilebot/l10n/gen/app_localizations.dart';

/// The 我的 → 关于 sheet. PackageInfo has no platform channel on a widget-test
/// host, so the version falls back to the dash; everything else is checked
/// against the overridden session so the assertions stay deterministic.
class _FakeSession extends SessionController {
  _FakeSession(this._state);

  final SessionState _state;

  @override
  SessionState build() => _state;
}

Widget _host({SessionState state = const SessionState()}) => ProviderScope(
  overrides: [
    sessionProvider.overrideWith(() => _FakeSession(state)),
  ],
  child: MaterialApp(
    locale: const Locale('zh'),
    localizationsDelegates: AppLocalizations.localizationsDelegates,
    supportedLocales: AppLocalizations.supportedLocales,
    home: Scaffold(
      body: Builder(
        builder: (context) => Center(
          child: TextButton(
            onPressed: () => showAboutSheet(context),
            child: const Text('open'),
          ),
        ),
      ),
    ),
  ),
);

SessionState _signedIn() => SessionState(
  loaded: true,
  active: Account(
    key: 'k1',
    label: 'me@example.com',
    hubUrl: 'https://tenant-a.example.test/api',
    token: 't',
    addedAt: DateTime(2026, 1, 1),
  ),
);

void main() {
  testWidgets('opens from a settings-style tap and lists the app facts', (
    tester,
  ) async {
    await tester.pumpWidget(_host(state: _signedIn()));
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(find.text('关于 MaclawGo'), findsOneWidget);
    expect(find.text('版本'), findsOneWidget);
    expect(find.text('运行平台'), findsOneWidget);
    expect(find.text('服务节点'), findsOneWidget);
    // Only the host is shown, not the tenant path of the hub URL.
    expect(find.text('tenant-a.example.test'), findsOneWidget);
    expect(find.text('当前账号'), findsOneWidget);
    expect(find.text('me@example.com'), findsOneWidget);
    expect(find.text('开源许可'), findsOneWidget);
  });

  testWidgets('without an account the node and account rows say so', (
    tester,
  ) async {
    await tester.pumpWidget(_host());
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(find.text('未登录'), findsNWidgets(2));
  });
}
