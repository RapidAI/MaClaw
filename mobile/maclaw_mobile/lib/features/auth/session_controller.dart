import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_bootstrap.dart';
import '../../core/storage/secure_vault.dart';
import 'auth_service.dart';

final secureVaultProvider = Provider<SecureVault>((ref) => const SecureVault());

final sessionControllerProvider =
    AsyncNotifierProvider<SessionController, SessionState>(
  SessionController.new,
);

final apiClientProvider = Provider<ApiClient?>((ref) {
  final session = ref.watch(sessionControllerProvider).valueOrNull;
  if (session == null || !session.authenticated) return null;
  return ApiClient(
    vault: ref.watch(secureVaultProvider),
    hubUrl: session.hubUrl,
  );
});

class SessionController extends AsyncNotifier<SessionState> {
  AuthService? _authService;

  String get currentHubUrl => state.valueOrNull?.hubUrl ?? '';

  @override
  Future<SessionState> build() async {
    final vault = ref.watch(secureVaultProvider);
    final hubUrl = await vault.readHubUrl();
    final token = await vault.readToken();
    if (hubUrl == null || token == null || hubUrl.isEmpty || token.isEmpty) {
      return const SessionState.signedOut();
    }
    try {
      final client = ApiClient(vault: vault, hubUrl: hubUrl);
      final bootstrap = await client.bootstrap();
      return SessionState.signedIn(
        hubUrl: hubUrl,
        bootstrap: bootstrap,
      );
    } catch (_) {
      await vault.clearSession();
      return const SessionState.signedOut();
    }
  }

  Future<EmailLoginRequestResult> requestEmailLogin({
    required String email,
  }) {
    final service = AuthService(vault: ref.watch(secureVaultProvider));
    _authService = service;
    return service.requestEmailLogin(email);
  }

  Future<EmailLoginRequestResult> requestEmailLoginOnHub({
    required String hubUrl,
    required String email,
    String hubCenterUrl = '',
  }) {
    final service = AuthService(vault: ref.watch(secureVaultProvider));
    _authService = service;
    return service.requestEmailLoginOnHub(
      hubUrl: hubUrl,
      email: email,
      hubCenterUrl: hubCenterUrl,
    );
  }

  Future<bool> pollEmailLogin({
    required String pollId,
  }) async {
    final vault = ref.watch(secureVaultProvider);
    final service = _authService ?? AuthService(vault: vault);
    _authService = service;
    final result = await service.pollEmailLogin(pollId);
    if (!result.confirmed) return false;
    if (result.hubUrl.isEmpty) return false;
    final client = ApiClient(vault: vault, hubUrl: result.hubUrl);
    final bootstrap = await client.bootstrap();
    state = AsyncData(
      SessionState.signedIn(
        hubUrl: result.hubUrl,
        bootstrap: bootstrap,
      ),
    );
    _authService = null;
    return true;
  }

  Future<bool> pollEmailLoginOnHub({
    required String hubUrl,
    required String pollId,
    String hubCenterUrl = '',
  }) async {
    final vault = ref.watch(secureVaultProvider);
    final service = _authService ?? AuthService(vault: vault);
    _authService = service;
    final result = await service.pollEmailLoginOnHub(
      hubUrl: hubUrl,
      pollId: pollId,
      hubCenterUrl: hubCenterUrl,
    );
    if (!result.confirmed) return false;
    final client = ApiClient(vault: vault, hubUrl: result.hubUrl);
    final bootstrap = await client.bootstrap();
    state = AsyncData(
      SessionState.signedIn(
        hubUrl: result.hubUrl,
        bootstrap: bootstrap,
      ),
    );
    _authService = null;
    return true;
  }

  Future<void> refreshBootstrap() async {
    final current = state.valueOrNull;
    if (current == null || !current.authenticated) return;
    try {
      final bootstrap = await ApiClient(
        vault: ref.watch(secureVaultProvider),
        hubUrl: current.hubUrl,
      ).bootstrap();
      state = AsyncData(current.copyWith(bootstrap: bootstrap));
    } catch (_) {
      state = AsyncData(current);
      rethrow;
    }
  }

  Future<MobileBootstrap> authorizeThirdPartyLlmWithDesktopQr(
    String qrPayload,
  ) async {
    final current = state.valueOrNull;
    if (current == null || !current.authenticated) {
      throw StateError('请先登录 MaClaw 官方服务。');
    }
    final bootstrap = await ApiClient(
      vault: ref.watch(secureVaultProvider),
      hubUrl: current.hubUrl,
    ).authorizeThirdPartyLlmWithDesktopQr(qrPayload);
    state = AsyncData(current.copyWith(bootstrap: bootstrap));
    return bootstrap;
  }

  Future<void> redeemOfficialServiceCode(String code) {
    return _connectWithMobileService(
      (service) => service.redeemOfficialServiceCode(code),
    );
  }

  Future<void> connectWithDesktopLlmQr(String qrPayload) {
    return _connectWithMobileService(
      (service) => service.connectWithDesktopLlmQr(qrPayload),
    );
  }

  Future<void> _connectWithMobileService(
    Future<MobileServiceConnectResult> Function(AuthService service) connect,
  ) async {
    final vault = ref.watch(secureVaultProvider);
    final service = AuthService(vault: vault);
    final result = await connect(service);
    final bootstrap = await ApiClient(
      vault: vault,
      hubUrl: result.hubUrl,
    ).bootstrap();
    state = AsyncData(
      SessionState.signedIn(
        hubUrl: result.hubUrl,
        bootstrap: bootstrap,
      ),
    );
  }

  Future<void> signOut() async {
    _authService = null;
    await ref.watch(secureVaultProvider).clearSession();
    state = const AsyncData(SessionState.signedOut());
  }
}

class SessionState {
  final String hubUrl;
  final MobileBootstrap? bootstrap;

  const SessionState._({required this.hubUrl, required this.bootstrap});

  const SessionState.signedOut() : this._(hubUrl: '', bootstrap: null);

  const SessionState.signedIn({
    required String hubUrl,
    required MobileBootstrap bootstrap,
  }) : this._(hubUrl: hubUrl, bootstrap: bootstrap);

  bool get authenticated => hubUrl.isNotEmpty && bootstrap != null;

  SessionState copyWith({
    String? hubUrl,
    MobileBootstrap? bootstrap,
  }) {
    return SessionState._(
      hubUrl: hubUrl ?? this.hubUrl,
      bootstrap: bootstrap ?? this.bootstrap,
    );
  }
}
