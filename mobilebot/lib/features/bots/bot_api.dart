import 'dart:convert';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:uuid/uuid.dart';

import '../../core/api_client.dart';
import '../../core/hub_config.dart';
import 'bot_models.dart';

/// Result of sending one message: admitted (poll the run), queued behind a
/// running turn, or already settled. `ack` is the server's first sentence
/// when one was ready inside the admission budget; `routedTitle` names the
/// task Hub routed the message to.
class SendResult {
  final BotRun? run;
  final BotReply? reply;
  final bool queued;
  final int position;
  final String ack;
  final String routedVia;
  final String routedTitle;
  final String kind;

  /// The srv conversation the post was admitted into — a `bot#<id>` value
  /// marks a per-run thread the person can continue via continue_session.
  final String session;

  const SendResult._({
    this.run,
    this.reply,
    this.queued = false,
    this.position = 0,
    this.ack = '',
    this.routedVia = '',
    this.routedTitle = '',
    this.kind = '',
    this.session = '',
  });

  factory SendResult.admitted(BotRun run,
          {String ack = '',
          String routedVia = '',
          String routedTitle = '',
          String kind = '',
          String session = ''}) =>
      SendResult._(
          run: run,
          ack: ack,
          routedVia: routedVia,
          routedTitle: routedTitle,
          kind: kind,
          session: session);

  factory SendResult.queued(
          {int position = 0,
          String ack = '',
          String routedVia = '',
          String routedTitle = '',
          String kind = '',
          String session = ''}) =>
      SendResult._(
          queued: true,
          position: position,
          ack: ack,
          routedVia: routedVia,
          routedTitle: routedTitle,
          kind: kind,
          session: session);

  factory SendResult.settled(BotReply reply,
          {String ack = '',
          String routedVia = '',
          String routedTitle = '',
          String kind = '',
          String session = ''}) =>
      SendResult._(
          reply: reply,
          ack: ack,
          routedVia: routedVia,
          routedTitle: routedTitle,
          kind: kind,
          session: session);

  bool get isAdmitted => run != null;
}

/// `/api/v1/bots` calls against the signed-in account's Hub.
class BotApi {
  final ApiClient _client;

  BotApi(this._client);

  String _botPath(String botId, [String suffix = '']) {
    final id = botId.trim();
    if (id.isEmpty) throw ArgumentError('botId is required');
    return '/api/v1/bots/${Uri.encodeComponent(id)}$suffix';
  }

  Future<BotAccess> fetchAccess() async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/access',
        options: _client.opts(),
      ),
    );
    return BotAccess.fromJson(data);
  }

  Future<List<Bot>> listBots() async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots',
        options: _client.opts(),
      ),
    );
    final raw = data['items'];
    if (raw is! List) return const [];
    return [
      for (final item in raw)
        if (item is Map) Bot.fromJson(Map<String, dynamic>.from(item)),
    ].where((b) => b.id.isNotEmpty).toList(growable: false);
  }

  Future<Bot> createBot({
    required String name,
    String description = '',
    String? color,
    String? shape,
  }) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/v1/bots',
        data: {
          'name': name.trim(),
          'description': description.trim(),
          'color': ?color,
          'shape': ?shape,
        },
        options: _client.opts(),
      ),
    );
    return Bot.fromJson(data);
  }

  Future<void> updateBot(
    String botId, {
    String? name,
    String? description,
  }) async {
    await _client.guard(
      () => _client.dio.patch<Map<String, dynamic>>(
        _botPath(botId),
        data: {
          if (name != null) 'name': name.trim(),
          if (description != null) 'description': description.trim(),
        },
        options: _client.opts(),
      ),
    );
  }

  Future<void> deleteBot(String botId) async {
    await _client.guard(
      () => _client.dio.delete<Map<String, dynamic>>(
        _botPath(botId),
        options: _client.opts(),
      ),
    );
  }

  Future<void> setDesktopMode(String botId, String mode) async {
    await _client.guard(
      () => _client.dio.put<Map<String, dynamic>>(
        _botPath(botId, '/desktop-mode'),
        data: {'desktop_mode': mode},
        options: _client.opts(),
      ),
    );
  }

  Future<Map<String, dynamic>> getStyle(String botId) async {
    return _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        _botPath(botId, '/style'),
        options: _client.opts(),
      ),
    );
  }

  Future<Map<String, dynamic>> setStyle(
    String botId, {
    String? soul,
    String? memory,
  }) async {
    return _client.guard(
      () => _client.dio.put<Map<String, dynamic>>(
        _botPath(botId, '/style'),
        data: {'soul': ?soul, 'memory': ?memory},
        options: _client.opts(),
      ),
    );
  }

  Future<void> setAppearance(
    String botId, {
    String? color,
    String? shape,
  }) async {
    await _client.guard(
      () => _client.dio.put<Map<String, dynamic>>(
        _botPath(botId, '/appearance'),
        data: {'color': ?color, 'shape': ?shape},
        options: _client.opts(),
      ),
    );
  }

  /// Sends one turn. `Prefer: respond-async` admits the run with a 202
  /// `{run_id}`; an older Hub answers 200 with the finished reply inline.
  Future<SendResult> sendMessage(
    String botId, {
    required String content,
    BotPhase? phase,
    String? clientMessageId,
    String? continueSession,
    String? askRun,
  }) async {
    final text = content.trim();
    if (text.isEmpty) throw ArgumentError('content is required');
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/messages'),
        data: {
          'content': text,
          if (phase != null) 'phase': phase.wireValue,
          if (clientMessageId != null && clientMessageId.isNotEmpty)
            'client_message_id': clientMessageId,
          if (continueSession != null && continueSession.isNotEmpty)
            'continue_session': continueSession,
          if (askRun != null && askRun.isNotEmpty) 'ask_run': askRun,
        },
        options: _client.opts(
          headers: const {'Prefer': 'respond-async'},
          receiveTimeout: const Duration(seconds: 60),
        ),
      ),
    );
    final ack = (data['ack'] as String? ?? '').trim();
    final routedVia = (data['routed_via'] as String? ?? '').trim();
    final routedTitle = (data['routed_title'] as String? ?? '').trim();
    final kind = (data['kind'] as String? ?? '').trim();
    final session = (data['session'] as String? ?? '').trim();
    // Queued admissions carry no run yet: Hub starts the run when the slot
    // frees, and the reply lands through the shared journal.
    if (data['queued'] == true) {
      return SendResult.queued(
        position: (data['position'] as num? ?? 0).toInt(),
        ack: ack,
        routedVia: routedVia,
        routedTitle: routedTitle,
        kind: kind,
        session: session,
      );
    }
    final run = BotRun.fromJson(data);
    return run.accepted
        ? SendResult.admitted(run,
            ack: ack,
            routedVia: routedVia,
            routedTitle: routedTitle,
            kind: kind,
            session: session)
        : SendResult.settled(BotReply.fromJson(data),
            ack: ack,
            routedVia: routedVia,
            routedTitle: routedTitle,
            kind: kind,
            session: session);
  }

  /// One run poll. Returns null while the run is in flight (a 202 answer).
  Future<BotReply?> pollRun(String botId, String runId) async {
    final response = await _client.call(
      () => _client.dio.get<Map<String, dynamic>>(
        _botPath(botId, '/runs/${Uri.encodeComponent(runId.trim())}'),
        options: _client.opts(receiveTimeout: const Duration(seconds: 15)),
      ),
    );
    final status = response.statusCode ?? 0;
    if (status == 202) return null;
    if (status < 200 || status >= 300) {
      throw hubException(response);
    }
    return BotReply.fromJson(response.data ?? const <String, dynamic>{});
  }

  Future<List<ActiveRun>> activeRuns(String botId) async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        _botPath(botId, '/runs/active'),
        options: _client.opts(),
      ),
    );
    return [
      for (final item in (data['items'] as List? ?? const []))
        if (item is Map) ActiveRun.fromJson(Map<String, dynamic>.from(item)),
    ];
  }

  Future<void> cancelRun(String botId, String runId) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/runs/${Uri.encodeComponent(runId)}/cancel'),
        options: _client.opts(),
      ),
    );
  }

  Future<void> pauseRun(String botId, String runId) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/runs/${Uri.encodeComponent(runId)}/pause'),
        options: _client.opts(),
      ),
    );
  }

  Future<void> resumeRun(String botId, String runId) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/runs/${Uri.encodeComponent(runId)}/resume'),
        options: _client.opts(),
      ),
    );
  }

  Future<void> ackRun(String botId, String runId) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/runs/${Uri.encodeComponent(runId)}/ack'),
        options: _client.opts(),
      ),
    );
  }

  /// `GET /api/v1/bots/pending` — finished runs no client has shown yet.
  Future<List<PendingDelivery>> pendingDeliveries() async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/pending',
        options: _client.opts(),
      ),
    );
    return [
      for (final item in (data['items'] as List? ?? const []))
        if (item is Map) PendingDelivery.fromJson(Map<String, dynamic>.from(item)),
    ];
  }

  Future<List<DesktopTaskStatus>> taskStatuses(String botId) async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        _botPath(botId, '/tasks'),
        options: _client.opts(),
      ),
    );
    return [
      for (final item in (data['items'] as List? ?? const []))
        if (item is Map)
          DesktopTaskStatus.fromJson(Map<String, dynamic>.from(item)),
    ];
  }

  /// Read the shared conversation journal. `after` is the client's seq
  /// cursor; `waitSeconds` long-polls (Hub holds at most ~25s) for changes.
  Future<ConversationPage> conversation({
    String botId = '',
    int after = 0,
    int limit = 200,
    int waitSeconds = 0,
  }) async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/conversation',
        queryParameters: {
          if (botId.isNotEmpty) 'bot_id': botId,
          if (after > 0) 'after': after,
          if (limit > 0) 'limit': limit,
          if (waitSeconds > 0) 'wait': waitSeconds,
        },
        options: _client.opts(
          receiveTimeout: Duration(seconds: waitSeconds + 15),
        ),
      ),
    );
    return ConversationPage.fromJson(data);
  }

  /// Publish local bubbles into the shared journal so the other GUIs of the
  /// same person see them (kind 'message', matching desktopBotSync.ts).
  Future<void> writeConversation(
    String clientId,
    List<Map<String, dynamic>> entries,
  ) async {
    if (entries.isEmpty) return;
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/v1/bots/conversation',
        data: {'client_id': clientId, 'entries': entries},
        options: _client.opts(),
      ),
    );
  }

  /// Attach, wake, or release the bot's desktop view.
  ///
  /// Empty hold keeps the screen pinned while open; `wake` is the human
  /// 唤醒; `release` lets Hub idle-sleep it. `epoch` identifies one open.
  Future<BotDesktop> watchDesktop(
    String botId, {
    int epoch = 0,
    bool wake = false,
    bool release = false,
  }) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/desktop'),
        data: {
          if (release) 'release': true,
          if (wake) 'wake': true,
          if (epoch > 0) 'epoch': epoch,
        },
        options: _client.opts(receiveTimeout: const Duration(seconds: 75)),
      ),
    );
    return BotDesktop.fromJson(data);
  }

  /// Hand the bot's desktop back so it can keep working unattended.
  Future<void> handBackDesktop(String botId) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/desktop/handback'),
        options: _client.opts(),
      ),
    );
  }

  /// Give the bot's desktop a login/captcha value straight through MaClawSrv.
  Future<void> fillSecret(
    String botId, {
    required String name,
    required String value,
  }) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/secret-fill'),
        data: {'name': name.trim(), 'value': value},
        options: _client.opts(),
      ),
    );
  }

  /// `GET /api/v1/bots/credits` — the owner's system-free credits view.
  Future<BotCredits> credits() async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/credits',
        options: _client.opts(),
      ),
    );
    return BotCredits.fromJson(data);
  }

  /// `GET /api/v1/bots/llm` — the model settings applying to every bot of
  /// this owner (mode, provider fields with the key removed, per-role view).
  Future<Map<String, dynamic>> getLLMSettings() async {
    return _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/llm',
        options: _client.opts(),
      ),
    );
  }

  /// `PUT /api/v1/bots/llm`. `{"mode":"default"}` restores the configured
  /// default; a custom save carries provider fields per role.
  Future<Map<String, dynamic>> putLLMSettings(
      Map<String, dynamic> body) async {
    return _client.guard(
      () => _client.dio.put<Map<String, dynamic>>(
        '/api/v1/bots/llm',
        data: body,
        options: _client.opts(),
      ),
    );
  }

  /// `GET /api/v1/bots/feed?all=1` — the person's display feed: messages
  /// between their bots, oldest first.
  Future<List<FeedNote>> feed({bool all = true}) async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/feed',
        queryParameters: {if (all) 'all': '1'},
        options: _client.opts(),
      ),
    );
    return [
      for (final item in (data['items'] as List? ?? const []))
        if (item is Map) FeedNote.fromJson(Map<String, dynamic>.from(item)),
    ];
  }

  /// `POST /api/v1/bots/feed {"shown":[ids]}` — mark feed notes displayed so
  /// later reads skip them.
  Future<void> markFeedShown(List<String> ids) async {
    if (ids.isEmpty) return;
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/v1/bots/feed',
        data: {'shown': ids},
        options: _client.opts(),
      ),
    );
  }

  /// `POST /api/v1/bots/{id}/primary` — make this bot the account primary.
  /// The Hub answers with the refreshed bot list.
  Future<void> setPrimary(String botId) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/primary'),
        options: _client.opts(),
      ),
    );
  }

  /// `POST /api/v1/bots/{id}/style/rollback {"version":N}` — restore a
  /// style version, returns the full style view.
  Future<Map<String, dynamic>> rollbackStyle(
      String botId, int version) async {
    return _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/style/rollback'),
        data: {'version': version},
        options: _client.opts(),
      ),
    );
  }

  /// `POST /api/v1/bots/{id}/desktop/files` — one upload chunk. The
  /// finished upload lands at `~/素材/<bot>/<name>` on the bot's desktop.
  /// `data` is the raw chunk; it is base64'd into the JSON body.
  Future<MaterialUpload> uploadMaterialChunk(
    String botId, {
    required String uploadId,
    required String name,
    required int offset,
    required int total,
    required List<int> data,
  }) async {
    final view = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/desktop/files'),
        data: {
          'upload_id': uploadId,
          'name': name,
          'offset': offset,
          'total': total,
          'data': base64Encode(data),
        },
        options: _client.opts(),
      ),
    );
    return MaterialUpload.fromJson(view);
  }

  /// Streams one on-disk file into chunked upload calls. Unlike
  /// [uploadMaterial] the whole file never lands in memory at once — a large
  /// pick (video, archive) cannot exhaust the heap.
  Future<String> uploadMaterialFile(
    String botId,
    String name,
    File file, {
    void Function(int sent, int total)? onProgress,
  }) async {
    final total = await file.length();
    final uploadId = const Uuid().v4();
    const chunkSize = 2 << 20; // same cap as uploadMaterial
    var offset = 0;
    var path = '';
    final raf = await file.open();
    try {
      while (offset < total) {
        await raf.setPosition(offset);
        final data = await raf.read(chunkSize);
        if (data.isEmpty) break;
        final result = await uploadMaterialChunk(
          botId,
          uploadId: uploadId,
          name: name,
          offset: offset,
          total: total,
          data: data,
        );
        offset += data.length;
        if (result.path.isNotEmpty) path = result.path;
        onProgress?.call(offset, total);
      }
    } finally {
      await raf.close();
    }
    return path;
  }

  /// Chunks one file into `desktop.UploadChunkMax`-sized upload calls.
  /// Returns the landing path (`~/素材/...`) once the last chunk lands.
  Future<String> uploadMaterial(
    String botId,
    String name,
    List<int> bytes, {
    void Function(int sent, int total)? onProgress,
  }) async {
    final uploadId = const Uuid().v4();
    const chunkSize = 2 << 20; // well under the Hub's 4 MiB decoded cap
    var offset = 0;
    var path = '';
    while (offset < bytes.length || (bytes.isEmpty && offset == 0)) {
      final end =
          (offset + chunkSize) > bytes.length ? bytes.length : offset + chunkSize;
      final result = await uploadMaterialChunk(
        botId,
        uploadId: uploadId,
        name: name,
        offset: offset,
        total: bytes.length,
        data: bytes.sublist(offset, end),
      );
      offset = end;
      if (result.path.isNotEmpty) path = result.path;
      onProgress?.call(offset, bytes.length);
      if (bytes.isEmpty || offset >= bytes.length) break;
    }
    return path;
  }

  /// `GET /api/v1/bots/assignments` — the collab board: who delegated what
  /// to whom and where it stands.
  Future<List<BotAssignment>> assignments() async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/assignments',
        options: _client.opts(),
      ),
    );
    return [
      for (final item in (data['items'] as List? ?? const []))
        if (item is Map)
          BotAssignment.fromJson(Map<String, dynamic>.from(item)),
    ];
  }

  /// `GET /api/v1/bots/{id}/inbox/{noteID}/files/{name}` — fetch a file a
  /// bot attached to a feed/mail note, as base64 bytes.
  Future<BotReplyFile> inboxFile(
      String botId, String noteId, String name) async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        _botPath(botId, '/inbox/${Uri.encodeComponent(noteId)}/files/${Uri.encodeComponent(name)}'),
        options: _client.opts(),
      ),
    );
    return BotReplyFile.fromJson(data);
  }

  /// `GET /api/v1/bots/collab-features` — which collaboration parts are on:
  /// `{assignments, peer_review, say, rooms}`.
  Future<Map<String, bool>> collabFeatures() async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/collab-features',
        options: _client.opts(),
      ),
    );
    return {for (final e in data.entries) e.key: e.value == true};
  }

  /// `GET /api/v1/bot-rooms` — the person's group rooms.
  Future<List<BotRoom>> rooms() async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bot-rooms',
        options: _client.opts(),
      ),
    );
    return [
      for (final item in (data['items'] as List? ?? const []))
        if (item is Map) BotRoom.fromJson(Map<String, dynamic>.from(item)),
    ];
  }

  /// `POST /api/v1/bot-rooms` (id empty) or `PUT /api/v1/bot-rooms/{id}`.
  /// Omitted fields stay unchanged on update.
  Future<BotRoom> saveRoom({
    String id = '',
    String? name,
    List<String>? members,
    Map<String, String>? attention,
  }) async {
    final body = <String, dynamic>{
      'name': ?name,
      'members': ?members,
      'attention': ?attention,
    };
    final data = await _client.guard(
      () => id.isEmpty
          ? _client.dio.post<Map<String, dynamic>>(
              '/api/v1/bot-rooms',
              data: body,
              options: _client.opts(),
            )
          : _client.dio.put<Map<String, dynamic>>(
              '/api/v1/bot-rooms/${Uri.encodeComponent(id)}',
              data: body,
              options: _client.opts(),
            ),
    );
    return BotRoom.fromJson(data);
  }

  /// `DELETE /api/v1/bot-rooms/{id}`.
  Future<void> deleteRoom(String id) async {
    await _client.guard(
      () => _client.dio.delete<Map<String, dynamic>>(
        '/api/v1/bot-rooms/${Uri.encodeComponent(id)}',
        options: _client.opts(),
      ),
    );
  }

  /// `POST /api/v1/bot-rooms/{id}/messages {"text"}` — the person writes in
  /// the room. Returns the bots Hub woke (attention permitting).
  Future<List<String>> postRoomMessage(String id, String text) async {
    final data = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/v1/bot-rooms/${Uri.encodeComponent(id)}/messages',
        data: {'text': text},
        options: _client.opts(),
      ),
    );
    return [
      for (final item in (data['woken'] as List? ?? const []))
        item.toString(),
    ];
  }

  /// `GET /api/v1/bots/said[?room=<id>]` — what bots told the person
  /// directly (no room) or one room's conversation, oldest first.
  Future<List<SaidNote>> said({String room = ''}) async {
    final data = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/said',
        queryParameters: {if (room.isNotEmpty) 'room': room},
        options: _client.opts(),
      ),
    );
    return [
      for (final item in (data['items'] as List? ?? const []))
        if (item is Map) SaidNote.fromJson(Map<String, dynamic>.from(item)),
    ];
  }

  /// `POST /api/v1/bots/{id}/cross-notes/{noteID}/start` — claim a queued
  /// delegate note so the target bot picks it up.
  Future<void> crossNoteStart(String botId, String noteId) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/cross-notes/${Uri.encodeComponent(noteId)}/start'),
        options: _client.opts(),
      ),
    );
  }

  /// `POST /api/v1/bots/{id}/demo/start {"title"}` — begin recording on the
  /// bot's isolated desktop. Returns e.g. `asr_available`.
  Future<Map<String, dynamic>> demoStart(String botId, String title) async {
    return _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/demo/start'),
        data: {'title': title},
        options: _client.opts(),
      ),
    );
  }

  /// `POST /api/v1/bots/{id}/demo/narrate` — one narration line, either
  /// typed `text` or `audio_base64`+`mime` the Hub transcribes.
  Future<Map<String, dynamic>> demoNarrate(
    String botId, {
    String text = '',
    String audioBase64 = '',
    String mime = '',
  }) async {
    return _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/demo/narrate'),
        data: {
          'text': text,
          'audio_base64': audioBase64,
          'mime': mime,
        },
        options: _client.opts(),
      ),
    );
  }

  /// `POST /api/v1/bots/{id}/demo/stop` — close the recording. The answer
  /// carries `timeline` (demoskill.Timeline) and `shot_dir`.
  Future<Map<String, dynamic>> demoStop(String botId) async {
    return _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/demo/stop'),
        options: _client.opts(),
      ),
    );
  }

  /// `POST /api/v1/bots/{id}/demo/distill {timeline, notes}` — distill the
  /// finished recording into a reusable skill (201).
  Future<Map<String, dynamic>> demoDistill(
    String botId,
    Map<String, dynamic> timeline, {
    String notes = '',
  }) async {
    return _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/demo/distill'),
        data: {'timeline': timeline, 'notes': notes},
        options: _client.opts(),
      ),
    );
  }

  /// `GET /api/v1/bots/{id}/demo/shots/{demoID}/{ref}` — one timeline
  /// screenshot as `{mime, data(base64)}`.
  Future<({String mime, String data})> demoShot(
      String botId, String demoId, String ref) async {
    final out = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        _botPath(botId,
            '/demo/shots/${Uri.encodeComponent(demoId)}/${Uri.encodeComponent(ref)}'),
        options: _client.opts(),
      ),
    );
    return (
      mime: (out['mime'] as String? ?? '').trim(),
      data: (out['data'] as String? ?? '').trim(),
    );
  }

  /// `POST /api/v1/bots/{id}/runs/{runID}/supplement` — extra instruction
  /// routed into a still-running task.
  Future<SupplementResult> supplementRun(
      String botId, String runId, String content) async {
    final out = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/runs/${Uri.encodeComponent(runId)}/supplement'),
        data: {'content': content},
        options: _client.opts(),
      ),
    );
    return SupplementResult.fromJson(out);
  }

  /// `POST /api/v1/bots/{id}/followup` — ask Hub to route the message into
  /// the teammate run that owns the delegated task (`routed=true`), or fall
  /// back to a normal message when `routed=false`.
  Future<FollowUpRoute> sendFollowUp(String botId, String content) async {
    final out = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/followup'),
        data: {'content': content},
        options: _client.opts(),
      ),
    );
    return FollowUpRoute.fromJson(out);
  }

  /// `GET /api/v1/bots/{id}/queue/{cmid}` — live view of a queued user
  /// message (state queued/held/running/done/missing + position).
  Future<QueueView> queueStatus(String botId, String clientMessageId) async {
    final out = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        _botPath(botId, '/queue/${Uri.encodeComponent(clientMessageId)}'),
        options: _client.opts(),
      ),
    );
    return QueueView.fromJson(out);
  }

  /// `POST /api/v1/bots/{id}/preference` — record one durable preference the
  /// bot honours across tasks. Append-only; there is no read endpoint.
  Future<void> savePreference(String botId, String content) async {
    await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        _botPath(botId, '/preference'),
        data: {'content': content},
        options: _client.opts(),
      ),
    );
  }

  /// `DELETE /api/v1/bots/{id}/preference` — withdraw every recorded
  /// preference for this bot.
  Future<void> withdrawPreference(String botId) async {
    await _client.guard(
      () => _client.dio.delete<Map<String, dynamic>>(
        _botPath(botId, '/preference'),
        options: _client.opts(),
      ),
    );
  }

  /// `GET /api/v1/bots/desktop-proxy-mode` — the caller's effective desktop
  /// route plus whether the tenant locks it.
  Future<DesktopProxyMode> desktopProxyMode() async {
    final out = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/desktop-proxy-mode',
        options: _client.opts(),
      ),
    );
    return DesktopProxyMode.fromJson(out);
  }

  /// `PUT /api/v1/bots/desktop-proxy-mode` — `""` follows the tenant
  /// default; otherwise `proxy` | `direct` | `smart`.
  Future<DesktopProxyMode> setDesktopProxyMode(String mode) async {
    final out = await _client.guard(
      () => _client.dio.put<Map<String, dynamic>>(
        '/api/v1/bots/desktop-proxy-mode',
        data: {'mode': mode},
        options: _client.opts(),
      ),
    );
    return DesktopProxyMode.fromJson(out);
  }

  /// `GET /api/v1/bots/desktop-computer` — which maintenance ops the tenant
  /// allows on the caller's pooled desktop.
  Future<DesktopComputerCaps> desktopComputer() async {
    final out = await _client.guard(
      () => _client.dio.get<Map<String, dynamic>>(
        '/api/v1/bots/desktop-computer',
        options: _client.opts(),
      ),
    );
    return DesktopComputerCaps.fromJson(out);
  }

  /// `POST /api/v1/bots/desktop-computer/{restart|reset}` — reboot or
  /// re-image the caller's pooled desktop. `force` skips the graceful stop.
  Future<ComputerResult> desktopComputerAction(String action,
      {bool force = false}) async {
    final out = await _client.guard(
      () => _client.dio.post<Map<String, dynamic>>(
        '/api/v1/bots/desktop-computer/$action',
        data: {'force': force},
        options: _client.opts(),
      ),
    );
    return ComputerResult.fromJson(out);
  }

  String absoluteUrl(String pathOrUrl) => _client.absoluteUrl(pathOrUrl);
}

HubApiException hubException(Response<dynamic> response) {
  final data = response.data;
  var code = 'HTTP_${response.statusCode}';
  var message = '请求失败';
  if (data is Map) {
    code = (data['error'] as String? ?? code).toString();
    message = (data['message'] as String? ?? message).toString();
  }
  return HubApiException(code, message, statusCode: response.statusCode);
}
