import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/shared_intents/mobile_shared_intent.dart';
import 'package:maclaw_mobile/shared/app_shell.dart';

const _lookupTab = '\u67e5\u4fe1\u606f';
const _documentsTab = '\u6587\u6863';
const _remoteTab = '\u8fdc\u7a0b';
const _employeesTab = '\u5458\u5de5';
const _accountTab = '\u6211\u7684';

void main() {
  test('mobile app tabs follow official bootstrap feature flags', () {
    const features = MobileFeatures(
      search: true,
      documents: false,
      localSsh: false,
      digitalEmployees: true,
      pushNotifications: false,
    );

    final tabs = mobileAppTabsForFeatures(features);

    expect(tabs.map((tab) => tab.path), [
      '/assistant',
      '/employees',
      '/account',
    ]);
    expect(tabs.map((tab) => tab.label), [
      _lookupTab,
      _employeesTab,
      _accountTab,
    ]);
  });

  test('mobile app keeps account tab when all optional features are disabled',
      () {
    const features = MobileFeatures(
      search: false,
      documents: false,
      localSsh: false,
      digitalEmployees: false,
      pushNotifications: false,
    );

    final tabs = mobileAppTabsForFeatures(features);

    expect(tabs.map((tab) => tab.path), ['/account']);
    expect(tabs.map((tab) => tab.label), [_accountTab]);
    expect(mobileInitialPathForFeatures(features), '/account');
  });

  test('mobile initial route starts at first enabled emergency feature', () {
    const features = MobileFeatures(
      search: false,
      documents: true,
      localSsh: true,
      digitalEmployees: true,
      pushNotifications: false,
    );

    final tabs = mobileAppTabsForFeatures(features);

    expect(mobileInitialPathForFeatures(features), '/documents');
    expect(tabs.map((tab) => tab.label), [
      _documentsTab,
      _remoteTab,
      _employeesTab,
      _accountTab,
    ]);
    expect(mobilePathEnabledForFeatures('/documents', features), isTrue);
    expect(mobilePathEnabledForFeatures('/assistant', features), isFalse);
  });

  test('shared file intents prefer documents when document feature is enabled',
      () {
    const features = MobileFeatures(
      search: true,
      documents: true,
      localSsh: true,
      digitalEmployees: false,
      pushNotifications: false,
    );

    final target = sharedIntentTargetPath(
      MobileSharedIntent(
        id: 'share-file',
        kind: MobileSharedIntentKind.file,
        value: '/tmp/report.pdf',
        receivedAt: DateTime.utc(2026, 7, 1),
      ),
      features,
    );

    expect(target, '/documents');
  });

  test('shared file intents skip assistant when documents are disabled', () {
    const features = MobileFeatures(
      search: true,
      documents: false,
      localSsh: true,
      digitalEmployees: true,
      pushNotifications: false,
    );

    final target = sharedIntentTargetPath(
      MobileSharedIntent(
        id: 'share-file-docs-disabled',
        kind: MobileSharedIntentKind.image,
        value: '/tmp/screenshot.png',
        receivedAt: DateTime.utc(2026, 7, 1),
      ),
      features,
    );

    expect(target, '/servers');
  });

  test('shared intents avoid disabled document and search tabs', () {
    const features = MobileFeatures(
      search: false,
      documents: false,
      localSsh: true,
      digitalEmployees: false,
      pushNotifications: false,
    );

    final target = sharedIntentTargetPath(
      MobileSharedIntent(
        id: 'share-1',
        kind: MobileSharedIntentKind.file,
        value: '/tmp/report.pdf',
        receivedAt: DateTime.utc(2026, 7, 1),
      ),
      features,
    );

    expect(target, '/servers');
  });
}
