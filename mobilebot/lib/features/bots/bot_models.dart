/// Wire shapes for `/api/v1/bots`, mirroring `hub/internal/botmgmt` the same
/// way the desktop GUI and maclaw_mobile do.
library;

import 'dart:convert';

import 'package:characters/characters.dart';

import '../../core/json_safe.dart';
import '../../l10n/gen/app_localizations.dart';

export '../../core/json_safe.dart';

/// What a bot is doing right now, derived from run state + last reply.
enum BotWorkState { idle, running, waitingForUser, awaitingAnswer, failed }

extension BotWorkStateLabel on BotWorkState {
  String label(AppLocalizations S) => switch (this) {
    BotWorkState.idle => S.workStateIdle,
    BotWorkState.running => S.workStateRunning,
    BotWorkState.waitingForUser => S.workStateWaitingUser,
    BotWorkState.awaitingAnswer => S.workStateAwaitingAnswer,
    BotWorkState.failed => S.workStateFailed,
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
    id: syncText(json['id']).trim(),
    name: syncTextAny(json['name'], json['title']).trim(),
    description: syncText(json['description']).trim(),
    instanceId: syncText(json['instance_id']).trim(),
    desktopMode: syncText(json['desktop_mode']).trim(),
    color: syncText(json['color']).trim(),
    shape: syncText(json['shape']).trim(),
    primary: json['primary'] == true,
    createdAt: DateTime.tryParse(syncText(json['created_at'])),
  );
}

/// Owner grant for bots that use a computer. A null [limit] is unlimited.
/// [known] is false when the list response has no `desktop_quota` (an older Hub).
/// [cloudDesktop] is the assigned-cloud flag; [lanDesktop] is the owner's
/// pinned node having MaClawSrv. Both are null on older hubs (= available).
class DesktopBotQuota {
  final int? limit;
  final int used;
  final bool known;
  final bool? cloudDesktop;
  final bool? lanDesktop;

  const DesktopBotQuota({
    this.limit,
    this.used = 0,
    this.known = false,
    this.cloudDesktop,
    this.lanDesktop,
  });

  bool get exhausted => known && limit != null && used >= limit!;

  /// A fresh create has no computer path at all: no assigned cloud desktop
  /// and no pinned LAN node with srv.
  bool get computerMissing =>
      known && cloudDesktop == false && lanDesktop != true;

  /// A node-less none bot flipping to a computer mode needs the cloud
  /// specifically — a LAN pin does not apply to an already-created bot.
  bool get cloudMissing => known && cloudDesktop == false;

  String label(AppLocalizations S) {
    if (!known) return '';
    if (limit == null) return S.quotaUsed(used);
    return S.quotaUsedOf(used, limit!);
  }

  DesktopBotQuota bump() => DesktopBotQuota(
    limit: limit,
    used: used + 1,
    known: known,
    cloudDesktop: cloudDesktop,
    lanDesktop: lanDesktop,
  );

  DesktopBotQuota drop() => DesktopBotQuota(
    limit: limit,
    used: used > 0 ? used - 1 : 0,
    known: known,
    cloudDesktop: cloudDesktop,
    lanDesktop: lanDesktop,
  );

  factory DesktopBotQuota.fromJson(Object? raw) {
    if (raw is! Map) return const DesktopBotQuota();
    final limitRaw = raw['limit'];
    final usedRaw = raw['used'];
    final cloudRaw = raw['cloud_desktop'];
    final lanRaw = raw['lan_desktop'];
    return DesktopBotQuota(
      limit: limitRaw is num ? limitRaw.toInt() : null,
      used: usedRaw is num ? usedRaw.toInt() : 0,
      known: true,
      cloudDesktop: cloudRaw is bool ? cloudRaw : null,
      lanDesktop: lanRaw is bool ? lanRaw : null,
    );
  }
}

/// List mark. An empty mode is the shared computer, matching the desktop client.
String desktopModeMark(String mode, AppLocalizations S) => switch (mode) {
  'none' => S.desktopMarkNone,
  'isolated' => S.desktopMarkIsolated,
  _ => S.desktopMarkShared,
};

bool botHasComputer(String mode) => mode != 'none';

/// The person-facing sentence for a desktop handoff reason. The desktop sends
/// short enums (login_wall, payment_confirm, …) that must not show raw; an
/// unknown reason falls back to itself, so a newer desktop still reads sensibly.
String attentionReasonLabel(String reason, AppLocalizations S) => switch (reason.trim()) {
  'login_wall' => S.chatAttentionLoginWall,
  'captcha_widget' => S.chatAttentionCaptcha,
  'mfa' => S.chatAttentionMfa,
  'login_qr' => S.chatAttentionQr,
  'payment_confirm' => S.chatAttentionPayment,
  'computer_takeover' => S.chatAttentionTakeover,
  _ => reason.trim(),
};

/// True when the ask on this message is really a desktop handoff: the next
/// step happens on the computer screen, not as a typed chat answer. Such an
/// ask renders as a takeover card instead of an answer field. A handoff ask
/// is always free text; an ask with option chips (choice/confirm) or a
/// secret field keeps the answer card even if a handoff rides along, so the
/// choices/obscured input stay usable.
bool isDesktopHandoffAsk(ChatMessage message) =>
    !message.question.isSecret &&
    message.question.options.isEmpty &&
    (message.attentionReason.isNotEmpty || message.handoffUrl.isNotEmpty);

/// True when another bot already has this name — the same quick check the
/// desktop dialog runs (trim + collapse whitespace + fold case). The Hub is
/// still authoritative for full-width and numbered-name forms.
bool botNameTaken(String name, List<Bot> bots, {String exceptId = ''}) {
  String norm(String s) =>
      s.trim().replaceAll(RegExp(r'\s+'), ' ').toLowerCase();
  final want = norm(name);
  if (want.isEmpty) return false;
  return bots.any((b) => b.id != exceptId && norm(b.name) == want);
}

class BotAccess {
  final bool enabled;
  final String message;

  const BotAccess({required this.enabled, this.message = ''});

  static const disabled = BotAccess(enabled: false);

  factory BotAccess.fromJson(Map<String, dynamic> json) => BotAccess(
    enabled: json['enabled'] == true,
    message: syncText(json['message']).trim(),
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
  bool get isAnswerable => question.trim().isNotEmpty || isChoice || isSecret;

  factory BotQuestion.fromJson(Map<String, dynamic> json) => BotQuestion(
    inputType: syncText(json['ask_user_input_type']).trim(),
    secretName: syncText(json['ask_user_secret_name']).trim(),
    question: syncText(json['ask_user_question']).trim(),
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
      Map m =>
        (m['label'] ?? m['text'] ?? m['value'] ?? m['name'] ?? '')
            .toString()
            .trim(),
      _ => '',
    },
].where((label) => label.isNotEmpty).toList(growable: false);

class BotReplyImage {
  final String mime;
  final String data;

  /// Server/volume id for this picture. Bytes are fetched on demand by the
  /// client that staged them; the id alone travels in the shared journal.
  final String attachmentId;

  const BotReplyImage({
    required this.mime,
    required this.data,
    this.attachmentId = '',
  });

  factory BotReplyImage.fromJson(Map<String, dynamic> json) => BotReplyImage(
    mime: syncText(json['mime']),
    data: syncText(json['data']),
    attachmentId: syncText(json['attachment_id']).isNotEmpty
        ? syncText(json['attachment_id'])
        : syncText(json['attachmentId']),
  );
}

class BotReplyFile {
  final String name;
  final String mime;
  final String data;
  final String attachmentId;

  const BotReplyFile({
    this.name = '',
    this.mime = '',
    this.data = '',
    this.attachmentId = '',
  });

  String displayName(AppLocalizations S) =>
      name.trim().isEmpty ? S.fallbackFileName : name.trim();

  factory BotReplyFile.fromJson(Map<String, dynamic> json) => BotReplyFile(
    name: syncText(json['name']),
    mime: syncText(json['mime']),
    data: syncText(json['data']),
    attachmentId: syncText(json['attachment_id']).isNotEmpty
        ? syncText(json['attachment_id'])
        : syncText(json['attachmentId']),
  );
}

/// Documents a reply produced on the computer. A desktop turn saves them on
/// its own disk and records only their paths (`local_file_paths`) — a phone
/// cannot open those, so it fetches the bytes by path (desktop-file).
/// Paths other than plain absolute ones are refused by the desktop reader.
List<String> localFilePathsFrom(Map<String, dynamic> data) {
  final paths = <String>[];
  for (final value in [
    data['localFilePath'],
    data['LocalFilePath'],
    data['local_file_path'],
    data['localFilePaths'],
    data['LocalFilePaths'],
    data['local_file_paths'],
  ]) {
    if (value is String && value.trim().isNotEmpty) paths.add(value.trim());
    if (value is List) {
      for (final item in value) {
        if (item is String && item.trim().isNotEmpty) paths.add(item.trim());
      }
    }
  }
  return paths.toSet().toList(growable: false);
}

/// Portable media ids — `desktop_attachment_ids` on a run result,
/// `attachmentIds` in a shared journal projection (mirrors
/// `attachmentIdsFrom` in desktopBotResultStore.ts).
List<String> attachmentIdsFrom(Map<String, dynamic> data) {
  final raw =
      data['attachmentIds'] ??
      data['desktop_attachment_ids'] ??
      data['DesktopAttachmentIDs'];
  if (raw is! List) return const [];
  final ok = RegExp(r'^[A-Za-z0-9_.:-]{1,80}$');
  return [
    for (final item in raw)
      if (ok.hasMatch(item.toString().trim())) item.toString().trim(),
  ];
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

  /// Files the computer left on its own disk for this reply (`local_file_paths`).
  /// It carries no bytes; a client that did not produce them fetches the
  /// file by path through `POST .../desktop-file`.
  final List<String> localFilePaths;
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
    this.localFilePaths = const [],
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
    text: syncText(json['text']).toString(),
    novncUrl: syncText(json['novnc_url']).trim(),
    handoff: json['handoff'] == true,
    userControl: json['user_control'] == true,
    attentionReason: syncText(json['attention_reason']).trim(),
    question: BotQuestion.fromJson(json),
    images: [
      for (final item in syncList(json['images']))
        if (item is Map)
          BotReplyImage.fromJson(Map<String, dynamic>.from(item)),
    ],
    files: [
      for (final item in syncList(json['files']))
        if (item is Map) BotReplyFile.fromJson(Map<String, dynamic>.from(item)),
    ],
    localFilePaths: localFilePathsFrom(json),
    phase: BotPhaseWire.fromWire(syncText(json['phase'])),
  );
}

class BotRun {
  final String runId;
  final String status;

  const BotRun({required this.runId, this.status = 'running'});

  bool get accepted => runId.trim().isNotEmpty;

  factory BotRun.fromJson(Map<String, dynamic> json) => BotRun(
    runId: syncText(json['run_id']).trim(),
    status: syncTextOr(syncText(json['status']), 'running').trim(),
  );
}

/// `POST .../desktop` answer: the watch URL plus who may touch the screen.
class BotDesktop {
  final String novncUrl;
  final bool userControl;

  /// Whether the keyboard claim this device made via `desktop/control` is
  /// the active one — `user_control` says the watcher may control at all,
  /// `input_granted` says this end actually holds the input right now.
  final bool inputGranted;
  final String attentionReason;
  final String desktopMode;
  final bool released;

  const BotDesktop({
    this.novncUrl = '',
    this.userControl = false,
    this.inputGranted = false,
    this.attentionReason = '',
    this.desktopMode = '',
    this.released = false,
  });

  bool get isRunning => novncUrl.trim().isNotEmpty;

  factory BotDesktop.fromJson(Map<String, dynamic> json) => BotDesktop(
    novncUrl: syncText(json['novnc_url']).trim(),
    userControl: json['user_control'] == true,
    inputGranted: json['input_granted'] == true,
    attentionReason: syncText(json['attention_reason']).trim(),
    desktopMode: syncText(json['desktop_mode']).trim(),
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
    runId: syncText(json['run_id']).trim(),
    botId: syncText(json['bot_id']).trim(),
    source: syncText(json['source']).trim(),
    status: syncText(json['status']).trim(),
    task: syncText(json['task']).trim(),
    step: syncText(json['step']).trim(),
    nextStep: syncText(json['next_step']).trim(),
    phase: syncText(json['phase']).trim(),
    waitingForUser: json['waiting_for_user'] == true,
    paused: json['paused'] == true,
    stale: json['stale'] == true,
    startedAt: DateTime.tryParse(syncText(json['started_at'])),
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
        botId: syncText(json['bot_id']).trim(),
        runId: syncText(json['run_id']).trim(),
        task: syncText(json['task']).trim(),
        status: syncText(json['status']).trim(),
        phase: syncText(json['phase']).trim(),
        error: syncText(json['error']).trim(),
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
        runId: syncText(json['run_id']).trim(),
        task: syncText(json['task']).trim(),
        status: syncText(json['status']).trim(),
        text: syncText(json['text']).trim(),
        error: syncText(json['error']).trim(),
        whereWeAre: syncText(json['where_we_are']).trim(),
        nextStep: syncText(json['next_step']).trim(),
        openProblems: syncText(json['open_problems']).trim(),
        taskOutcome: syncText(json['task_outcome']).trim(),
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
        seq: syncInt(json['seq']),
        createdSeq: syncInt(json['created_seq']),
        botId: syncText(json['bot_id']),
        id: syncText(json['id']),
        kind: syncText(json['kind']),
        origin: syncText(json['origin']),
        data: json['data'] is Map
            ? Map<String, dynamic>.from(json['data'] as Map)
            : const {},
        at: syncText(json['at']),
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
          for (final item in syncList(json['entries']))
            if (item is Map)
              ConversationEntry.fromJson(Map<String, dynamic>.from(item)),
        ],
        next: syncInt(json['next']),
        head: syncInt(json['head']),
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
    limit: syncDouble(json['limit']),
    used: syncDouble(json['used']),
    remaining: syncDouble(json['remaining']),
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
    id: syncText(json['id']).trim(),
    from: syncText(json['from']).trim(),
    target: syncText(json['target']).trim(),
    kind: syncText(json['kind']).trim(),
    text: syncText(json['text']).trim(),
    order: syncText(json['order']).trim(),
    topic: syncText(json['topic']).trim(),
    runId: syncText(json['run_id']).trim(),
    createdAt: syncText(json['created_at']).trim(),
    artifacts: [
      for (final item in syncList(json['artifacts'])) item.toString(),
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

  factory MaterialUpload.fromJson(Map<String, dynamic> json) => MaterialUpload(
    uploadId: syncText(json['upload_id']).trim(),
    name: syncText(json['name']).trim(),
    received: syncInt(json['received']),
    total: syncInt(json['total']),
    done: json['done'] == true,
    path: syncText(json['path']).trim(),
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
    id: syncText(json['id']).trim(),
    ownerBot: syncText(json['owner_bot']).trim(),
    targetBot: syncText(json['target_bot']).trim(),
    task: syncText(json['task']).trim(),
    state: syncText(json['state']).trim(),
    lastReport: syncText(json['last_report']).trim(),
    openQuestion: syncText(json['open_question']).trim(),
    review: syncText(json['review']).trim(),
    artifacts: [
      for (final item in syncList(json['artifacts'])) item.toString(),
    ],
    resultNoteId: syncText(json['result_note_id']).trim(),
    createdAt: DateTime.tryParse(syncText(json['created_at'])),
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
    id: syncText(json['id']).trim(),
    name: syncText(json['name']).trim(),
    members: [for (final item in syncList(json['members'])) item.toString()],
    attention: json['attention'] is Map
        ? {
            for (final e in (json['attention'] as Map).entries)
              e.key.toString(): e.value.toString(),
          }
        : const {},
    createdAt: DateTime.tryParse(syncText(json['created_at'])),
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
      for (final item in syncList(json['artifacts'])) item.toString(),
    ];
    for (final item in syncList(json['files'])) {
      if (item is Map) {
        final name = syncText(item['name']).trim();
        if (name.isNotEmpty && !files.contains(name)) files.add(name);
      }
    }
    return SaidNote(
      id: syncText(json['id']).trim(),
      from: syncText(json['from']).trim(),
      kind: syncText(json['kind']).trim(),
      text: syncText(json['text']).trim(),
      files: files,
      answeredBy: [
        for (final item in syncList(json['answered_by'])) item.toString(),
      ],
      createdAt: DateTime.tryParse(syncText(json['created_at'])),
    );
  }
}

/// One chat bubble. Local state for pending/failed flags; everything else is
/// Journal `at` may arrive as an ISO string (this app's projection) or a
/// millisecond number (other clients); anything else reads as no timestamp.
DateTime? parseMessageAt(Object? value) {
  if (value is num) {
    return DateTime.fromMillisecondsSinceEpoch(value.toInt());
  }
  return DateTime.tryParse(value is String ? value : '');
}

/// a projection compatible with the desktop journal entry data.
class ChatMessage {
  final String id;
  final String role; // 'user' | 'assistant'
  final String content;
  final bool pending;
  final bool failed;

  /// Why this send failed. Local only — not written to the shared journal.
  /// Empty when the bubble is not failed, or when a synced copy has no reason.
  final String failureReason;
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

  /// The run this turn produced. Attachment bytes never travel in the
  /// journal; a synced bubble re-fetches them through this id.
  final String runId;

  /// Media the owning client staged under these ids (screenshots, files).
  /// Its presence on a synced bubble says the run is worth re-fetching.
  final List<String> attachmentIds;

  /// Files this reply left on the computer, by path. They stay here only
  /// until their bytes are fetched (see [files]); a path alone is never
  /// shown as a file, because a phone cannot open it.
  final List<String> localFilePaths;
  final int syncSeq;
  final DateTime? at;

  const ChatMessage({
    required this.id,
    required this.role,
    this.content = '',
    this.pending = false,
    this.failed = false,
    this.failureReason = '',
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
    this.runId = '',
    this.attachmentIds = const [],
    this.localFilePaths = const [],
    this.syncSeq = 0,
    this.at,
  });

  bool get isUser => role == 'user';

  ChatMessage copyWith({
    String? content,
    bool? pending,
    bool? failed,
    String? failureReason,
    bool? queued,
    int? queuePosition,
    bool? queueHeld,
    bool? askResolved,
    bool? optionsUsed,
    List<BotReplyImage>? images,
    List<BotReplyFile>? files,
    List<String>? localFilePaths,
    String? runId,
    List<String>? attachmentIds,
    int? syncSeq,
  }) => ChatMessage(
    id: id,
    role: role,
    content: content ?? this.content,
    pending: pending ?? this.pending,
    failed: failed ?? this.failed,
    failureReason: failureReason ?? this.failureReason,
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
    images: images ?? this.images,
    files: files ?? this.files,
    localFilePaths: localFilePaths ?? this.localFilePaths,
    runId: runId ?? this.runId,
    attachmentIds: attachmentIds ?? this.attachmentIds,
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
    if (pending) out['pending'] = true;
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
    if (runId.isNotEmpty) out['runId'] = runId;
    final mediaIds = [
      for (final image in images)
        if (image.attachmentId.isNotEmpty) image.attachmentId,
      for (final file in files)
        if (file.attachmentId.isNotEmpty) file.attachmentId,
      ...attachmentIds,
    ];
    if (mediaIds.isNotEmpty) out['attachmentIds'] = mediaIds;
    // The file bytes never travel in the journal either. The path is the
    // fetch key a client this reply did not originate (a phone) uses.
    if (localFilePaths.isNotEmpty) out['localFilePaths'] = localFilePaths;
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
    final requestId = syncText(data['requestId']);
    final runId = syncText(data['runId']);
    return ChatMessage(
      id: id,
      role: data['role'] is String
          ? (data['role'] as String).trim()
          : 'assistant',
      content: syncText(data['content']),
      pending: data['pending'] == true,
      failed: data['failed'] == true,
      failureReason: syncText(data['failureReason']),
      queued: data['queued'] == true,
      ack: data['ack'] == true,
      chat: data['chat'] == true,
      phase: syncText(data['phase']),
      requestId: requestId.isNotEmpty ? requestId : runId,
      workOrder: syncText(data['workOrder']),
      handoffUrl: syncText(data['handoffUrl']),
      userControl: data['userControl'] == true,
      attentionReason: syncText(data['attentionReason']),
      conversation: syncText(data['conversation']).isNotEmpty
          ? syncText(data['conversation'])
          : syncText(data['Conversation']),
      question: BotQuestion(
        inputType: syncText(askMap['inputType']),
        secretName: syncText(askMap['secretName']),
        question: syncText(askMap['question']),
        options: [
          for (final item in syncList(askMap['options']))
            item.toString().trim(),
        ].where((s) => s.isNotEmpty).toList(),
      ),
      askResolved: data['askResolved'] == true,
      options: [
        for (final item in syncList(data['options'])) item.toString().trim(),
      ].where((s) => s.isNotEmpty).toList(),
      optionsUsed: data['optionsUsed'] == true,
      images: [
        for (final item in syncList(data['images']))
          if (item is Map)
            BotReplyImage.fromJson(Map<String, dynamic>.from(item)),
      ],
      files: [
        for (final item in syncList(data['files']))
          if (item is Map)
            BotReplyFile.fromJson(Map<String, dynamic>.from(item)),
      ],
      runId: runId.isNotEmpty ? runId : syncText(data['run_id']),
      attachmentIds: attachmentIdsFrom(data),
      localFilePaths: localFilePathsFrom(data),
      syncSeq: syncSeq,
      at: parseMessageAt(data['at']),
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
        supplementId: syncText(json['supplement_id']).trim(),
        accepted: json['accepted'] == true,
        state: syncText(json['state']).trim(),
        detail: syncText(json['detail']).trim(),
        runId: syncText(json['run_id']).trim(),
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
    reason: syncText(json['reason']).trim(),
    botId: syncText(json['bot_id']).trim(),
    runId: syncText(json['run_id']).trim(),
    supplementId: syncText(json['supplement_id']).trim(),
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
        mode: syncTextOr(syncText(json['mode']), 'smart').trim(),
        userMode: syncText(json['user_mode']).trim(),
        tenantDefault: syncText(json['tenant_default']).trim(),
        locked: json['locked'] == true,
        applied: json['applied'] != false,
        warning: syncText(json['warning']).trim(),
      );
}

/// `GET /api/v1/bots/desktop-computer` — which maintenance ops are allowed.
class DesktopComputerCaps {
  final bool restartAllowed;
  final bool resetAllowed;

  const DesktopComputerCaps({
    this.restartAllowed = false,
    this.resetAllowed = false,
  });

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
    action: syncText(json['action']).trim(),
    container: syncText(json['container']).trim(),
    image: syncText(json['image']).trim(),
    status: syncText(json['status']).trim(),
    backup: syncText(json['backup']).trim(),
  );
}

/// `GET /bots/{id}/queue/{cmid}` — live view of a parked user message.
/// state: queued | held | running | done | missing | gave_up.
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
    state: syncText(json['state']).trim(),
    runId: syncTextAny(json['run_id'], json['runId']).trim(),
    held: json['held'] == true,
    position: syncInt(json['position']),
    outcome: syncText(json['outcome']).trim(),
  );
}

/// One timer from `GET .../schedules`. The clock lives on the server.
class BotSchedule {
  final String id;
  final String name;
  final String action;
  final int intervalMinutes;
  final int hour;
  final int minute;
  final int dayOfWeek;
  final String nextRunAt;
  final String status;

  const BotSchedule({
    this.id = '',
    this.name = '',
    this.action = '',
    this.intervalMinutes = 0,
    this.hour = 0,
    this.minute = 0,
    this.dayOfWeek = -1,
    this.nextRunAt = '',
    this.status = '',
  });

  factory BotSchedule.fromJson(Map<String, dynamic> json) => BotSchedule(
    id: syncText(json['id']).trim(),
    name: syncText(json['name']).trim(),
    action: syncText(json['action']).trim(),
    intervalMinutes: syncInt(json['interval_minutes']),
    hour: syncInt(json['hour']),
    minute: syncInt(json['minute']),
    dayOfWeek: syncIntOr(json['day_of_week'], -1),
    nextRunAt: syncText(json['next_run_at']).trim(),
    status: syncText(json['status']).trim(),
  );

  String when(AppLocalizations S) {
    if (intervalMinutes > 0) return S.scheduleEveryMinutes(intervalMinutes);
    final hh = hour.toString().padLeft(2, '0');
    final mm = minute.toString().padLeft(2, '0');
    final day = switch (dayOfWeek) {
      0 => S.scheduleDowSun,
      1 => S.scheduleDowMon,
      2 => S.scheduleDowTue,
      3 => S.scheduleDowWed,
      4 => S.scheduleDowThu,
      5 => S.scheduleDowFri,
      6 => S.scheduleDowSat,
      _ => S.scheduleEveryDay,
    };
    return '$day $hh:$mm';
  }
}

/// Fixed confirmation sentence the person sends to approve a plan —
/// matches `BOT_CONFIRM_TASK` in desktopBots.ts. The Hub recognises it
/// (typed-confirm sentence) and folds the plan reading.
const String botConfirmTaskText = '安排已确认。请按你上一条安排执行。做完、失败或需要我时，在对话里告诉我。';
