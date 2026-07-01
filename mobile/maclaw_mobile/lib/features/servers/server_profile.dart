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
}

