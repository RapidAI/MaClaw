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
      },
    });

    expect(bootstrap.user.email, 'u1@example.com');
    expect(bootstrap.services.hubStatus, 'online');
    expect(bootstrap.features.search, isTrue);
    expect(bootstrap.features.localSsh, isTrue);
    expect(bootstrap.features.digitalEmployees, isTrue);
  });
}
