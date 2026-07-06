const serverAuthModePassword = 'password';
const serverAuthModePrivateKey = 'private_key';

class ServerProfile {
  final String id;
  final String name;
  final String host;
  final int port;
  final String username;
  final String authMode;
  final String? tag;
  final String? note;
  final String sourceMachineId;
  final DateTime? updatedAt;

  const ServerProfile({
    required this.id,
    required this.name,
    required this.host,
    required this.port,
    required this.username,
    required this.authMode,
    this.tag,
    this.note,
    this.sourceMachineId = '',
    this.updatedAt,
  });

  bool get isValid =>
      host.trim().isNotEmpty &&
      port > 0 &&
      port <= 65535 &&
      username.trim().isNotEmpty;

  factory ServerProfile.fromJson(Map<String, dynamic> json) {
    return ServerProfile(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      host: json['host'] as String? ?? '',
      port: json['port'] as int? ?? 22,
      username: json['username'] as String? ?? '',
      authMode: json['auth_mode'] as String? ?? serverAuthModePassword,
      tag: json['tag'] as String?,
      note: json['note'] as String?,
      sourceMachineId: json['source_machine_id'] as String? ?? '',
      updatedAt: DateTime.tryParse(json['updated_at'] as String? ?? ''),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'name': name,
      'host': host,
      'port': port,
      'username': username,
      'auth_mode': authMode,
      if (tag != null) 'tag': tag,
      if (note != null) 'note': note,
      if (sourceMachineId.trim().isNotEmpty)
        'source_machine_id': sourceMachineId,
      if (updatedAt != null) 'updated_at': updatedAt!.toUtc().toIso8601String(),
    };
  }
}

String serverAuthModeLabel(String authMode) {
  return switch (authMode) {
    serverAuthModePrivateKey => '私钥',
    _ => '密码',
  };
}
