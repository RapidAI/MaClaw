class MobileBootstrap {
  final MobileUser user;
  final MobileServices services;
  final MobileFeatures features;

  const MobileBootstrap({
    required this.user,
    required this.services,
    required this.features,
  });

  factory MobileBootstrap.fromJson(Map<String, dynamic> json) {
    return MobileBootstrap(
      user: MobileUser.fromJson(
        Map<String, dynamic>.from(json['user'] as Map? ?? const {}),
      ),
      services: MobileServices.fromJson(
        Map<String, dynamic>.from(json['services'] as Map? ?? const {}),
      ),
      features: MobileFeatures.fromJson(
        Map<String, dynamic>.from(json['features'] as Map? ?? const {}),
      ),
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
  final String llmStatusPath;
  final String modelsPath;

  const MobileServices({
    required this.hubStatus,
    required this.llmStatusPath,
    required this.modelsPath,
  });

  factory MobileServices.fromJson(Map<String, dynamic> json) {
    return MobileServices(
      hubStatus: json['hub_status'] as String? ?? 'unknown',
      llmStatusPath: json['llm_status_path'] as String? ?? '',
      modelsPath: json['models_path'] as String? ?? '',
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
