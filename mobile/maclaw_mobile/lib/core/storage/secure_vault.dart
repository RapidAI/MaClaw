import 'package:flutter_secure_storage/flutter_secure_storage.dart';

class SecureVault {
  static const _tokenKey = 'maclaw.viewer_token';
  static const _hubUrlKey = 'maclaw.hub_url';
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
}

