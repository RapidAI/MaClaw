import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/notifications/mobile_notification_service.dart';

void main() {
  test('notification permission result explains granted permission', () {
    const result = MobileNotificationPermissionResult(androidGranted: true);

    expect(result.granted, isTrue);
    expect(result.message, contains('通知权限已开启'));
  });

  test('notification permission result explains denied permission', () {
    const result = MobileNotificationPermissionResult(
      androidGranted: false,
      iosGranted: false,
    );

    expect(result.granted, isFalse);
    expect(result.message, contains('系统设置'));
  });

  test(
      'notification permission result treats platforms without runtime prompt as ready',
      () {
    const result = MobileNotificationPermissionResult();

    expect(result.hasPlatformResult, isFalse);
    expect(result.granted, isTrue);
    expect(result.message, contains('无需额外通知授权'));
  });
}
