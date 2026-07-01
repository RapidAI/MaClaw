import 'package:flutter_secure_storage/flutter_secure_storage.dart';

class SecureVault {
  static const _tokenKey = 'maclaw.viewer_token';
  static const _hubUrlKey = 'maclaw.hub_url';
  static const _sshPasswordPrefix = 'maclaw.ssh.password.';
  static const _sshPrivateKeyPrefix = 'maclaw.ssh.private_key.';
  static const _sshPrivateKeyPassphrasePrefix =
      'maclaw.ssh.private_key_passphrase.';
  final FlutterSecureStorage _storage;

  const SecureVault({
    FlutterSecureStorage storage = const FlutterSecureStorage(),
  }) : _storage = storage;

  Future<void> saveSession({required String hubUrl, required String token}) {
    return Future.wait([
      _storage.write(key: _hubUrlKey, value: hubUrl),
      _storage.write(key: _tokenKey, value: token),
    ]);
  }

  Future<String?> readHubUrl() => _storage.read(key: _hubUrlKey);

  Future<String?> readToken() => _storage.read(key: _tokenKey);

  Future<void> clearSession() {
    return Future.wait([
      _storage.delete(key: _hubUrlKey),
      _storage.delete(key: _tokenKey),
    ]);
  }

  Future<void> saveServerPassword({
    required String serverId,
    required String password,
  }) {
    return _storage.write(key: '$_sshPasswordPrefix$serverId', value: password);
  }

  Future<String?> readServerPassword(String serverId) {
    return _storage.read(key: '$_sshPasswordPrefix$serverId');
  }

  Future<void> deleteServerPassword(String serverId) {
    return _storage.delete(key: '$_sshPasswordPrefix$serverId');
  }

  Future<void> saveServerPrivateKey({
    required String serverId,
    required String privateKey,
    String passphrase = '',
  }) {
    return Future.wait([
      _storage.write(key: '$_sshPrivateKeyPrefix$serverId', value: privateKey),
      if (passphrase.isNotEmpty)
        _storage.write(
          key: '$_sshPrivateKeyPassphrasePrefix$serverId',
          value: passphrase,
        )
      else
        _storage.delete(key: '$_sshPrivateKeyPassphrasePrefix$serverId'),
    ]);
  }

  Future<String?> readServerPrivateKey(String serverId) {
    return _storage.read(key: '$_sshPrivateKeyPrefix$serverId');
  }

  Future<String?> readServerPrivateKeyPassphrase(String serverId) {
    return _storage.read(key: '$_sshPrivateKeyPassphrasePrefix$serverId');
  }

  Future<void> deleteServerPrivateKey(String serverId) {
    return Future.wait([
      _storage.delete(key: '$_sshPrivateKeyPrefix$serverId'),
      _storage.delete(key: '$_sshPrivateKeyPassphrasePrefix$serverId'),
    ]);
  }
}
