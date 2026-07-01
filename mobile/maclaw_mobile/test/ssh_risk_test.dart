import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/servers/ssh_risk.dart';

void main() {
  test('classifies high risk server commands', () {
    expect(classifyCommandRisk('ls -la'), CommandRisk.normal);
    expect(classifyCommandRisk('rm app.log'), CommandRisk.caution);
    expect(classifyCommandRisk('sudo systemctl restart nginx'), CommandRisk.dangerous);
    expect(classifyCommandRisk('rm -rf /'), CommandRisk.dangerous);
  });
}

