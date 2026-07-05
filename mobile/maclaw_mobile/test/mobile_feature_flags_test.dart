import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/shared_intents/mobile_shared_intent.dart';
import 'package:maclaw_mobile/shared/app_shell.dart';

const _assistantTab = 'AI助手';
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
      _assistantTab,
      _employeesTab,
      _accountTab,
    ]);
    expect(tabs.map((tab) => tab.label), isNot(contains('查信息')));
  });

  test('mobile app presents the GUI-like AI assistant as the first tab', () {
    final tabs = mobileAppTabsForFeatures(defaultMobileFeatures);

    expect(tabs.first.path, '/assistant');
    expect(tabs.first.label, _assistantTab);
    expect(tabs.map((tab) => tab.label), isNot(contains('查信息')));
    expect(mobileInitialPathForFeatures(defaultMobileFeatures), '/assistant');
  });

  test(
      'mobile app keeps assistant and account when optional features are disabled',
      () {
    const features = MobileFeatures(
      search: false,
      documents: false,
      localSsh: false,
      digitalEmployees: false,
      pushNotifications: false,
    );

    final tabs = mobileAppTabsForFeatures(features);

    expect(tabs.map((tab) => tab.path), ['/assistant', '/account']);
    expect(tabs.map((tab) => tab.label), [_assistantTab, _accountTab]);
    expect(mobileInitialPathForFeatures(features), '/assistant');
  });

  test(
      'mobile initial route keeps assistant first even when search flag is off',
      () {
    const features = MobileFeatures(
      search: false,
      documents: true,
      localSsh: true,
      digitalEmployees: true,
      pushNotifications: false,
    );

    final tabs = mobileAppTabsForFeatures(features);

    expect(mobileInitialPathForFeatures(features), '/assistant');
    expect(tabs.map((tab) => tab.label), [
      _assistantTab,
      _documentsTab,
      _remoteTab,
      _employeesTab,
      _accountTab,
    ]);
    expect(mobilePathEnabledForFeatures('/documents', features), isTrue);
    expect(mobilePathEnabledForFeatures('/assistant', features), isTrue);
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

    final intent = MobileSharedIntent(
      id: 'share-file',
      kind: MobileSharedIntentKind.file,
      value: '/tmp/report.pdf',
      receivedAt: DateTime.utc(2026, 7, 1),
    );
    final target = sharedIntentTargetPath(intent, features);

    expect(target, '/documents');
    expect(sharedIntentCanBeConsumedAtTarget(intent, target), isTrue);
  });

  test('shared file intents skip assistant when documents are disabled', () {
    const features = MobileFeatures(
      search: true,
      documents: false,
      localSsh: true,
      digitalEmployees: true,
      pushNotifications: false,
    );

    final intent = MobileSharedIntent(
      id: 'share-file-docs-disabled',
      kind: MobileSharedIntentKind.image,
      value: '/tmp/screenshot.png',
      receivedAt: DateTime.utc(2026, 7, 1),
    );
    final target = sharedIntentTargetPath(intent, features);

    expect(target, '/servers');
    expect(sharedIntentCanBeConsumedAtTarget(intent, target), isFalse);
  });

  test('shared intents avoid disabled document and assistant online tabs', () {
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

  test('shared link intents are consumed only by the assistant tab', () {
    final intent = MobileSharedIntent(
      id: 'share-link',
      kind: MobileSharedIntentKind.link,
      value: 'https://example.com/incident',
      receivedAt: DateTime.utc(2026, 7, 1),
    );

    expect(
      sharedIntentCanBeConsumedAtTarget(intent, '/assistant'),
      isTrue,
    );
    expect(
      sharedIntentCanBeConsumedAtTarget(intent, '/documents'),
      isFalse,
    );
  });
}
