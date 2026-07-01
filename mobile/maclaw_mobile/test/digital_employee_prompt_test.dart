import 'package:flutter_test/flutter_test.dart';
import 'package:maclaw_mobile/features/digital_employees/digital_employee_prompt.dart';

void main() {
  test('round trips digital employee prompt json', () {
    final entry = DigitalEmployeePromptEntry(
      id: 'prompt-1',
      employeeId: 'employee-1',
      prompt: 'check remote server status',
      createdAt: DateTime.utc(2026, 7, 1),
    );

    final restored = DigitalEmployeePromptEntry.fromJson(entry.toJson());

    expect(restored.id, entry.id);
    expect(restored.employeeId, entry.employeeId);
    expect(restored.prompt, entry.prompt);
    expect(restored.createdAt, entry.createdAt);
  });

  test('parses missing fields safely', () {
    final entry = DigitalEmployeePromptEntry.fromJson({});

    expect(entry.id, '');
    expect(entry.employeeId, '');
    expect(entry.prompt, '');
    expect(entry.createdAt, DateTime.fromMillisecondsSinceEpoch(0));
  });
}
