import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/desktop_llm_qr.dart';
import 'package:maclaw_mobile/features/account/llm_qr_authorization_screen.dart';

void main() {
  test('desktop GUI authorization errors redact upstream URLs', () {
    expect(
      friendlyDesktopLlmAuthorizationError(
        StateError('upstream https://tenant-a.maclaw.top failed'),
      ),
      '桌面 GUI 授权服务暂时不可用，请确认二维码有效后重试。',
    );
    expect(
      friendlyDesktopLlmAuthorizationError(StateError('session expired')),
      '桌面 GUI 授权二维码已过期，请回到电脑端重新生成。',
    );
  });

  test('accepts an active desktop GUI authorization session', () {
    final payload = parseMaclawDesktopLlmQrPayload(
      '{"v":2,"type":"maclaw_mobile_llm_authorization",'
      '"session_id":"mlqr_active",'
      '"hub_url":"https://tenant-a.maclaw.top",'
      '"expires_at":"2099-01-01T00:00:00Z"}',
    );

    expect(payload.sessionId, 'mlqr_active');
    expect(payload.hubUrl, 'https://tenant-a.maclaw.top');
    expect(payload.expiresAt, DateTime.utc(2099));
  });

  test('rejects an expired desktop GUI authorization session locally', () {
    expect(
      () => parseMaclawDesktopLlmQrPayload(
        '{"v":2,"type":"maclaw_mobile_llm_authorization",'
        '"session_id":"mlqr_expired",'
        '"hub_url":"https://tenant-a.maclaw.top",'
        '"expires_at":"2020-01-01T00:00:00Z"}',
      ),
      throwsA(
        isA<FormatException>().having(
          (error) => error.message,
          'message',
          contains('expired'),
        ),
      ),
    );
  });

  test('rejects malformed QR expiry before authorization', () {
    expect(
      () => parseMaclawDesktopLlmQrPayload(
        '{"v":2,"type":"maclaw_mobile_llm_authorization",'
        '"session_id":"mlqr_bad_expiry",'
        '"hub_url":"https://tenant-a.maclaw.top",'
        '"expires_at":"soon"}',
      ),
      throwsA(
        isA<FormatException>().having(
          (error) => error.message,
          'message',
          contains('ISO-8601'),
        ),
      ),
    );
  });
}
