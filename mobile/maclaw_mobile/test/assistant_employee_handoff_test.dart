import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:maclaw_mobile/features/assistant/assistant_employee_handoff.dart';

void main() {
  test('buildAssistantEmployeeHandoffPrompt includes query and answer', () {
    final prompt = buildAssistantEmployeeHandoffPrompt(
      query: 'nginx 502 怎么办',
      answer: '结论：先看 upstream。',
    );
    expect(prompt, contains('AI 助手结论'));
    expect(prompt, contains('【用户问题】'));
    expect(prompt, contains('nginx 502 怎么办'));
    expect(prompt, contains('【助手结论】'));
    expect(prompt, contains('结论：先看 upstream。'));
    expect(prompt, contains('人工确认'));
  });

  test('buildAssistantEmployeeHandoffPrompt redacts secrets', () {
    final prompt = buildAssistantEmployeeHandoffPrompt(
      query: 'password: SuperSecretValue123!',
      answer: 'Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789',
    );
    expect(prompt, isNot(contains('SuperSecretValue123!')));
    expect(prompt, isNot(contains('abcdefghijklmnopqrstuvwxyz0123456789')));
    expect(prompt, contains('[REDACTED'));
  });

  test('buildAssistantEmployeeHandoffPrompt truncates very long answer', () {
    final long = 'x' * 5000;
    final prompt = buildAssistantEmployeeHandoffPrompt(query: 'q', answer: long);
    expect(prompt.length, lessThan(long.length + 200));
    expect(prompt, contains('…'));
  });

  test('assistantEmployeeHandoffProvider stores task draft', () {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    container.read(assistantEmployeeHandoffProvider.notifier).state =
        AssistantEmployeeHandoff(
      query: '状态',
      answer: '正常',
      createdAt: DateTime.utc(2026, 7, 11),
    );
    final handoff = container.read(assistantEmployeeHandoffProvider);
    expect(handoff, isNotNull);
    expect(handoff!.taskPrompt, contains('状态'));
    expect(handoff.taskPrompt, contains('正常'));
  });
}
