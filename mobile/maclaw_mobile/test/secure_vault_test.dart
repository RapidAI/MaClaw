import 'dart:io';

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

  test('clears legacy SSH password, private key, and passphrase residue',
      () async {
    FlutterSecureStorage.setMockInitialValues({
      'maclaw.ssh.password.srv-secure': 'ssh-password',
      'maclaw.ssh.private_key.srv-secure': '-----BEGIN KEY-----',
      'maclaw.ssh.private_key_passphrase.srv-secure': 'key-passphrase',
    });
    const vault = SecureVault();
    const storage = FlutterSecureStorage();

    expect(
      await storage.read(key: 'maclaw.ssh.password.srv-secure'),
      'ssh-password',
    );
    expect(
      await storage.read(key: 'maclaw.ssh.private_key.srv-secure'),
      '-----BEGIN KEY-----',
    );
    expect(
      await storage.read(
        key: 'maclaw.ssh.private_key_passphrase.srv-secure',
      ),
      'key-passphrase',
    );

    await vault.clearLegacyServerCredentials('srv-secure');

    expect(await storage.read(key: 'maclaw.ssh.password.srv-secure'), isNull);
    expect(
      await storage.read(key: 'maclaw.ssh.private_key.srv-secure'),
      isNull,
    );
    expect(
      await storage.read(
        key: 'maclaw.ssh.private_key_passphrase.srv-secure',
      ),
      isNull,
    );
  });

  test('does not expose phone-side SSH credential save or read APIs', () {
    final source =
        File('lib/core/storage/secure_vault.dart').readAsStringSync();

    for (final forbidden in [
      'saveServerPassword',
      'readServerPassword',
      'saveServerPrivateKey',
      'readServerPrivateKey',
      'readServerPrivateKeyPassphrase',
    ]) {
      expect(source, isNot(contains(forbidden)));
    }
    expect(source, contains('clearLegacyServerCredentials'));
  });
}
