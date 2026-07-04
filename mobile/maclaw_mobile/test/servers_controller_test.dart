import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/mobile_bootstrap.dart';
import 'package:maclaw_mobile/core/storage/mobile_local_store.dart';
import 'package:maclaw_mobile/core/storage/secure_vault.dart';
import 'package:maclaw_mobile/features/auth/session_controller.dart';
import 'package:maclaw_mobile/features/servers/server_command.dart';
import 'package:maclaw_mobile/features/servers/server_profile.dart';
import 'package:maclaw_mobile/features/servers/servers_controller.dart';

class _FakeMobileLocalStore extends MobileLocalStore {
  List<ServerProfile> profiles;
  List<ServerCommandEntry> commands;

  _FakeMobileLocalStore(this.profiles) : commands = const [];

  @override
  Future<List<ServerProfile>> loadServerProfiles() async => profiles;

  @override
  Future<void> saveServerProfiles(List<ServerProfile> profiles) async {
    this.profiles = profiles;
  }

  @override
  Future<List<ServerCommandEntry>> loadServerCommands() async => commands;

  @override
  Future<void> saveServerCommands(List<ServerCommandEntry> commands) async {
    this.commands = commands;
  }
}

class _RecordingSecureVault extends SecureVault {
  final deletedPasswords = <String>[];
  final deletedPrivateKeys = <String>[];
  final savedPasswords = <String, String>{};
  final savedPrivateKeys = <String, String>{};

  @override
  Future<void> saveServerPassword({
    required String serverId,
    required String password,
  }) async {
    savedPasswords[serverId] = password;
  }

  @override
  Future<void> saveServerPrivateKey({
    required String serverId,
    required String privateKey,
    String passphrase = '',
  }) async {
    savedPrivateKeys[serverId] = privateKey;
  }

  @override
  Future<void> deleteServerPassword(String serverId) async {
    deletedPasswords.add(serverId);
  }

  @override
  Future<void> deleteServerPrivateKey(String serverId) async {
    deletedPrivateKeys.add(serverId);
  }
}

class _SignedInSessionController extends SessionController {
  @override
  Future<SessionState> build() async => SessionState.signedIn(
        hubUrl: 'https://tenant-a.maclaw.top',
        bootstrap: MobileBootstrap.fromJson({
          'user': {
            'user_id': 'u1',
            'phone_number': '19900001111',
            'tenant_id': 'tenant-a',
          },
          'services': {
            'hub_status': 'online',
            'llm_status': 'available',
            'search_status': 'available',
            'documents_status': 'available',
            'digital_employees_status': 'available',
            'search_path': '/api/mobile/search',
            'documents_path': '/api/mobile/documents',
            'digital_employees_path': '/api/mobile/digital-employees',
            'realtime_path': '/api/mobile/realtime',
          },
          'llm_access': {
            'mode': 'maclaw_official',
            'status': 'available',
            'credits_account': 'phone:19900001111',
          },
        }),
      );
}

class _RecordingApiClient extends ApiClient {
  String? analyzedOutput;

  _RecordingApiClient() : super(hubUrl: 'https://tenant-a.maclaw.top');

  @override
  Future<MobileSSHAnalysis> analyzeSSHOutput(String output) async {
    analyzedOutput = output;
    return const MobileSSHAnalysis(
      summary: 'summary',
      recommendation: 'recommendation',
      commandDraft: 'systemctl status app',
    );
  }
}

void main() {
  test('invalid server profile is rejected before storage or credential writes',
      () async {
    final store = _FakeMobileLocalStore(const []);
    final vault = _RecordingSecureVault();
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
        secureVaultProvider.overrideWithValue(vault),
      ],
    );
    addTearDown(container.dispose);

    await container.read(serverProfilesProvider.future);

    await expectLater(
      container.read(serverProfilesProvider.notifier).addProfile(
            const ServerProfile(
              id: 'srv-invalid',
              name: 'invalid',
              host: '',
              port: 70000,
              username: '',
              authMode: serverAuthModePassword,
            ),
            password: 'should-not-save',
            privateKey: 'should-not-save-key',
          ),
      throwsA(isA<ArgumentError>()),
    );

    expect(store.profiles, isEmpty);
    expect(vault.savedPasswords, isEmpty);
    expect(vault.savedPrivateKeys, isEmpty);
    expect(container.read(serverProfilesProvider).valueOrNull, isEmpty);
  });

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

  test('server command history redacts labels but preserves executable command',
      () async {
    final store = _FakeMobileLocalStore(const []);
    final container = ProviderContainer(
      overrides: [
        mobileLocalStoreProvider.overrideWithValue(store),
      ],
    );
    addTearDown(container.dispose);

    await container.read(serverCommandsProvider.future);

    const command = 'deploy password=prod-token';
    await container.read(serverCommandsProvider.notifier).record(
          command,
          favorite: true,
        );

    expect(store.commands, hasLength(1));
    expect(store.commands.single.command, command);
    expect(store.commands.single.command, contains('prod-token'));
    expect(store.commands.single.label, contains('password=[REDACTED_SECRET]'));
    expect(store.commands.single.label, isNot(contains('prod-token')));
    expect(store.commands.single.favorite, isTrue);
  });

  test('SSH AI analysis waits for session and redacts output before API call',
      () async {
    final api = _RecordingApiClient();
    final container = ProviderContainer(
      overrides: [
        sessionControllerProvider.overrideWith(_SignedInSessionController.new),
        apiClientProvider.overrideWithValue(api),
      ],
    );
    addTearDown(container.dispose);
    await container.read(sshAnalysisProvider.future);

    await container.read(sshAnalysisProvider.notifier).analyze(
          'Authorization: Bearer raw-token\n'
          'password=prod-password\n'
          'https://admin:pass@example.com',
        );

    expect(
      api.analyzedOutput,
      contains('Authorization: Bearer [REDACTED_TOKEN]'),
    );
    expect(api.analyzedOutput, contains('password=[REDACTED_SECRET]'));
    expect(
      api.analyzedOutput,
      contains('https://[REDACTED_CREDENTIALS]@example.com'),
    );
    expect(api.analyzedOutput, isNot(contains('raw-token')));
    expect(api.analyzedOutput, isNot(contains('prod-password')));
    expect(api.analyzedOutput, isNot(contains('admin:pass')));
    expect(
      container.read(sshAnalysisProvider).valueOrNull?.commandDraft,
      'systemctl status app',
    );
  });
}
