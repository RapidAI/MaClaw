import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/servers/server_profile.dart';

void main() {
  test('server profile requires host, port, and user', () {
    const valid = ServerProfile(
      id: 'srv-1',
      name: 'prod',
      host: '10.0.0.8',
      port: 22,
      username: 'ops',
      authMode: serverAuthModePassword,
    );
    expect(valid.isValid, isTrue);

    const invalid = ServerProfile(
      id: 'srv-2',
      name: 'broken',
      host: '',
      port: 0,
      username: '',
      authMode: serverAuthModePassword,
    );
    expect(invalid.isValid, isFalse);

    const invalidHighPort = ServerProfile(
      id: 'srv-3',
      name: 'broken-high-port',
      host: '10.0.0.8',
      port: 70000,
      username: 'ops',
      authMode: serverAuthModePassword,
    );
    expect(invalidHighPort.isValid, isFalse);
  });

  test('round trips server profile json without secrets', () {
    const profile = ServerProfile(
      id: 'srv-1',
      name: 'prod',
      host: '10.0.0.8',
      port: 22,
      username: 'ops',
      authMode: serverAuthModePassword,
      tag: 'ops',
      note: 'primary',
    );

    final restored = ServerProfile.fromJson(profile.toJson());

    expect(restored.id, profile.id);
    expect(restored.name, profile.name);
    expect(restored.host, profile.host);
    expect(restored.port, profile.port);
    expect(restored.username, profile.username);
    expect(restored.authMode, profile.authMode);
    expect(serverAuthModeLabel(restored.authMode), '密码');
    expect(profile.toJson().containsKey('password'), isFalse);
  });

  test('supports private key auth metadata without storing secrets', () {
    const profile = ServerProfile(
      id: 'srv-key',
      name: 'jump',
      host: 'jump.example.com',
      port: 22,
      username: 'ops',
      authMode: serverAuthModePrivateKey,
    );

    final json = profile.toJson();
    final restored = ServerProfile.fromJson(json);

    expect(restored.authMode, serverAuthModePrivateKey);
    expect(serverAuthModeLabel(restored.authMode), '私钥');
    expect(json.containsKey('private_key'), isFalse);
    expect(json.containsKey('private_key_passphrase'), isFalse);
  });
}
