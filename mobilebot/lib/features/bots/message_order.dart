import 'dart:math';

/// Message ids mirror the desktop GUI scheme — `m-<base36 ms>-<base36 rand>`.
/// The clock in the id is not the chat order; Hub's created_seq is.
final _rand = Random();

String newDesktopMessageId() {
  final ms = DateTime.now().millisecondsSinceEpoch;
  final suffix = _rand.nextInt(36 * 36 * 36 * 36 * 36 * 36);
  return 'm-${ms.toRadixString(36)}-${suffix.toRadixString(36).padLeft(6, '0')}';
}

/// Milliseconds embedded in an `m-` id, or 0 for ids in any other scheme
/// (`delivery-…`, legacy uuids, ids other clients chose).
int botMessageStartedAt(String id) {
  final match = RegExp(r'^m-([0-9a-z]+)-', caseSensitive: false).firstMatch(id);
  if (match == null) return 0;
  final started = int.tryParse(match.group(1)!, radix: 36) ?? 0;
  return started > 0 ? started : 0;
}

/// Best-effort wall-clock instant for ordering a bubble: the id's embedded
/// timestamp first, then its recorded `at`, else 0 (caller falls back to the
/// journal sequence so undated messages keep hub arrival order).
int messageOrderMs({required String id, DateTime? at}) {
  final started = botMessageStartedAt(id);
  if (started > 0) return started;
  return at?.millisecondsSinceEpoch ?? 0;
}

/// Same bound the GUI uses (BOT_PENDING_REPLY_WAIT_MS): a pending bubble older
/// than this was orphaned — its run died before answering.
const botPendingReplyWaitMs = 31 * 60 * 1000;

/// Mirrors `pendingBotReplyIsStale` on the desktop side. Undated bubbles
/// (unknown id scheme and no `at`) cannot prove their age and are kept.
bool pendingReplyIsStale({
  required bool pending,
  required String id,
  DateTime? at,
  required int nowMs,
}) {
  if (!pending) return false;
  final started = messageOrderMs(id: id, at: at);
  return started > 0 && nowMs - started > botPendingReplyWaitMs;
}

/// True when a `delivery-{runId}` pull would re-show a reply the transcript
/// already has. `_drainPendingDeliveries` used to dedupe only on the
/// `delivery-{runId}` id, so a settle that already landed the same run as an
/// `m-…` bubble (or a journal echo carrying the run/request id) was added a
/// second time — the duplicate reply, attachments included.
///
/// Twin rule mirrors `_mergeEntries`: a shown assistant bubble covers the
/// delivery when it carries the same run id, or the GUI's
/// `desktop-bot-deliver-{runId}` request id.
bool botDeliveryAlreadyShown({
  required Iterable<String> shownRunIds,
  required Iterable<String> shownRequestIds,
  required String runId,
}) {
  final run = runId.trim();
  if (run.isEmpty) return false;
  final deliverKey = 'desktop-bot-deliver-$run';
  for (final r in shownRunIds) {
    if (r.trim() == run) return true;
  }
  for (final r in shownRequestIds) {
    final req = r.trim();
    if (req == run || req == deliverKey) return true;
  }
  return false;
}
