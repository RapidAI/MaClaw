import 'bot.dart';

/// Who wrote a chat message.
enum BotMessageRole { user, assistant }

/// One message in a bot conversation.
///
/// Bot replies carry more than text: the bot may hand back its desktop, ask a
/// question, or attach an image or a document. Those signals are kept on the
/// message rather than collapsed into the body, because each one drives a
/// different control in the chat (a takeover card, an answer sheet, a file
/// tile) and each has to survive an app restart mid-turn.
class BotMessage {
  final String id;
  final BotMessageRole role;
  final String content;
  final DateTime createdAt;

  /// Phase the turn was sent with, or that the reply carried.
  final BotPhase? phase;

  /// The bot gave its desktop to the person for this turn.
  final bool handoff;

  final String attentionReason;
  final BotQuestion question;
  final List<BotReplyImage> images;
  final List<BotReplyFile> files;
  final String novncUrl;

  /// The send failed, or a poll gave up. Kept distinct from a bot reply that
  /// merely says something went wrong.
  final bool failed;

  /// This message is a locally pending bubble for a turn that has been
  /// admitted but has not replied yet.
  final bool pending;

  const BotMessage({
    required this.id,
    required this.role,
    required this.content,
    required this.createdAt,
    this.phase,
    this.handoff = false,
    this.attentionReason = '',
    this.question = const BotQuestion(),
    this.images = const [],
    this.files = const [],
    this.novncUrl = '',
    this.failed = false,
    this.pending = false,
  });

  bool get isUser => role == BotMessageRole.user;

  bool get needsDesktop => handoff || novncUrl.trim().isNotEmpty;

  /// The work state this message implies, used to drive the rail badge.
  BotWorkState get workState {
    if (failed) return BotWorkState.failed;
    if (pending) return BotWorkState.running;
    if (question.isAnswerable) return BotWorkState.awaitingAnswer;
    if (needsDesktop) return BotWorkState.waitingForUser;
    return BotWorkState.idle;
  }

  /// Whether this message is the last one in its conversation, which is what
  /// the rail reads to decide the badge.
  bool isNewerThan(BotMessage other) => createdAt.isAfter(other.createdAt);

  factory BotMessage.user({
    required String content,
    String? id,
    DateTime? createdAt,
    BotPhase? phase,
    bool pending = false,
  }) {
    final now = createdAt ?? DateTime.now().toUtc();
    return BotMessage(
      id: id ?? 'u-${now.microsecondsSinceEpoch}',
      role: BotMessageRole.user,
      content: content,
      createdAt: now,
      phase: phase,
      pending: pending,
    );
  }

  factory BotMessage.fromReply({
    required String id,
    required BotReply reply,
    DateTime? createdAt,
  }) {
    return BotMessage(
      id: id,
      role: BotMessageRole.assistant,
      content: reply.text,
      createdAt: createdAt ?? DateTime.now().toUtc(),
      phase: reply.phase,
      handoff: reply.handoff,
      attentionReason: reply.attentionReason,
      question: reply.question,
      images: reply.images,
      files: reply.files,
      novncUrl: reply.novncUrl,
    );
  }

  factory BotMessage.fromJson(Map<String, dynamic> json) {
    return BotMessage(
      id: (json['id'] as String? ?? '').trim(),
      role: (json['role'] as String? ?? 'assistant') == 'user'
          ? BotMessageRole.user
          : BotMessageRole.assistant,
      content: (json['content'] as String? ?? '').toString(),
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? '') ??
          DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
      phase: BotPhaseWire.fromWire(json['phase'] as String? ?? ''),
      handoff: json['handoff'] == true,
      attentionReason: (json['attention_reason'] as String? ?? '').trim(),
      question: BotQuestion(
        inputType: (json['ask_user_input_type'] as String? ?? '').trim(),
        secretName: (json['ask_user_secret_name'] as String? ?? '').trim(),
        question: (json['ask_user_question'] as String? ?? '').trim(),
        options: parseBotAskOptions(json['ask_user_options_json']),
      ),
      images: [
        for (final item in (json['images'] as List? ?? const []))
          if (item is Map)
            BotReplyImage.fromJson(Map<String, dynamic>.from(item)),
      ],
      files: [
        for (final item in (json['files'] as List? ?? const []))
          if (item is Map)
            BotReplyFile.fromJson(Map<String, dynamic>.from(item)),
      ],
      novncUrl: (json['novnc_url'] as String? ?? '').trim(),
      failed: json['failed'] == true,
      pending: json['pending'] == true,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'role': isUser ? 'user' : 'assistant',
      'content': content,
      'created_at': createdAt.toIso8601String(),
      if (phase != null) 'phase': phase!.wireValue,
      if (handoff) 'handoff': true,
      if (attentionReason.isNotEmpty) 'attention_reason': attentionReason,
      if (question.inputType.isNotEmpty)
        'ask_user_input_type': question.inputType,
      if (question.secretName.isNotEmpty)
        'ask_user_secret_name': question.secretName,
      if (question.question.isNotEmpty) 'ask_user_question': question.question,
      if (question.options.isNotEmpty)
        'ask_user_options_json': question.options,
      if (images.isNotEmpty)
        'images': [
          for (final image in images) {'mime': image.mime, 'data': image.data},
        ],
      if (files.isNotEmpty)
        'files': [
          for (final file in files)
            {'name': file.name, 'mime': file.mime, 'data': file.data},
        ],
      if (novncUrl.isNotEmpty) 'novnc_url': novncUrl,
      if (failed) 'failed': true,
      if (pending) 'pending': true,
    };
  }

  BotMessage copyWith({
    String? id,
    String? content,
    DateTime? createdAt,
    BotPhase? phase,
    bool? handoff,
    String? attentionReason,
    BotQuestion? question,
    List<BotReplyImage>? images,
    List<BotReplyFile>? files,
    String? novncUrl,
    bool? failed,
    bool? pending,
  }) {
    return BotMessage(
      id: id ?? this.id,
      role: role,
      content: content ?? this.content,
      createdAt: createdAt ?? this.createdAt,
      phase: phase ?? this.phase,
      handoff: handoff ?? this.handoff,
      attentionReason: attentionReason ?? this.attentionReason,
      question: question ?? this.question,
      images: images ?? this.images,
      files: files ?? this.files,
      novncUrl: novncUrl ?? this.novncUrl,
      failed: failed ?? this.failed,
      pending: pending ?? this.pending,
    );
  }
}

/// Whether a pending bubble has aged out.
///
/// A pending bubble means "sent, waiting for Hub". The run lives on the Hub, so
/// if the app was killed while it was pending the run may never be read back.
/// The clock only covers that case: once this process has admitted the run it
/// is tracked in memory and stays busy for as long as the turn takes, however
/// long a bot needs.
const Duration botPendingReplyWait = Duration(minutes: 31);

/// Whether a stored pending bubble is stale and should be settled as failed.
///
/// [now] is injectable so a test can place itself on either side of the
/// boundary instead of waiting.
bool botPendingReplyIsStale(BotMessage message, {DateTime? now}) {
  if (!message.pending) return false;
  final at = now ?? DateTime.now().toUtc();
  return at.difference(message.createdAt) > botPendingReplyWait;
}

/// One bot's conversation, kept locally so a reload shows history immediately.
class BotConversation {
  final String botId;
  final List<BotMessage> messages;

  const BotConversation({required this.botId, this.messages = const []});

  /// The newest message, which is what the rail badge is derived from.
  BotMessage? get latest => messages.isEmpty
      ? null
      : messages.reduce((a, b) => b.isNewerThan(a) ? b : a);

  /// Whether the conversation needs the person: an unanswered question, a
  /// desktop handoff, or a turn that failed.
  bool get needsUser {
    final last = latest;
    if (last == null) return false;
    if (botPendingReplyIsStale(last)) return false;
    return last.workState.needsUser || last.failed;
  }

  /// Whether a turn is in flight right now.
  bool get busy {
    final last = latest;
    if (last == null) return false;
    if (botPendingReplyIsStale(last)) return false;
    return last.workState == BotWorkState.running;
  }
}

/// Keep the newest [limit] messages of a conversation.
///
/// Bot turns can be long and carry base64 attachments, so an unbounded log
/// would grow without limit on disk. The tail is what a reader needs.
List<BotMessage> retainRecentBotMessages(
  List<BotMessage> messages, {
  int limit = 200,
}) {
  if (limit <= 0 || messages.length <= limit) return messages;
  return messages.sublist(messages.length - limit);
}

/// Append [message], replacing an existing message with the same id.
///
/// A pending user bubble is settled in place by id when its reply lands, so the
/// transcript never shows the same turn twice.
List<BotMessage> upsertBotMessage(
  List<BotMessage> messages,
  BotMessage message, {
  int limit = 200,
}) {
  final next = [
    for (final item in messages)
      if (item.id != message.id) item,
    message,
  ]..sort((a, b) => a.createdAt.compareTo(b.createdAt));
  return retainRecentBotMessages(next, limit: limit);
}
