import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/features/auth/auth_service.dart';
import 'package:maclaw_mobile/features/auth/llm_setup_screen.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';

class _PendingRedemptionSessionController extends SessionController {
  String? redeemedCode;
  String? requestedEmail;
  String? requestedHubUrl;
  String? requestedHubCenterUrl;
  String? polledHubUrl;
  String? polledHubCenterUrl;
  String? polledPollId;
  int pollCalls = 0;

  @override
  String get currentHubUrl => 'https://tenant-a.maclaw.top';

  @override
  Future<SessionState> build() async => const SessionState.signedOut();

  @override
  Future<void> redeemOfficialServiceCode(String code) async {
    redeemedCode = code;
    throw const MobileServiceConnectionPendingException(
      MobileServiceConnectResult(
        status: 'requires_email_login',
        nextAction: 'email_login',
        message: 'continue with email login',
        accessToken: '',
        hubUrl: 'https://tenant-a.maclaw.top',
        hubId: 'hub-a',
        tenantId: 'tenant-a',
        hubCenterUrl: 'https://hubs.maclaw.top',
      ),
    );
  }

  @override
  Future<EmailLoginRequestResult> requestEmailLoginOnHub({
    required String hubUrl,
    required String email,
    String hubCenterUrl = '',
  }) async {
    requestedHubUrl = hubUrl;
    requestedEmail = email;
    requestedHubCenterUrl = hubCenterUrl;
    return const EmailLoginRequestResult(
      status: 'sent',
      message: 'check login',
      pollId: 'poll-1',
      hubCenterUrl: 'https://hubs.maclaw.top',
    );
  }

  @override
  Future<bool> pollEmailLoginOnHub({
    required String hubUrl,
    required String pollId,
    String hubCenterUrl = '',
  }) async {
    pollCalls += 1;
    polledHubUrl = hubUrl;
    polledPollId = pollId;
    polledHubCenterUrl = hubCenterUrl;
    state = AsyncData(
      SessionState.signedIn(
        hubUrl: hubUrl,
        bootstrap: _bootstrap(),
      ),
    );
    return true;
  }
}

const _connectOfficialService = '\u63a5\u5165\u5b98\u65b9\u670d\u52a1';
const _continueHubEmailLogin =
    '\u7ee7\u7eed\u5b8c\u6210 Hub \u90ae\u7bb1\u767b\u5f55';
const _email = '\u90ae\u7bb1';
const _sendLoginConfirmation = '\u53d1\u9001\u767b\u5f55\u786e\u8ba4';
const _loginConfirmed =
    '\u767b\u5f55\u5df2\u786e\u8ba4\uff0c\u6b63\u5728\u8fdb\u5165 MaClaw Mobile\u3002';

void main() {
  testWidgets('continues official redemption with targeted Hub email login',
      (tester) async {
    await tester.binding.setSurfaceSize(const Size(900, 1200));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = _PendingRedemptionSessionController();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionControllerProvider.overrideWith(() => controller),
        ],
        child: const MaterialApp(
          home: LlmSetupScreen(
            scanner: ColoredBox(color: Colors.black12),
          ),
        ),
      ),
    );

    await tester.enterText(find.byType(TextField).first, ' CODE-123 ');
    await tester.tap(find.text(_connectOfficialService));
    await tester.pump();

    expect(controller.redeemedCode, 'CODE-123');
    expect(find.text(_continueHubEmailLogin), findsOneWidget);
    expect(find.text('https://tenant-a.maclaw.top'), findsOneWidget);
    expect(find.text('tenant-a'), findsOneWidget);
    expect(find.text('https://hubs.maclaw.top'), findsWidgets);

    final emailField = find.widgetWithText(TextField, _email);
    await tester.enterText(emailField, ' user@example.com ');
    await tester.ensureVisible(find.text(_sendLoginConfirmation));
    await tester.tap(find.text(_sendLoginConfirmation));
    await tester.pump();

    expect(controller.requestedHubUrl, 'https://tenant-a.maclaw.top');
    expect(controller.requestedHubCenterUrl, 'https://hubs.maclaw.top');
    expect(controller.requestedEmail, 'user@example.com');
    expect(find.text('check login'), findsOneWidget);

    await tester.pump(const Duration(milliseconds: 3100));
    await tester.pump();

    expect(controller.pollCalls, 1);
    expect(controller.polledHubUrl, 'https://tenant-a.maclaw.top');
    expect(controller.polledHubCenterUrl, 'https://hubs.maclaw.top');
    expect(controller.polledPollId, 'poll-1');
    expect(find.text(_loginConfirmed), findsOneWidget);
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
