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
        'llm_status_path': '/api/llm/service/status',
        'models_path': '/api/llm/v1/models',
        'search_path': '/api/mobile/search',
        'documents_path': '/api/mobile/documents',
        'digital_employees_path': '/api/mobile/digital-employees',
      },
      'limits': {
        'max_upload_bytes': 26214400,
        'max_export_jobs': 3,
      },
    });

    expect(bootstrap.user.email, 'u1@example.com');
    expect(bootstrap.services.hubStatus, 'online');
    expect(bootstrap.services.modelsPath, '/api/llm/v1/models');
    expect(bootstrap.services.searchPath, '/api/mobile/search');
    expect(bootstrap.services.documentsPath, '/api/mobile/documents');
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
}
