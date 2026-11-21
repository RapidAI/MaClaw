import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'message_order.dart';

import '../../core/hub_config.dart';
import '../../core/locale.dart';
import '../../core/providers.dart';
import 'bot_api.dart';
import 'bot_models.dart';
import 'bots_controller.dart';

class ChatState {
  final String botId;
  final List<ChatMessage> messages;
  final bool loadingHistory;
  final bool sending;
  final String? activeRunId;
  final String error;

  /// The live step phrase Hub reports for the run still in flight
  /// (`GET /runs/{id}` 202 body: `activity`). Empty when the run is
  /// not actively reporting a step.
  final String activity;

  /// A `bot#<id>` thread the person explicitly chose to continue — sent as
  /// `continue_session` on the next message, then cleared.
  final String continueThread;

  /// Sticky notice that the last send never reached the server. Cleared by
  /// a successful send, an explicit dismiss, or a Hub business rejection.
  /// A later send is never blocked by this flag.
  final String channelError;

  const ChatState({
    required this.botId,
    this.messages = const [],
    this.loadingHistory = true,
    this.sending = false,
    this.activeRunId,
    this.error = '',
    this.activity = '',
    this.continueThread = '',
    this.channelError = '',
  });

  ChatState copyWith({
    List<ChatMessage>? messages,
    bool? loadingHistory,
    bool? sending,
    String? Function()? activeRunId,
    String? error,
    String? activity,
    String? continueThread,
    String? channelError,
  }) => ChatState(
    botId: botId,
    messages: messages ?? this.messages,
    loadingHistory: loadingHistory ?? this.loadingHistory,
    sending: sending ?? this.sending,
    activeRunId: activeRunId != null ? activeRunId() : this.activeRunId,
    error: error ?? this.error,
    activity: activity ?? this.activity,
    continueThread: continueThread ?? this.continueThread,
    channelError: channelError ?? this.channelError,
  );
}

/// One bot's chat: history from the shared journal, sends via the messages
/// API with run polling, and a long-poll loop that keeps this transcript in
/// step with the desktop GUI's.
class _OutboundSend {
  final String messageId;
  final String text;
  final BotPhase? phase;
  final String? askRun;
  final String? continueSession;
  final Completer<bool> done;
  final DateTime enqueuedAt;

  _OutboundSend({
    required this.messageId,
    required this.text,
    required this.phase,
    required this.askRun,
    required this.continueSession,
    required this.done,
    DateTime? enqueuedAt,
  }) : enqueuedAt = enqueuedAt ?? DateTime.now();
}

class BotChatController extends Notifier<ChatState> {
  BotApi? get _apiOrNull => ref.read(botApiProvider);
  BotApi get _api => _apiOrNull!;
  String get _clientId => ref.read(sessionProvider).active?.clientId ?? '';

  Timer? _pollTimer;
  Timer? _syncTimer;
  Timer? _sendWatch;

  /// POSTs waiting behind the one already on the wire. Each already has a
  /// pending bubble, so a second tap is visible and still gets a result.
  final List<_OutboundSend> _outbound = [];
  bool _sendBusy = false;
  String? _postingId;

  /// The job currently being posted. It leaves _outbound before its POST
  /// starts, so this is the only handle on a send that is on the wire.
  _OutboundSend? _postingJob;

  /// How long a pending send may stay unanswered before the bubble is
  /// marked failed. The clock starts when the bubble is shown, not when
  /// its POST starts, so a line waiting behind a stuck send cannot sit on
  /// "sending…" with no ending of its own. Tests inject a short bound;
  /// production uses the hub send timeout.
  @visibleForTesting
  static Duration sendWatchLimit = hubSendTimeout;
  int _cursor = 0;
  bool _disposed = false;
  bool _foreground = true;

  /// The srv conversation the in-flight run was admitted into — echoed onto
  /// its reply bubble so a per-run thread offers "继续这个任务".
  String _sessionForRun = '';

  /// Message ids whose media a run re-fetch was already tried for. Journal
  /// projections never carry the bytes, so a synced bubble with
  /// `attachmentIds` gets one `GET /runs/{id}` per controller lifetime.
  final Set<String> _mediaFetches = {};

  @override
  ChatState build() {
    final botId = ref.read(botChatBotIdProvider);
    ref.onDispose(() {
      _disposed = true;
      _pollTimer?.cancel();
      _sendWatch?.cancel();
      _syncTimer?.cancel();
    });
    Future.microtask(_loadHistory);
    return ChatState(botId: botId);
  }

  Future<void> _loadHistory() async {
    try {
      var after = 0;
      var head = 0;
      var caughtUp = false;
      for (var guard = 0; guard < 20; guard++) {
        final page = await _api.conversation(
          botId: state.botId,
          after: after,
          limit: 300,
        );
        head = page.head;
        if (page.reset) {
          after = 0;
          _mergeEntries(page.entries, replace: true);
          continue;
        }
        _mergeEntries(page.entries);
        if (!page.more) {
          caughtUp = true;
          break;
        }
        if (page.next <= after) break;
        after = page.next;
      }
      // A partial read must stay on the page it reached. Jumping to head
      // would skip the lines that did not fit.
      _cursor = caughtUp && head > 0 ? head : after;
      // Surface runs that finished while no client was watching.
      unawaited(_drainPendingDeliveries());
      // And adopt a turn that is still running so the composer lock,
      // supplement and cancel affordances survive an app restart.
      unawaited(_recoverActiveRun());
      state = state.copyWith(loadingHistory: false);
      _scheduleSync();
    } on Object catch (error) {
      if (_disposed) return;
      noteAuthFailure(ref, error);
      state = state.copyWith(
        loadingHistory: false,
        error: failureMessage(error),
      );
      _scheduleSync(delay: const Duration(seconds: 10));
    }
  }

  /// Foreground gate driven by the chat screen's lifecycle listener: the
  /// long-poll holds a connection for up to 25s and must not burn a
  /// pocketed phone's battery. Resuming re-arms the sync right away and
  /// re-attaches the run poll when a turn is still in flight.
  void setForeground(bool foreground) {
    if (_disposed || foreground == _foreground) return;
    _foreground = foreground;
    if (!foreground) {
      _syncTimer?.cancel();
      _pollTimer?.cancel();
      return;
    }
    _scheduleSync();
    final runId = state.activeRunId;
    if (runId != null) _pollRun(runId);
  }

  /// Long-poll loop: Hub holds the read up to 25s and answers when the
  /// journal changed, so other clients' writes arrive promptly.
  void _scheduleSync({Duration delay = const Duration(milliseconds: 400)}) {
    if (_disposed || !_foreground) return;
    _syncTimer?.cancel();
    _syncTimer = Timer(delay, _syncOnce);
  }

  Future<void> _syncOnce() async {
    if (_disposed) return;
    try {
      final page = await _api.conversation(
        botId: state.botId,
        after: _cursor,
        waitSeconds: 20,
        limit: 300,
      );
      if (_disposed) return;
      if (page.reset) {
        _cursor = 0;
        _mergeEntries(page.entries, replace: true);
      } else {
        _mergeEntries(page.entries);
        if (page.more && page.next > _cursor) {
          _cursor = page.next;
        } else if (!page.more && page.head > _cursor) {
          _cursor = page.head;
        } else if (page.next > _cursor) {
          _cursor = page.next;
        }
      }
      unawaited(_refreshQueued());
      _scheduleSync();
    } on Object catch (error) {
      noteAuthFailure(ref, error);
      _scheduleSync(delay: const Duration(seconds: 8));
    }
  }

  /// Poll `queue/{cmid}` for each parked user bubble so the tag shows a
  /// live position and clears once the message leaves the queue
  /// (running/done/missing all mean it is no longer waiting).
  Future<void> _refreshQueued() async {
    final parked = state.messages
        .where((m) => m.queued && m.isUser)
        .take(3)
        .toList();
    for (final msg in parked) {
      try {
        final view = await _api.queueStatus(state.botId, msg.id);
        if (_disposed) return;
        final messages = [...state.messages];
        final index = messages.indexWhere((m) => m.id == msg.id);
        if (index < 0 || !messages[index].queued) continue;
        switch (view.state) {
          case 'queued':
          case 'held':
            messages[index] = messages[index].copyWith(
              queuePosition: view.position,
              queueHeld: view.held || view.state == 'held',
            );
          case 'gave_up':
            // The message never started a run, so the journal will not
            // deliver a reply. Show the reason Hub stored on the queue.
            final outcome = view.outcome.trim();
            messages[index] = messages[index].copyWith(
              queued: false,
              queuePosition: 0,
              queueHeld: false,
            );
            final replyId = '${msg.id}:gave-up';
            if (outcome.isNotEmpty && !messages.any((m) => m.id == replyId)) {
              final reply = ChatMessage(
                id: replyId,
                role: 'assistant',
                content: outcome,
                at: DateTime.now(),
              );
              messages.add(reply);
              _writeProjection(reply);
            }
          default:
            messages[index] = messages[index].copyWith(
              queued: false,
              queuePosition: 0,
              queueHeld: false,
            );
        }
        state = state.copyWith(messages: messages);
      } on Object {
        // Position refresh is best-effort — the next sync tick retries.
      }
    }
  }

  /// A line keeps the sequence of its first write. A later edit, or a second
  /// copy of the same server line, must not move it down the chat.
  ChatMessage _keepEarlierSeq(ChatMessage current, ChatMessage incoming) {
    var next = incoming;
    if (current.syncSeq > 0 &&
        (next.syncSeq <= 0 || current.syncSeq < next.syncSeq)) {
      next = next.copyWith(syncSeq: current.syncSeq);
    }
    // Media never travels in the journal — the echo of this client's own
    // settled bubble must not strip the bytes the run poll staged.
    if (next.images.isEmpty && current.images.isNotEmpty) {
      next = next.copyWith(images: current.images);
    }
    if (next.files.isEmpty && current.files.isNotEmpty) {
      next = next.copyWith(files: current.files);
    }
    // The failure ending is local. A journal echo of the optimistic
    // projection must not clear it or put the spinner back.
    if (current.failed) {
      next = next.copyWith(
        failed: true,
        pending: false,
        failureReason: next.failureReason.isNotEmpty
            ? next.failureReason
            : current.failureReason,
      );
    } else if (current.failureReason.isNotEmpty && next.failureReason.isEmpty) {
      next = next.copyWith(failureReason: current.failureReason);
    }
    return next;
  }

  int _entryDisplaySeq(ConversationEntry entry) =>
      entry.createdSeq > 0 ? entry.createdSeq : entry.seq;

  /// A still-pending bubble older than the reply wait was orphaned — the
  /// run that would settle it died (app killed, reply lost to another
  /// channel) and no answer is coming. Mirrors the desktop side.
  bool _isOrphanedPending(ChatMessage m, int nowMs) =>
      pendingReplyIsStale(pending: m.pending, id: m.id, at: m.at, nowMs: nowMs);

  /// Merge journal entries into the transcript (mirrors desktopBotSync):
  /// upsert by id in created order, patch merges flags, deleted drops it.
  void _mergeEntries(List<ConversationEntry> entries, {bool replace = false}) {
    final ordered = [...entries]
      ..sort((a, b) {
        final byCreated = _entryDisplaySeq(a).compareTo(_entryDisplaySeq(b));
        return byCreated != 0 ? byCreated : a.seq.compareTo(b.seq);
      });
    // On a journal reset keep local in-flight bubbles; everything synced is
    // rebuilt from the page.
    var messages = replace
        ? state.messages.where((m) => m.pending || m.queued).toList()
        : [...state.messages];
    var changed = replace;
    var newReplies = 0;
    for (final entry in ordered) {
      if (entry.botId != state.botId && entry.botId.isNotEmpty) continue;
      if (entry.kind.isNotEmpty && entry.kind != 'message') continue;
      if (entry.id.isEmpty) continue;
      final index = messages.indexWhere((m) => m.id == entry.id);
      final data = entry.data;
      if (data['deleted'] == true) {
        final requestId = syncText(data['requestId']);
        for (var i = messages.length - 1; i >= 0; i--) {
          final item = messages[i];
          final sameId = item.id == entry.id;
          final sameRequest =
              requestId.isNotEmpty && item.requestId == requestId;
          if (!sameId && !sameRequest) continue;
          if (!sameId && item.pending) continue;
          messages.removeAt(i);
          changed = true;
        }
        continue;
      }
      final isPatch =
          data['patch'] == true ||
          (data.keys.length <= 3 &&
              (data.containsKey('askResolved') ||
                  data.containsKey('optionsUsed')));
      if (isPatch) {
        if (index >= 0) {
          final seq = _entryDisplaySeq(entry);
          final currentSeq = messages[index].syncSeq;
          messages[index] = messages[index].copyWith(
            askResolved: data['askResolved'] == true
                ? true
                : messages[index].askResolved,
            optionsUsed: data['optionsUsed'] == true
                ? true
                : messages[index].optionsUsed,
            syncSeq: currentSeq > 0 && (seq <= 0 || currentSeq < seq)
                ? currentSeq
                : seq,
          );
          changed = true;
        }
        continue;
      }
      if (data['role'] is! String) continue;
      final incoming = ChatMessage.fromSyncData(
        entry.id,
        data,
        syncSeq: entry.createdSeq > 0 ? entry.createdSeq : entry.seq,
      );
      if (index >= 0) {
        // Never clobber a local pending bubble with its older projection.
        // Still keep the sequence: without it the next sort treats the
        // question as unsynced and drops it below the reply.
        if (messages[index].pending) {
          final seq = incoming.syncSeq;
          if (seq > 0 &&
              (messages[index].syncSeq <= 0 || seq < messages[index].syncSeq)) {
            messages[index] = messages[index].copyWith(syncSeq: seq);
            changed = true;
          }
        } else {
          messages[index] = _keepEarlierSeq(messages[index], incoming);
          changed = true;
        }
      } else {
        // requestId is the line's own key; runId is the turn, so it only
        // collapses assistant-to-assistant copies — a person's line and the
        // reply of the same turn must never merge. Mirrors the GUI twin
        // rule (`item.runId === runId || requestId === desktop-bot-deliver-…`).
        final requestId = syncText(data['requestId']);
        final runId = incoming.role == 'assistant'
            ? syncText(data['runId'])
            : '';
        final twin = (requestId.isEmpty && runId.isEmpty)
            ? -1
            : messages.indexWhere(
                (m) =>
                    (runId.isNotEmpty &&
                        m.role == 'assistant' &&
                        (m.runId == runId ||
                            m.requestId == 'desktop-bot-deliver-$runId')) ||
                    (requestId.isNotEmpty && m.requestId == requestId),
              );
        if (twin >= 0) {
          final current = messages[twin];
          var next = current;
          if (incoming.syncSeq > 0 &&
              (current.syncSeq <= 0 || incoming.syncSeq < current.syncSeq)) {
            next = next.copyWith(syncSeq: incoming.syncSeq);
          }
          // Adopt the fetch keys the local copy lacks — a synced line can
          // carry attachment ids for media the local reply never staged.
          if (next.attachmentIds.isEmpty && incoming.attachmentIds.isNotEmpty) {
            next = next.copyWith(attachmentIds: incoming.attachmentIds);
          }
          if (next.runId.isEmpty && incoming.runId.isNotEmpty) {
            next = next.copyWith(runId: incoming.runId);
          }
          if (!identical(next, current)) {
            messages[twin] = next;
            changed = true;
          }
          continue;
        }
        messages.add(incoming);
        changed = true;
        if (incoming.role == 'assistant' &&
            !incoming.ack &&
            incoming.content.isNotEmpty) {
          newReplies++;
        }
      }
    }
    if (changed) {
      // Hub's first-write sequence is the order. Client clocks in the id
      // disagree, so they are not used once a line has a sequence.
      final synced = messages.where((m) => m.syncSeq > 0).toList()
        ..sort((a, b) => a.syncSeq.compareTo(b.syncSeq));
      final unsynced = messages.where((m) => m.syncSeq <= 0).toList();
      final merged = [...synced, ...unsynced];
      // Sweep orphaned pending bubbles — a spinner that can never settle is
      // worse than admitting the turn died. Mark failed with a reason so
      // the bubble does not vanish while still saying "sending…".
      final nowMs = DateTime.now().millisecondsSinceEpoch;
      for (var i = 0; i < merged.length; i++) {
        if (!_isOrphanedPending(merged[i], nowMs)) continue;
        merged[i] = merged[i].copyWith(
          pending: false,
          failed: true,
          failureReason: sharedL10n.chatReplyTimedOut,
        );
      }
      // The queue is serial per bot: every real reply that landed in this
      // page consumes the oldest still-queued user instruction, so the
      // "排队中" tag clears exactly when its turn produces an answer.
      for (var i = 0; i < merged.length && newReplies > 0; i++) {
        final m = merged[i];
        if (m.queued && m.isUser) {
          merged[i] = m.copyWith(queued: false);
          newReplies--;
        }
      }
      state = state.copyWith(messages: merged);
    }
    _restoreSyncedMedia();
  }

  /// Attachment bytes never travel in the journal. A synced assistant
  /// bubble used to carry `attachmentIds` + `runId`, but the desktop saves
  /// delivered files on its own disk and Hub's ReplyFile carries no
  /// attachment id — so a projection usually has NEITHER bytes NOR ids, and
  /// gating the refetch on ids made attachments vanish after an app
  /// restart (or on a phone watching a desktop-sent turn). Now:
  ///  - explicit `attachmentIds` still refetch unconditionally (a stated
  ///    media list is worth asking for whatever its age);
  ///  - idless bubbles PROBE one refetch, bounded to [mediaRestoreWindow]
  ///    so reopening a chat does not poll the whole transcript — older runs
  ///    are reaped server-side anyway. `_fetchRunMedia` is a no-op for
  ///    text-only replies, and the run poll is authoritative for the rest.
  static const mediaRestoreWindow = Duration(hours: 48);

  void _restoreSyncedMedia() {
    if (_disposed) return;
    final freshBefore = DateTime.now().subtract(mediaRestoreWindow);
    for (final m in state.messages) {
      if (m.role != 'assistant' || m.failed || m.ack) continue;
      if (m.images.isNotEmpty || m.files.isNotEmpty) continue;
      if (m.runId.isEmpty) continue;
      if (m.attachmentIds.isEmpty &&
          (m.at == null || m.at!.isBefore(freshBefore))) {
        continue;
      }
      if (!_mediaFetches.add(m.id)) continue;
      unawaited(_fetchRunMedia(m.id, m.runId));
    }
  }

  Future<void> _fetchRunMedia(String messageId, String runId) async {
    try {
      final reply = await _api.pollRun(state.botId, runId);
      if (_disposed || reply == null) return;
      if (reply.images.isEmpty && reply.files.isEmpty) return;
      final index = state.messages.indexWhere((m) => m.id == messageId);
      if (index < 0) return;
      final current = state.messages[index];
      if (current.images.isNotEmpty || current.files.isNotEmpty) return;
      final messages = [...state.messages];
      messages[index] = current.copyWith(
        images: reply.images,
        files: reply.files,
      );
      state = state.copyWith(messages: messages);
    } on Object catch (error) {
      // Text stays. A 4xx means the run is reaped — no point asking again;
      // a transport or 5xx error may succeed on a later sync tick.
      final status = error is HubApiException ? error.statusCode : null;
      if (status == null || status >= 500) {
        _mediaFetches.remove(messageId);
      }
    }
  }

  Future<void> _drainPendingDeliveries() async {
    try {
      final items = await _api.pendingDeliveries();
      if (_disposed || items.isEmpty) return;
      final mine = items.where((d) => d.botId == state.botId).toList();
      if (mine.isEmpty) return;
      final messages = [...state.messages];
      final writes = <Map<String, dynamic>>[];
      for (final delivery in mine) {
        final reply = delivery.reply;
        final message = ChatMessage(
          id: 'delivery-${delivery.runId}',
          role: 'assistant',
          content: reply.text,
          phase: delivery.phase,
          handoffUrl: reply.novncUrl,
          userControl: reply.userControl,
          attentionReason: reply.attentionReason,
          question: reply.question,
          images: reply.images,
          files: reply.files,
          runId: delivery.runId,
          // The GUI twin-matches a delivery bubble on this request id.
          requestId: 'desktop-bot-deliver-${delivery.runId}',
          at: DateTime.now(),
        );
        // Dedupe on the run, not just the `delivery-{runId}` id: a settle
        // may already have landed this reply as an `m-…` bubble (or the
        // journal echo may carry its run/request id), and adding the
        // delivery again showed the same reply — attachments too — twice.
        // Assistant-only, mirroring _mergeEntries: a runId must collapse
        // assistant-to-assistant copies, never a person's line of the turn.
        final shown = messages.where((m) => m.role == 'assistant');
        final alreadyShown =
            messages.any((m) => m.id == message.id) ||
            botDeliveryAlreadyShown(
              shownRunIds: shown.map((m) => m.runId),
              shownRequestIds: shown.map((m) => m.requestId),
              runId: delivery.runId,
            );
        if (alreadyShown) continue;
        messages.add(message);
        final projection = message.syncProjection;
        if (projection != null) {
          writes.add({
            'bot_id': state.botId,
            'id': message.id,
            'kind': 'message',
            'data': projection,
          });
        }
        unawaited(_api.ackRun(state.botId, delivery.runId).catchError((_) {}));
      }
      state = state.copyWith(messages: messages);
      if (writes.isNotEmpty) {
        unawaited(_api.writeConversation(_clientId, writes).catchError((_) {}));
      }
    } on Object {
      // Pending pulls are best-effort; history already renders.
    }
  }

  /// Returns false when the message never reached the wire (empty, or the
  /// request failed). A send already in flight is queued, not dropped —
  /// callers that gate follow-up state (ask cards, options) wait for the
  /// real result.
  Future<bool> send(
    String content, {
    BotPhase? phase,
    String? askRun,
    String? continueSession,
  }) async {
    final text = content.trim();
    if (text.isEmpty) return false;
    if (text.toLowerCase() == '/clear') {
      // Local only, and only when nothing is on the wire. Queuing it would
      // post "/clear" to Hub, which this client never does.
      if (_sendBusy) return false;
      state = state.copyWith(
        messages: state.messages.where((m) => m.pending || m.queued).toList(),
        error: '',
      );
      return false;
    }
    final userMessage = ChatMessage(
      id: newDesktopMessageId(),
      role: 'user',
      content: text,
      pending: true,
      phase: phase?.wireValue ?? '',
      at: DateTime.now(),
    );
    state = state.copyWith(
      messages: [...state.messages, userMessage],
      error: '',
    );
    _writeProjection(userMessage.copyWith(pending: false));
    final job = _OutboundSend(
      messageId: userMessage.id,
      text: text,
      phase: phase,
      askRun: askRun,
      continueSession: continueSession,
      done: Completer<bool>(),
    );
    _outbound.add(job);
    _armSendWatch(job.messageId);
    if (!_sendBusy) unawaited(_drainOutbound());
    return job.done.future;
  }

  Future<void> _drainOutbound() async {
    if (_sendBusy) return;
    _sendBusy = true;
    try {
      while (_outbound.isNotEmpty && !_disposed) {
        _expireStaleSends();
        if (_outbound.isEmpty) break;
        final job = _outbound.removeAt(0);
        if (_alreadyFailed(job.messageId)) {
          if (!job.done.isCompleted) job.done.complete(false);
          continue;
        }
        var ok = false;
        try {
          ok = await _postOutbound(job);
        } on Object {
          ok = false;
        }
        if (!job.done.isCompleted) job.done.complete(ok);
      }
    } finally {
      _sendBusy = false;
      if (_disposed) {
        for (final leftover in _outbound) {
          if (!leftover.done.isCompleted) leftover.done.complete(false);
        }
        _outbound.clear();
      }
    }
  }

  Future<bool> _postOutbound(_OutboundSend job) async {
    final text = job.text;
    final phase = job.phase;
    final askRun = job.askRun;
    final continueSession = job.continueSession;
    _postingId = job.messageId;
    _postingJob = job;
    state = state.copyWith(sending: true);
    _armSendWatch(job.messageId);
    final api = _apiOrNull;
    if (api == null) {
      _sendWatch?.cancel();
      _markFailed(
        job.messageId,
        sharedL10n.netConnectionError,
        channelDown: true,
      );
      return false;
    }
    try {
      final result = await api.sendMessage(
        state.botId,
        content: text,
        phase: phase,
        clientMessageId: job.messageId,
        continueSession:
            continueSession ??
            (state.continueThread.isEmpty ? null : state.continueThread),
        askRun: askRun,
      );
      if (_disposed) return false;
      _sendWatch?.cancel();
      // The watch already gave this bubble a failed ending. A late admit
      // must not flip it back to "sent" and hide the timeout.
      if (state.messages.any((m) => m.id == job.messageId && m.failed)) {
        return false;
      }
      if (state.continueThread.isNotEmpty || state.channelError.isNotEmpty) {
        state = state.copyWith(continueThread: '', channelError: '');
      }
      if (result.queued) {
        // Hub is holding the message behind a running turn — no run yet.
        // The reply arrives through the shared-journal long-poll, so mark
        // the bubble queued and free the composer for the next message.
        //
        // Keep the previous run's activeRunId so the supplement affordance
        // stays available: a queued message goes to the same run that is
        // currently in flight, and steering that run is still the right
        // action while the queued message waits its turn.
        final previousRunId = state.activeRunId;
        final messages = [...state.messages];
        final index = messages.indexWhere((m) => m.id == job.messageId);
        if (index >= 0) {
          messages[index] = messages[index].copyWith(
            pending: false,
            queued: true,
            queuePosition: result.position,
          );
        }
        state = state.copyWith(messages: messages, sending: false);
        _appendAck(result);
        // Poll the previous run so its activity keeps updating the header
        // and the settle path clears the queued bubble when it lands.
        if (previousRunId != null) _pollRun(previousRunId);
      } else if (result.isAdmitted) {
        _sessionForRun = result.session;
        // Free the composer: `sending` only guards the in-flight request.
        // While the run executes the person may queue the next message
        // (Hub answers `queued`) or answer an ask card — desktop parity.
        // Mark the user bubble settled immediately: Hub accepted it, the
        // run is in flight — the header tracks progress via `activity`.
        final messages = [...state.messages];
        final index = messages.indexWhere((m) => m.id == job.messageId);
        if (index >= 0 && !messages[index].failed) {
          messages[index] = messages[index].copyWith(pending: false);
        }
        state = state.copyWith(
          messages: messages,
          sending: false,
          activeRunId: () => result.run!.runId,
        );
        _appendAck(result);
        _pollRun(result.run!.runId);
      } else {
        _sessionForRun = result.session;
        _settleReply(
          result.reply ?? const BotReply(),
          sentMessageId: job.messageId,
        );
      }
      return true;
    } on Object catch (error) {
      if (_disposed) return false;
      _sendWatch?.cancel();
      if (state.messages.any((m) => m.id == job.messageId && m.failed)) {
        return false;
      }
      noteAuthFailure(ref, error);
      _markFailed(
        job.messageId,
        failureMessage(error),
        channelDown: sendNeverReachedServer(error),
      );
      return false;
    } finally {
      if (_postingId == job.messageId) _postingId = null;
      if (_postingJob == job) _postingJob = null;
    }
  }

  bool _alreadyFailed(String messageId) =>
      state.messages.any((m) => m.id == messageId && m.failed);

  bool _sendAgedOut(_OutboundSend job, {required bool onWire}) {
    final waited = DateTime.now().difference(job.enqueuedAt);
    if (onWire) return waited >= sendWatchLimit;
    // A line still waiting its turn may sit behind one full POST. Past
    // that, "sending…" is a lie: this bubble has had no ending of its own.
    return waited >= sendWatchLimit * 2;
  }

  /// Pending bubbles must not keep saying "sending…" with no explanation.
  /// The on-wire clock is one send timeout. A line waiting behind that POST
  /// gets the same ending once its own wait runs out. A late answer must
  /// not rewrite a bubble this watch already marked failed.
  void _armSendWatch(String messageId) {
    _OutboundSend? job;
    for (final candidate in _outbound) {
      if (candidate.messageId == messageId) job = candidate;
    }
    // A job taken off _outbound is on the wire and keeps its own clock:
    // without it a short injected bound found no job and failed the bubble
    // on the spot, and a real one lost the age of a long POST.
    job ??= _postingJob;
    final onWire = messageId == _postingId;
    final limit = onWire ? sendWatchLimit : sendWatchLimit * 2;
    final waited = job == null
        ? Duration.zero
        : DateTime.now().difference(job.enqueuedAt);
    final wait = limit - waited;
    _sendWatch?.cancel();
    _sendWatch = Timer(wait.isNegative ? Duration.zero : wait, () {
      if (_disposed) return;
      _expireStaleSends();
    });
  }

  void _expireStaleSends() {
    if (_disposed) return;
    String? expired;
    // The line being posted is no longer in _outbound, so it has to be
    // aged on its own: a send that never answers must stop saying
    // "sending…" even though nothing is queued behind it.
    final onWire = _postingJob;
    if (onWire != null && !_alreadyFailed(onWire.messageId)) {
      if (_sendAgedOut(onWire, onWire: true)) expired = onWire.messageId;
    }
    for (final job in _outbound) {
      if (expired != null) break;
      if (_alreadyFailed(job.messageId)) continue;
      if (_sendAgedOut(job, onWire: job.messageId == _postingId)) {
        expired = job.messageId;
        break;
      }
    }
    if (expired == null) {
      _armNextSendWatch();
      return;
    }
    _markFailed(expired, sharedL10n.netTimeout, channelDown: true);
  }

  void _armNextSendWatch() {
    final onWire = _postingJob;
    if (_outbound.isEmpty && onWire == null) {
      _sendWatch?.cancel();
      _sendWatch = null;
      return;
    }
    if (_outbound.isEmpty) {
      _armSendWatch(onWire!.messageId);
      return;
    }
    var next = _outbound.first;
    for (final job in _outbound) {
      if (job.enqueuedAt.isBefore(next.enqueuedAt)) next = job;
    }
    if (onWire != null && onWire.enqueuedAt.isBefore(next.enqueuedAt)) {
      next = onWire;
    }
    _armSendWatch(next.messageId);
  }

  /// Continue a per-run thread (`bot#<id>`) explicitly — the next send goes
  /// to that conversation via `continue_session`. Call with '' to cancel.
  void continueTask(String conversation) {
    final thread = conversation.trim();
    state = state.copyWith(
      continueThread: thread.startsWith('${state.botId}#') ? thread : '',
    );
  }

  /// Re-send a failed user message — drops the failed bubble and posts the
  /// same content again as a fresh message.
  Future<void> retry(ChatMessage message) async {
    if (!message.failed) return;
    final content = message.content;
    state = state.copyWith(
      messages: [
        for (final m in state.messages)
          if (m.id != message.id) m,
      ],
    );
    await send(content);
  }

  /// The admission's first sentence (ack) lands as an ack bubble; a routed
  /// message names the task it went to.
  void _appendAck(SendResult result) {
    final text = result.ack.trim();
    final routed = result.routedTitle.trim();
    if (text.isEmpty && routed.isEmpty) return;
    final content = routed.isEmpty
        ? text
        : (text.isEmpty
              ? sharedL10n.chatRoutedTo(routed)
              : sharedL10n.chatRoutedWithText(text, routed));
    state = state.copyWith(
      messages: [
        ...state.messages,
        ChatMessage(
          id: newDesktopMessageId(),
          role: 'assistant',
          content: content,
          ack: true,
          at: DateTime.now(),
        ),
      ],
    );
  }

  /// A turn started elsewhere (or before an app restart) still shows in
  /// `runs/active` — resume watching it so cancel/supplement stay usable
  /// and the reply settles the pending state when it lands.
  Future<void> _recoverActiveRun() async {
    try {
      final runs = await _api.activeRuns(state.botId);
      if (_disposed || state.activeRunId != null) return;
      String runId = '';
      for (final run in runs) {
        if (run.runId.isNotEmpty) {
          runId = run.runId;
          break;
        }
      }
      if (runId.isEmpty) return;
      // Do NOT lock the composer: a waiting-for-user run still needs the
      // person to answer ask cards, and everyone may queue the next task.
      state = state.copyWith(activeRunId: () => runId);
      _pollRun(runId);
    } on Object {
      // Best-effort — the journal still delivers the reply on sync.
    }
  }

  void _pollRun(String runId) {
    _pollTimer?.cancel();
    final startedAt = DateTime.now();
    void tick() async {
      if (_disposed || !_foreground || state.activeRunId != runId) return;
      if (DateTime.now().difference(startedAt) > botTurnMaxWait) {
        _settleError(sharedL10n.chatReplyTimedOut);
        return;
      }
      try {
        final reply = await _api.pollRun(state.botId, runId);
        if (_disposed || state.activeRunId != runId) return;
        if (reply == null) {
          // A 202 answer: the run is still in flight. Hub may report a live
          // step phrase (activity), a status line, or process steps — show
          // the most specific in the header, newest step first.
          final activity = _api.lastActivityForRun(runId);
          final process = _api.lastProcessForRun(runId);
          var header = activity;
          if (header.isEmpty && process.isNotEmpty) {
            header = process.last;
          }
          if (header != state.activity) {
            state = state.copyWith(activity: header);
          }
          if (_foreground) _pollTimer = Timer(botPollInterval, tick);
          return;
        }
        _settleReply(reply);
      } on Object catch (error) {
        if (_disposed) return;
        noteAuthFailure(ref, error);
        if (error is HubApiException && error.isAuthRejected) {
          _settleError(failureMessage(error));
          return;
        }
        // A transient poll failure must not kill the watch — reschedule
        // and let botTurnMaxWait bound the real timeout.
        if (_foreground) _pollTimer = Timer(botPollInterval, tick);
      }
    }

    _pollTimer = Timer(botPollInterval, tick);
  }

  bool _ownedBySend(String id) =>
      id == _postingId || _outbound.any((job) => job.messageId == id);

  void _settleReply(BotReply reply, {String? sentMessageId}) {
    final messages = [...state.messages];
    final text = reply.isEmpty ? sharedL10n.chatDoneNoReply : reply.text;
    // The same reply may already be visible: another client (or the Hub's
    // own journal projection) can land it through the long-poll before the
    // run poll returns. Adding it again would double the bubble — and
    // writing its projection would double it for every other client too.
    // Windowed: an identical reply from older history is a new turn's
    // genuine answer (bots repeat "收到" all the time), not a duplicate.
    int shownAt = -1;
    if (!reply.isEmpty) {
      for (var i = messages.length - 1; i >= 0; i--) {
        final m = messages[i];
        if (m.role == 'assistant' &&
            !m.ack &&
            !m.failed &&
            m.content == reply.text &&
            (m.at == null ||
                DateTime.now().difference(m.at!) <
                    const Duration(minutes: 5))) {
          shownAt = i;
          break;
        }
      }
    }
    if (shownAt < 0) {
      final assistantMessage = ChatMessage(
        id: newDesktopMessageId(),
        role: 'assistant',
        content: text,
        phase: reply.phase?.wireValue ?? '',
        handoffUrl: reply.novncUrl,
        userControl: reply.userControl,
        attentionReason: reply.attentionReason,
        question: reply.question,
        images: reply.images,
        files: reply.files,
        requestId: state.activeRunId ?? '',
        runId: state.activeRunId ?? '',
        conversation: _sessionForRun,
        at: DateTime.now(),
      );
      messages.add(assistantMessage);
      _writeProjection(assistantMessage);
    } else if ((reply.images.isNotEmpty && messages[shownAt].images.isEmpty) ||
        (reply.files.isNotEmpty && messages[shownAt].files.isEmpty)) {
      // The projected bubble won the race but never carried the bytes: the
      // journal strips media and the desktop saves delivered files on its
      // own disk (no attachment ids to refetch). Patch this reply's media
      // into the shown bubble — the desktop stages the picture on first
      // paint the same way. Per-field: the sync media restore may already
      // have staged the other list on this bubble. Hub ReplyFile carries
      // no attachment id, so the projection's media ids are unchanged and
      // no re-publish is needed.
      final shown = messages[shownAt];
      messages[shownAt] = shown.copyWith(
        images: shown.images.isEmpty ? reply.images : null,
        files: shown.files.isEmpty ? reply.files : null,
      );
      state = state.copyWith(messages: messages);
    }
    _sessionForRun = '';
    // Mark the pending user bubble settled. A failed send already has
    // its ending and must not be rewritten as a success.
    for (var i = messages.length - 1; i >= 0; i--) {
      final pending = messages[i];
      if (!pending.pending || pending.failed) continue;
      if (sentMessageId != null) {
        if (pending.id != sentMessageId) continue;
      } else if (_ownedBySend(pending.id)) {
        continue;
      }
      messages[i] = pending.copyWith(pending: false);
      break;
    }
    state = state.copyWith(
      messages: messages,
      sending: false,
      activeRunId: () => null,
      activity: '',
    );
  }

  void _failQueuedSends(String error) {
    if (_outbound.isEmpty) return;
    final ids = {for (final job in _outbound) job.messageId};
    for (final job in _outbound) {
      if (!job.done.isCompleted) job.done.complete(false);
    }
    _outbound.clear();
    final messages = [
      for (final m in state.messages)
        ids.contains(m.id) && !m.failed
            ? m.copyWith(pending: false, failed: true, failureReason: error)
            : m,
    ];
    state = state.copyWith(messages: messages, sending: false);
  }

  void _markFailed(String messageId, String error, {bool channelDown = false}) {
    final messages = [
      for (final m in state.messages)
        m.id == messageId
            ? m.copyWith(pending: false, failed: true, failureReason: error)
            : m,
    ];
    // A failed send does not touch an active run: the failed bubble gets
    // its retry button, the running turn keeps its watch and controls.
    // Channel failures stay on the composer. A business rejection stays on
    // its own bubble and must not clear a channel notice that is still true.
    state = state.copyWith(
      messages: messages,
      sending: false,
      error: error,
      channelError: channelDown ? error : null,
    );
    // The wire never accepted this turn. Sibling bubbles still saying
    // "sending…" never left either — fail them with the same channel state
    // instead of leaving a silent queue behind one failed bubble.
    if (channelDown) _failQueuedSends(error);
  }

  void _settleError(String error) {
    final messages = [...state.messages];
    for (var i = messages.length - 1; i >= 0; i--) {
      if (messages[i].pending && !_ownedBySend(messages[i].id)) {
        messages[i] = messages[i].copyWith(
          pending: false,
          failed: true,
          failureReason: error,
        );
        break;
      }
    }
    state = state.copyWith(
      messages: messages,
      sending: false,
      activeRunId: () => null,
      activity: '',
      error: error,
      channelError: '',
    );
  }

  Future<void> cancelActiveRun() async {
    final runId = state.activeRunId;
    if (runId == null) return;
    try {
      await _api.cancelRun(state.botId, runId);
    } on Object catch (error) {
      if (!_disposed) {
        state = state.copyWith(error: failureMessage(error));
      }
      return;
    }
    if (_disposed) return;
    _pollTimer?.cancel();
    final messages = [...state.messages];
    for (var i = messages.length - 1; i >= 0; i--) {
      if (messages[i].pending && !messages[i].failed) {
        messages[i] = messages[i].copyWith(pending: false);
        break;
      }
    }
    state = state.copyWith(
      messages: messages,
      sending: false,
      activeRunId: () => null,
      activity: '',
    );
  }

  /// Route an extra instruction into the run still in flight. Returns the
  /// supplement outcome for the caller to surface (state/detail).
  Future<SupplementResult?> supplement(String text) async {
    final runId = state.activeRunId;
    if (runId == null || text.trim().isEmpty) return null;
    try {
      return await _api.supplementRun(state.botId, runId, text.trim());
    } on Object catch (error) {
      if (!_disposed) {
        state = state.copyWith(error: failureMessage(error));
      }
      return null;
    }
  }

  /// Answer an ask-card: secrets go through secret-fill (never on the wire
  /// transcript), everything else is a normal user message.
  Future<void> answerQuestion(ChatMessage message, String answer) async {
    if (message.question.isSecret) {
      try {
        await _api.fillSecret(
          state.botId,
          name: message.question.secretName,
          value: answer,
        );
        _patchFlags(message.id, askResolved: true);
      } on Object catch (error) {
        if (!_disposed) {
          state = state.copyWith(error: failureMessage(error));
        }
      }
      return;
    }
    // Route the answer back into the run that asked: continue_session names
    // the thread, ask_run names the run itself (matching the desktop's
    // `send(reply, …, message.conversation, message.runId)`). A synced card's
    // requestId is the asking client's message id, not the run — prefer
    // runId, with requestId covering bubbles written before it existed.
    // Only mark the card resolved once the answer actually left — a failed
    // or blocked send would otherwise lock it with the answer lost.
    final sent = await send(
      answer,
      askRun: message.runId.isEmpty
          ? (message.requestId.isEmpty ? null : message.requestId)
          : message.runId,
      continueSession: message.conversation.isEmpty
          ? null
          : message.conversation,
    );
    if (sent) _patchFlags(message.id, askResolved: true);
  }

  Future<void> pickOption(ChatMessage message, String option) async {
    final sent = await send(
      option,
      askRun: message.runId.isEmpty
          ? (message.requestId.isEmpty ? null : message.requestId)
          : message.runId,
      continueSession: message.conversation.isEmpty
          ? null
          : message.conversation,
    );
    if (sent) _patchFlags(message.id, optionsUsed: true);
  }

  void _patchFlags(
    String messageId, {
    bool askResolved = false,
    bool optionsUsed = false,
  }) {
    final messages = [
      for (final m in state.messages)
        m.id == messageId
            ? m.copyWith(
                askResolved: askResolved ? true : m.askResolved,
                optionsUsed: optionsUsed ? true : m.optionsUsed,
              )
            : m,
    ];
    state = state.copyWith(messages: messages);
    final flags = <String, dynamic>{};
    if (askResolved) flags['askResolved'] = true;
    if (optionsUsed) flags['optionsUsed'] = true;
    if (flags.isEmpty) return;
    // botApiProvider is null the moment the session drops — _api's `!` would
    // throw synchronously here, escaping into an unhandled async error.
    final api = ref.read(botApiProvider);
    if (api == null) return;
    unawaited(
      api
          .writeConversation(_clientId, [
            {
              'bot_id': state.botId,
              'id': messageId,
              'kind': 'message',
              'patch': true,
              'data': flags,
            },
          ])
          .catchError((_) {}),
    );
  }

  void clearError() {
    if (state.error.isNotEmpty) {
      state = state.copyWith(error: '');
    }
  }

  /// Dismiss the composer channel line without blocking the next send.
  void dismissChannelError() {
    if (state.channelError.isNotEmpty) {
      state = state.copyWith(channelError: '');
    }
  }

  void _writeProjection(ChatMessage message) {
    final projection = message.syncProjection;
    if (projection == null) return;
    final api = ref.read(botApiProvider);
    if (api == null) return;
    unawaited(
      api
          .writeConversation(_clientId, [
            {
              'bot_id': state.botId,
              'id': message.id,
              'kind': 'message',
              'data': projection,
            },
          ])
          .catchError((_) {}),
    );
  }
}

/// Which bot the chat controller serves — set once per route.
final botChatBotIdProvider = Provider<String>(
  (ref) => throw UnimplementedError('botChatBotIdProvider not overridden'),
);

final botChatProvider = NotifierProvider<BotChatController, ChatState>(
  BotChatController.new,
  // Without this the provider would mount on the root container even when
  // first read inside the route's ProviderScope — scoping follows declared
  // dependencies only, not runtime ref.read calls.
  dependencies: [botChatBotIdProvider],
);
