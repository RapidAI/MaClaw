import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/features/auth/auth_service.dart';
import 'package:maclaw_mobile/features/auth/login_screen.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';

class _HubDiscoverySessionController extends SessionController {
  int pollCalls = 0;
  String _hubUrl = '';

  @override
  String get currentHubUrl => _hubUrl;

  @override
  Future<SessionState> build() async => const SessionState.signedOut();

  @override
  Future<EmailLoginRequestResult> requestEmailLogin({
    required String email,
  }) async {
    return const EmailLoginRequestResult(
      status: 'sent',
      message: 'check login',
      pollId: 'poll-1',
      hubCenterUrl: 'https://hubs.maclaw.top',
    );
  }

  @override
  Future<bool> pollEmailLogin({required String pollId}) async {
    pollCalls += 1;
    _hubUrl = 'https://tenant-a.maclaw.top';
    state = AsyncData(
      SessionState.signedIn(
        hubUrl: _hubUrl,
        bootstrap: _bootstrap(),
      ),
    );
    return true;
  }
}

void main() {
  testWidgets('shows selected HubCenter and discovered Hub during login',
      (tester) async {
    final controller = _HubDiscoverySessionController();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionControllerProvider.overrideWith(
            () => controller,
          ),
        ],
        child: const MaterialApp(home: LoginScreen()),
      ),
    );

    await tester.enterText(find.byType(TextField), 'user@example.com');
    await tester.tap(find.byType(FilledButton));
    await tester.pump();

    expect(find.text('https://hubs.maclaw.top'), findsOneWidget);
    expect(find.byIcon(Icons.verified_outlined), findsOneWidget);

    await tester.pump(const Duration(milliseconds: 3100));
    await tester.pump();

    expect(controller.pollCalls, 1);
    expect(find.text('https://tenant-a.maclaw.top'), findsOneWidget);
  });
}

MobileBootstrap _bootstrap() {
  return const MobileBootstrap(
    user: MobileUser(
      userId: 'u1',
      email: 'user@example.com',
      tenantId: 'tenant-a',
    ),
    services: MobileServices(
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
    features: MobileFeatures(
      search: true,
      documents: true,
      localSsh: true,
      digitalEmployees: true,
      pushNotifications: false,
    ),
    limits: MobileLimits(
      maxUploadBytes: 1024,
      maxExportJobs: 2,
    ),
  );
}
