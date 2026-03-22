import 'message.dart';

/// A chat channel — either a direct (1:1) or group conversation.
class Channel {
  final String id;
  final ChannelType type;
  final String? name;
  final String? avatarUrl;
  final String createdBy;
  final DateTime createdAt;
  final int lastSeq;
  final int readSeq;
  final Message? lastMessage;
  final List<ChannelMember> members;

  const Channel({
    required this.id,
    required this.type,
    this.name,
    this.avatarUrl,
    required this.createdBy,
    required this.createdAt,
    this.lastSeq = 0,
    this.readSeq = 0,
    this.lastMessage,
    this.members = const [],
  });

  int get unreadCount => lastSeq - readSeq;
  bool get isDirect => type == ChannelType.direct;
  bool get isGroup => type == ChannelType.group;

  Channel copyWith({int? lastSeq, int? readSeq, Message? lastMessage}) {
    return Channel(
      id: id,
      type: type,
      name: name,
      avatarUrl: avatarUrl,
      createdBy: createdBy,
      createdAt: createdAt,
      lastSeq: lastSeq ?? this.lastSeq,
      readSeq: readSeq ?? this.readSeq,
      lastMessage: lastMessage ?? this.lastMessage,
      members: members,
    );
  }

  factory Channel.fromJson(Map<String, dynamic> json) {
    // Backend sends type as int (0=direct, 1=group); Flutter API sends string.
    ChannelType chType;
    final rawType = json['type'];
    if (rawType is int) {
      chType = rawType == 0 ? ChannelType.direct : ChannelType.group;
    } else if (rawType is String) {
      chType = ChannelType.values.byName(rawType);
    } else {
      chType = ChannelType.group;
    }

    // created_at may be ISO string (from Go time.Time) or int millis.
    DateTime createdAt;
    final rawCreated = json['created_at'];
    if (rawCreated is int) {
      createdAt = DateTime.fromMillisecondsSinceEpoch(rawCreated);
    } else if (rawCreated is String) {
      createdAt = DateTime.parse(rawCreated);
    } else {
      createdAt = DateTime.now();
    }

    return Channel(
      id: json['id'] as String,
      type: chType,
      name: json['name'] as String?,
      avatarUrl: json['avatar_url'] as String?,
      createdBy: json['created_by'] as String,
      createdAt: createdAt,
      lastSeq: (json['last_seq'] as num?)?.toInt() ?? 0,
      readSeq: (json['read_seq'] as num?)?.toInt() ?? 0,
      lastMessage: json['last_message'] != null
          ? Message.fromJson(json['last_message'] as Map<String, dynamic>)
          : null,
      members: (json['members'] as List<dynamic>?)
              ?.map((m) => ChannelMember.fromJson(m as Map<String, dynamic>))
              .toList() ??
          [],
    );
  }
}

enum ChannelType { direct, group }

class ChannelMember {
  final String userId;
  final MemberRole role;
  final bool muted;
  final String? nickname;

  const ChannelMember({
    required this.userId,
    this.role = MemberRole.member,
    this.muted = false,
    this.nickname,
  });

  factory ChannelMember.fromJson(Map<String, dynamic> json) {
    return ChannelMember(
      userId: json['user_id'] as String,
      role: MemberRole.values[json['role'] as int? ?? 0],
      muted: json['mute'] == 1 || json['mute'] == true,
      nickname: json['nickname'] as String?,
    );
  }
}

enum MemberRole { member, admin, owner }
