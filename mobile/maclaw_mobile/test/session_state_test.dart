import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';

void main() {
  test(
      'session state copyWith updates bootstrap without changing discovered Hub',
      () {
    final original = SessionState.signedIn(
      hubUrl: 'https://tenant-a.maclaw.top',
      bootstrap: _bootstrap(email: 'old@example.com'),
    );
    final updated = original.copyWith(
      bootstrap: _bootstrap(email: 'new@example.com'),
    );

    expect(updated.hubUrl, 'https://tenant-a.maclaw.top');
    expect(updated.bootstrap?.user.email, 'new@example.com');
    expect(updated.authenticated, isTrue);
  });
}

MobileBootstrap _bootstrap({required String email}) {
  return MobileBootstrap(
    user: MobileUser(
      userId: 'u1',
      email: email,
      tenantId: 'tenant_a',
    ),
    services: const MobileServices(
      hubStatus: 'online',
      llmStatus: 'available',
      searchStatus: 'available',
      documentsStatus: 'available',
      digitalEmployeesStatus: 'available',
      llmStatusPath: '/api/llm/status',
      modelsPath: '/api/llm/models',
      searchPath: '/api/mobile/search',
      documentsPath: '/api/mobile/documents',
      digitalEmployeesPath: '/api/mobile/digital-employees',
      realtimePath: '/api/mobile/realtime',
    ),
    features: const MobileFeatures(
      search: true,
      documents: true,
      localSsh: true,
      digitalEmployees: true,
      pushNotifications: true,
    ),
    limits: const MobileLimits(
      maxUploadBytes: 1024,
      maxExportJobs: 2,
    ),
  );
}
