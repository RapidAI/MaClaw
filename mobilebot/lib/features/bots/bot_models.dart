/// Wire shapes for `/api/v1/bots`, mirroring `hub/internal/botmgmt` the same
/// way the desktop GUI and maclaw_mobile do.
library;

import 'dart:convert';

import 'package:characters/characters.dart';

/// What a bot is doing right now, derived from run state + last reply.
enum BotWorkState { idle, running, waitingForUser, awaitingAnswer, failed }

extension BotWorkStateLabel on BotWorkState {
  String label() => switch (this) {
        BotWorkState.idle => '空闲',
        BotWorkState.running => '运行中',
        BotWorkState.waitingForUser => '待接管',
        BotWorkState.awaitingAnswer => '待回答',
        BotWorkState.failed => '失败',
      };

  bool get needsUser =>
      this == BotWorkState.waitingForUser ||
      this == BotWorkState.awaitingAnswer;
}

class Bot {
  final String id;
  final String name;
  final String description;
  final String instanceId;
  final String desktopMode;
  final String color;
  final String shape;
  final bool primary;
  final DateTime? createdAt;

  const Bot({
    required this.id,
    required this.name,
    this.description = '',
    this.instanceId = '',
    this.desktopMode = '',
    this.color = '',
    this.shape = '',
    this.primary = false,
    this.createdAt,
  });

  String get title {
    if (name.trim().isNotEmpty) return name.trim();
    if (description.trim().isNotEmpty) return description.trim();
    return id;
  }

  String get initials {
    final source = title.trim();
    if (source.isEmpty) return '?';
    final words = source.split(RegExp(r'\s+')).where((w) => w.isNotEmpty);
    // Grapheme clusters, not UTF-16 units — emoji/CJK names must not be
    // sliced mid-surrogate-pair.
    if (words.length >= 2) {
      return '${words.first.characters.first}${words.elementAt(1).characters.first}'
          .toUpperCase();
    }
    return source.characters.first.toUpperCase();
  }

  factory Bot.fromJson(Map<String, dynamic> json) => Bot(
        id: (json['id'] as String? ?? '').trim(),
        name: (json['name'] as String? ?? json['title'] as String? ?? '')
            .trim(),
        description: (json['description'] as String? ?? '').trim(),
        instanceId: (json['instance_id'] as String? ?? '').trim(),
        desktopMode: (json['desktop_mode'] as String? ?? '').trim(),
        color: (json['color'] as String? ?? '').trim(),
        shape: (json['shape'] as String? ?? '').trim(),
        primary: json['primary'] == true,
        createdAt: DateTime.tryParse(json['created_at'] as String? ?? ''),
      );
}

class BotAccess {
  final bool enabled;
  final String message;

  const BotAccess({required this.enabled, this.message = ''});

  static const disabled = BotAccess(enabled: false);

  factory BotAccess.fromJson(Map<String, dynamic> json) => BotAccess(
        enabled: json['enabled'] == true,
        message: (json['message'] as String? ?? '').trim(),
      );
}

enum BotPhase { plan, execute }

extension BotPhaseWire on BotPhase {
  String get wireValue => this == BotPhase.plan ? 'plan' : 'execute';

  static BotPhase? fromWire(String raw) => switch (raw.trim().toLowerCase()) {
        'plan' => BotPhase.plan,
        'execute' => BotPhase.execute,
        _ => null,
      };
}

/// A question or secret the bot cannot finish without the person.
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
  bool get isAnswerable =>
      question.trim().isNotEmpty || isChoice || isSecret;

  factory BotQuestion.fromJson(Map<String, dynamic> json) => BotQuestion(
        inputType: (json['ask_user_input_type'] as String? ?? '').trim(),
        secretName: (json['ask_user_secret_name'] as String? ?? '').trim(),
        question: (json['ask_user_question'] as String? ?? '').trim(),
        options: parseBotAskOptions(json['ask_user_options_json']),
      );
}

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
    if (inner is List) return _optionLabels(inner);
    return const [];
  }
  if (value is List) return _optionLabels(value);
  return const [];
}

List<String> _optionLabels(List<dynamic> items) => [
      for (final item in items)
        switch (item) {
          String s => s.trim(),
          Map m => (m['label'] ?? m['text'] ?? m['value'] ?? m['name'] ?? '')
              .toString()
              .trim(),
          _ => '',
        },
    ].where((label) => label.isNotEmpty).toList(growable: false);

class BotReplyImage {
  final String mime;
  final String data;

  const BotReplyImage({required this.mime, required this.data});

  factory BotReplyImage.fromJson(Map<String, dynamic> json) => BotReplyImage(
        mime: (json['mime'] as String? ?? '').trim(),
        data: (json['data'] as String? ?? '').trim(),
      );
}

class BotReplyFile {
  final String name;
  final String mime;
  final String data;

  const BotReplyFile({this.name = '', this.mime = '', this.data = ''});

  String get displayName => name.trim().isEmpty ? '文件' : name.trim();

  factory BotReplyFile.fromJson(Map<String, dynamic> json) => BotReplyFile(
        name: (json['name'] as String? ?? '').trim(),
        mime: (json['mime'] as String? ?? '').trim(),
        data: (json['data'] as String? ?? '').trim(),
      );
}

/// One finished turn — the synchronous message answer and the run poll answer
/// share this shape.
class BotReply {
  final String text;
  final String novncUrl;
  final bool handoff;
  final bool userControl;
  final String attentionReason;
  final BotQuestion question;
  final List<BotReplyImage> images;
  final List<BotReplyFile> files;
  final BotPhase? phase;

  const BotReply({
    this.text = '',
    this.novncUrl = '',
    this.handoff = false,
    this.userControl = false,
    this.attentionReason = '',
    this.question = const BotQuestion(),
    this.images = const [],
    this.files = const [],
    this.phase,
  });

  bool get isEmpty => text.trim().isEmpty && images.isEmpty && files.isEmpty;

  bool get needsDesktop => handoff || novncUrl.trim().isNotEmpty;

  BotWorkState get workState {
    if (question.isAnswerable) return BotWorkState.awaitingAnswer;
    if (needsDesktop) return BotWorkState.waitingForUser;
    return BotWorkState.idle;
  }

  factory BotReply.fromJson(Map<String, dynamic> json) => BotReply(
        text: (json['text'] as String? ?? '').toString(),
        novncUrl: (json['novnc_url'] as String? ?? '').trim(),
        handoff: json['handoff'] == true,
        userControl: json['user_control'] == true,
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

class BotRun {
  final String runId;
  final String status;

  const BotRun({required this.runId, this.status = 'running'});

  bool get accepted => runId.trim().isNotEmpty;

  factory BotRun.fromJson(Map<String, dynamic> json) => BotRun(
        runId: (json['run_id'] as String? ?? '').trim(),
        status: (json['status'] as String? ?? 'running').trim(),
      );
}

/// `POST .../desktop` answer: the watch URL plus who may touch the screen.
class BotDesktop {
  final String novncUrl;
  final bool userControl;
  final String attentionReason;
  final String desktopMode;
  final bool released;

  const BotDesktop({
    this.novncUrl = '',
    this.userControl = false,
    this.attentionReason = '',
    this.desktopMode = '',
    this.released = false,
  });

  bool get isRunning => novncUrl.trim().isNotEmpty;

  factory BotDesktop.fromJson(Map<String, dynamic> json) => BotDesktop(
        novncUrl: (json['novnc_url'] as String? ?? '').trim(),
        userControl: json['user_control'] == true,
        attentionReason: (json['attention_reason'] as String? ?? '').trim(),
        desktopMode: (json['desktop_mode'] as String? ?? '').trim(),
        released: json['released'] == true,
      );
}

/// One live run from `GET .../runs/active`.
class ActiveRun {
  final String runId;
  final String botId;
  final String source;
  final String status;
  final String task;
  final String step;
  final String nextStep;
  final String phase;
  final bool waitingForUser;
  final bool paused;
  final bool stale;
  final DateTime? startedAt;

  const ActiveRun({
    this.runId = '',
    this.botId = '',
    this.source = '',
    this.status = '',
    this.task = '',
    this.step = '',
    this.nextStep = '',
    this.phase = '',
    this.waitingForUser = false,
    this.paused = false,
    this.stale = false,
    this.startedAt,
  });

  factory ActiveRun.fromJson(Map<String, dynamic> json) => ActiveRun(
        runId: (json['run_id'] as String? ?? '').trim(),
        botId: (json['bot_id'] as String? ?? '').trim(),
        source: (json['source'] as String? ?? '').trim(),
        status: (json['status'] as String? ?? '').trim(),
        task: (json['task'] as String? ?? '').trim(),
        step: (json['step'] as String? ?? '').trim(),
        nextStep: (json['next_step'] as String? ?? '').trim(),
        phase: (json['phase'] as String? ?? '').trim(),
        waitingForUser: json['waiting_for_user'] == true,
        paused: json['paused'] == true,
        stale: json['stale'] == true,
        startedAt: DateTime.tryParse(json['started_at'] as String? ?? ''),
      );
}

/// One finished-but-unshown run from `GET /api/v1/bots/pending`.
class PendingDelivery {
  final String botId;
  final String runId;
  final String task;
  final String status;
  final String phase;
  final String error;
  final BotReply reply;

  const PendingDelivery({
    this.botId = '',
    this.runId = '',
    this.task = '',
    this.status = '',
    this.phase = '',
    this.error = '',
    this.reply = const BotReply(),
  });

  factory PendingDelivery.fromJson(Map<String, dynamic> json) =>
      PendingDelivery(
        botId: (json['bot_id'] as String? ?? '').trim(),
        runId: (json['run_id'] as String? ?? '').trim(),
        task: (json['task'] as String? ?? '').trim(),
        status: (json['status'] as String? ?? '').trim(),
        phase: (json['phase'] as String? ?? '').trim(),
        error: (json['error'] as String? ?? '').trim(),
        reply: json['reply'] is Map
            ? BotReply.fromJson(Map<String, dynamic>.from(json['reply']))
            : const BotReply(),
      );
}

/// `GET .../tasks` item — a status question about a recent desktop run.
class DesktopTaskStatus {
  final String runId;
  final String task;
  final String status;
  final String text;
  final String error;
  final String whereWeAre;
  final String nextStep;
  final String openProblems;
  final String taskOutcome;

  const DesktopTaskStatus({
    this.runId = '',
    this.task = '',
    this.status = '',
    this.text = '',
    this.error = '',
    this.whereWeAre = '',
    this.nextStep = '',
    this.openProblems = '',
    this.taskOutcome = '',
  });

  factory DesktopTaskStatus.fromJson(Map<String, dynamic> json) =>
      DesktopTaskStatus(
        runId: (json['run_id'] as String? ?? '').trim(),
        task: (json['task'] as String? ?? '').trim(),
        status: (json['status'] as String? ?? '').trim(),
        text: (json['text'] as String? ?? '').trim(),
        error: (json['error'] as String? ?? '').trim(),
        whereWeAre: (json['where_we_are'] as String? ?? '').trim(),
        nextStep: (json['next_step'] as String? ?? '').trim(),
        openProblems: (json['open_problems'] as String? ?? '').trim(),
        taskOutcome: (json['task_outcome'] as String? ?? '').trim(),
      );
}

/// One journal entry from `GET /api/v1/bots/conversation`. `data` holds the
/// bubble projection the writing client published (see desktopBotSync.ts).
class ConversationEntry {
  final int seq;
  final int createdSeq;
  final String botId;
  final String id;
  final String kind;
  final String origin;
  final Map<String, dynamic> data;
  final String at;

  const ConversationEntry({
    this.seq = 0,
    this.createdSeq = 0,
    this.botId = '',
    this.id = '',
    this.kind = '',
    this.origin = '',
    this.data = const {},
    this.at = '',
  });

  factory ConversationEntry.fromJson(Map<String, dynamic> json) =>
      ConversationEntry(
        seq: (json['seq'] as num? ?? 0).toInt(),
        createdSeq: (json['created_seq'] as num? ?? 0).toInt(),
        botId: (json['bot_id'] as String? ?? '').trim(),
        id: (json['id'] as String? ?? '').trim(),
        kind: (json['kind'] as String? ?? '').trim(),
        origin: (json['origin'] as String? ?? '').trim(),
        data: json['data'] is Map
            ? Map<String, dynamic>.from(json['data'] as Map)
            : const {},
        at: (json['at'] as String? ?? '').trim(),
      );
}

class ConversationPage {
  final List<ConversationEntry> entries;
  final int next;
  final int head;
  final bool reset;
  final bool more;

  const ConversationPage({
    this.entries = const [],
    this.next = 0,
    this.head = 0,
    this.reset = false,
    this.more = false,
  });

  factory ConversationPage.fromJson(Map<String, dynamic> json) =>
      ConversationPage(
        entries: [
          for (final item in (json['entries'] as List? ?? const []))
            if (item is Map)
              ConversationEntry.fromJson(Map<String, dynamic>.from(item)),
        ],
        next: (json['next'] as num? ?? 0).toInt(),
        head: (json['head'] as num? ?? 0).toInt(),
        reset: json['reset'] == true,
        more: json['more'] == true,
      );
}

/// `GET /api/v1/bots/credits` — the owner's system-free allowance.
class BotCredits {
  final bool applies;
  final bool unlimited;
  final double limit;
  final double used;
  final double remaining;

  const BotCredits({
    this.applies = false,
    this.unlimited = false,
    this.limit = 0,
    this.used = 0,
    this.remaining = 0,
  });

  factory BotCredits.fromJson(Map<String, dynamic> json) => BotCredits(
        applies: json['applies'] == true,
        unlimited: json['unlimited'] == true,
        limit: (json['limit'] as num? ?? 0).toDouble(),
        used: (json['used'] as num? ?? 0).toDouble(),
        remaining: (json['remaining'] as num? ?? 0).toDouble(),
      );
}

/// One feed note (`BotCrossNote`): a message between the person's bots, a
/// delegate order or a report, shown on the 动态 view.
class FeedNote {
  final String id;
  final String from;
  final String target;
  final String kind;
  final String text;
  final String order;
  final String topic;
  final String runId;
  final String createdAt;
  final List<String> artifacts;

  const FeedNote({
    this.id = '',
    this.from = '',
    this.target = '',
    this.kind = '',
    this.text = '',
    this.order = '',
    this.topic = '',
    this.runId = '',
    this.createdAt = '',
    this.artifacts = const [],
  });

  DateTime? get at => DateTime.tryParse(createdAt);

  factory FeedNote.fromJson(Map<String, dynamic> json) => FeedNote(
        id: (json['id'] as String? ?? '').trim(),
        from: (json['from'] as String? ?? '').trim(),
        target: (json['target'] as String? ?? '').trim(),
        kind: (json['kind'] as String? ?? '').trim(),
        text: (json['text'] as String? ?? '').trim(),
        order: (json['order'] as String? ?? '').trim(),
        topic: (json['topic'] as String? ?? '').trim(),
        runId: (json['run_id'] as String? ?? '').trim(),
        createdAt: (json['created_at'] as String? ?? '').trim(),
        artifacts: [
          for (final item in (json['artifacts'] as List? ?? const []))
            item.toString(),
        ],
      );
}

/// State of one chunked material upload (`MaterialUpload`): the file lands
/// at `~/素材/<bot>/<name>` on the bot's desktop when done.
class MaterialUpload {
  final String uploadId;
  final String name;
  final int received;
  final int total;
  final bool done;
  final String path;

  const MaterialUpload({
    this.uploadId = '',
    this.name = '',
    this.received = 0,
    this.total = 0,
    this.done = false,
    this.path = '',
  });

  factory MaterialUpload.fromJson(Map<String, dynamic> json) =>
      MaterialUpload(
        uploadId: (json['upload_id'] as String? ?? '').trim(),
        name: (json['name'] as String? ?? '').trim(),
        received: (json['received'] as num? ?? 0).toInt(),
        total: (json['total'] as num? ?? 0).toInt(),
        done: json['done'] == true,
        path: (json['path'] as String? ?? '').trim(),
      );
}

/// One delegation on the collab board (`GET /api/v1/bots/assignments`):
/// owner bot → target bot, state and the latest report.
class BotAssignment {
  final String id;
  final String ownerBot;
  final String targetBot;
  final String task;
  final String state;
  final String lastReport;
  final String openQuestion;
  final String review;
  final List<String> artifacts;
  final String resultNoteId;
  final DateTime? createdAt;

  const BotAssignment({
    this.id = '',
    this.ownerBot = '',
    this.targetBot = '',
    this.task = '',
    this.state = '',
    this.lastReport = '',
    this.openQuestion = '',
    this.review = '',
    this.artifacts = const [],
    this.resultNoteId = '',
    this.createdAt,
  });

  factory BotAssignment.fromJson(Map<String, dynamic> json) => BotAssignment(
        id: (json['id'] as String? ?? '').trim(),
        ownerBot: (json['owner_bot'] as String? ?? '').trim(),
        targetBot: (json['target_bot'] as String? ?? '').trim(),
        task: (json['task'] as String? ?? '').trim(),
        state: (json['state'] as String? ?? '').trim(),
        lastReport: (json['last_report'] as String? ?? '').trim(),
        openQuestion: (json['open_question'] as String? ?? '').trim(),
        review: (json['review'] as String? ?? '').trim(),
        artifacts: [
          for (final item in (json['artifacts'] as List? ?? const []))
            item.toString(),
        ],
        resultNoteId: (json['result_note_id'] as String? ?? '').trim(),
        createdAt: DateTime.tryParse(json['created_at'] as String? ?? ''),
      );
}

/// A group room between the person and their bots (`GET /api/v1/bot-rooms`).
class BotRoom {
  final String id;
  final String name;
  final List<String> members;
  final Map<String, String> attention;
  final DateTime? createdAt;

  const BotRoom({
    this.id = '',
    this.name = '',
    this.members = const [],
    this.attention = const {},
    this.createdAt,
  });

  factory BotRoom.fromJson(Map<String, dynamic> json) => BotRoom(
        id: (json['id'] as String? ?? '').trim(),
        name: (json['name'] as String? ?? '').trim(),
        members: [
          for (final item in (json['members'] as List? ?? const []))
            item.toString(),
        ],
        attention: json['attention'] is Map
            ? {
                for (final e in (json['attention'] as Map).entries)
                  e.key.toString(): e.value.toString(),
              }
            : const {},
        createdAt: DateTime.tryParse(json['created_at'] as String? ?? ''),
      );
}

/// One note in a room conversation or a direct "bot 对你说" say
/// (`GET /api/v1/bots/said[?room=]`). The person's own room messages come
/// back as `kind=room, from=human`.
class SaidNote {
  final String id;
  final String from;
  final String kind;
  final String text;
  final List<String> files;
  final List<String> answeredBy;
  final DateTime? createdAt;

  const SaidNote({
    this.id = '',
    this.from = '',
    this.kind = '',
    this.text = '',
    this.files = const [],
    this.answeredBy = const [],
    this.createdAt,
  });

  bool get isMine => from == 'human' || kind == 'room';

  factory SaidNote.fromJson(Map<String, dynamic> json) {
    final files = <String>[
      for (final item in (json['artifacts'] as List? ?? const []))
        item.toString(),
    ];
    for (final item in (json['files'] as List? ?? const [])) {
      if (item is Map) {
        final name = (item['name'] as String? ?? '').trim();
        if (name.isNotEmpty && !files.contains(name)) files.add(name);
      }
    }
    return SaidNote(
      id: (json['id'] as String? ?? '').trim(),
      from: (json['from'] as String? ?? '').trim(),
      kind: (json['kind'] as String? ?? '').trim(),
      text: (json['text'] as String? ?? '').trim(),
      files: files,
      answeredBy: [
        for (final item in (json['answered_by'] as List? ?? const []))
          item.toString(),
      ],
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? ''),
    );
  }
}

/// One chat bubble. Local state for pending/failed flags; everything else is
/// a projection compatible with the desktop journal entry data.
class ChatMessage {
  final String id;
  final String role; // 'user' | 'assistant'
  final String content;
  final bool pending;
  final bool failed;
  final bool queued;

  /// Live queue position from `GET /bots/{id}/queue/{cmid}` — 0 when the
  /// Hub did not report a position. `queueHeld` marks the `held` state
  /// (the person still has the keyboard).
  final int queuePosition;
  final bool queueHeld;
  final bool ack;
  final bool chat;
  final String phase;
  final String requestId;
  final String workOrder;
  final String handoffUrl;
  final bool userControl;
  final String attentionReason;

  /// The srv conversation this turn ran in — `bot#<id>` marks a per-run
  /// thread the person can explicitly continue (continue_session).
  final String conversation;
  final BotQuestion question;
  final bool askResolved;
  final List<String> options;
  final bool optionsUsed;
  final List<BotReplyImage> images;
  final List<BotReplyFile> files;
  final int syncSeq;
  final DateTime? at;

  const ChatMessage({
    required this.id,
    required this.role,
    this.content = '',
    this.pending = false,
    this.failed = false,
    this.queued = false,
    this.queuePosition = 0,
    this.queueHeld = false,
    this.ack = false,
    this.chat = false,
    this.phase = '',
    this.requestId = '',
    this.workOrder = '',
    this.handoffUrl = '',
    this.userControl = false,
    this.attentionReason = '',
    this.conversation = '',
    this.question = const BotQuestion(),
    this.askResolved = false,
    this.options = const [],
    this.optionsUsed = false,
    this.images = const [],
    this.files = const [],
    this.syncSeq = 0,
    this.at,
  });

  bool get isUser => role == 'user';

  ChatMessage copyWith({
    String? content,
    bool? pending,
    bool? failed,
    bool? queued,
    int? queuePosition,
    bool? queueHeld,
    bool? askResolved,
    bool? optionsUsed,
    int? syncSeq,
  }) =>
      ChatMessage(
        id: id,
        role: role,
        content: content ?? this.content,
        pending: pending ?? this.pending,
        failed: failed ?? this.failed,
        queued: queued ?? this.queued,
        queuePosition: queuePosition ?? this.queuePosition,
        queueHeld: queueHeld ?? this.queueHeld,
        ack: ack,
        chat: chat,
        phase: phase,
        requestId: requestId,
        workOrder: workOrder,
        handoffUrl: handoffUrl,
        userControl: userControl,
        attentionReason: attentionReason,
        conversation: conversation,
        question: question,
        askResolved: askResolved ?? this.askResolved,
        options: options,
        optionsUsed: optionsUsed ?? this.optionsUsed,
        images: images,
        files: files,
        syncSeq: syncSeq ?? this.syncSeq,
        at: at,
      );

  /// Projection written back to the shared journal — matches
  /// `syncProjection` in desktopBotSync.ts (local-only fields dropped).
  Map<String, dynamic>? get syncProjection {
    if (id.isEmpty || (role != 'user' && role != 'assistant')) return null;
    if (pending) return null;
    final out = <String, dynamic>{
      'role': role,
      'content': content.length > 6000 ? content.substring(0, 6000) : content,
    };
    if (phase.isNotEmpty) out['phase'] = phase;
    if (requestId.isNotEmpty && !requestId.startsWith('local:')) {
      out['requestId'] = requestId;
    }
    if (workOrder.isNotEmpty) out['workOrder'] = workOrder;
    if (handoffUrl.isNotEmpty) out['handoffUrl'] = handoffUrl;
    if (userControl) out['userControl'] = true;
    if (attentionReason.isNotEmpty) out['attentionReason'] = attentionReason;
    if (conversation.isNotEmpty) out['conversation'] = conversation;
    if (ack) out['ack'] = true;
    if (chat) out['chat'] = true;
    if (queued) out['queued'] = true;
    if (options.isNotEmpty) out['options'] = options;
    if (optionsUsed) out['optionsUsed'] = true;
    if (askResolved) out['askResolved'] = true;
    if (question.isAnswerable) {
      out['askUser'] = {
        'inputType': question.inputType,
        'secretName': question.secretName,
        'question': question.question,
        'options': question.options,
      };
    }
    if (at != null) out['at'] = at!.toIso8601String();
    return out;
  }

  factory ChatMessage.fromSyncData(
    String id,
    Map<String, dynamic> data, {
    int syncSeq = 0,
  }) {
    final ask = data['askUser'];
    final askMap = ask is Map ? Map<String, dynamic>.from(ask) : const {};
    return ChatMessage(
      id: id,
      role: (data['role'] as String? ?? 'assistant').trim(),
      content: (data['content'] as String? ?? '').toString(),
      failed: data['failed'] == true,
      queued: data['queued'] == true,
      ack: data['ack'] == true,
      chat: data['chat'] == true,
      phase: (data['phase'] as String? ?? '').trim(),
      requestId: (data['requestId'] as String? ??
              data['runId'] as String? ??
              '')
          .trim(),
      workOrder: (data['workOrder'] as String? ?? '').trim(),
      handoffUrl: (data['handoffUrl'] as String? ?? '').trim(),
      userControl: data['userControl'] == true,
      attentionReason: (data['attentionReason'] as String? ?? '').trim(),
      conversation: (data['conversation'] as String? ??
              data['Conversation'] as String? ??
              '')
          .trim(),
      question: BotQuestion(
        inputType: (askMap['inputType'] as String? ?? '').trim(),
        secretName: (askMap['secretName'] as String? ?? '').trim(),
        question: (askMap['question'] as String? ?? '').trim(),
        options: [
          for (final item in (askMap['options'] as List? ?? const []))
            item.toString().trim(),
        ].where((s) => s.isNotEmpty).toList(),
      ),
      askResolved: data['askResolved'] == true,
      options: [
        for (final item in (data['options'] as List? ?? const []))
          item.toString().trim(),
      ].where((s) => s.isNotEmpty).toList(),
      optionsUsed: data['optionsUsed'] == true,
      syncSeq: syncSeq,
      at: DateTime.tryParse(data['at'] as String? ?? ''),
    );
  }
}

/// Outcome of `POST /runs/{runID}/supplement` — extra instruction routed into
/// a still-running task.
class SupplementResult {
  final String supplementId;
  final bool accepted;
  final String state;
  final String detail;
  final String runId;

  const SupplementResult({
    this.supplementId = '',
    this.accepted = false,
    this.state = '',
    this.detail = '',
    this.runId = '',
  });

  factory SupplementResult.fromJson(Map<String, dynamic> json) =>
      SupplementResult(
        supplementId: (json['supplement_id'] as String? ?? '').trim(),
        accepted: json['accepted'] == true,
        state: (json['state'] as String? ?? '').trim(),
        detail: (json['detail'] as String? ?? '').trim(),
        runId: (json['run_id'] as String? ?? '').trim(),
      );
}

/// Outcome of `POST /{id}/followup` — a person's message routed to the
/// teammate run that owns the delegated task.
class FollowUpRoute {
  final bool routed;
  final String reason;
  final String botId;
  final String runId;
  final String supplementId;

  const FollowUpRoute({
    this.routed = false,
    this.reason = '',
    this.botId = '',
    this.runId = '',
    this.supplementId = '',
  });

  factory FollowUpRoute.fromJson(Map<String, dynamic> json) => FollowUpRoute(
        routed: json['routed'] == true,
        reason: (json['reason'] as String? ?? '').trim(),
        botId: (json['bot_id'] as String? ?? '').trim(),
        runId: (json['run_id'] as String? ?? '').trim(),
        supplementId: (json['supplement_id'] as String? ?? '').trim(),
      );
}

/// `GET /api/v1/bots/desktop-proxy-mode` — the caller's desktop route.
class DesktopProxyMode {
  /// Effective mode: proxy | direct | smart.
  final String mode;

  /// The user's own choice; "" follows the tenant default.
  final String userMode;
  final String tenantDefault;
  final bool locked;

  /// False when the choice was stored but the host could not be told yet.
  final bool applied;
  final String warning;

  const DesktopProxyMode({
    this.mode = 'smart',
    this.userMode = '',
    this.tenantDefault = '',
    this.locked = false,
    this.applied = true,
    this.warning = '',
  });

  factory DesktopProxyMode.fromJson(Map<String, dynamic> json) =>
      DesktopProxyMode(
        mode: (json['mode'] as String? ?? 'smart').trim(),
        userMode: (json['user_mode'] as String? ?? '').trim(),
        tenantDefault: (json['tenant_default'] as String? ?? '').trim(),
        locked: json['locked'] == true,
        applied: json['applied'] != false,
        warning: (json['warning'] as String? ?? '').trim(),
      );
}

/// `GET /api/v1/bots/desktop-computer` — which maintenance ops are allowed.
class DesktopComputerCaps {
  final bool restartAllowed;
  final bool resetAllowed;

  const DesktopComputerCaps(
      {this.restartAllowed = false, this.resetAllowed = false});

  factory DesktopComputerCaps.fromJson(Map<String, dynamic> json) =>
      DesktopComputerCaps(
        restartAllowed: json['restart_allowed'] == true,
        resetAllowed: json['reset_allowed'] == true,
      );
}

/// `POST /api/v1/bots/desktop-computer/{restart|reset}` outcome.
class ComputerResult {
  final String action;
  final String container;
  final String image;
  final String status;
  final String backup;

  const ComputerResult({
    this.action = '',
    this.container = '',
    this.image = '',
    this.status = '',
    this.backup = '',
  });

  factory ComputerResult.fromJson(Map<String, dynamic> json) => ComputerResult(
        action: (json['action'] as String? ?? '').trim(),
        container: (json['container'] as String? ?? '').trim(),
        image: (json['image'] as String? ?? '').trim(),
        status: (json['status'] as String? ?? '').trim(),
        backup: (json['backup'] as String? ?? '').trim(),
      );
}

/// `GET /bots/{id}/queue/{cmid}` — live view of a parked user message.
/// state: queued | held | running | done | missing.
class QueueView {
  final String state;
  final String runId;
  final bool held;
  final int position;
  final String outcome;

  const QueueView({
    this.state = '',
    this.runId = '',
    this.held = false,
    this.position = 0,
    this.outcome = '',
  });

  factory QueueView.fromJson(Map<String, dynamic> json) => QueueView(
        state: (json['state'] as String? ?? '').trim(),
        runId: (json['run_id'] as String? ?? json['runId'] as String? ?? '')
            .trim(),
        held: json['held'] == true,
        position: (json['position'] as num? ?? 0).toInt(),
        outcome: (json['outcome'] as String? ?? '').trim(),
      );
}

/// Fixed confirmation sentence the person sends to approve a plan —
/// matches `BOT_CONFIRM_TASK` in desktopBots.ts. The Hub recognises it
/// (typed-confirm sentence) and folds the plan reading.
const String botConfirmTaskText =
    '安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。';
