import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/security/mobile_redaction.dart';
import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import 'server_command.dart';
import 'server_profile.dart';

final serverProfilesProvider =
    AsyncNotifierProvider<ServerProfilesController, List<ServerProfile>>(
  ServerProfilesController.new,
);

final sshAnalysisProvider =
    AsyncNotifierProvider<SSHAnalysisController, MobileSSHAnalysis?>(
  SSHAnalysisController.new,
);

final serverCommandsProvider =
    AsyncNotifierProvider<ServerCommandsController, List<ServerCommandEntry>>(
  ServerCommandsController.new,
);

class ServerProfilesController extends AsyncNotifier<List<ServerProfile>> {
  @override
  Future<List<ServerProfile>> build() {
    return ref.watch(mobileLocalStoreProvider).loadServerProfiles();
  }

  Future<void> addProfile(
    ServerProfile profile, {
    String password = '',
    String privateKey = '',
    String privateKeyPassphrase = '',
  }) async {
    if (!profile.isValid) {
      throw ArgumentError.value(
        profile,
        'profile',
        'Server profile requires host, port, and username.',
      );
    }
    final current = state.valueOrNull ?? await future;
    final next = [
      ...current.where((item) => item.id != profile.id),
      profile,
    ];
    await ref.read(mobileLocalStoreProvider).saveServerProfiles(next);
    if (password.isNotEmpty) {
      await ref.read(secureVaultProvider).saveServerPassword(
            serverId: profile.id,
            password: password,
          );
    }
    if (privateKey.isNotEmpty) {
      await ref.read(secureVaultProvider).saveServerPrivateKey(
            serverId: profile.id,
            privateKey: privateKey,
            passphrase: privateKeyPassphrase,
          );
    }
    state = AsyncData(next);
  }

  Future<void> removeProfile(String id) async {
    final current = state.valueOrNull ?? await future;
    final next = current.where((item) => item.id != id).toList();
    await ref.read(mobileLocalStoreProvider).saveServerProfiles(next);
    final vault = ref.read(secureVaultProvider);
    await Future.wait([
      vault.deleteServerPassword(id),
      vault.deleteServerPrivateKey(id),
    ]);
    state = AsyncData(next);
  }
}

class ServerCommandsController extends AsyncNotifier<List<ServerCommandEntry>> {
  @override
  Future<List<ServerCommandEntry>> build() {
    return ref.watch(mobileLocalStoreProvider).loadServerCommands();
  }

  Future<void> record(String command, {bool favorite = false}) async {
    final text = command.trim();
    if (text.isEmpty) return;
    final current = state.valueOrNull ?? await future;
    ServerCommandEntry? existing;
    for (final item in current) {
      if (item.command == text) {
        existing = item;
        break;
      }
    }
    final now = DateTime.now().toUtc();
    final entry = existing == null
        ? ServerCommandEntry(
            id: now.microsecondsSinceEpoch.toString(),
            command: text,
            label: _labelFor(text),
            favorite: favorite,
            createdAt: now,
            lastUsedAt: now,
          )
        : existing.copyWith(
            label: _labelFor(text),
            favorite: existing.favorite || favorite,
            lastUsedAt: now,
          );
    final next = [
      entry,
      ...current.where((item) => item.id != entry.id),
    ].take(80).toList();
    await _save(next);
  }

  Future<void> toggleFavorite(String id) async {
    final current = state.valueOrNull ?? await future;
    final next = [
      for (final item in current)
        if (item.id == id) item.copyWith(favorite: !item.favorite) else item,
    ];
    await _save(next);
  }

  Future<void> remove(String id) async {
    final current = state.valueOrNull ?? await future;
    await _save(current.where((item) => item.id != id).toList());
  }

  Future<void> _save(List<ServerCommandEntry> next) async {
    await ref.read(mobileLocalStoreProvider).saveServerCommands(next);
    state = AsyncData(next);
  }

  String _labelFor(String command) {
    final redacted = redactMobileSensitiveText(command);
    final first = redacted.split(RegExp(r'\s+')).take(3).join(' ');
    return first.length > 64 ? '${first.substring(0, 64)}...' : first;
  }
}

class SSHAnalysisController extends AsyncNotifier<MobileSSHAnalysis?> {
  @override
  Future<MobileSSHAnalysis?> build() async => null;

  Future<void> analyze(String output) async {
    final text = output.trim();
    if (text.isEmpty) return;
    await ref.read(sessionControllerProvider.future);
    final client = ref.read(apiClientProvider);
    if (client == null) {
      state = AsyncError(StateError('请先登录官方服务。'), StackTrace.current);
      return;
    }
    final redacted = redactMobileSensitiveText(text);
    state = const AsyncLoading();
    state = await AsyncValue.guard(() => client.analyzeSSHOutput(redacted));
  }
}
