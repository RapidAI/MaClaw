import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/storage/mobile_local_store.dart';
import '../auth/session_controller.dart';
import 'bot.dart';
import 'bot_api.dart';
import 'bot_message.dart';

/// The signed-in user's Hub, or null when signed out.
///
/// BotApi needs its own Dio (bot turns have their own timeouts and status
/// handling), but it must point at the Hub that authenticated this user.
final botApiProvider = Provider<BotApi?>((ref) {
  final session = ref.watch(sessionControllerProvider).valueOrNull;
  if (session == null || !session.authenticated) return null;
  return BotApi(
    vault: ref.watch(secureVaultProvider),
    hubUrl: session.hubUrl,
  );
});

/// Whether the signed-in user may use bots at all, plus the reason when not.
///
/// The rail shows [BotAccess.message] verbatim when disabled, because that text
/// is written by whoever administers the tenant and this client has no better
/// explanation to offer.
final botAccessProvider = FutureProvider<BotAccess>((ref) async {
  final api = ref.watch(botApiProvider);
  if (api == null) return BotAccess.disabled;
  try {
    return api.fetchAccess();
  } on Object {
    // A failed probe must not lock the user out of a feature that may well be
    // enabled; the list page reports the real failure if the list call fails too.
    return BotAccess.disabled;
  }
});

/// The user's bots.
final botListProvider =
    AsyncNotifierProvider<BotListController, List<Bot>>(BotListController.new);

class BotListController extends AsyncNotifier<List<Bot>> {
  @override
  Future<List<Bot>> build() async {
    final api = ref.watch(botApiProvider);
    if (api == null) return const [];
    return api.listBots();
  }

  Future<void> refresh() async {
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
      final api = ref.read(botApiProvider);
      if (api == null) return const <Bot>[];
      return api.listBots();
    });
  }

  /// Create a bot and put it at the head of the rail.
  Future<Bot> create({required String name, String description = ''}) async {
    final api = ref.read(botApiProvider);
    if (api == null) {
      throw const BotApiException('UNAUTHORIZED', '请先登录');
    }
    final bot = await api.createBot(name: name, description: description);
    final current = state.valueOrNull ?? const <Bot>[];
    state = AsyncData([bot, ...current.where((item) => item.id != bot.id)]);
    return bot;
  }

  Future<void> rename(
    String botId, {
    required String name,
    String description = '',
  }) async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    final previous = state.valueOrNull ?? const <Bot>[];
    // Show the new name right away; a failed save puts the old one back.
    state = AsyncData([
      for (final bot in previous)
        if (bot.id == botId)
          Bot(
            id: bot.id,
            name: name.trim(),
            description: description.trim(),
            instanceId: bot.instanceId,
            createdAt: bot.createdAt,
          )
        else
          bot,
    ]);
    try {
      await api.renameBot(botId, name: name, description: description);
    } on Object {
      state = AsyncData(previous);
      rethrow;
    }
  }

  Future<void> remove(String botId) async {
    final api = ref.read(botApiProvider);
    if (api == null) return;
    await api.deleteBot(botId);
    final current = state.valueOrNull ?? const <Bot>[];
    state = AsyncData(current.where((bot) => bot.id != botId).toList());
    // The transcript belongs to a bot that no longer exists.
    await ref.read(mobileLocalStoreProvider).deleteBotMessages(botId);
  }
}

/// Which bots are working and which still owe the person something.
///
/// A run lives on the Hub and can outlive the widget that started it, so this
/// outlives pages too. Without it the rail could only show state for the bot
/// currently on screen.
final botActivityProvider =
    NotifierProvider<BotActivityController, BotActivity>(
  BotActivityController.new,
);

/// Bots with a turn in flight, and those whose finished turn needs the person.
class BotActivity {
  final Set<String> running;
  final Set<String> awaitingUser;

  const BotActivity({this.running = const {}, this.awaitingUser = const {}});

  bool isRunning(String botId) => running.contains(botId);

  bool needsUser(String botId) => awaitingUser.contains(botId);

  BotActivity markRunning(String botId) {
    if (botId.trim().isEmpty) return this;
    return BotActivity(
      running: {...running, botId},
      awaitingUser: {...awaitingUser}..remove(botId),
    );
  }

  BotActivity markSettled(String botId, BotWorkState workState) {
    if (botId.trim().isEmpty) return this;
    final active = {...running}..remove(botId);
    final waiting = {...awaitingUser}..remove(botId);
    if (workState.needsUser) waiting.add(botId);
    return BotActivity(running: active, awaitingUser: waiting);
  }
}

class BotActivityController extends Notifier<BotActivity> {
  @override
  BotActivity build() => const BotActivity();

  void markRunning(String botId) => state = state.markRunning(botId);

  void markSettled(String botId, BotWorkState workState) =>
      state = state.markSettled(botId, workState);
}

/// The newest message of each bot, used for the rail preview and badge.
///
/// Kept apart from [botListProvider] so refreshing the list does not rebuild
/// every open conversation, and it reads one row per bot rather than whole
/// transcripts.
final botLatestMessagesProvider =
    AsyncNotifierProvider<BotLatestMessagesController, Map<String, BotMessage>>(
  BotLatestMessagesController.new,
);

class BotLatestMessagesController
    extends AsyncNotifier<Map<String, BotMessage>> {
  @override
  Future<Map<String, BotMessage>> build() async {
    return ref.watch(mobileLocalStoreProvider).loadLatestBotMessageByBotId();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(
      () => ref.read(mobileLocalStoreProvider).loadLatestBotMessageByBotId(),
    );
  }
}

/// One bot's conversation plus the poll lifecycle of its in-flight turn.
///
/// The poll lives here rather than in a widget so a rotation, a tab switch or a
/// backgrounded app does not restart a turn Hub is already running.
final botConversationProvider = AsyncNotifierProvider.family<
    BotConversationController, List<BotMessage>, String>(
  BotConversationController.new,
);

class BotConversationController
    extends FamilyAsyncNotifier<List<BotMessage>, String> {
  Timer? _pollTimer;
  bool _disposed = false;
  int _consecutivePollFailures = 0;
  DateTime? _startedAt;

  /// Runs this process has adopted and is still reading.
  ///
  /// This is what separates "a turn is running" from "a pending bubble was left
  /// behind by a reload", since only the latter can age out on a clock.
  static final Set<String> _liveRuns = <String>{};

  /// Whether this process is still responsible for reading [runId].
  static bool runIsLive(String runId) => _liveRuns.contains(runId);

  String get botId => arg;

  @override
  Future<List<BotMessage>> build(String arg) async {
    ref.onDispose(() {
      _disposed = true;
      _pollTimer?.cancel();
      _pollTimer = null;
    });
    final stored =
        await ref.watch(mobileLocalStoreProvider).loadBotMessages(arg);
    // A pending bubble whose run this process never adopted belongs to a turn
    // nothing is left to read back, so it is surfaced as failed rather than
    // leaving the conversation stuck on a spinner forever.
    return [
      for (final message in stored)
        if (message.pending && botPendingReplyIsStale(message))
          message.copyWith(pending: false, failed: true)
        else
          message,
    ];
  }

  List<BotMessage> get _current => state.valueOrNull ?? const [];

  Future<void> _persist(List<BotMessage> messages) async {
    state = AsyncData(messages);
    try {
      await ref.read(mobileLocalStoreProvider).saveBotMessages(arg, messages);
      // The rail preview reads the newest message per bot; refresh it only
      // after the write landed so the preview cannot disagree with the disk.
      await ref.read(botLatestMessagesProvider.notifier).refresh();
    } on Object {
      // The live transcript is authoritative; the cache only restores a reload.
    }
  }

  /// The transcript, waiting for the first load to finish if it has not.
  ///
  /// Reading `state.valueOrNull` directly would hand back an empty list while
  /// the stored transcript is still loading, and the message written on top of
  /// it would then be overwritten when `build` completes — losing the turn the
  /// person just sent. Every write goes through here so that cannot happen.
  Future<List<BotMessage>> _loadedMessages() async {
    if (state.isLoading) {
      await future;
    }
    return state.valueOrNull ?? const [];
  }

  /// Send one turn and follow it to a reply.
  ///
  /// The user bubble is written first so the send feels instant, and it is
  /// settled in place when the reply lands. On failure it is marked failed
  /// rather than removed, so the person can see what was not delivered.
  Future<void> send(String content, {BotPhase? phase}) async {
    final text = content.trim();
    if (text.isEmpty) return;
    final api = ref.read(botApiProvider);
    // The send never reached Hub, so the bubble is written already-failed
    // rather than pending: there is no run to wait for or settle later.
    if (api == null) {
      await _persist([
        BotMessage.user(content: text, phase: phase)
            .copyWith(failed: true, attentionReason: '请先登录后再发送'),
      ]);
      return;
    }
    await _persist(
      upsertBotMessage(
        await _loadedMessages(),
        BotMessage.user(content: text, phase: phase, pending: true),
      ),
    );
    _startedAt = DateTime.now();
    _consecutivePollFailures = 0;
    try {
      final result = await api.sendMessage(arg, content: text, phase: phase);
      if (result.isAdmitted) {
        _adopt(result.runId!);
      } else {
        await _settle(result.reply!);
      }
    } on Object catch (error) {
      await _settleFailure(botFailureMessage(error));
    }
  }

  void _adopt(String runId) {
    _liveRuns.add(runId);
    ref.read(botActivityProvider.notifier).markRunning(botId);
    if (_pollTimer?.isActive ?? false) return;
    _pollTimer = Timer.periodic(botRunPollInterval, (_) {
      unawaited(_pollOnce(runId));
    });
    unawaited(_pollOnce(runId));
  }

  Future<void> _pollOnce(String runId) async {
    if (_disposed) return;
    final api = ref.read(botApiProvider);
    if (api == null) {
      _stopPolling();
      return;
    }
    try {
      final reply = await api.pollRun(botId, runId);
      if (reply == null) {
        // Still working. A dropped read is retried by the failure budget below.
        _consecutivePollFailures = 0;
        return;
      }
      await _settle(reply, runId: runId);
    } on Object {
      _consecutivePollFailures++;
      if (_consecutivePollFailures < botRunPollMaxFailures) return;
      // Several reads in a row failed. Give up on the run rather than poll a
      // turn Hub may have dropped; the user's message is kept and marked failed.
      _liveRuns.remove(runId);
      _stopPolling();
      _startedAt = null;
      ref
          .read(botActivityProvider.notifier)
          .markSettled(botId, BotWorkState.failed);
      await _persist([
        for (final message in _current)
          if (message.pending)
            message.copyWith(
              pending: false,
              failed: true,
              attentionReason: '与 Bot 的连接中断，请重试',
            )
          else
            message,
      ]);
    }
  }

  /// Replace the pending bubble with the finished reply.
  Future<void> _settle(BotReply reply, {String? runId}) async {
    if (runId != null) {
      _liveRuns.remove(runId);
    }
    _stopPolling();
    _consecutivePollFailures = 0;
    _startedAt = null;

    // One snapshot for the whole settle. Reading state per list-comprehension
    // could mix a transcript from before an await with one from after it, and
    // the pending bubble being replaced is matched by id.
    final current = await _loadedMessages();
    BotMessage? pending;
    for (final message in current) {
      if (message.pending) {
        pending = message;
        break;
      }
    }

    // A reply with neither text nor an attachment is not a result. Surface the
    // failed turn instead of dropping the user's message without explanation.
    if (reply.isEmpty) {
      ref
          .read(botActivityProvider.notifier)
          .markSettled(botId, BotWorkState.failed);
      if (pending == null) return;
      await _persist([
        for (final message in current)
          if (message.id == pending.id)
            message.copyWith(
              pending: false,
              failed: true,
              attentionReason: 'Bot 没有返回结果，请重试',
            )
          else
            message,
      ]);
      return;
    }

    final answered = BotMessage.fromReply(
      id: 'a-${DateTime.now().toUtc().microsecondsSinceEpoch}',
      reply: reply,
    );
    ref.read(botActivityProvider.notifier).markSettled(botId, reply.workState);
    if (pending == null) {
      await _persist(upsertBotMessage(current, answered));
      return;
    }
    await _persist([
      for (final message in current)
        if (message.id == pending.id) answered else message,
    ]);
  }

  /// Mark the in-flight turn failed, keeping the text the person sent.
  ///
  /// The message is not removed: a turn that failed still has to show what was
  /// asked, or the person has no way to tell what went missing.
  Future<void> _settleFailure(String reason) async {
    _stopPolling();
    _startedAt = null;
    ref
        .read(botActivityProvider.notifier)
        .markSettled(botId, BotWorkState.failed);
    final current = await _loadedMessages();
    await _persist([
      for (final item in current)
        if (item.pending)
          item.copyWith(pending: false, failed: true, attentionReason: reason)
        else
          item,
    ]);
  }

  void _stopPolling() {
    _pollTimer?.cancel();
    _pollTimer = null;
  }

  /// Answer a question the bot asked.
  ///
  /// A secret answer is handed to the bot's desktop instead of being sent as
  /// chat text, so the value never lands in the transcript. Either way the
  /// question is settled so it does not reappear unanswered.
  Future<void> answerQuestion(BotQuestion question, String answer) async {
    final text = answer.trim();
    if (text.isEmpty) return;
    if (question.isSecret) {
      final api = ref.read(botApiProvider);
      if (api == null) throw const BotApiException('UNAUTHORIZED', '请先登录');
      await api.fillSecret(botId, name: question.secretName, value: text);
      _markQuestionAnswered(question);
      return;
    }
    await send(text);
  }

  /// Clear a settled question so it does not reappear unanswered.
  ///
  /// The card keeps its wording but loses the input type, which is what makes
  /// it answerable. A secret answer is redacted entirely rather than kept as a
  /// filled-in field.
  void _markQuestionAnswered(BotQuestion question) {
    unawaited(() async {
      final current = await _loadedMessages();
      await _persist([
        for (final message in current)
          if (message.question.question == question.question &&
              message.question.secretName == question.secretName)
            message.copyWith(
              question: BotQuestion(
                inputType: message.question.inputType,
                question: message.question.question,
              ),
            )
          else
            message,
      ]);
    }());
  }

  /// Confirm a plan reply so the bot carries out the arrangement it proposed.
  Future<void> confirmPlan(BotMessage plan) async {
    await send(plan.content, phase: BotPhase.execute);
  }

  /// Elapsed time of the in-flight turn, for the chat header.
  Duration? get runningFor {
    final started = _startedAt;
    if (started == null) return null;
    return DateTime.now().difference(started);
  }

  bool get isBusy =>
      !state.isLoading && _current.any((message) => message.pending);
}

/// Gap between reads of an admitted run.
///
/// Hub answers each poll with the run's current state, so a short gap keeps the
/// status honest without hammering the Hub.
const Duration botRunPollInterval = Duration(seconds: 1);

/// Consecutive failed reads before a turn is given up on.
///
/// One dropped read is a normal mobile network event and must not cancel a turn
/// Hub is still working on, so several have to fail in a row.
const int botRunPollMaxFailures = 3;
