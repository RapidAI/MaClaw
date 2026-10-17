import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/api/official_service.dart';
import 'package:maclaw_mobile/features/auth/auth_service.dart';
import 'package:maclaw_mobile/features/auth/login_screen.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';

class _HubDiscoverySessionController extends SessionController {
  String _hubUrl = '';
  String? verifiedCode;
  final requestedPhones = <String>[];
  bool failRequest = false;

  @override
  String get currentHubUrl => _hubUrl;

  @override
  Future<SessionState> build() async => const SessionState.signedOut();

  @override
  Future<PhoneLoginRequestResult> requestPhoneLogin({
    required String phoneNumber,
  }) async {
    if (failRequest) {
      throw const OfficialHubCenterUnavailableException([
        OfficialHubCenterAttempt(
          url: 'https://hubs.mypapers.top',
          available: false,
          message: 'HTTP 502',
        ),
      ]);
    }
    return PhoneLoginRequestResult(
      status: 'sent',
      message: 'code sent',
      phoneNumber: phoneNumber,
      hubUrl: 'https://tenant-a.maclaw.top',
      hubId: 'hub-a',
      tenantId: 'tenant-a',
      tenantName: 'Tenant A',
      hubCenterUrl: 'https://hubs.maclaw.top',
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
    _hubUrl = hubUrl;
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
  testWidgets('phone login rejects non-phone input before HubCenter discovery',
      (tester) async {
    await tester.binding.setSurfaceSize(const Size(900, 1200));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = _HubDiscoverySessionController();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionControllerProvider.overrideWith(() => controller),
        ],
        child: const MaterialApp(home: LoginScreen()),
      ),
    );

    await tester.enterText(find.byType(TextField).first, 'user19900001111');
    await tester.tap(find.text('发送验证码'));
    await tester.pump();

    expect(controller.requestedPhones, isEmpty);
    expect(
      find.text('请输入有效手机号，只支持数字和常见手机号分隔符。'),
      findsOneWidget,
    );
    expect(find.text('官方接入状态'), findsNothing);
  });
  testWidgets('phone login is the only signed-out first screen path',
      (tester) async {
    await tester.binding.setSurfaceSize(const Size(900, 1200));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final controller = _HubDiscoverySessionController();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionControllerProvider.overrideWith(() => controller),
        ],
        child: const MaterialApp(home: LoginScreen()),
      ),
    );

    expect(find.text('手机号注册/登录'), findsOneWidget);
    expect(find.bySemanticsLabel('MaClaw'), findsOneWidget);
    expect(find.textContaining('仅支持手机号账户接入'), findsNothing);
    expect(find.text('发送验证码'), findsOneWidget);
    expect(find.text('桌面二维码接入'), findsNothing);
    expect(find.text('扫码接入'), findsNothing);
    expect(find.byIcon(Icons.qr_code_scanner_outlined), findsNothing);

    await tester.enterText(find.byType(TextField).first, '19900001111');
    await tester.tap(find.text('发送验证码'));
    await tester.pump();

    expect(find.text('https://hubs.maclaw.top'), findsNothing);
    expect(find.text('https://tenant-a.maclaw.top'), findsNothing);
    expect(find.textContaining('HubCenter'), findsNothing);
    expect(find.text('官方接入状态'), findsNothing);
    expect(find.byIcon(Icons.verified_outlined), findsNothing);
    expect(find.text('重新发送验证码（60秒）'), findsOneWidget);
    expect(
      tester
          .widget<FilledButton>(
            find.widgetWithText(FilledButton, '重新发送验证码（60秒）'),
          )
          .onPressed,
      isNull,
    );

    await tester.enterText(find.byType(TextField).at(1), '303246');
    await tester.ensureVisible(find.text('验证并登录'));
    await tester.tap(find.text('验证并登录'));
    await tester.pump();

    expect(controller.verifiedCode, '303246');
    expect(
      find.text('登录成功，已接入手机号账户的官方服务 credits。'),
      findsOneWidget,
    );
  });

  testWidgets('phone login failures do not render HubCenter candidates',
      (tester) async {
    final controller = _HubDiscoverySessionController()..failRequest = true;
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          sessionControllerProvider.overrideWith(() => controller),
        ],
        child: const MaterialApp(home: LoginScreen()),
      ),
    );

    await tester.enterText(find.byType(TextField).first, '19900001111');
    await tester.tap(find.text('发送验证码'));
    await tester.pump();

    expect(find.text('验证码发送失败：无法连接官方服务'), findsOneWidget);
    expect(find.textContaining('https://'), findsNothing);
    expect(find.textContaining('HubCenter'), findsNothing);
  });
}

MobileBootstrap _bootstrap() {
  return const MobileBootstrap(
    user: MobileUser(
      userId: 'u1',
      email: 'phone:19900001111',
      phoneNumber: '19900001111',
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
      backendSshSessions: true,
      digitalEmployees: true,
      pushNotifications: false,
    ),
    limits: MobileLimits(
      maxUploadBytes: 1024,
      maxExportJobs: 2,
    ),
  );
}
