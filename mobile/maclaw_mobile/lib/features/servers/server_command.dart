class ServerCommandEntry {
  final String id;
  final String command;
  final String label;
  final bool favorite;
  final DateTime createdAt;
  final DateTime lastUsedAt;

  const ServerCommandEntry({
    required this.id,
    required this.command,
    required this.label,
    required this.favorite,
    required this.createdAt,
    required this.lastUsedAt,
  });

  factory ServerCommandEntry.fromJson(Map<String, dynamic> json) {
    final createdAt = DateTime.tryParse(json['created_at'] as String? ?? '') ??
        DateTime.fromMillisecondsSinceEpoch(0);
    return ServerCommandEntry(
      id: json['id'] as String? ?? '',
      command: json['command'] as String? ?? '',
      label: json['label'] as String? ?? '',
      favorite: json['favorite'] as bool? ?? false,
      createdAt: createdAt,
      lastUsedAt:
          DateTime.tryParse(json['last_used_at'] as String? ?? '') ?? createdAt,
    );
  }

  ServerCommandEntry copyWith({
    String? id,
    String? command,
    String? label,
    bool? favorite,
    DateTime? createdAt,
    DateTime? lastUsedAt,
  }) {
    return ServerCommandEntry(
      id: id ?? this.id,
      command: command ?? this.command,
      label: label ?? this.label,
      favorite: favorite ?? this.favorite,
      createdAt: createdAt ?? this.createdAt,
      lastUsedAt: lastUsedAt ?? this.lastUsedAt,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'command': command,
      'label': label,
      'favorite': favorite,
      'created_at': createdAt.toUtc().toIso8601String(),
      'last_used_at': lastUsedAt.toUtc().toIso8601String(),
    };
  }
}
