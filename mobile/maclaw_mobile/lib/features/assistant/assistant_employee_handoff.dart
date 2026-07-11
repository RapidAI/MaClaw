import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/security/mobile_redaction.dart';

/// Pending task draft handed from AI assistant to the digital-employees tab.
class AssistantEmployeeHandoff {
  final String query;
  final String answer;
  final DateTime createdAt;

  const AssistantEmployeeHandoff({
    required this.query,
    required this.answer,
    required this.createdAt,
  });

  /// Prompt prefilled into the employee task sheet.
  String get taskPrompt => buildAssistantEmployeeHandoffPrompt(
        query: query,
        answer: answer,
      );
}

String _clip(String text, int maxRunes) {
  final runes = text.runes.toList();
  if (runes.length <= maxRunes) return text;
  return '${String.fromCharCodes(runes.take(maxRunes))}…';
}

/// Builds a concise handoff prompt for digital employees.
/// Secrets are redacted before the draft leaves the assistant surface.
String buildAssistantEmployeeHandoffPrompt({
  required String query,
  required String answer,
}) {
  final q = _clip(redactMobileSensitiveText(query.trim()), 800);
  final a = _clip(redactMobileSensitiveText(answer.trim()), 4000);
  final buffer = StringBuffer()
    ..writeln('请根据 AI 助手结论继续跟进（手机端交接）：');
  if (q.isNotEmpty) {
    buffer
      ..writeln()
      ..writeln('【用户问题】')
      ..writeln(q);
  }
  if (a.isNotEmpty) {
    buffer
      ..writeln()
      ..writeln('【助手结论】')
      ..writeln(a);
  }
  buffer
    ..writeln()
    ..writeln('请给出可执行步骤；高风险或破坏性操作须人工确认后再执行。');
  return buffer.toString().trim();
}

/// One-shot handoff; consumers should clear after opening the task sheet.
final assistantEmployeeHandoffProvider =
    StateProvider<AssistantEmployeeHandoff?>((ref) => null);

void offerAssistantEmployeeHandoff(
  WidgetRef ref, {
  required String query,
  required String answer,
}) {
  final q = query.trim();
  final a = answer.trim();
  if (q.isEmpty && a.isEmpty) return;
  ref.read(assistantEmployeeHandoffProvider.notifier).state =
      AssistantEmployeeHandoff(
    query: q,
    answer: a,
    createdAt: DateTime.now(),
  );
}
