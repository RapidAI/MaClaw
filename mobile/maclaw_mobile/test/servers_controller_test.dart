import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/core/storage/secure_vault.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/servers/server_command.dart';
import 'package:maclaw_mobile/features/servers/server_profile.dart';
import 'package:maclaw_mobile/features/servers/servers_controller.dart';

class _FakeMobileLocalStore extends MobileLocalStore {
  List<ServerProfile> profiles;

  _FakeMobileLocalStore(this.profiles);

  @override
  Future<List<ServerProfile>> loadServerProfiles() async => profiles;

  @override
  Future<void> saveServerProfiles(List<ServerProfile> profiles) async {
    this.profiles = profiles;
  }

  @override
  Future<List<ServerCommandEntry>> loadServerCommands() async => const [];
}

class _RecordingSecureVault extends SecureVault {
  final deletedPasswords = <String>[];
  final deletedPrivateKeys = <String>[];

  @override
  Future<void> deleteServerPassword(String serverId) async {
    deletedPasswords.add(serverId);
  }

  @override
  Future<void> deleteServerPrivateKey(String serverId) async {
    deletedPrivateKeys.add(serverId);
  }
}

void main() {
  test('removing a server profile clears its SSH credentials', () async {
    final store = _FakeMobileLocalStore(
      const [
        ServerProfile(
          id: 'srv-delete',
          name: 'prod',
          host: '10.0.0.8',
          port: 22,
          username: 'ops',
          authMode: serverAuthModePassword,
        ),
        ServerProfile(
          id: 'srv-keep',
          name: 'jump',
          host: '10.0.0.9',
          port: 22,
          username: 'root',
          authMode: serverAuthModePrivateKey,
        ),
      ],
    );
    final vault = _RecordingSecureVault();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        secureVaultProvider.overrideWithValue(vault),
      ],
    );
    addTearDown(container.dispose);

    await container.read(serverProfilesProvider.future);
    await container
        .read(serverProfilesProvider.notifier)
        .removeProfile('srv-delete');

    expect(store.profiles.map((profile) => profile.id), ['srv-keep']);
    expect(vault.deletedPasswords, ['srv-delete']);
    expect(vault.deletedPrivateKeys, ['srv-delete']);
    expect(
      container.read(serverProfilesProvider).valueOrNull?.single.id,
      'srv-keep',
    );
  });
}
