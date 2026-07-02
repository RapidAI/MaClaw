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
  bool get canSubmitTask => online && !runtimeMissing;

  String get accessPolicyLabel {
    return switch (accessPolicy.toLowerCase()) {
      'public' => '公开可用',
      'private' => '私有授权',
      'per_request' => '按次授权',
      'owner_confirm' => '需拥有者确认',
      _ => '策略：$accessPolicy',
    };
  }

  String get residencyLabel => resident ? '常驻远程端' : '按需唤起';

  String get runtimeLabel {
    if (runtimeMissing) return '远程运行时缺失';
    return online ? '远程端在线' : '远程端离线';
  }

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
