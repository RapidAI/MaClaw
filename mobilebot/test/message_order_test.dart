import 'package:flutter_test/flutter_test.dart';
import 'package:mobilebot/features/bots/bot_models.dart';
import 'package:mobilebot/features/bots/message_order.dart';

void main() {
  test('newDesktopMessageId matches the GUI m- scheme', () {
    final id = newDesktopMessageId();
    expect(RegExp(r'^m-[0-9a-z]+-[0-9a-z]{6}$').hasMatch(id), isTrue);
    final started = botMessageStartedAt(id);
    final now = DateTime.now().millisecondsSinceEpoch;
    expect(started, greaterThan(now - 5000));
    expect(started, lessThanOrEqualTo(now + 1000));
  });

  test('botMessageStartedAt parses the embedded instant', () {
    expect(botMessageStartedAt('m-k0-abc'), 20 * 36); // k=20
    expect(botMessageStartedAt('m-2z-x'), 2 * 36 + 35);
  });

  test('botMessageStartedAt ignores other id schemes', () {
    expect(botMessageStartedAt('delivery-run-1'), 0);
    expect(botMessageStartedAt('f47ac10b-58cc-4372'), 0);
    expect(botMessageStartedAt('m1'), 0);
    expect(botMessageStartedAt(''), 0);
    expect(botMessageStartedAt('m--x'), 0);
    expect(botMessageStartedAt('m-zzzzzz'), 0); // no second hyphen segment
  });

  test('messageOrderMs falls back to at then zero', () {
    final at = DateTime.fromMillisecondsSinceEpoch(5000);
    expect(messageOrderMs(id: 'delivery-1', at: at), 5000);
    expect(messageOrderMs(id: 'm-k0-x', at: at), 720); // id wins
    expect(messageOrderMs(id: 'legacy'), 0);
  });

  test('pendingReplyIsStale mirrors the desktop orphan rule', () {
    final now = DateTime.now().millisecondsSinceEpoch;
    final oldId =
        'm-${(now - botPendingReplyWaitMs - 60000).toRadixString(36)}-x';
    expect(pendingReplyIsStale(pending: true, id: oldId, nowMs: now), isTrue);
    // Settled bubbles and fresh pendings stay.
    expect(pendingReplyIsStale(pending: false, id: oldId, nowMs: now), isFalse);
    expect(
      pendingReplyIsStale(pending: true, id: newDesktopMessageId(), nowMs: now),
      isFalse,
    );
    // Undated bubbles cannot prove their age — kept, never swept.
    expect(
      pendingReplyIsStale(pending: true, id: 'legacy', nowMs: now),
      isFalse,
    );
    // `at` proves age when the id does not carry a timestamp.
    expect(
      pendingReplyIsStale(
        pending: true,
        id: 'legacy',
        at: DateTime.fromMillisecondsSinceEpoch(
          now - botPendingReplyWaitMs - 1,
        ),
        nowMs: now,
      ),
      isTrue,
    );
  });

  // Regression for the duplicate-reply bug. `_drainPendingDeliveries` used to
  // skip a pending run only when a bubble's *id* equalled `delivery-{runId}`.
  // The settle path lands the same reply under a fresh `m-…` id, so the two
  // id schemes never matched and the reply was appended twice — text and
  // attachments included. This test reproduces the scenario with the
  // transcript already holding the settle bubble: the pre-fix id-only check
  // reports "not a duplicate" (would re-add), while botDeliveryAlreadyShown
  // reports it already shown.
  test('pending delivery is not re-added when the run is already shown', () {
    // Bubbles already in the transcript, as `_settleReply`/`_mergeEntries`
    // leave them: settle uses an `m-…` id with runId = requestId = the run,
    // a journal deliver echo carries `desktop-bot-deliver-{run}`.
    const settle = ChatMessage(
      id: 'm-k0-abc123',
      role: 'assistant',
      content: 'done',
      runId: 'r1',
      requestId: 'r1',
      at: null,
    );
    const deliverEcho = ChatMessage(
      id: 'delivery-r2',
      role: 'assistant',
      content: 'done',
      runId: 'r2',
      requestId: 'desktop-bot-deliver-r2',
    );
    const userLine = ChatMessage(
      id: 'u-1',
      role: 'user',
      content: 'go',
      runId: 'r1',
      requestId: 'r1',
    );
    final messages = [settle, deliverEcho, userLine];

    // What the pre-fix `_drainPendingDeliveries` did: match only on the
    // `delivery-{runId}` id. For run r1 the shown bubble is `m-…`, so this
    // returns false — the buggy path re-added the delivery.
    bool preFixWouldReAdd(String runId) => !messages.any(
          (m) => m.id == 'delivery-$runId',
        );
    expect(preFixWouldReAdd('r1'), isTrue); // bug: duplicate was coming
    expect(preFixWouldReAdd('r2'), isFalse); // same-id case already deduped

    // The fixed check: dedupe by run among *assistant* bubbles only, like
    // _mergeEntries — a user's line of the same turn must not mask a reply.
    final shown = messages.where((m) => m.role == 'assistant');
    bool alreadyShown(String runId) =>
        messages.any((m) => m.id == 'delivery-$runId') ||
        botDeliveryAlreadyShown(
          shownRunIds: shown.map((m) => m.runId),
          shownRequestIds: shown.map((m) => m.requestId),
          runId: runId,
        );

    expect(alreadyShown('r1'), isTrue); // settle's m- bubble covers it
    expect(alreadyShown('r2'), isTrue); // deliver echo covers it
    expect(alreadyShown('r3'), isFalse); // a different run still lands
    expect(alreadyShown(' '), isFalse); // blank run is never a twin
  });
}
