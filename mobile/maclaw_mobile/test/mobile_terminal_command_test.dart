import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/servers/servers_screen.dart';

void main() {
  test(
      'mobile terminal command payload trims input and appends carriage return',
      () {
    expect(
      mobileTerminalCommandPayload('  systemctl status nginx --no-pager  '),
      'systemctl status nginx --no-pager\r',
    );
    expect(mobileTerminalCommandPayload('   '), isNull);
  });

  test('mobile ssh reconnect prefers the last active profile when available',
      () {
    expect(
      mobileSshReconnectProfileId(
        selectedId: 'staging',
        activeProfileId: 'prod',
        availableProfileIds: const ['prod', 'staging'],
      ),
      'prod',
    );
    expect(
      mobileSshReconnectProfileId(
        selectedId: 'staging',
        activeProfileId: 'missing',
        availableProfileIds: const ['prod', 'staging'],
      ),
      'staging',
    );
    expect(
      mobileSshReconnectProfileId(
        selectedId: 'missing',
        activeProfileId: null,
        availableProfileIds: const ['prod'],
      ),
      isNull,
    );
  });
}
