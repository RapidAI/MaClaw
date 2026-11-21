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
        () => parseDesktopAuthQrPayload(
          _payload(type: 'maclaw_mobile_llm_authorization'),
        ),
        throwsFormatException,
      );
    });

    test('rejects expired sessions', () {
      expect(
        () => parseDesktopAuthQrPayload(
          _payload(
            expiresAt: DateTime.now()
                .toUtc()
                .subtract(const Duration(minutes: 1))
                .toIso8601String(),
          ),
        ),
        throwsFormatException,
      );
    });

    test('rejects non-https hub urls', () {
      expect(
        () => parseDesktopAuthQrPayload(
          _payload().replaceAll(
            'https://hub.example.com',
            'http://hub.example.com',
          ),
        ),
        throwsFormatException,
      );
    });

    test('rejects non-JSON and empty input', () {
      expect(() => parseDesktopAuthQrPayload(''), throwsFormatException);
      expect(
        () => parseDesktopAuthQrPayload('not json'),
        throwsFormatException,
      );
      expect(() => parseDesktopAuthQrPayload('[1,2]'), throwsFormatException);
    });

    test('does not accept third-party authorize QR type', () {
      expect(
        () => parseDesktopAuthQrPayload(_payload(type: thirdPartyAuthQrType)),
        throwsFormatException,
      );
    });
  });

  group('parseThirdPartyAuthQrPayload', () {
    test('accepts a live third-party authorization QR', () {
      final payload = parseThirdPartyAuthQrPayload(
        _payload(type: thirdPartyAuthQrType).replaceFirst(
          '"hub_url"',
          '"name":"RapidEditor AI","hub_url"',
        ),
      );
      expect(payload.sessionId, 'maqr_abc123');
      expect(payload.hubUrl, 'https://hub.example.com');
      expect(payload.clientName, 'RapidEditor AI');
      expect(payload.expiresAt, isNotNull);
    });

    test('rejects the desktop login QR type', () {
      expect(
        () => parseThirdPartyAuthQrPayload(_payload()),
        throwsFormatException,
      );
    });

    test('rejects expired third-party QR', () {
      expect(
        () => parseThirdPartyAuthQrPayload(
          _payload(
            type: thirdPartyAuthQrType,
            expiresAt: DateTime.now()
                .toUtc()
                .subtract(const Duration(minutes: 1))
                .toIso8601String(),
          ),
        ),
        throwsFormatException,
      );
    });

    test('accepts a HubCenter relay QR with center_url and no hub_url', () {
      final qr =
          '{"v":2,"type":"$thirdPartyAuthQrType","session_id":"tpr_relay1",'
          '"center_url":"https://hubs.mypapers.top",'
          '"expires_at":"${DateTime.now().toUtc().add(const Duration(minutes: 5)).toIso8601String()}"}';
      final payload = parseThirdPartyAuthQrPayload(qr);
      expect(payload.sessionId, 'tpr_relay1');
      expect(payload.hubUrl, isEmpty);
      expect(payload.centerUrl, 'https://hubs.mypapers.top');
      expect(payload.isRelay, isTrue);
    });

    test('legacy hub_url QR is not relay', () {
      final payload = parseThirdPartyAuthQrPayload(
        _payload(type: thirdPartyAuthQrType),
      );
      expect(payload.isRelay, isFalse);
      expect(payload.hubUrl, 'https://hub.example.com');
    });

    test('rejects third-party QR with neither hub_url nor center_url', () {
      final qr =
          '{"v":2,"type":"$thirdPartyAuthQrType","session_id":"maqr_tp789"}';
      expect(() => parseThirdPartyAuthQrPayload(qr), throwsFormatException);
    });

    test('rejects non-https center_url', () {
      final qr =
          '{"v":2,"type":"$thirdPartyAuthQrType","session_id":"tpr_relay2",'
          '"center_url":"http://hubs.mypapers.top"}';
      expect(() => parseThirdPartyAuthQrPayload(qr), throwsFormatException);
    });
  });
}
