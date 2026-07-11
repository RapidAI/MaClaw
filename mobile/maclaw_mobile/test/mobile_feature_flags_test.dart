import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/api/mobile_credits.dart';
import 'package:maclaw_mobile/core/settings/app_preferences_model.dart';
import 'package:maclaw_mobile/core/shared_intents/mobile_shared_intent.dart';
import 'package:maclaw_mobile/l10n/app_strings.dart';
import 'package:maclaw_mobile/shared/app_shell.dart';

const _assistantTab = 'AI助手';
const _twinTab = '数字分身';
const _documentsTab = '文档';
const _tasksTab = '后台';
const _employeesTab = '数字员工';
const _accountTab = '我的';

void main() {
  test(
      'mobile app tabs follow hybrid IA: assistant, documents, tasks, employees, account',
      () {
    final tabs = mobileAppTabsForFeatures(defaultMobileFeatures);

    expect(tabs.map((tab) => tab.path), [
      '/assistant',
      '/documents',
      '/tasks',
      '/employees',
      '/account',
    ]);
    expect(tabs.map((tab) => tab.label), [
      _assistantTab,
      _documentsTab,
      _tasksTab,
      _employeesTab,
      _accountTab,
    ]);
    expect(tabs.map((tab) => tab.label), isNot(contains('查信息')));
    expect(tabs.map((tab) => tab.label), contains('文档'));
  });

  test('English UI strings relabel bottom tabs', () {
    final en = AppStrings.forLanguage(appLanguageEnglish);
    final tabs = mobileAppTabsForFeatures(defaultMobileFeatures, strings: en);
    expect(tabs.map((tab) => tab.label), [
      'Assistant',
      'Docs',
      'Tasks',
      'Employees',
      'Me',
    ]);
  });

  test('mobile app presents the GUI-like AI assistant as the first tab', () {
    final tabs = mobileAppTabsForFeatures(defaultMobileFeatures);

    expect(tabs.first.path, '/assistant');
    expect(tabs.first.label, _assistantTab);
    expect(mobileInitialPathForFeatures(defaultMobileFeatures), '/assistant');
  });

  test('digital twin mode renames the first tab label', () {
    const bootstrap = MobileBootstrap(
      user: MobileUser(userId: 'u1', email: 'phone:19900001111', tenantId: 't1'),
      services: MobileServices(
        hubStatus: 'online',
        llmStatus: 'unavailable',
        searchStatus: 'available',
        documentsStatus: 'available',
        digitalEmployeesStatus: 'available',
        llmStatusPath: '',
        modelsPath: '',
        searchPath: '',
        documentsPath: '',
        digitalEmployeesPath: '',
        realtimePath: '',
      ),
      llmAccess: MobileLlmAccess(
        mode: 'maclaw_official',
        status: 'unavailable',
        authorizationId: '',
        authorizedBy: '',
        authorizedAt: null,
      ),
      features: defaultMobileFeatures,
      limits: MobileLimits(maxUploadBytes: 0, maxExportJobs: 0),
      assistantMode: mobileAssistantModeDigitalTwin,
    );

    final tabs = mobileAppTabsForFeatures(
      defaultMobileFeatures,
      bootstrap: bootstrap,
    );
    expect(tabs.first.label, _twinTab);
    expect(usesDigitalTwinAssistant(bootstrap), isTrue);
  });

  test(
      'mobile app keeps assistant and account when optional features are disabled',
      () {
    const features = MobileFeatures(
      search: false,
      documents: false,
      tasks: false,
      backendSshSessions: false,
      digitalEmployees: false,
      pushNotifications: false,
    );

    final tabs = mobileAppTabsForFeatures(features);

    expect(tabs.map((tab) => tab.path), ['/assistant', '/account']);
    expect(tabs.map((tab) => tab.label), [_assistantTab, _accountTab]);
    expect(mobileInitialPathForFeatures(features), '/assistant');
  });

  test('documents is a primary bottom tab before tasks', () {
    const features = MobileFeatures(
      search: false,
      documents: true,
      tasks: true,
      backendSshSessions: true,
      digitalEmployees: true,
      pushNotifications: false,
    );

    final tabs = mobileAppTabsForFeatures(features);

    expect(mobileInitialPathForFeatures(features), '/assistant');
    expect(tabs.map((tab) => tab.path), [
      '/assistant',
      '/documents',
      '/tasks',
      '/employees',
      '/account',
    ]);
    expect(mobilePathEnabledForFeatures('/documents', features), isTrue);
    expect(mobilePathEnabledForFeatures('/tasks', features), isTrue);
    expect(mobilePathEnabledForFeatures('/assistant', features), isTrue);
  });

  test('shared document intents prefer the documents tab', () {
    const features = MobileFeatures(
      search: true,
      documents: true,
      tasks: true,
      backendSshSessions: true,
      digitalEmployees: true,
      pushNotifications: false,
    );
    final intent = MobileSharedIntent(
      id: 'share-1',
      kind: MobileSharedIntentKind.file,
      value: '/tmp/a.docx',
      receivedAt: DateTime.utc(2026, 1, 1),
    );
    expect(intent.opensDocuments, isTrue);
    expect(sharedIntentTargetPath(intent, features), '/documents');
    expect(sharedIntentCanBeConsumedAtTarget(intent, '/documents'), isTrue);
  });

  test('resolveMobileAssistantMode prefers declared mode then LLM readiness',
      () {
    expect(resolveMobileAssistantMode(null), mobileAssistantModeOfficial);

    const twin = MobileBootstrap(
      user: MobileUser(userId: 'u', email: 'a@b.c', tenantId: 't'),
      services: MobileServices(
        hubStatus: 'online',
        llmStatus: 'unavailable',
        searchStatus: 'available',
        documentsStatus: 'available',
        digitalEmployeesStatus: 'available',
        llmStatusPath: '',
        modelsPath: '',
        searchPath: '',
        documentsPath: '',
        digitalEmployeesPath: '',
        realtimePath: '',
      ),
      llmAccess: MobileLlmAccess(
        mode: 'maclaw_official',
        status: 'unavailable',
        authorizationId: '',
        authorizedBy: '',
        authorizedAt: null,
      ),
      features: defaultMobileFeatures,
      limits: MobileLimits(maxUploadBytes: 0, maxExportJobs: 0),
      assistantMode: mobileAssistantModeOfficial,
    );
    // Declared official wins even if LLM not configured (Hub authority).
    expect(resolveMobileAssistantMode(twin), mobileAssistantModeOfficial);
  });

  test('default document quota is 100 MiB when Hub omits quota fields', () {
    const limits = MobileLimits(maxUploadBytes: 0, maxExportJobs: 0);
    expect(limits.effectiveDocumentQuotaBytes, mobileDefaultDocumentQuotaBytes);
  });
}
