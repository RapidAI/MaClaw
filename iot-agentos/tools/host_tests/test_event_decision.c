/*
 * Host test of the pending-approval decision state machine (plan N1-6 device
 * side).
 *
 * Compiles the public header only: the whole "what may a tap do, and what do we
 * still owe the Hub" contract is inline and free of ESP-IDF, so it is exercised
 * without a device, a Hub, a board or a clock.
 *
 * The cases worth having are the awkward ones.  The easy path -- card arrives,
 * user taps, ack is sent -- is not where a high-risk approval goes wrong; the
 * failures that matter are a late tap on an expired card, a card that offers no
 * way to decline, a decision overwritten before it reached the Hub, and an ack
 * retried after it already landed.
 */

#include <stdio.h>
#include <string.h>

#include "services/event_decision.h"

static int fail(const char *msg) {
    fprintf(stderr, "FAIL: %s\n", msg);
    return 1;
}

/* A card shaped like the one guiapp/device_approval_event.go builds: an
 * affirmative `approve` (kind=primary), a `reject` (kind=secondary), and the
 * section 4.1 audit contract satisfied. */
static event_ingest_fields_t approval_card(void) {
    static const event_ingest_action_t actions[2] = {
        {"approve", "OK", "primary", "high"},
        {"reject", "No", "secondary", NULL},
    };
    event_ingest_fields_t fields = {
        .event_id = "evt_1",
        .category = "approval",
        .severity = "interrupt",
        .title = "Need your OK",
        .summary = "Delete report-final-v2.xlsx?",
        .dedupe_key = "approval:1",
        .requires_ack = true,
        .persist = true,
        .ttl_sec = 300,
        .actions = actions,
        .action_count = 2,
    };
    return fields;
}

static int test_begin_takes_over_the_slot(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();

    if (!event_decision_begin(&pending, &fields, 1000)) {
        return fail("a card with actions must become the pending decision");
    }
    if (strcmp(pending.event_id, "evt_1") != 0) {
        return fail("the event id must be recorded for correlation");
    }
    if (pending.action_count != 2) {
        return fail("both offered actions must be recorded");
    }
    if (!pending.actions[0].affirmative || pending.actions[1].affirmative) {
        return fail("only the kind=primary action is affirmative");
    }
    if (pending.expires_at_ms != 1000 + 300 * 1000) {
        return fail("the deadline must be the absolute ttl expiry");
    }
    if (!event_decision_active(&pending, 2000)) {
        return fail("a fresh card must be active");
    }
    return 0;
}

static int test_begin_rejects_a_card_with_no_actions(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    fields.actions = NULL;
    fields.action_count = 0;

    if (event_decision_begin(&pending, &fields, 1000)) {
        return fail("a card with no actions has nothing to answer");
    }
    if (pending.event_id[0] != '\0') {
        return fail("a refused card must leave the slot empty");
    }
    /* Every action id unusable is the same failure as no actions at all. */
    static const event_ingest_action_t blank[1] = {{"", "OK", "primary", NULL}};
    fields.actions = blank;
    fields.action_count = 1;
    if (event_decision_begin(&pending, &fields, 1000)) {
        return fail("an action without an id cannot be answered");
    }
    return 0;
}

static int test_begin_requires_an_event_id(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    fields.event_id = NULL;
    if (event_decision_begin(&pending, &fields, 1000)) {
        return fail("without an event id the ack could not be correlated");
    }
    fields.event_id = "";
    if (event_decision_begin(&pending, &fields, 1000)) {
        return fail("an empty event id is the same failure");
    }
    return 0;
}

/* An action that never declared a kind is not affirmative.  "Absent != low"
 * applies to buttons too: the fail-closed reading of an unlabelled control is
 * that it approves nothing. */
static int test_absent_kind_is_not_affirmative(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    static const event_ingest_action_t actions[1] = {{"approve", "OK", NULL, NULL}};
    event_ingest_fields_t fields = approval_card();
    fields.actions = actions;
    fields.action_count = 1;

    if (!event_decision_begin(&pending, &fields, 1000)) {
        return fail("the card must still be answerable");
    }
    if (event_decision_affirmative_index(&pending) != -1) {
        return fail("an undeclared kind must not be treated as affirmative");
    }
    if (event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 2000)) {
        return fail("a tap must not approve through an unlabelled button");
    }
    return 0;
}

static int test_primary_confirms_and_secondary_declines(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    (void)event_decision_begin(&pending, &fields, 1000);

    if (!event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 2000)) {
        return fail("the confirm gesture must be consumed");
    }
    if (!pending.decided || !pending.decided_approved) {
        return fail("the confirm gesture must record an approval");
    }
    if (strcmp(pending.decided_action_id, "approve") != 0) {
        return fail("the pressed action id must be the affirmative one");
    }
    if (event_decision_active(&pending, 2000)) {
        return fail("a decided card is no longer answerable");
    }
    /* A second gesture must not change the answer. */
    if (event_decision_apply_input(&pending, EVENT_DECISION_INPUT_SECONDARY, 2100)) {
        return fail("a decided card must not accept another answer");
    }
    if (!pending.decided_approved) {
        return fail("the first answer must stand");
    }

    event_decision_reset(&pending);
    (void)event_decision_begin(&pending, &fields, 1000);
    if (!event_decision_apply_input(&pending, EVENT_DECISION_INPUT_SECONDARY, 2000)) {
        return fail("the decline gesture must be consumed");
    }
    if (!pending.decided || pending.decided_approved) {
        return fail("the decline gesture must record a rejection");
    }
    if (strcmp(pending.decided_action_id, "reject") != 0) {
        return fail("the pressed action id must be the declined one");
    }
    return 0;
}

/* A card that offers only an affirmative button offers no way to decline.  The
 * device must fall through to the ordinary input policy rather than invent a
 * rejection the user was never shown. */
static int test_a_single_button_card_consumes_only_the_confirm(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    static const event_ingest_action_t actions[1] = {{"approve", "OK", "primary", "high"}};
    event_ingest_fields_t fields = approval_card();
    fields.actions = actions;
    fields.action_count = 1;
    (void)event_decision_begin(&pending, &fields, 1000);

    if (event_decision_apply_input(&pending, EVENT_DECISION_INPUT_SECONDARY, 2000)) {
        return fail("there is nothing to decline, so the input must fall through");
    }
    if (pending.decided) {
        return fail("a fallen-through gesture must not decide anything");
    }
    if (!event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 2000)) {
        return fail("the confirm gesture must still work");
    }
    return 0;
}

/* "Timed out means not approved" (plan D5) is a safety property, so expiry is
 * checked before the gesture is interpreted. */
static int test_an_expired_card_refuses_a_late_gesture(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    (void)event_decision_begin(&pending, &fields, 1000);
    const int64_t deadline = pending.expires_at_ms;

    if (event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, deadline)) {
        return fail("the expiry instant itself must already be closed");
    }
    if (pending.decided) {
        return fail("a late gesture must not record an approval");
    }
    if (!event_decision_note_window(&pending, deadline)) {
        return fail("the first observation of the closed window must report it");
    }
    if (pending.decided) {
        return fail("a timeout is not a decision");
    }
    if (event_decision_note_window(&pending, deadline + 1)) {
        return fail("the window closes once, not on every poll");
    }
    return 0;
}

static int test_a_card_without_a_window_never_expires(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    fields.ttl_sec = 0;
    (void)event_decision_begin(&pending, &fields, 1000);

    if (pending.expires_at_ms != 0) {
        return fail("no declared window means no deadline");
    }
    if (event_decision_note_window(&pending, 1000 + 24 * 3600 * 1000)) {
        return fail("a card without a window must not time out");
    }
    if (!event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 1 << 30)) {
        return fail("a card without a window stays answerable");
    }
    return 0;
}

/* A decision outranks a receipt: sending "the card arrived" after the user has
 * already answered would be noise the Hub has to reconcile. */
static int test_a_decision_outranks_a_receipt(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    (void)event_decision_begin(&pending, &fields, 1000);

    if (event_decision_next_ack(&pending, 2000) != EVENT_DECISION_ACK_RECEIPT) {
        return fail("requiresAck means the card must report that it arrived");
    }
    (void)event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 2000);
    if (event_decision_next_ack(&pending, 2000) != EVENT_DECISION_ACK_DECISION) {
        return fail("the answer must be what gets sent next");
    }
    if (pending.receipt_delivered) {
        return fail("a superseded receipt must not be marked delivered");
    }
    return 0;
}

static int test_no_receipt_is_owed_without_requires_ack(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    fields.requires_ack = false;
    (void)event_decision_begin(&pending, &fields, 1000);

    if (event_decision_next_ack(&pending, 2000) != EVENT_DECISION_ACK_NONE) {
        return fail("without requiresAck there is no receipt to send");
    }
    return 0;
}

static int test_an_expired_window_reports_a_timeout_once(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    (void)event_decision_begin(&pending, &fields, 1000);
    const int64_t deadline = pending.expires_at_ms;

    /* Reported even before note_window ran, so a poll that never observed the
     * transition still tells the Hub. */
    if (event_decision_next_ack(&pending, deadline) != EVENT_DECISION_ACK_TIMEOUT) {
        return fail("a closed window owes a timeout report");
    }
    (void)event_decision_note_window(&pending, deadline);
    if (event_decision_next_ack(&pending, deadline) != EVENT_DECISION_ACK_TIMEOUT) {
        return fail("the timeout stays owed until it is delivered");
    }
    event_decision_mark_ack_delivered(&pending, EVENT_DECISION_ACK_TIMEOUT);
    if (event_decision_next_ack(&pending, deadline) != EVENT_DECISION_ACK_NONE) {
        return fail("a delivered timeout must not be resent");
    }
    if (event_decision_next_ack(&pending, deadline + 10 * 60 * 1000) != EVENT_DECISION_ACK_NONE) {
        return fail("a timeout must never be re-reported later");
    }

    /* The poll loop may deliver a timeout it learned about from next_ack alone,
     * before anyone called note_window.  Marking it delivered must then be
     * enough to stop the resend, or every poll would report it forever. */
    event_decision_reset(&pending);
    (void)event_decision_begin(&pending, &fields, 1000);
    if (event_decision_next_ack(&pending, deadline) != EVENT_DECISION_ACK_TIMEOUT) {
        return fail("a closed window owes a timeout report even unobserved");
    }
    event_decision_mark_ack_delivered(&pending, EVENT_DECISION_ACK_TIMEOUT);
    if (event_decision_next_ack(&pending, deadline) != EVENT_DECISION_ACK_NONE) {
        return fail("a delivered timeout must stop the resend even without note_window");
    }
    return 0;
}

/* The ack payload must satisfy the Hub's audit rules, which reject a receipt
 * carrying a decision and an expiry carrying an actionId. */
static int test_the_ack_payload_matches_the_wire_contract(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    (void)event_decision_begin(&pending, &fields, 1000);

    if (strcmp(event_decision_ack_status(&pending, EVENT_DECISION_ACK_RECEIPT),
               "received") != 0) {
        return fail("the receipt status must be `received`");
    }
    if (event_decision_ack_action_id(&pending, EVENT_DECISION_ACK_RECEIPT) != NULL ||
        event_decision_ack_decided_by(EVENT_DECISION_ACK_RECEIPT) != NULL) {
        return fail("a receipt must carry neither an action nor a decider");
    }

    (void)event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 2000);
    if (strcmp(event_decision_ack_status(&pending, EVENT_DECISION_ACK_DECISION),
               "approved") != 0) {
        return fail("an approval must report `approved`");
    }
    if (strcmp(event_decision_ack_action_id(&pending, EVENT_DECISION_ACK_DECISION),
               "approve") != 0) {
        return fail("a decision must name the pressed action");
    }
    if (strcmp(event_decision_ack_decided_by(EVENT_DECISION_ACK_DECISION), "button") != 0) {
        return fail("a device decision is made by a button");
    }

    event_decision_reset(&pending);
    (void)event_decision_begin(&pending, &fields, 1000);
    (void)event_decision_apply_input(&pending, EVENT_DECISION_INPUT_SECONDARY, 2000);
    if (strcmp(event_decision_ack_status(&pending, EVENT_DECISION_ACK_DECISION),
               "rejected") != 0) {
        return fail("a decline must report `rejected`");
    }

    event_decision_reset(&pending);
    (void)event_decision_begin(&pending, &fields, 1000);
    if (strcmp(event_decision_ack_status(&pending, EVENT_DECISION_ACK_TIMEOUT), "expired") != 0) {
        return fail("a timeout must report `expired`");
    }
    if (event_decision_ack_action_id(&pending, EVENT_DECISION_ACK_TIMEOUT) != NULL) {
        return fail("a timeout pressed no button, so it carries no action");
    }
    if (strcmp(event_decision_ack_decided_by(EVENT_DECISION_ACK_TIMEOUT), "timeout") != 0) {
        return fail("a timeout's decider is the timeout itself");
    }
    return 0;
}

static int test_a_decision_is_retried_until_it_lands(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    (void)event_decision_begin(&pending, &fields, 1000);
    (void)event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 2000);

    if (event_decision_next_ack(&pending, 2000) != EVENT_DECISION_ACK_DECISION) {
        return fail("an undelivered decision must stay owed");
    }
    if (!event_decision_busy(&pending)) {
        return fail("an undelivered decision must hold the slot");
    }
    event_decision_mark_ack_delivered(&pending, EVENT_DECISION_ACK_DECISION);
    if (event_decision_next_ack(&pending, 2000) != EVENT_DECISION_ACK_NONE) {
        return fail("a delivered decision must not be resent");
    }
    if (event_decision_busy(&pending)) {
        return fail("a delivered decision must free the slot");
    }
    if (!event_decision_settled(&pending, 2000)) {
        return fail("a delivered decision settles the card");
    }
    return 0;
}

/* The one case where dropping a card is worse than blocking the next one: the
 * user's answer has not reached the Hub yet. */
static int test_a_new_card_cannot_overwrite_an_undelivered_answer(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t first = approval_card();
    (void)event_decision_begin(&pending, &first, 1000);
    (void)event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 2000);

    event_ingest_fields_t second = approval_card();
    second.event_id = "evt_2";
    if (event_decision_begin(&pending, &second, 3000)) {
        return fail("a second card must not overwrite an undelivered answer");
    }
    if (strcmp(pending.event_id, "evt_1") != 0 || !pending.decided) {
        return fail("the outstanding answer must survive");
    }

    event_decision_mark_ack_delivered(&pending, EVENT_DECISION_ACK_DECISION);
    if (!event_decision_begin(&pending, &second, 3000)) {
        return fail("once the answer landed the slot must be reusable");
    }
    if (strcmp(pending.event_id, "evt_2") != 0 || pending.decided) {
        return fail("the new card must start unanswered");
    }
    return 0;
}

/* A decided card that never got its receipt delivered must not be held open
 * forever by the receipt: the decision supersedes it (see next_ack). */
static int test_a_superseded_receipt_does_not_block_settling(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    (void)event_decision_begin(&pending, &fields, 1000);
    (void)event_decision_apply_input(&pending, EVENT_DECISION_INPUT_SECONDARY, 2000);
    event_decision_mark_ack_delivered(&pending, EVENT_DECISION_ACK_DECISION);

    if (!event_decision_settled(&pending, 2000)) {
        return fail("a delivered decision settles even if no receipt was sent");
    }
    if (event_decision_next_ack(&pending, 2000) != EVENT_DECISION_ACK_NONE) {
        return fail("a settled card owes nothing");
    }
    return 0;
}

/* A card that owes the Hub nothing is still a card.  requiresAck=false means
 * there is no receipt to send, which is not the same as "nothing left to do":
 * the user still has to answer, and a caller that treated this as settled
 * would clear the slot and silently drop the answer. */
static int test_an_answerable_card_is_never_settled(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    fields.requires_ack = false;
    (void)event_decision_begin(&pending, &fields, 1000);

    if (event_decision_settled(&pending, 2000)) {
        return fail("a card waiting for an answer is not settled");
    }
    if (!event_decision_active(&pending, 2000)) {
        return fail("the card must still be answerable");
    }

    (void)event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 2000);
    if (event_decision_settled(&pending, 2000)) {
        return fail("an undelivered answer keeps the card unsettled");
    }
    event_decision_mark_ack_delivered(&pending, EVENT_DECISION_ACK_DECISION);
    if (!event_decision_settled(&pending, 2000)) {
        return fail("once the answer landed the card is settled");
    }
    return 0;
}

static int test_an_empty_slot_is_settled(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    if (!event_decision_settled(&pending, 0)) {
        return fail("an empty slot is settled");
    }
    if (event_decision_active(&pending, 0)) {
        return fail("an empty slot is not active");
    }
    if (event_decision_next_ack(&pending, 0) != EVENT_DECISION_ACK_NONE) {
        return fail("an empty slot owes nothing");
    }
    if (event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 0)) {
        return fail("a gesture with no card must fall through");
    }
    /* NULL safety: the dispatcher and the input binding both call these. */
    event_decision_reset(NULL);
    if (event_decision_begin(NULL, NULL, 0) || event_decision_active(NULL, 0) ||
        event_decision_busy(NULL) || !event_decision_settled(NULL, 0)) {
        return fail("NULL must be treated as an empty, settled slot");
    }
    if (event_decision_next_ack(NULL, 0) != EVENT_DECISION_ACK_NONE) {
        return fail("NULL owes nothing");
    }
    if (event_decision_ack_status(NULL, EVENT_DECISION_ACK_DECISION) == NULL) {
        return fail("a NULL slot must still yield a status string");
    }
    event_decision_mark_ack_delivered(NULL, EVENT_DECISION_ACK_DECISION);
    return 0;
}

/* A long event id or action id must be cut, never overflowed: they come from
 * the Hub, but the device copies them into fixed buffers. */
static int test_overlong_identifiers_are_cut_not_overflowed(void) {
    static char long_id[512];
    memset(long_id, 'x', sizeof(long_id) - 1);
    long_id[sizeof(long_id) - 1] = '\0';
    static const event_ingest_action_t actions[1] = {{long_id, "OK", "primary", NULL}};

    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    fields.event_id = long_id;
    fields.actions = actions;
    fields.action_count = 1;
    if (!event_decision_begin(&pending, &fields, 1000)) {
        return fail("an overlong id must be cut, not refused");
    }
    if (strlen(pending.event_id) != EVENT_DECISION_MAX_EVENT_ID_LEN) {
        return fail("the event id must be cut to the protocol bound");
    }
    if (strlen(pending.actions[0].id) != EVENT_DECISION_MAX_ACTION_ID_LEN) {
        return fail("the action id must be cut to the protocol bound");
    }
    if (!event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 2000)) {
        return fail("a cut action id must still be answerable");
    }
    return 0;
}

/* A timeout is its own outcome, never a rejection (plan D5).  This is the one
 * classification a caller could plausibly get wrong by reading `decided` and
 * `timed_out` itself, so the module answers it instead of leaving it implied. */
static int test_outcome_never_reports_a_timeout_as_a_rejection(void) {
    event_decision_t pending;
    event_decision_reset(&pending);
    event_ingest_fields_t fields = approval_card();
    (void)event_decision_begin(&pending, &fields, 1000);
    if (event_decision_outcome(&pending) != EVENT_DECISION_OUTCOME_NONE) {
        return fail("an unanswered card has no outcome yet");
    }

    (void)event_decision_apply_input(&pending, EVENT_DECISION_INPUT_PRIMARY, 2000);
    if (event_decision_outcome(&pending) != EVENT_DECISION_OUTCOME_APPROVED) {
        return fail("a primary gesture must report an approval");
    }

    event_decision_reset(&pending);
    (void)event_decision_begin(&pending, &fields, 1000);
    (void)event_decision_apply_input(&pending, EVENT_DECISION_INPUT_SECONDARY, 2000);
    if (event_decision_outcome(&pending) != EVENT_DECISION_OUTCOME_DECLINED) {
        return fail("a secondary gesture must report a decline");
    }

    event_decision_reset(&pending);
    (void)event_decision_begin(&pending, &fields, 1000);
    (void)event_decision_note_window(&pending, pending.expires_at_ms);
    if (event_decision_outcome(&pending) != EVENT_DECISION_OUTCOME_TIMED_OUT) {
        return fail("an expired window must report a timeout");
    }
    if (event_decision_outcome(&pending) == EVENT_DECISION_OUTCOME_DECLINED) {
        return fail("a timeout must never be reported as a decline");
    }

    /* The closed set must stay closed: a caller switches on it. */
    if (EVENT_DECISION_OUTCOME_COUNT != 4) {
        return fail("the outcome set must stay at none/approved/declined/timed out");
    }
    return 0;
}

int main(void) {
    if (test_begin_takes_over_the_slot()) return 1;
    if (test_begin_rejects_a_card_with_no_actions()) return 1;
    if (test_begin_requires_an_event_id()) return 1;
    if (test_absent_kind_is_not_affirmative()) return 1;
    if (test_primary_confirms_and_secondary_declines()) return 1;
    if (test_a_single_button_card_consumes_only_the_confirm()) return 1;
    if (test_an_expired_card_refuses_a_late_gesture()) return 1;
    if (test_a_card_without_a_window_never_expires()) return 1;
    if (test_a_decision_outranks_a_receipt()) return 1;
    if (test_no_receipt_is_owed_without_requires_ack()) return 1;
    if (test_an_expired_window_reports_a_timeout_once()) return 1;
    if (test_the_ack_payload_matches_the_wire_contract()) return 1;
    if (test_a_decision_is_retried_until_it_lands()) return 1;
    if (test_a_new_card_cannot_overwrite_an_undelivered_answer()) return 1;
    if (test_a_superseded_receipt_does_not_block_settling()) return 1;
    if (test_an_answerable_card_is_never_settled()) return 1;
    if (test_an_empty_slot_is_settled()) return 1;
    if (test_overlong_identifiers_are_cut_not_overflowed()) return 1;
    if (test_outcome_never_reports_a_timeout_as_a_rejection()) return 1;
    printf("event decision: 19 groups passed\n");
    return 0;
}
