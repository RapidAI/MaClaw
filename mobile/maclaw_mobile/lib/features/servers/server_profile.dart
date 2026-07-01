class ServerProfile {
  final String id;
  final String name;
  final String host;
  final int port;
  final String username;
  final String authMode;
  final String? tag;
  final String? note;

  const ServerProfile({
    required this.id,
    required this.name,
    required this.host,
    required this.port,
    required this.username,
    required this.authMode,
    this.tag,
    this.note,
  });

  bool get isValid => host.trim().isNotEmpty && port > 0 && username.isNotEmpty;

  factory ServerProfile.fromJson(Map<String, dynamic> json) {
    return ServerProfile(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      host: json['host'] as String? ?? '',
      port: json['port'] as int? ?? 22,
      username: json['username'] as String? ?? '',
      authMode: json['auth_mode'] as String? ?? 'password',
      tag: json['tag'] as String?,
      note: json['note'] as String?,
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
    };
  }
}
