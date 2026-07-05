import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';

void main() {
  test('parses bootstrap defaults safely', () {
    final bootstrap = MobileBootstrap.fromJson({
      'user': {
        'user_id': 'u1',
        'email': 'phone:19900001111',
        'phone_number': '19900001111',
        'account_id': 'phone:19900001111',
        'credits_account': ' phone:199 0000-1111 ',
        'tenant_id': 'tenant_a',
      },
      'services': {
        'hub_status': 'online',
        'llm_status': 'available',
        'search_status': 'available',
        'documents_status': 'available',
        'digital_employees_status': 'available',
        'llm_status_path': '/api/llm/service/status',
        'models_path': '/api/llm/v1/models',
        'search_path': '/api/mobile/search',
        'documents_path': '/api/mobile/documents',
        'digital_employees_path': '/api/mobile/digital-employees',
      },
      'connection': {
        'hubcenter_candidates': [
          'https://hubs.mypapers.top',
          'https://hubs.maclaw.top',
          'https://hubs2.maclaw.top',
        ],
        'selected_hubcenter_url': 'https://hubs.maclaw.top',
        'hub': {
          'id': 'hub-a',
          'base_url': 'https://tenant-a.maclaw.top',
        },
        'tenant_id': 'tenant_a',
      },
      'llm_access': {
        'mode': 'desktop_qr_third_party',
        'status': 'available',
        'authorization_id': 'llm-auth-1',
        'authorized_by': 'maclaw-gui',
        'credits_account': 'phone:199 0000-1111',
        'authorized_at': '2026-07-02T00:00:00Z',
      },
      'limits': {
        'max_upload_bytes': 26214400,
        'max_export_jobs': 3,
      },
    });

    expect(bootstrap.user.email, 'phone:19900001111');
    expect(bootstrap.user.phoneNumber, '19900001111');
    expect(bootstrap.user.accountId, 'phone:19900001111');
    expect(bootstrap.user.creditsAccount, 'phone:19900001111');
    expect(bootstrap.services.hubStatus, 'online');
    expect(bootstrap.services.llmStatus, 'available');
    expect(bootstrap.services.searchStatus, 'available');
    expect(bootstrap.services.documentsStatus, 'available');
    expect(bootstrap.services.digitalEmployeesStatus, 'available');
    expect(bootstrap.services.modelsPath, '/api/llm/v1/models');
    expect(bootstrap.services.searchPath, '/api/mobile/search');
    expect(bootstrap.services.documentsPath, '/api/mobile/documents');
    expect(bootstrap.services.realtimePath, '/api/mobile/realtime');
    expect(bootstrap.services.realtimeConfigured, isTrue);
    expect(bootstrap.connection.hubCenterCandidates, hasLength(3));
    expect(
      bootstrap.connection.selectedHubCenterUrl,
      'https://hubs.maclaw.top',
    );
    expect(bootstrap.connection.hubUrl, 'https://tenant-a.maclaw.top');
    expect(bootstrap.connection.hubId, 'hub-a');
    expect(bootstrap.connection.tenantId, 'tenant_a');
    expect(bootstrap.llmAccess.desktopQrDelegated, isTrue);
    expect(bootstrap.llmAccess.authorizationId, 'llm-auth-1');
    expect(bootstrap.llmAccess.creditsAccount, 'phone:19900001111');
    expect(
      bootstrap.services.digitalEmployeesPath,
      '/api/mobile/digital-employees',
    );
    expect(bootstrap.features.assistant, isTrue);
    expect(bootstrap.features.search, isTrue);
    expect(bootstrap.features.backendSshSessions, isTrue);
    expect(bootstrap.features.digitalEmployees, isTrue);
    expect(bootstrap.limits.maxUploadBytes, 26214400);
    expect(bootstrap.limits.maxExportJobs, 3);
  });

  test(
      'backend SSH session feature prefers new field and accepts legacy Hub field',
      () {
    final enabled = MobileBootstrap.fromJson({
      'features': {
        'backend_ssh_sessions': true,
        'local_ssh': false,
      },
    });
    final legacy = MobileBootstrap.fromJson({
      'features': {
        'local_ssh': true,
      },
    });

    expect(enabled.features.backendSshSessions, isTrue);
    expect(legacy.features.backendSshSessions, isTrue);
    expect(legacy.features.localSsh, isTrue);
  });

  test('assistant feature is independent from search feature', () {
    final bootstrap = MobileBootstrap.fromJson({
      'features': {
        'assistant': true,
        'search': false,
      },
    });

    expect(bootstrap.features.assistant, isTrue);
    expect(bootstrap.features.search, isFalse);
  });

  test('assistant can be explicitly disabled by Hub bootstrap', () {
    final bootstrap = MobileBootstrap.fromJson({
      'features': {
        'assistant': false,
        'search': true,
      },
    });

    expect(bootstrap.features.assistant, isFalse);
    expect(bootstrap.features.search, isTrue);
  });

  test('detects missing realtime path from bootstrap services', () {
    final bootstrap = MobileBootstrap.fromJson({
      'services': {'realtime_path': ''},
    });

    expect(bootstrap.services.realtimePath, '');
    expect(bootstrap.services.realtimeConfigured, isFalse);
    expect(bootstrap.services.llmStatus, 'unknown');
    expect(bootstrap.services.searchStatus, 'unknown');
  });

  test('defaults official LLM credits to verified phone account', () {
    final bootstrap = MobileBootstrap.fromJson({
      'user': {
        'user_id': 'u-phone',
        'phone_number': '199 0000-1111',
        'tenant_id': 'tenant_a',
      },
      'llm_access': {
        'mode': 'maclaw_official',
        'status': 'available',
      },
    });

    expect(bootstrap.user.creditsAccount, 'phone:19900001111');
    expect(bootstrap.llmAccess.official, isTrue);
    expect(bootstrap.llmAccess.creditsAccount, 'phone:19900001111');
  });

  test('verified phone login can supply official credits after bootstrap', () {
    final bootstrap = MobileBootstrap.fromJson({
      'user': {
        'user_id': 'u-phone',
        'tenant_id': 'tenant_a',
      },
      'llm_access': {
        'mode': 'maclaw_official',
        'status': 'available',
      },
    }).withVerifiedPhoneCredits('199 0000-1111');

    expect(bootstrap.user.creditsAccount, 'phone:19900001111');
    expect(bootstrap.llmAccess.official, isTrue);
    expect(bootstrap.llmAccess.creditsAccount, 'phone:19900001111');
  });

  test('verified phone login can supply official phone credits account', () {
    final bootstrap = MobileBootstrap.fromJson({
      'user': {
        'user_id': 'u-phone',
        'tenant_id': 'tenant_a',
      },
      'llm_access': {
        'mode': 'maclaw_official',
        'status': 'available',
      },
    }).withVerifiedPhoneCredits('phone:199 0000-1111');

    expect(bootstrap.user.creditsAccount, 'phone:19900001111');
    expect(bootstrap.llmAccess.official, isTrue);
    expect(bootstrap.llmAccess.creditsAccount, 'phone:19900001111');
  });

  test('verified phone login rejects malformed credits account', () {
    final bootstrap = MobileBootstrap.fromJson({
      'user': {
        'user_id': 'u-phone',
        'tenant_id': 'tenant_a',
      },
      'llm_access': {
        'mode': 'maclaw_official',
        'status': 'available',
      },
    }).withVerifiedPhoneCredits('phone:user19900001111');

    expect(bootstrap.user.creditsAccount, '');
    expect(bootstrap.llmAccess.official, isTrue);
    expect(bootstrap.llmAccess.creditsAccount, '');
  });

  test('does not normalize malformed phone credits with letters', () {
    final bootstrap = MobileBootstrap.fromJson({
      'user': {
        'user_id': 'u-phone',
        'phone_number': '19900001111',
        'credits_account': 'phone:user19900001111',
        'tenant_id': 'tenant_a',
      },
      'llm_access': {
        'mode': 'maclaw_official',
        'status': 'available',
        'credits_account': 'phone:user19900001111',
      },
    });

    expect(bootstrap.user.creditsAccount, 'phone:user19900001111');
    expect(bootstrap.llmAccess.creditsAccount, 'phone:user19900001111');
  });
}
