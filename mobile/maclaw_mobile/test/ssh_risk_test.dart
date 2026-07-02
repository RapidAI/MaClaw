import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/servers/ssh_risk.dart';

void main() {
  test('classifies high risk server commands', () {
    expect(classifyCommandRisk('ls -la'), CommandRisk.normal);
    expect(classifyCommandRisk('rm app.log'), CommandRisk.caution);
    expect(
      classifyCommandRisk('sudo systemctl restart nginx'),
      CommandRisk.dangerous,
    );
    expect(classifyCommandRisk('rm -rf /'), CommandRisk.dangerous);
  });

  test('flags destructive mobile maintenance variants as dangerous', () {
    final dangerous = [
      'sudo rm -rf /var/lib/mysql',
      'sudo rm -fr /var/lib/mysql',
      'rm -r -f -- /etc',
      'sudo rm --one-file-system -Rf /home/app',
      'rm -fR /opt/app',
      'rm -rf /*',
      'sudo dd if=/dev/zero of=/dev/sda bs=1M',
      'sudo iptables -F',
      'ufw disable',
      'sudo chmod -R 777 /',
      'sudo chown -R nobody:nogroup /etc',
      'docker rm -f api',
      'mkfs.ext4 /dev/sdb',
      'systemctl stop postgresql',
    ];

    for (final command in dangerous) {
      expect(
        classifyCommandRisk(command),
        CommandRisk.dangerous,
        reason: command,
      );
    }
  });

  test('keeps read-only diagnostics as normal commands', () {
    final normal = [
      'systemctl status nginx --no-pager',
      'journalctl -u nginx -n 100 --no-pager',
      'docker ps --format "table {{.Names}}\\t{{.Status}}"',
      'df -h && free -m',
      'tail -n 120 /var/log/syslog',
    ];

    for (final command in normal) {
      expect(classifyCommandRisk(command), CommandRisk.normal, reason: command);
    }
  });

  test('classifies reversible changes as caution', () {
    final caution = [
      'sudo systemctl reload nginx',
      'docker restart api',
      'iptables -L',
      'chmod 640 app.log',
      'mv app.log app.log.bak',
    ];

    for (final command in caution) {
      expect(
        classifyCommandRisk(command),
        CommandRisk.caution,
        reason: command,
      );
    }
  });
}
