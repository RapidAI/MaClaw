class DigitalEmployeePromptEntry {
  final String id;
  final String employeeId;
  final String prompt;
  final DateTime createdAt;

  const DigitalEmployeePromptEntry({
    required this.id,
    required this.employeeId,
    required this.prompt,
    required this.createdAt,
  });

  factory DigitalEmployeePromptEntry.fromJson(Map<String, dynamic> json) {
    return DigitalEmployeePromptEntry(
      id: json['id'] as String? ?? '',
      employeeId: json['employee_id'] as String? ?? '',
      prompt: json['prompt'] as String? ?? '',
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? '') ??
          DateTime.fromMillisecondsSinceEpoch(0),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'employee_id': employeeId,
      'prompt': prompt,
      'created_at': createdAt.toUtc().toIso8601String(),
    };
  }
}
