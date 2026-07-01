import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee.dart';

void main() {
  test('parses digital employee status', () {
    final employee = DigitalEmployee.fromJson({
      'id': 've-1',
      'machine_id': 'srv-1',
      'name': 'Server Assistant',
      'skill_description': 'ops',
      'online_status': 'online',
      'access_policy': 'per_request',
      'resident': true,
    });

    expect(employee.id, 've-1');
    expect(employee.online, isTrue);
    expect(employee.accessPolicy, 'per_request');
  });
}

