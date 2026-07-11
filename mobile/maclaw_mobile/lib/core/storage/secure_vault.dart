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

  Future<void> clearLegacyServerCredentials(String serverId) {
    return Future.wait([
      _storage.delete(key: '$_sshPasswordPrefix$serverId'),
      _storage.delete(key: '$_sshPrivateKeyPrefix$serverId'),
      _storage.delete(key: '$_sshPrivateKeyPassphrasePrefix$serverId'),
    ]);
  }

  /// Generic secure read (push device identity, non-auth secrets).
  Future<String?> readKey(String key) {
    final k = key.trim();
    if (k.isEmpty) return Future.value(null);
    return _storage.read(key: k);
  }

  Future<void> writeKey(String key, String value) {
    final k = key.trim();
    if (k.isEmpty) return Future.value();
    return _storage.write(key: k, value: value);
  }
}
