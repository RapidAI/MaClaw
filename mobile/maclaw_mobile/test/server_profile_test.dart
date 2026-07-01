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
      authMode: 'password',
    );
    expect(valid.isValid, isTrue);

    const invalid = ServerProfile(
      id: 'srv-2',
      name: 'broken',
      host: '',
      port: 0,
      username: '',
      authMode: 'password',
    );
    expect(invalid.isValid, isFalse);
  });

  test('round trips server profile json without secrets', () {
    const profile = ServerProfile(
      id: 'srv-1',
      name: 'prod',
      host: '10.0.0.8',
      port: 22,
      username: 'ops',
      authMode: 'password',
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
    expect(profile.toJson().containsKey('password'), isFalse);
  });
}
