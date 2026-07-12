/// Catalog wrapper for GET /api/mobile/digital-employees (includes scope).
class MobileDigitalEmployeesCatalog {
  final List<DigitalEmployee> employees;
  /// `own` | `shared`
  final String scope;
  final bool sharedEmployees;

  const MobileDigitalEmployeesCatalog({
    this.employees = const [],
    this.scope = 'own',
    this.sharedEmployees = false,
  });

  factory MobileDigitalEmployeesCatalog.fromJson(Map<String, dynamic> json) {
    final raw = json['employees'];
    final list = <DigitalEmployee>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map) {
          list.add(DigitalEmployee.fromJson(Map<String, dynamic>.from(item)));
        }
      }
    }
    final scope = (json['scope'] as String? ?? 'own').trim().toLowerCase();
    return MobileDigitalEmployeesCatalog(
      employees: list,
      scope: scope.isEmpty ? 'own' : scope,
      sharedEmployees: json['shared_employees'] as bool? ?? scope == 'shared',
    );
  }
}

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

  String get accessPolicyLabel => accessPolicyLabelFor(isZh: true);

  String accessPolicyLabelFor({bool isZh = true}) {
    return switch (accessPolicy.toLowerCase()) {
      'public' => isZh ? '公开可用' : 'Public',
      'private' => isZh ? '私有授权' : 'Private',
      'per_request' => isZh ? '按次授权' : 'Per request',
      'owner_confirm' => isZh ? '需拥有者确认' : 'Owner confirm',
      _ => isZh ? '策略：$accessPolicy' : 'Policy: $accessPolicy',
    };
  }

  String get residencyLabel => residencyLabelFor(isZh: true);

  String residencyLabelFor({bool isZh = true}) => resident
      ? (isZh ? '常驻远程端' : 'Always-on remote')
      : (isZh ? '按需唤起' : 'On demand');

  String get runtimeLabel => runtimeLabelFor(isZh: true);

  String runtimeLabelFor({bool isZh = true}) {
    if (runtimeMissing) {
      return isZh ? '远程运行时缺失' : 'Remote runtime missing';
    }
    return online
        ? (isZh ? '远程端在线' : 'Remote online')
        : (isZh ? '远程端离线' : 'Remote offline');
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

/// Mobile lists only surface online employees; offline ones stay hidden.
List<DigitalEmployee> filterOnlineDigitalEmployees(
  Iterable<DigitalEmployee> employees,
) {
  return [
    for (final employee in employees)
      if (employee.online) employee,
  ];
}

/// Hub placeholder strings that should not be shown as "live progress".
const digitalEmployeeTaskProgressPlaceholders = <String>{
  '远程数字员工已领取任务，正在处理。',
  '远程数字员工正在处理手机任务。',
  '任务已提交，等待远程数字员工或授权策略处理。',
  '任务已提交，等待远程数字员工领取。',
};

/// Whether the mobile task is still actively running remotely.
bool digitalEmployeeTaskIsRunning(String status) {
  switch (status.trim().toLowerCase()) {
    case 'queued':
    case 'claimed':
    case 'running':
    case 'in_progress':
      return true;
    default:
      return false;
  }
}

/// Prefer newest agent text for live progress (result), then message.
///
/// Filters generic claim/queue placeholders so UI can show real streaming
/// output from Hub realtime patches.
String digitalEmployeeTaskProgressPreview({
  required String result,
  required String message,
}) {
  final trimmedResult = result.trim();
  final trimmedMessage = message.trim();
  if (trimmedResult.isNotEmpty &&
      !digitalEmployeeTaskProgressPlaceholders.contains(trimmedResult)) {
    return trimmedResult;
  }
  if (trimmedMessage.isNotEmpty &&
      !digitalEmployeeTaskProgressPlaceholders.contains(trimmedMessage) &&
      !trimmedMessage.contains('等待远程') &&
      !trimmedMessage.toLowerCase().contains('waiting')) {
    return trimmedMessage;
  }
  if (trimmedResult.isNotEmpty &&
      !digitalEmployeeTaskProgressPlaceholders.contains(trimmedResult)) {
    return trimmedResult;
  }
  return '';
}
