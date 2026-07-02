class MobileBootstrap {
  final MobileUser user;
  final MobileServices services;
  final MobileConnection connection;
  final MobileLlmAccess llmAccess;
  final MobileFeatures features;
  final MobileLimits limits;

  const MobileBootstrap({
    required this.user,
    required this.services,
    this.connection = const MobileConnection(
      hubCenterCandidates: [],
      selectedHubCenterUrl: '',
      hubUrl: '',
      hubId: '',
      tenantId: '',
    ),
    this.llmAccess = const MobileLlmAccess(
      mode: 'maclaw_official',
      status: 'available',
      authorizationId: '',
      authorizedBy: '',
      authorizedAt: null,
    ),
    required this.features,
    required this.limits,
  });

  factory MobileBootstrap.fromJson(Map<String, dynamic> json) {
    return MobileBootstrap(
      user: MobileUser.fromJson(
        Map<String, dynamic>.from(json['user'] as Map? ?? const {}),
      ),
      services: MobileServices.fromJson(
        Map<String, dynamic>.from(json['services'] as Map? ?? const {}),
      ),
      connection: MobileConnection.fromJson(
        Map<String, dynamic>.from(json['connection'] as Map? ?? const {}),
      ),
      llmAccess: MobileLlmAccess.fromJson(
        Map<String, dynamic>.from(json['llm_access'] as Map? ?? const {}),
      ),
      features: MobileFeatures.fromJson(
        Map<String, dynamic>.from(json['features'] as Map? ?? const {}),
      ),
      limits: MobileLimits.fromJson(
        Map<String, dynamic>.from(json['limits'] as Map? ?? const {}),
      ),
    );
  }
}

class MobileConnection {
  final List<String> hubCenterCandidates;
  final String selectedHubCenterUrl;
  final String hubUrl;
  final String hubId;
  final String tenantId;

  const MobileConnection({
    required this.hubCenterCandidates,
    required this.selectedHubCenterUrl,
    required this.hubUrl,
    required this.hubId,
    required this.tenantId,
  });

  factory MobileConnection.fromJson(Map<String, dynamic> json) {
    final hub = Map<String, dynamic>.from(json['hub'] as Map? ?? const {});
    return MobileConnection(
      hubCenterCandidates: [
        for (final item in (json['hubcenter_candidates'] as List? ?? const []))
          item.toString(),
      ],
      selectedHubCenterUrl: json['selected_hubcenter_url'] as String? ??
          json['hubcenter_url'] as String? ??
          '',
      hubUrl: json['hub_url'] as String? ??
          hub['base_url'] as String? ??
          hub['url'] as String? ??
          '',
      hubId: json['hub_id'] as String? ?? hub['id'] as String? ?? '',
      tenantId: json['tenant_id'] as String? ?? '',
    );
  }
}

class MobileLlmAccess {
  final String mode;
  final String status;
  final String authorizationId;
  final String authorizedBy;
  final DateTime? authorizedAt;

  const MobileLlmAccess({
    required this.mode,
    required this.status,
    required this.authorizationId,
    required this.authorizedBy,
    required this.authorizedAt,
  });

  bool get official => mode == 'maclaw_official';

  bool get desktopQrDelegated => mode == 'desktop_qr_third_party';

  factory MobileLlmAccess.fromJson(Map<String, dynamic> json) {
    return MobileLlmAccess(
      mode: json['mode'] as String? ?? 'maclaw_official',
      status: json['status'] as String? ?? 'available',
      authorizationId: json['authorization_id'] as String? ?? '',
      authorizedBy: json['authorized_by'] as String? ?? '',
      authorizedAt: DateTime.tryParse(json['authorized_at'] as String? ?? ''),
    );
  }
}

class MobileUser {
  final String userId;
  final String email;
  final String tenantId;

  const MobileUser({
    required this.userId,
    required this.email,
    required this.tenantId,
  });

  factory MobileUser.fromJson(Map<String, dynamic> json) {
    return MobileUser(
      userId: json['user_id'] as String? ?? '',
      email: json['email'] as String? ?? '',
      tenantId: json['tenant_id'] as String? ?? '',
    );
  }
}

class MobileServices {
  final String hubStatus;
  final String llmStatus;
  final String searchStatus;
  final String documentsStatus;
  final String digitalEmployeesStatus;
  final String llmStatusPath;
  final String modelsPath;
  final String searchPath;
  final String documentsPath;
  final String digitalEmployeesPath;
  final String realtimePath;

  const MobileServices({
    required this.hubStatus,
    required this.llmStatus,
    required this.searchStatus,
    required this.documentsStatus,
    required this.digitalEmployeesStatus,
    required this.llmStatusPath,
    required this.modelsPath,
    required this.searchPath,
    required this.documentsPath,
    required this.digitalEmployeesPath,
    required this.realtimePath,
  });

  bool get realtimeConfigured => realtimePath.trim().isNotEmpty;

  factory MobileServices.fromJson(Map<String, dynamic> json) {
    return MobileServices(
      hubStatus: json['hub_status'] as String? ?? 'unknown',
      llmStatus: json['llm_status'] as String? ?? 'unknown',
      searchStatus: json['search_status'] as String? ?? 'unknown',
      documentsStatus: json['documents_status'] as String? ?? 'unknown',
      digitalEmployeesStatus:
          json['digital_employees_status'] as String? ?? 'unknown',
      llmStatusPath: json['llm_status_path'] as String? ?? '',
      modelsPath: json['models_path'] as String? ?? '',
      searchPath: json['search_path'] as String? ?? '',
      documentsPath: json['documents_path'] as String? ?? '',
      digitalEmployeesPath: json['digital_employees_path'] as String? ?? '',
      realtimePath: json['realtime_path'] as String? ?? '/api/mobile/realtime',
    );
  }
}

class MobileFeatures {
  final bool search;
  final bool documents;
  final bool localSsh;
  final bool digitalEmployees;
  final bool pushNotifications;

  const MobileFeatures({
    required this.search,
    required this.documents,
    required this.localSsh,
    required this.digitalEmployees,
    required this.pushNotifications,
  });

  factory MobileFeatures.fromJson(Map<String, dynamic> json) {
    return MobileFeatures(
      search: json['search'] as bool? ?? true,
      documents: json['documents'] as bool? ?? true,
      localSsh: json['local_ssh'] as bool? ?? true,
      digitalEmployees: json['digital_employees'] as bool? ?? true,
      pushNotifications: json['push_notifications'] as bool? ?? false,
    );
  }
}

class MobileLimits {
  final int maxUploadBytes;
  final int maxExportJobs;

  const MobileLimits({
    required this.maxUploadBytes,
    required this.maxExportJobs,
  });

  factory MobileLimits.fromJson(Map<String, dynamic> json) {
    return MobileLimits(
      maxUploadBytes: json['max_upload_bytes'] as int? ?? 0,
      maxExportJobs: json['max_export_jobs'] as int? ?? 0,
    );
  }
}
