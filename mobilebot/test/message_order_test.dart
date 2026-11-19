import 'package:flutter_test/flutter_test.dart';
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
}
