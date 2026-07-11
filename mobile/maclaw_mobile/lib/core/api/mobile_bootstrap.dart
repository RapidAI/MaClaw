/// Hub-declared first-tab mode. Prefer server value; client may derive when empty.
const mobileAssistantModeOfficial = 'official';
const mobileAssistantModeDigitalTwin = 'digital_twin';

/// Default free-tier document storage quota (100 MiB).
const mobileDefaultDocumentQuotaBytes = 100 * 1024 * 1024;

class MobileBootstrap {
  final MobileUser user;
  final MobileServices services;
  final MobileConnection connection;
  final MobileLlmAccess llmAccess;
  final MobileFeatures features;
  final MobileLimits limits;
  final MobileEntitlements entitlements;
  /// `official` | `digital_twin` | empty (derive on client).
  final String assistantMode;

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
    this.entitlements = const MobileEntitlements(),
    this.assistantMode = '',
  });

  factory MobileBootstrap.fromJson(Map<String, dynamic> json) {
    final user = MobileUser.fromJson(
      Map<String, dynamic>.from(json['user'] as Map? ?? const {}),
    );
    final llmAccess = MobileLlmAccess.fromJson(
      Map<String, dynamic>.from(json['llm_access'] as Map? ?? const {}),
    );
    return MobileBootstrap(
      user: user,
      services: MobileServices.fromJson(
        Map<String, dynamic>.from(json['services'] as Map? ?? const {}),
      ),
      connection: MobileConnection.fromJson(
        Map<String, dynamic>.from(json['connection'] as Map? ?? const {}),
      ),
      llmAccess: llmAccess.withFallbackCreditsAccount(user.creditsAccount),
      features: MobileFeatures.fromJson(
        Map<String, dynamic>.from(json['features'] as Map? ?? const {}),
      ),
      limits: MobileLimits.fromJson(
        Map<String, dynamic>.from(json['limits'] as Map? ?? const {}),
      ),
      entitlements: MobileEntitlements.fromJson(
        Map<String, dynamic>.from(json['entitlements'] as Map? ?? const {}),
      ),
      assistantMode: (json['assistant_mode'] as String? ?? '').trim(),
    );
  }

  MobileBootstrap withVerifiedPhoneCredits(String phoneNumber) {
    final value = phoneNumber.trim();
    final creditsAccount = _trustedPhoneCreditsAccount(
      value.toLowerCase().startsWith('phone:') ? value : 'phone:$value',
    );
    if (!llmAccess.official || creditsAccount.isEmpty) {
      return this;
    }
    return MobileBootstrap(
      user: user.withCreditsAccount(creditsAccount),
      services: services,
      connection: connection,
      llmAccess: llmAccess.withCreditsAccount(creditsAccount),
      features: features,
      limits: limits,
      entitlements: entitlements,
      assistantMode: assistantMode,
    );
  }
}

String _trustedPhoneCreditsAccount(String value) {
  final normalized = _normalizeCreditsAccount(value);
  if (!normalized.toLowerCase().startsWith('phone:')) return '';
  final phone = normalized.substring(normalized.indexOf(':') + 1);
  if (phone.isEmpty) return '';
  final allDigits =
      phone.codeUnits.every((codeUnit) => codeUnit >= 48 && codeUnit <= 57);
  return allDigits ? 'phone:$phone' : '';
}

String _phoneDigits(String value) {
  final buffer = StringBuffer();
  for (final codeUnit in value.trim().codeUnits) {
    if (codeUnit >= 48 && codeUnit <= 57) {
      buffer.writeCharCode(codeUnit);
    }
  }
  return buffer.toString();
}

String _normalizeCreditsAccount(String value) {
  final trimmed = value.trim();
  if (!trimmed.toLowerCase().startsWith('phone:')) return trimmed;
  final phone = trimmed.substring(trimmed.indexOf(':') + 1);
  if (!_phoneAccountValueCanNormalize(phone)) return trimmed;
  final digits = _phoneDigits(phone);
  return digits.isEmpty ? trimmed : 'phone:$digits';
}

bool _phoneAccountValueCanNormalize(String value) {
  var hasDigit = false;
  for (final codeUnit in value.trim().codeUnits) {
    if (codeUnit >= 48 && codeUnit <= 57) {
      hasDigit = true;
      continue;
    }
    if (codeUnit == 32 ||
        codeUnit == 43 ||
        codeUnit == 45 ||
        codeUnit == 40 ||
        codeUnit == 41) {
      continue;
    }
    return false;
  }
  return hasDigit;
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
  final String creditsAccount;
  final DateTime? authorizedAt;

  const MobileLlmAccess({
    required this.mode,
    required this.status,
    required this.authorizationId,
    required this.authorizedBy,
    this.creditsAccount = '',
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
      creditsAccount: _normalizeCreditsAccount(
        json['credits_account'] as String? ?? '',
      ),
      authorizedAt: DateTime.tryParse(json['authorized_at'] as String? ?? ''),
    );
  }

  MobileLlmAccess withFallbackCreditsAccount(String fallbackCreditsAccount) {
    if (creditsAccount.trim().isNotEmpty ||
        fallbackCreditsAccount.trim().isEmpty) {
      return this;
    }
    return MobileLlmAccess(
      mode: mode,
      status: status,
      authorizationId: authorizationId,
      authorizedBy: authorizedBy,
      creditsAccount: _normalizeCreditsAccount(fallbackCreditsAccount),
      authorizedAt: authorizedAt,
    );
  }

  MobileLlmAccess withCreditsAccount(String value) {
    final normalized = _normalizeCreditsAccount(value);
    if (normalized == creditsAccount) return this;
    return MobileLlmAccess(
      mode: mode,
      status: status,
      authorizationId: authorizationId,
      authorizedBy: authorizedBy,
      creditsAccount: normalized,
      authorizedAt: authorizedAt,
    );
  }
}

class MobileUser {
  final String userId;
  final String email;
  final String phoneNumber;
  final String accountId;
  final String creditsAccount;
  final String tenantId;

  const MobileUser({
    required this.userId,
    required this.email,
    this.phoneNumber = '',
    this.accountId = '',
    this.creditsAccount = '',
    required this.tenantId,
  });

  factory MobileUser.fromJson(Map<String, dynamic> json) {
    final email = json['email'] as String? ?? '';
    final accountId = json['account_id'] as String? ?? '';
    final phoneNumber = json['phone_number'] as String? ?? '';
    return MobileUser(
      userId: json['user_id'] as String? ?? '',
      email: email,
      phoneNumber: phoneNumber,
      accountId: accountId,
      creditsAccount: _normalizeCreditsAccount(
        json['credits_account'] as String? ??
            _defaultCreditsAccount(
              phoneNumber: phoneNumber,
              accountId: accountId,
              email: email,
            ),
      ),
      tenantId: json['tenant_id'] as String? ?? '',
    );
  }

  static String _defaultCreditsAccount({
    required String phoneNumber,
    required String accountId,
    required String email,
  }) {
    final phone = _phoneDigits(phoneNumber);
    if (phone.isNotEmpty) return 'phone:$phone';
    if (accountId.trim().isNotEmpty) return accountId;
    return email.trim();
  }

  MobileUser withCreditsAccount(String value) {
    final normalized = _normalizeCreditsAccount(value);
    if (normalized == creditsAccount) return this;
    return MobileUser(
      userId: userId,
      email: email,
      phoneNumber: phoneNumber,
      accountId: accountId,
      creditsAccount: normalized,
      tenantId: tenantId,
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
  final String documentsQuotaPath;
  final String entitlementsCapsPath;
  final String digitalEmployeesPath;
  final String realtimePath;
  final String pushDevicesPath;
  final String pushPendingPath;
  final String pushPendingAckPath;

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
    this.documentsQuotaPath = '/api/mobile/documents/quota',
    this.entitlementsCapsPath = '/api/mobile/entitlements/caps',
    required this.digitalEmployeesPath,
    required this.realtimePath,
    this.pushDevicesPath = '/api/mobile/push/devices',
    this.pushPendingPath = '/api/mobile/push/pending',
    this.pushPendingAckPath = '/api/mobile/push/pending/ack',
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
      documentsQuotaPath: json['documents_quota_path'] as String? ??
          '/api/mobile/documents/quota',
      entitlementsCapsPath: json['entitlements_caps_path'] as String? ??
          '/api/mobile/entitlements/caps',
      digitalEmployeesPath: json['digital_employees_path'] as String? ?? '',
      realtimePath: json['realtime_path'] as String? ?? '/api/mobile/realtime',
      pushDevicesPath:
          json['push_devices_path'] as String? ?? '/api/mobile/push/devices',
      pushPendingPath:
          json['push_pending_path'] as String? ?? '/api/mobile/push/pending',
      pushPendingAckPath: json['push_pending_ack_path'] as String? ??
          '/api/mobile/push/pending/ack',
    );
  }
}

class MobileFeatures {
  final bool assistant;
  final bool search;
  final bool documents;
  /// Long-running task center (bottom tab "后台").
  final bool tasks;
  final bool backendSshSessions;
  final bool digitalEmployees;
  final bool pushNotifications;
  /// Offline completion queue (GET pending on resume); always preferred on.
  final bool pushPendingSync;

  const MobileFeatures({
    bool? assistant,
    required this.search,
    required this.documents,
    bool? tasks,
    bool? backendSshSessions,
    bool? localSsh,
    required this.digitalEmployees,
    required this.pushNotifications,
    bool? pushPendingSync,
  })  : assistant = assistant ?? true,
        tasks = tasks ?? true,
        backendSshSessions = backendSshSessions ?? localSsh ?? true,
        pushPendingSync = pushPendingSync ?? true;

  /// Backward-compatible alias for older Hub bootstrap payloads that used
  /// `local_ssh`. The mobile feature is GUI/agent-managed backend SSH sessions.
  bool get localSsh => backendSshSessions;

  factory MobileFeatures.fromJson(Map<String, dynamic> json) {
    return MobileFeatures(
      assistant: json['assistant'] as bool? ?? true,
      search: json['search'] as bool? ?? true,
      documents: json['documents'] as bool? ?? true,
      tasks: json['tasks'] as bool? ?? true,
      // Prefer the GUI/agent-managed backend session flag. Accept `local_ssh`
      // only so older Hubs do not hide the remote-management tab.
      backendSshSessions: json['backend_ssh_sessions'] as bool? ??
          json['remote_ssh_sessions'] as bool? ??
          json['local_ssh'] as bool? ??
          true,
      digitalEmployees: json['digital_employees'] as bool? ?? true,
      pushNotifications: json['push_notifications'] as bool? ?? false,
      pushPendingSync: json['push_pending_sync'] as bool? ?? true,
    );
  }
}

/// Plan and feature gates for Mobile (storage / agent / shared employees).
/// Caps mirror Hub `mobilePlanCapsFor` (document quota, export concurrency, etc.).
class MobileEntitlements {
  final bool mobileOfficial;
  final bool mobileAgent;
  final bool documentAi;
  final bool sharedEmployees;
  final bool hubSshExec;
  final String plan;
  final bool serviceActive;
  final double creditsAvailable;
  final double creditsRemaining;
  final int serviceGroupCount;
  final bool hasServiceCardGrant;
  final int documentQuotaBytes;
  final int maxUploadBytes;
  final int maxExportJobs;
  final int hubFileDownloadMaxBytes;

  const MobileEntitlements({
    this.mobileOfficial = false,
    this.mobileAgent = false,
    this.documentAi = false,
    this.sharedEmployees = false,
    this.hubSshExec = false,
    this.plan = 'free',
    this.serviceActive = false,
    this.creditsAvailable = 0,
    this.creditsRemaining = 0,
    this.serviceGroupCount = 0,
    this.hasServiceCardGrant = false,
    this.documentQuotaBytes = 0,
    this.maxUploadBytes = 0,
    this.maxExportJobs = 0,
    this.hubFileDownloadMaxBytes = 0,
  });

  factory MobileEntitlements.fromJson(Map<String, dynamic> json) {
    final planRaw = (json['plan'] as String? ?? 'free').trim();
    return MobileEntitlements(
      mobileOfficial: json['mobile_official'] as bool? ?? false,
      mobileAgent: json['mobile_agent'] as bool? ?? false,
      documentAi: json['document_ai'] as bool? ?? false,
      sharedEmployees: json['shared_employees'] as bool? ?? false,
      hubSshExec: json['hub_ssh_exec'] as bool? ?? false,
      plan: planRaw.isEmpty ? 'free' : planRaw,
      serviceActive: json['service_active'] as bool? ?? false,
      creditsAvailable: _doubleValue(json['credits_available']),
      creditsRemaining: _doubleValue(json['credits_remaining']),
      serviceGroupCount: _intValue(json['service_group_count']),
      hasServiceCardGrant: json['has_service_card_grant'] as bool? ?? false,
      documentQuotaBytes: _intValue(json['document_quota_bytes']),
      maxUploadBytes: _intValue(json['max_upload_bytes']),
      maxExportJobs: _intValue(json['max_export_jobs']),
      hubFileDownloadMaxBytes: _intValue(json['hub_file_download_max_bytes']),
    );
  }
}

class MobileLimits {
  final int maxUploadBytes;
  final int maxExportJobs;
  final int documentQuotaBytes;
  final int documentQuotaUsedBytes;

  const MobileLimits({
    required this.maxUploadBytes,
    required this.maxExportJobs,
    this.documentQuotaBytes = 0,
    this.documentQuotaUsedBytes = 0,
  });

  int get effectiveDocumentQuotaBytes => documentQuotaBytes > 0
      ? documentQuotaBytes
      : mobileDefaultDocumentQuotaBytes;

  factory MobileLimits.fromJson(Map<String, dynamic> json) {
    return MobileLimits(
      maxUploadBytes: json['max_upload_bytes'] as int? ?? 0,
      maxExportJobs: json['max_export_jobs'] as int? ?? 0,
      documentQuotaBytes: json['document_quota_bytes'] as int? ?? 0,
      documentQuotaUsedBytes: json['document_quota_used_bytes'] as int? ?? 0,
    );
  }
}

class LlmServiceStatus {
  final bool active;
  final bool skipLlmConfig;
  final String authMode;
  final String defaultModel;
  final List<String> availableModels;
  final List<String> serviceGroupNames;
  final List<String> inactiveReasons;
  final String nearestExpiresAt;
  final double creditsTotal;
  final double creditsUsed;
  final double creditsRemaining;
  final double creditsAvailable;
  final int tokensPerCredit;

  const LlmServiceStatus({
    required this.active,
    required this.skipLlmConfig,
    required this.authMode,
    required this.defaultModel,
    required this.availableModels,
    required this.serviceGroupNames,
    required this.inactiveReasons,
    required this.nearestExpiresAt,
    required this.creditsTotal,
    required this.creditsUsed,
    required this.creditsRemaining,
    required this.creditsAvailable,
    required this.tokensPerCredit,
  });

  factory LlmServiceStatus.fromJson(Map<String, dynamic> json) {
    return LlmServiceStatus(
      active: json['active'] as bool? ?? false,
      skipLlmConfig: json['skip_llm_config'] as bool? ?? false,
      authMode: json['auth_mode'] as String? ?? '',
      defaultModel: json['default_model'] as String? ?? '',
      availableModels: _stringList(json['available_models']),
      serviceGroupNames: _stringList(json['service_group_names']),
      inactiveReasons: _stringList(json['inactive_reasons']),
      nearestExpiresAt: json['nearest_expires_at'] as String? ??
          json['effective_expires_at'] as String? ??
          '',
      creditsTotal: _doubleValue(json['credits_total']),
      creditsUsed: _doubleValue(json['credits_used']),
      creditsRemaining: _doubleValue(json['credits_remaining']),
      creditsAvailable: _doubleValue(json['credits_available']),
      tokensPerCredit: json['tokens_per_credit'] as int? ?? 0,
    );
  }
}

List<String> _stringList(Object? value) {
  return [
    for (final item in (value as List? ?? const [])) item.toString(),
  ];
}

double _doubleValue(Object? value) {
  if (value is num) return value.toDouble();
  return double.tryParse(value?.toString() ?? '') ?? 0;
}

int _intValue(Object? value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '') ?? 0;
}
