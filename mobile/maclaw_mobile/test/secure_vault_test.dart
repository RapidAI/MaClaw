import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/storage/secure_vault.dart';

void main() {
  setUp(() {
    FlutterSecureStorage.setMockInitialValues({});
  });

  test('clears session token and official service metadata', () async {
    const vault = SecureVault();

    await vault.saveSession(
      hubUrl: 'https://hubs.mypapers.top',
      token: 'mobile-token',
    );

    expect(await vault.readHubUrl(), 'https://hubs.mypapers.top');
    expect(await vault.readToken(), 'mobile-token');

    await vault.clearSession();

    expect(await vault.readHubUrl(), isNull);
    expect(await vault.readToken(), isNull);
  });

  test('deletes SSH password, private key, and private key passphrase',
      () async {
    const vault = SecureVault();

    await vault.saveServerPassword(
      serverId: 'srv-secure',
      password: 'ssh-password',
    );
    await vault.saveServerPrivateKey(
      serverId: 'srv-secure',
      privateKey: '-----BEGIN KEY-----',
      passphrase: 'key-passphrase',
    );

    expect(await vault.readServerPassword('srv-secure'), 'ssh-password');
    expect(
      await vault.readServerPrivateKey('srv-secure'),
      '-----BEGIN KEY-----',
    );
    expect(
      await vault.readServerPrivateKeyPassphrase('srv-secure'),
      'key-passphrase',
    );

    await vault.deleteServerPassword('srv-secure');
    await vault.deleteServerPrivateKey('srv-secure');

    expect(await vault.readServerPassword('srv-secure'), isNull);
    expect(await vault.readServerPrivateKey('srv-secure'), isNull);
    expect(await vault.readServerPrivateKeyPassphrase('srv-secure'), isNull);
  });

  test('saving a private key without passphrase removes old passphrase',
      () async {
    const vault = SecureVault();

    await vault.saveServerPrivateKey(
      serverId: 'srv-rotate',
      privateKey: 'old-key',
      passphrase: 'old-passphrase',
    );
    await vault.saveServerPrivateKey(
      serverId: 'srv-rotate',
      privateKey: 'new-key',
    );

    expect(await vault.readServerPrivateKey('srv-rotate'), 'new-key');
    expect(await vault.readServerPrivateKeyPassphrase('srv-rotate'), isNull);
  });
}
