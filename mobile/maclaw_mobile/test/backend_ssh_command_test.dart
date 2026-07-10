import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/core/api/api_client.dart';
import 'package:maclaw_mobile/features/servers/servers_screen.dart';

void main() {
  test('backend SSH command payload trims input and appends carriage return',
      () {
    expect(
      backendSshCommandPayload('  systemctl status nginx --no-pager  '),
      'systemctl status nginx --no-pager\r',
    );
    expect(backendSshCommandPayload('   '), isNull);
  });

  test('backend SSH reconnect prefers the last active profile when available',
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

  test('backend SSH handoff id uses GUI agent backend session id only', () {
    expect(
      mobileBackendSessionHandoffId(
        const MobileBackendSSHSession(
          sessionId: 'hub-session-1',
          serverProfileId: 'srv-prod',
          backendSessionId: 'mobile-ssh:hub-session-1',
          status: 'connected',
        ),
      ),
      'mobile-ssh:hub-session-1',
    );

    expect(
      mobileBackendSessionHandoffId(
        const MobileBackendSSHSession(
          sessionId: 'hub-session-2',
          serverProfileId: 'srv-prod',
          status: 'connected',
        ),
        fallback: 'mobile-ssh:previous',
      ),
      'mobile-ssh:previous',
    );

    expect(
      mobileBackendSessionHandoffId(
        const MobileBackendSSHSession(
          sessionId: 'hub-session-3',
          serverProfileId: 'srv-prod',
          status: 'queued',
        ),
      ),
      isNull,
    );
  });
  test('backend SSH claimed-by handoff uses GUI agent worker identity only',
      () {
    expect(
      mobileBackendSessionClaimedBy(
        const MobileBackendSSHSession(
          sessionId: 'hub-session-1',
          serverProfileId: 'srv-prod',
          status: 'connected',
          claimedBy: 'desktop-agent-1',
        ),
      ),
      'desktop-agent-1',
    );

    expect(
      mobileBackendSessionClaimedBy(
        const MobileBackendSSHSession(
          sessionId: 'hub-session-1',
          serverProfileId: 'srv-prod',
          status: 'connected',
        ),
        fallback: 'desktop-agent-previous',
      ),
      'desktop-agent-previous',
    );

    expect(
      mobileBackendSessionClaimedBy(
        const MobileBackendSSHSession(
          sessionId: 'hub-session-1',
          serverProfileId: 'srv-prod',
          status: 'queued',
        ),
      ),
      isNull,
    );
  });
}
