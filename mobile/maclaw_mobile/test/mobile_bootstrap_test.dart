import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';

void main() {
  test('parses bootstrap defaults safely', () {
    final bootstrap = MobileBootstrap.fromJson({
      'user': {
        'user_id': 'u1',
        'email': 'u1@example.com',
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
        'authorized_at': '2026-07-02T00:00:00Z',
      },
      'limits': {
        'max_upload_bytes': 26214400,
        'max_export_jobs': 3,
      },
    });

    expect(bootstrap.user.email, 'u1@example.com');
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
    expect(
      bootstrap.services.digitalEmployeesPath,
      '/api/mobile/digital-employees',
    );
    expect(bootstrap.features.search, isTrue);
    expect(bootstrap.features.localSsh, isTrue);
    expect(bootstrap.features.digitalEmployees, isTrue);
    expect(bootstrap.limits.maxUploadBytes, 26214400);
    expect(bootstrap.limits.maxExportJobs, 3);
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
}
