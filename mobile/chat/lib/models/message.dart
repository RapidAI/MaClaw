/// A chat message supporting text, image, voice note, and file types.
class Message {
  final String id;
  final String channelId;
  final int seq;
  final String senderId;
  final String content;
  final MessageType type;
  final List<Attachment> attachments;
  final DateTime createdAt;
  final String clientMsgId;
  final bool recalled;
  final DateTime? editedAt;
  final SendStatus sendStatus;

  const Message({
    required this.id,
    required this.channelId,
    required this.seq,
    required this.senderId,
    required this.content,
    this.type = MessageType.text,
    this.attachments = const [],
    required this.createdAt,
    required this.clientMsgId,
    this.recalled = false,
    this.editedAt,
    this.sendStatus = SendStatus.sent,
  });

  bool get isText => type == MessageType.text;
  bool get isImage => type == MessageType.image;
  bool get isVoiceNote => type == MessageType.voiceNote;
  bool get hasAttachments => attachments.isNotEmpty;

  Message copyWith({SendStatus? sendStatus, String? id, int? seq}) {
    return Message(
      id: id ?? this.id,
      channelId: channelId,
      seq: seq ?? this.seq,
      senderId: senderId,
      content: content,
      type: type,
      attachments: attachments,
      createdAt: createdAt,
      clientMsgId: clientMsgId,
      recalled: recalled,
      editedAt: editedAt,
      sendStatus: sendStatus ?? this.sendStatus,
    );
  }

  factory Message.fromJson(Map<String, dynamic> json) {
    // created_at may be ISO string (from Go time.Time) or int millis (from local DB).
    DateTime createdAt;
    final rawCreated = json['created_at'];
    if (rawCreated is int) {
      createdAt = DateTime.fromMillisecondsSinceEpoch(rawCreated);
    } else if (rawCreated is String) {
      createdAt = DateTime.parse(rawCreated);
    } else {
      createdAt = DateTime.now();
    }

    DateTime? editedAt;
    final rawEdited = json['edited_at'];
    if (rawEdited is int) {
      editedAt = DateTime.fromMillisecondsSinceEpoch(rawEdited);
    } else if (rawEdited is String) {
      editedAt = DateTime.parse(rawEdited);
    }

    return Message(
      id: json['id'] as String,
      channelId: json['channel_id'] as String,
      seq: (json['seq'] as num?)?.toInt() ?? 0,
      senderId: json['sender_id'] as String,
      content: json['content'] as String? ?? '',
      type: MessageType.values[(json['msg_type'] as num?)?.toInt() ?? 0],
      attachments: (json['attachments'] as List<dynamic>?)
              ?.map((a) => Attachment.fromJson(a as Map<String, dynamic>))
              .toList() ??
          [],
      createdAt: createdAt,
      clientMsgId: json['client_msg_id'] as String? ?? '',
      recalled: json['recalled'] == 1 || json['recalled'] == true,
      editedAt: editedAt,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'channel_id': channelId,
        'seq': seq,
        'sender_id': senderId,
        'content': content,
        'msg_type': type.index,
        'attachments': attachments.map((a) => a.toJson()).toList(),
        'created_at': createdAt.millisecondsSinceEpoch,
        'client_msg_id': clientMsgId,
        'recalled': recalled ? 1 : 0,
        'edited_at': editedAt?.millisecondsSinceEpoch,
      };
}

enum MessageType { text, image, voiceNote, file }

/// Send status for optimistic UI updates.
enum SendStatus { sending, sent, failed }

class Attachment {
  final String type; // "image", "voice", "file"
  final String url;
  final String? thumbUrl;
  final int size;
  final int? durationMs; // for voice notes

  const Attachment({
    required this.type,
    required this.url,
    this.thumbUrl,
    required this.size,
    this.durationMs,
  });

  factory Attachment.fromJson(Map<String, dynamic> json) {
    return Attachment(
      type: json['type'] as String,
      url: json['url'] as String,
      thumbUrl: json['thumb_url'] as String?,
      size: json['size'] as int,
      durationMs: json['duration_ms'] as int?,
    );
  }

  Map<String, dynamic> toJson() => {
        'type': type,
        'url': url,
        'thumb_url': thumbUrl,
        'size': size,
        'duration_ms': durationMs,
      };
}
