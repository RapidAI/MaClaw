import 'package:flutter_test/flutter_test.dart';
import 'package:dio/dio.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/core/api/official_service.dart';
import 'package:maclaw_mobile/features/auth/auth_service.dart';

void main() {
  test('mobile app targets only the MaClaw official service', () {
    expect(maclawOfficialServiceUrl, 'https://hubs.mypapers.top');
    expect(maclawOfficialServiceUrl.startsWith('https://'), isTrue);
  });

  test('api client resolves relative paths against official service', () {
    final client = ApiClient();

    expect(
      client.absoluteUrl('/api/mobile/bootstrap'),
      'https://hubs.mypapers.top/api/mobile/bootstrap',
    );
  });

  test('api client accepts only official absolute URLs', () {
    final client = ApiClient();

    expect(
      client.absoluteUrl('https://hubs.mypapers.top/api/mobile/bootstrap'),
      'https://hubs.mypapers.top/api/mobile/bootstrap',
    );
    expect(
      () => client.absoluteUrl('https://example.invalid/api/mobile/bootstrap'),
      throwsUnsupportedError,
    );
  });

  test('api client rejects non-official service clients', () {
    expect(
      () => ApiClient(
        dio: Dio(BaseOptions(baseUrl: 'https://example.invalid')),
      ),
      throwsUnsupportedError,
    );
  });

  test('auth service rejects non-official service clients', () {
    expect(
      () => AuthService(
        dio: Dio(BaseOptions(baseUrl: 'https://example.invalid')),
      ),
      throwsUnsupportedError,
    );
  });
}
