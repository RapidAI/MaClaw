class DigitalEmployee {
  final String id;
  final String machineId;
  final String name;
  final String skillDescription;
  final String onlineStatus;
  final String accessPolicy;
  final bool resident;
  final bool runtimeMissing;

  const DigitalEmployee({
    required this.id,
    required this.machineId,
    required this.name,
    required this.skillDescription,
    required this.onlineStatus,
    required this.accessPolicy,
    required this.resident,
    required this.runtimeMissing,
  });

  bool get online => onlineStatus.toLowerCase() == 'online';

  factory DigitalEmployee.fromJson(Map<String, dynamic> json) {
    return DigitalEmployee(
      id: json['id'] as String? ?? '',
      machineId: json['machine_id'] as String? ?? '',
      name: json['name'] as String? ?? '数字员工',
      skillDescription: json['skill_description'] as String? ?? '',
      onlineStatus: json['online_status'] as String? ?? 'offline',
      accessPolicy: json['access_policy'] as String? ?? 'public',
      resident: json['resident'] as bool? ?? false,
      runtimeMissing: json['runtime_missing'] as bool? ?? false,
    );
  }
}

