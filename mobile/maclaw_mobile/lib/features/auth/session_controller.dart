import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/api/mobile_bootstrap.dart';
import '../../core/api/official_service.dart';
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
  );
});

class SessionController extends AsyncNotifier<SessionState> {
  @override
  Future<SessionState> build() async {
    final vault = ref.watch(secureVaultProvider);
    final hubUrl = await vault.readHubUrl();
    final token = await vault.readToken();
    if (hubUrl == null || token == null || hubUrl.isEmpty || token.isEmpty) {
      return const SessionState.signedOut();
    }
    if (hubUrl != maclawOfficialServiceUrl) {
      await vault.clearSession();
      return const SessionState.signedOut();
    }
    try {
      final client = ApiClient(vault: vault);
      final bootstrap = await client.bootstrap();
      return SessionState.signedIn(
        hubUrl: maclawOfficialServiceUrl,
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
    return AuthService(vault: ref.watch(secureVaultProvider))
        .requestEmailLogin(email);
  }

  Future<bool> pollEmailLogin({
    required String pollId,
  }) async {
    final vault = ref.watch(secureVaultProvider);
    final result = await AuthService(vault: vault).pollEmailLogin(pollId);
    if (!result.confirmed) return false;
    final client = ApiClient(vault: vault);
    final bootstrap = await client.bootstrap();
    state = AsyncData(
      SessionState.signedIn(
        hubUrl: maclawOfficialServiceUrl,
        bootstrap: bootstrap,
      ),
    );
    return true;
  }

  Future<void> signOut() async {
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
}
