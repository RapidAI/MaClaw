/// Represents a user or machine in the chat system.
class ChatUser {
  final String id;
  final String name;
  final String? avatarUrl;
  final UserType type;
  final PresenceStatus presence;
  final DateTime? lastSeenAt;

  const ChatUser({
    required this.id,
    required this.name,
    this.avatarUrl,
    this.type = UserType.human,
    this.presence = PresenceStatus.offline,
    this.lastSeenAt,
  });

  bool get isOnline => presence == PresenceStatus.online;
  bool get isMachine => type == UserType.machine;

  ChatUser copyWith({PresenceStatus? presence, DateTime? lastSeenAt}) {
    return ChatUser(
      id: id,
      name: name,
      avatarUrl: avatarUrl,
      type: type,
      presence: presence ?? this.presence,
      lastSeenAt: lastSeenAt ?? this.lastSeenAt,
    );
  }

  factory ChatUser.fromJson(Map<String, dynamic> json) {
    return ChatUser(
      id: json['id'] as String,
      name: json['name'] as String,
      avatarUrl: json['avatar_url'] as String?,
      type: UserType.values.byName(json['type'] as String? ?? 'human'),
      presence: PresenceStatus.values.byName(json['presence'] as String? ?? 'offline'),
      lastSeenAt: json['last_seen_at'] != null
          ? DateTime.fromMillisecondsSinceEpoch(json['last_seen_at'] as int)
          : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'avatar_url': avatarUrl,
        'type': type.name,
        'presence': presence.name,
        'last_seen_at': lastSeenAt?.millisecondsSinceEpoch,
      };
}

enum UserType { human, machine }

enum PresenceStatus { online, offline }
