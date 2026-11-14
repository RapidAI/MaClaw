import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:uuid/uuid.dart';

import 'account_store.dart';
import 'api_client.dart';
import 'hub_config.dart';
import '../features/auth/auth_api.dart';

/// Whole session state: the stored accounts and which one is active.
class SessionState {
  final bool loaded;
  final List<Account> accounts;
  final Account? active;

  /// The active account's credential was rejected outright (401 /
  /// UNAUTHORIZED / MACHINE_UNAUTHORIZED) — the router bounces to
  /// re-verification while the account stays stored for a smooth
  /// re-login upsert.
  final bool expired;

  const SessionState({
    this.loaded = false,
    this.accounts = const [],
    this.active,
    this.expired = false,
  });
}

class SessionController extends Notifier<SessionState> {
  final AccountStore _store = AccountStore();

  @override
  SessionState build() {
    Future.microtask(load);
    return const SessionState();
  }

  Future<void> load() async {
    await _store.load();
    state = SessionState(
      loaded: true,
      accounts: _store.accounts,
      active: _store.active,
    );
  }

  Future<void> addAccount(Account account) async {
    await _store.upsert(account);
    state = SessionState(
      loaded: true,
      accounts: _store.accounts,
      active: _store.active,
      // A successful verify refreshes the token — the expiry flag clears.
      expired: false,
    );
  }

  Future<void> switchAccount(String key) async {
    if (key == state.active?.key) return;
    await _store.setActive(key);
    state = SessionState(
      loaded: true,
      accounts: _store.accounts,
      active: _store.active,
    );
  }

  /// The Hub rejected the active token — flag it so the router sends the
  /// user through re-verification without dropping the stored account.
  void expireActive() {
    if (state.active == null || state.expired) return;
    state = SessionState(
      loaded: state.loaded,
      accounts: state.accounts,
      active: state.active,
      expired: true,
    );
  }

  Future<void> removeAccount(String key) async {
    await _store.remove(key);
    state = SessionState(
      loaded: true,
      accounts: _store.accounts,
      active: _store.active,
    );
  }

  /// Clears every account — the "退出登录" of the current one only removes it,
  /// this is for an explicit full reset.
  Future<void> signOutActive() async {
    final key = state.active?.key;
    if (key != null) await removeAccount(key);
  }
}

final sessionProvider =
    NotifierProvider<SessionController, SessionState>(SessionController.new);

/// API client for the active account; rebuilt on every account switch.
final apiClientProvider = Provider<ApiClient?>((ref) {
  final account = ref.watch(sessionProvider).active;
  if (account == null) return null;
  // In-flight requests on a replaced client may reject AFTER the user
  // switched accounts — only expire when the rejecting client still owns
  // the active slot, otherwise the healthy new account gets flagged.
  final key = account.key;
  return ApiClient(
    account,
    onAuthRejected: () {
      if (ref.read(sessionProvider).active?.key == key) {
        ref.read(sessionProvider.notifier).expireActive();
      }
    },
  );
});

final authApiProvider = Provider<AuthApi>((ref) => AuthApi());

/// Persistent per-device client id sent on enrollment.
final clientIdProvider = FutureProvider<String>((ref) async {
  final prefs = await SharedPreferences.getInstance();
  var id = prefs.getString('mobilebot.clientId');
  if (id == null || id.isEmpty) {
    id = const Uuid().v4();
    await prefs.setString('mobilebot.clientId', id);
  }
  return id;
});

/// Route an unauthorized Hub failure into the session: the router then
/// bounces the person to `/login?mode=relogin` to re-verify the account.
void noteAuthFailure(Ref ref, Object error) {
  if (error is HubApiException && error.isAuthRejected) {
    ref.read(sessionProvider.notifier).expireActive();
  }
}
