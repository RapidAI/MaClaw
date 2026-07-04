import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/features/auth/auth_service.dart';
import 'package:maclaw_mobile/features/auth/llm_setup_screen.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';

class _PendingRedemptionSessionController extends SessionController {
  String? redeemedCode;
  String? requestedPhone;
  String? requestedHubUrl;
  String? requestedHubCenterUrl;
  String? verifiedCode;
  String? verifiedTenantId;

  @override
  String get currentHubUrl => 'https://tenant-a.maclaw.top';

  @override
  Future<SessionState> build() async => const SessionState.signedOut();

  @override
  Future<void> redeemOfficialServiceCode(String code) async {
    redeemedCode = code;
    throw const MobileServiceConnectionPendingException(
      MobileServiceConnectResult(
        status: 'requires_phone_login',
        nextAction: 'phone_login',
        message: 'continue with phone login',
        accessToken: '',
        hubUrl: 'https://tenant-a.maclaw.top',
        hubId: 'hub-a',
        tenantId: 'tenant-a',
        hubCenterUrl: 'https://hubs.maclaw.top',
      ),
    );
  }

  @override
  Future<PhoneLoginRequestResult> requestPhoneLoginOnHub({
    required String hubUrl,
    required String phoneNumber,
    String tenantId = '',
    String hubCenterUrl = '',
  }) async {
    requestedHubUrl = hubUrl;
    requestedPhone = phoneNumber;
    requestedHubCenterUrl = hubCenterUrl;
    return PhoneLoginRequestResult(
      status: 'sent',
      message: 'code sent',
      phoneNumber: phoneNumber.trim(),
      hubUrl: hubUrl,
      hubId: 'hub-a',
      tenantId: tenantId,
      tenantName: 'Tenant A',
      hubCenterUrl: hubCenterUrl,
      expiresMinutes: 5,
      codeLength: 6,
    );
  }

  @override
  Future<bool> verifyPhoneLoginOnHub({
    required String hubUrl,
    required String phoneNumber,
    required String verifyCode,
    String tenantId = '',
    String hubCenterUrl = '',
  }) async {
    verifiedCode = verifyCode;
    verifiedTenantId = tenantId;
    state = AsyncData(
      SessionState.signedIn(
        hubUrl: hubUrl,
        bootstrap: _bootstrap(),
      ),
    );
    return true;
  }
}

class _RecordingQrSessionController extends SessionController {
  final qrPayloads = <String>[];

  @override
  Future<SessionState> build() async => const SessionState.signedOut();

  @override
  Future<void> connectWithDesktopLlmQr(String qrPayload) async {
    qrPayloads.add(qrPayload);
  }
}

void main() {
  testWidgets('continues official redemption with targeted Hub phone login',
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
    await tester.tap(find.text('接入官方服务'));
    await tester.pump();

    expect(controller.redeemedCode, 'CODE-123');
    expect(find.text('继续完成手机号登录'), findsOneWidget);
    expect(find.text('https://tenant-a.maclaw.top'), findsOneWidget);
    expect(find.text('tenant-a'), findsOneWidget);
    expect(find.text('https://hubs.maclaw.top'), findsWidgets);

    await tester.enterText(
      find.widgetWithText(TextField, '手机号'),
      '19900001111',
    );
    await tester.ensureVisible(find.text('发送验证码'));
    await tester.tap(find.text('发送验证码'));
    await tester.pump();

    expect(controller.requestedHubUrl, 'https://tenant-a.maclaw.top');
    expect(controller.requestedHubCenterUrl, 'https://hubs.maclaw.top');
    expect(controller.requestedPhone, '19900001111');
    expect(find.text('code sent'), findsOneWidget);

    await tester.enterText(find.widgetWithText(TextField, '验证码'), '303246');
    await tester.ensureVisible(find.text('验证并登录'));
    await tester.tap(find.text('验证并登录'));
    await tester.pump();

    expect(controller.verifiedCode, '303246');
    expect(controller.verifiedTenantId, 'tenant-a');
    expect(find.textContaining('官方服务 credits'), findsOneWidget);
  });

  testWidgets('rejects non MaClaw GUI LLM QR payload before connecting',
      (tester) async {
    await tester.binding.setSurfaceSize(const Size(900, 1200));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = _RecordingQrSessionController();
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

    await tester.enterText(
      find.widgetWithText(TextField, '粘贴二维码内容'),
      '{"type":"maclaw_llm","url":"https://llm.example.com/v1","key":"sk-test"}',
    );
    await tester.scrollUntilVisible(
      find.text('接入二维码服务商'),
      240,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('接入二维码服务商'));
    await tester.pumpAndSettle();

    expect(controller.qrPayloads, isEmpty);
    expect(
      find.textContaining('MaClaw GUI 生成的移动端 LLM 授权二维码'),
      findsOneWidget,
    );
  });
}

MobileBootstrap _bootstrap() {
  return const MobileBootstrap(
    user: MobileUser(
      userId: 'u1',
      email: 'phone:19900001111',
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
