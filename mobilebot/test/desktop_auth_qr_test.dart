import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/features/auth/desktop_auth_qr.dart';

String _payload({String type = desktopAuthQrType, String? expiresAt}) {
  return '{"v":2,"type":"$type","session_id":"maqr_abc123",'
      '"hub_url":"https://hub.example.com",'
      '"expires_at":"${expiresAt ?? DateTime.now().toUtc().add(const Duration(minutes: 5)).toIso8601String()}"}';
}

void main() {
  group('parseDesktopAuthQrPayload', () {
    test('accepts a live desktop auth QR', () {
      final payload = parseDesktopAuthQrPayload(_payload());
      expect(payload.sessionId, 'maqr_abc123');
      expect(payload.hubUrl, 'https://hub.example.com');
      expect(payload.expiresAt, isNotNull);
    });

    test('rejects non-auth QR types', () {
      expect(
        () => parseDesktopAuthQrPayload(_payload(type: 'maclaw_mobile_llm_authorization')),
        throwsFormatException,
      );
    });

    test('rejects expired sessions', () {
      expect(
        () => parseDesktopAuthQrPayload(
          _payload(expiresAt: DateTime.now().toUtc().subtract(const Duration(minutes: 1)).toIso8601String()),
        ),
        throwsFormatException,
      );
    });

    test('rejects non-https hub urls', () {
      expect(
        () => parseDesktopAuthQrPayload(
          _payload().replaceAll('https://hub.example.com', 'http://hub.example.com'),
        ),
        throwsFormatException,
      );
    });

    test('rejects non-JSON and empty input', () {
      expect(() => parseDesktopAuthQrPayload(''), throwsFormatException);
      expect(() => parseDesktopAuthQrPayload('not json'), throwsFormatException);
      expect(() => parseDesktopAuthQrPayload('[1,2]'), throwsFormatException);
    });
  });
}
