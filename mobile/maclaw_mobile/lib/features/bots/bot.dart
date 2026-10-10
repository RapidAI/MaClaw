/// Domain model for the MaClaw Bot client.
///
/// A bot is a Hub-provisioned worker with its own cloud desktop. This client
/// lists a user's bots, creates them, chats with them, and reports the work
/// state Hub reports back. The wire shapes mirror `hub/internal/botmgmt`
/// (`Bot` and `Reply`), so a field rename on the server shows up here as a
/// parse change rather than as silent empty UI.
library;

import 'dart:convert';

/// What a bot is doing right now.
///
/// Derived from the run lifecycle and the reply, not from a dedicated status
/// endpoint: Hub has no per-bot status call, and [BotRun] plus [BotReply]
/// already carry every signal.
enum BotWorkState {
  /// Nothing in flight; the last turn finished.
  idle,

  /// Hub admitted the command and the run has not produced a reply yet.
  running,

  /// The bot handed the desktop to the person (a login, captcha, or consent
  /// step it cannot finish alone).
  waitingForUser,

  /// The bot asked a question and is blocked on the answer.
  awaitingAnswer,

  /// The last turn failed.
  failed,
}

extension BotWorkStateLabel on BotWorkState {
  /// Human label shown on the rail card and the chat header.
  String label() => switch (this) {
        BotWorkState.idle => '空闲',
        BotWorkState.running => '运行中',
        BotWorkState.waitingForUser => '待接管',
        BotWorkState.awaitingAnswer => '待回答',
        BotWorkState.failed => '失败',
      };

  /// Whether this state means the user still owes the bot something.
  bool get needsUser =>
      this == BotWorkState.waitingForUser ||
      this == BotWorkState.awaitingAnswer;
}

/// One bot owned by the signed-in user.
///
/// Hub returns `id`/`name`/`description`/`instance_id`/`created_at`
/// (`hub/internal/botmgmt/service.go`); the desktop client additionally
/// exposes `title`, which is an alias the GUI carries. Both are read here so a
/// client talking to either shape still renders a name.
class Bot {
  final String id;
  final String name;
  final String description;
  final String instanceId;
  final DateTime? createdAt;

  const Bot({
    required this.id,
    required this.name,
    this.description = '',
    this.instanceId = '',
    this.createdAt,
  });

  /// Display name, falling back to the description then the id so a bot with a
  /// blank name is still addressable in the rail.
  String get title {
    if (name.trim().isNotEmpty) return name.trim();
    if (description.trim().isNotEmpty) return description.trim();
    return id;
  }

  /// One or two letters for the rail avatar. Derived, so it is stable across
  /// clients that never received an avatar.
  String get initials {
    final source = title.trim();
    if (source.isEmpty) return '?';
    final words = source.split(RegExp(r'\s+')).where((w) => w.isNotEmpty);
    if (words.length >= 2) {
      return '${words.first[0]}${words.elementAt(1)[0]}'.toUpperCase();
    }
    return source.substring(0, 1).toUpperCase();
  }

  factory Bot.fromJson(Map<String, dynamic> json) {
    return Bot(
      id: (json['id'] as String? ?? '').trim(),
      name: (json['name'] as String? ?? json['title'] as String? ?? '').trim(),
      description: (json['description'] as String? ?? '').trim(),
      instanceId: (json['instance_id'] as String? ?? '').trim(),
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? ''),
    );
  }
}

/// Phase of one turn, mirroring the desktop client's `plan`/`execute`.
///
/// A `plan` reply proposes an arrangement that the user confirms; the
/// confirmation is sent back as `execute`. Sending the wrong phase makes the
/// bot repeat or skip the approval, so it is parsed from the wire rather than
/// tracked locally.
enum BotPhase { plan, execute }

extension BotPhaseWire on BotPhase {
  String get wireValue => this == BotPhase.plan ? 'plan' : 'execute';

  static BotPhase? fromWire(String raw) => switch (raw.trim().toLowerCase()) {
        'plan' => BotPhase.plan,
        'execute' => BotPhase.execute,
        _ => null,
      };
}

/// A question or a login the bot cannot answer on its own.
class BotQuestion {
  final String inputType;
  final String secretName;
  final String question;
  final List<String> options;

  const BotQuestion({
    this.inputType = '',
    this.secretName = '',
    this.question = '',
    this.options = const [],
  });

  bool get isSecret => secretName.trim().isNotEmpty;

  bool get isChoice => options.isNotEmpty;

  bool get isAnswerable => question.trim().isNotEmpty || isChoice || isSecret;

  factory BotQuestion.fromJson(Map<String, dynamic> json) {
    return BotQuestion(
      inputType: (json['ask_user_input_type'] as String? ?? '').trim(),
      secretName: (json['ask_user_secret_name'] as String? ?? '').trim(),
      question: (json['ask_user_question'] as String? ?? '').trim(),
      options: parseBotAskOptions(json['ask_user_options_json']),
    );
  }
}

/// `ask_user_options_json` is a JSON-encoded string on the wire, not a nested
/// object. It has shipped as a bare array and as `{"options": [...]}`, so both
/// are accepted; anything else yields no options rather than throwing.
List<String> parseBotAskOptions(Object? raw) {
  Object? value = raw;
  if (value is String) {
    final text = value.trim();
    if (text.isEmpty) return const [];
    try {
      value = jsonDecode(text);
    } on FormatException {
      return const [];
    }
  }
  if (value is Map) {
    final inner = value['options'] ?? value['choices'];
    if (inner is List) return _botOptionLabels(inner);
    return const [];
  }
  if (value is List) return _botOptionLabels(value);
  return const [];
}

List<String> _botOptionLabels(List<dynamic> items) => [
      for (final item in items) _botOptionLabel(item),
    ].where((label) => label.isNotEmpty).toList(growable: false);

String _botOptionLabel(Object? item) {
  if (item is String) return item.trim();
  if (item is Map) {
    final label =
        item['label'] ?? item['text'] ?? item['value'] ?? item['name'];
    if (label is String) return label.trim();
  }
  return '';
}

/// One inline image the bot produced. `data` is standard base64 with no
/// `data:` prefix, matching the Go side.
class BotReplyImage {
  final String mime;
  final String data;

  const BotReplyImage({required this.mime, required this.data});

  factory BotReplyImage.fromJson(Map<String, dynamic> json) {
    return BotReplyImage(
      mime: (json['mime'] as String? ?? '').trim(),
      data: (json['data'] as String? ?? '').trim(),
    );
  }
}

/// One document the bot produced, inline as base64.
class BotReplyFile {
  final String name;
  final String mime;
  final String data;

  const BotReplyFile({
    required this.name,
    required this.mime,
    required this.data,
  });

  String get displayName => name.trim().isEmpty ? '文件' : name.trim();

  factory BotReplyFile.fromJson(Map<String, dynamic> json) {
    return BotReplyFile(
      name: (json['name'] as String? ?? '').trim(),
      mime: (json['mime'] as String? ?? '').trim(),
      data: (json['data'] as String? ?? '').trim(),
    );
  }
}

/// One bot turn's reply, the same object the synchronous message call returns
/// and the run poll returns once the run finishes.
class BotReply {
  final String text;
  final String novncUrl;
  final bool handoff;
  final String attentionReason;
  final BotQuestion question;
  final List<BotReplyImage> images;
  final List<BotReplyFile> files;
  final BotPhase? phase;

  const BotReply({
    this.text = '',
    this.novncUrl = '',
    this.handoff = false,
    this.attentionReason = '',
    this.question = const BotQuestion(),
    this.images = const [],
    this.files = const [],
    this.phase,
  });

  bool get isEmpty => text.trim().isEmpty && images.isEmpty && files.isEmpty;

  /// The bot gave the desktop back to the person.
  bool get needsDesktop => handoff || novncUrl.trim().isNotEmpty;

  /// The work state a finished reply implies.
  BotWorkState get workState {
    if (question.isAnswerable) return BotWorkState.awaitingAnswer;
    if (needsDesktop) return BotWorkState.waitingForUser;
    return BotWorkState.idle;
  }

  factory BotReply.fromJson(Map<String, dynamic> json) {
    return BotReply(
      text: (json['text'] as String? ?? '').toString(),
      novncUrl: (json['novnc_url'] as String? ?? '').trim(),
      handoff: json['handoff'] == true,
      attentionReason: (json['attention_reason'] as String? ?? '').trim(),
      question: BotQuestion.fromJson(json),
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
      phase: BotPhaseWire.fromWire(json['phase'] as String? ?? ''),
    );
  }
}

/// An admitted-but-unfinished run.
///
/// Hub answers `202` with this shape both when a message is admitted and when a
/// run poll is still in flight, so one type covers "we just started" and
/// "still working".
class BotRun {
  final String runId;
  final String status;

  const BotRun({required this.runId, this.status = 'running'});

  bool get accepted => runId.trim().isNotEmpty;

  bool get isRunning =>
      status.trim().toLowerCase() == 'running' ||
      status.trim().toLowerCase() == 'accepted' ||
      status.trim().toLowerCase() == 'pending';

  factory BotRun.fromJson(Map<String, dynamic> json) {
    return BotRun(
      runId: (json['run_id'] as String? ?? '').trim(),
      status: (json['status'] as String? ?? 'running').trim(),
    );
  }
}

/// The desktop view Hub hands back while a person watches or takes over.
class BotDesktop {
  final String novncUrl;
  final bool userControl;
  final String attentionReason;

  const BotDesktop({
    this.novncUrl = '',
    this.userControl = false,
    this.attentionReason = '',
  });

  bool get isRunning => novncUrl.trim().isNotEmpty;

  factory BotDesktop.fromJson(Map<String, dynamic> json) {
    return BotDesktop(
      novncUrl: (json['novnc_url'] as String? ?? '').trim(),
      userControl: json['user_control'] == true,
      attentionReason: (json['attention_reason'] as String? ?? '').trim(),
    );
  }
}

/// Whether this user may use bots at all, from `GET /api/v1/bots/access`.
///
/// When it is off the reason belongs to the admin, so the message Hub returns
/// is shown verbatim rather than replaced with generic copy.
class BotAccess {
  final bool enabled;
  final String message;

  const BotAccess({required this.enabled, this.message = ''});

  static const disabled = BotAccess(enabled: false);

  factory BotAccess.fromJson(Map<String, dynamic> json) {
    return BotAccess(
      enabled: json['enabled'] == true,
      message: (json['message'] as String? ?? '').trim(),
    );
  }
}
