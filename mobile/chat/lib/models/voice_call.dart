/// Represents a voice call session.
class VoiceCall {
  final String id;
  final String? channelId;
  final String callerId;
  final CallType type;
  final CallStatus status;
  final List<CallParticipant> participants;
  final DateTime createdAt;
  final DateTime? startedAt;
  final DateTime? endedAt;

  const VoiceCall({
    required this.id,
    this.channelId,
    required this.callerId,
    required this.type,
    this.status = CallStatus.ringing,
    this.participants = const [],
    required this.createdAt,
    this.startedAt,
    this.endedAt,
  });

  Duration? get duration =>
      startedAt != null && endedAt != null ? endedAt!.difference(startedAt!) : null;

  factory VoiceCall.fromJson(Map<String, dynamic> json) {
    return VoiceCall(
      id: json['id'] as String,
      channelId: json['channel_id'] as String?,
      callerId: json['caller_id'] as String,
      type: CallType.values[json['call_type'] as int? ?? 0],
      status: CallStatus.values[json['status'] as int? ?? 0],
      participants: (json['participants'] as List<dynamic>?)
              ?.map((p) => CallParticipant.fromJson(p as Map<String, dynamic>))
              .toList() ??
          [],
      createdAt: DateTime.fromMillisecondsSinceEpoch(json['created_at'] as int),
      startedAt: json['started_at'] != null
          ? DateTime.fromMillisecondsSinceEpoch(json['started_at'] as int)
          : null,
      endedAt: json['ended_at'] != null
          ? DateTime.fromMillisecondsSinceEpoch(json['ended_at'] as int)
          : null,
    );
  }
}

enum CallType { oneToOne, conference }

enum CallStatus { ringing, active, ended, missed, rejected }

class CallParticipant {
  final String userId;
  final DateTime? joinedAt;
  final DateTime? leftAt;

  const CallParticipant({required this.userId, this.joinedAt, this.leftAt});

  factory CallParticipant.fromJson(Map<String, dynamic> json) {
    return CallParticipant(
      userId: json['user_id'] as String,
      joinedAt: json['joined_at'] != null
          ? DateTime.fromMillisecondsSinceEpoch(json['joined_at'] as int)
          : null,
      leftAt: json['left_at'] != null
          ? DateTime.fromMillisecondsSinceEpoch(json['left_at'] as int)
          : null,
    );
  }
}
