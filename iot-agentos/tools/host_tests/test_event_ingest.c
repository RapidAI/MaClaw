/*
 * Host test of event ingest validation (plan N1-2 device-side wiring).
 *
 * Compiles the public header only: every rule is inline and free of ESP-IDF,
 * so the whole "may this payload reach the screen" contract is exercised
 * without a device, a Hub or a clock.
 *
 * The point of these cases is the *rejections*.  A validator that only has
 * "valid input passes" tests is indistinguishable from `return true`, and the
 * failures it is meant to prevent -- a half-rendered approval card, a
 * typo'd severity that silently became `silent` -- are all of the form "we
 * showed something we should not have".
 */

#include <stdio.h>
#include <string.h>

#include "services/event_ingest.h"

static int fail(const char *msg) {
    fprintf(stderr, "FAIL: %s\n", msg);
    return 1;
}

/* A well-formed approval that satisfies the section 4.1 audit contract. */
static event_ingest_fields_t valid_approval(void) {
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

static int test_valid_approval_passes(void) {
    event_ingest_fields_t fields = valid_approval();
    if (!event_ingest_validate(&fields)) {
        return fail("a well-formed approval must validate");
    }
    return 0;
}

static int test_category_is_a_closed_set(void) {
    static const char *const known[EVENT_CATEGORY_COUNT] = {
        "approval", "task_done", "schedule", "ve", "system",
    };
    for (int i = 0; i < (int)EVENT_CATEGORY_COUNT; ++i) {
        event_category_t parsed = EVENT_CATEGORY_COUNT;
        if (!event_category_parse(known[i], &parsed)) {
            fprintf(stderr, "FAIL: known category %s was rejected\n", known[i]);
            return 1;
        }
        if ((int)parsed != i) {
            fprintf(stderr, "FAIL: category %s parsed to %d\n", known[i], (int)parsed);
            return 1;
        }
    }
    if (event_category_parse("notice", NULL) || event_category_parse("", NULL) ||
        event_category_parse(NULL, NULL) || event_category_parse("Approval", NULL)) {
        return fail("an unknown/mis-cased category must be rejected");
    }
    if (strcmp(event_category_name(EVENT_CATEGORY_COUNT), "?") != 0) {
        return fail("an unknown category must not borrow a name");
    }
    return 0;
}

/* section 4.1: approval implies all three audit guarantees.  Each one missing
 * must reject the whole event -- they are the three failures that are silent
 * in production. */
static int test_approval_audit_contract_is_enforced(void) {
    event_ingest_fields_t no_ttl = valid_approval();
    no_ttl.ttl_sec = 0;
    if (event_ingest_validate(&no_ttl)) return fail("approval without ttl must be rejected");

    event_ingest_fields_t no_ack = valid_approval();
    no_ack.requires_ack = false;
    if (event_ingest_validate(&no_ack)) return fail("approval without requiresAck must be rejected");

    event_ingest_fields_t no_persist = valid_approval();
    no_persist.persist = false;
    if (event_ingest_validate(&no_persist)) return fail("approval without persist must be rejected");
    return 0;
}

/* The audit contract is scoped to approvals: a reminder has no decision to
 * lose, so requiring it to persist would be over-reach. */
static int test_non_approval_needs_no_ttl(void) {
    event_ingest_fields_t reminder = {
        .event_id = "evt_2",
        .category = "schedule",
        .severity = "soft",
        .title = "Stand-up in 10 minutes",
        .ttl_sec = 0,
        .requires_ack = false,
        .persist = false,
    };
    if (!event_ingest_validate(&reminder)) {
        return fail("a non-approval event must not need the approval audit contract");
    }
    return 0;
}

static int test_title_is_required(void) {
    event_ingest_fields_t no_title = valid_approval();
    no_title.title = NULL;
    if (event_ingest_validate(&no_title)) return fail("a titleless event must be rejected");

    event_ingest_fields_t empty_title = valid_approval();
    empty_title.title = "";
    if (event_ingest_validate(&empty_title)) return fail("an empty title must be rejected");
    return 0;
}

static int test_ttl_bounds(void) {
    event_ingest_fields_t fields = valid_approval();
    const long accepted[] = {1, 300, EVENT_INGEST_MAX_TTL_SEC};
    for (size_t i = 0; i < sizeof(accepted) / sizeof(accepted[0]); ++i) {
        fields.ttl_sec = accepted[i];
        if (!event_ingest_validate(&fields)) {
            fprintf(stderr, "FAIL: ttl %ld must be accepted\n", accepted[i]);
            return 1;
        }
    }
    const long rejected[] = {-1, EVENT_INGEST_MAX_TTL_SEC + 1, 100000};
    for (size_t i = 0; i < sizeof(rejected) / sizeof(rejected[0]); ++i) {
        fields.ttl_sec = rejected[i];
        if (event_ingest_validate(&fields)) {
            fprintf(stderr, "FAIL: ttl %ld must be rejected\n", rejected[i]);
            return 1;
        }
    }
    return 0;
}

static int test_action_bounds_and_fields(void) {
    event_ingest_fields_t fields = valid_approval();

    static const event_ingest_action_t too_many[EVENT_INGEST_MAX_ACTIONS + 1] = {
        {"a", "A", NULL, NULL}, {"b", "B", NULL, NULL}, {"c", "C", NULL, NULL},
        {"d", "D", NULL, NULL}, {"e", "E", NULL, NULL},
    };
    fields.actions = too_many;
    fields.action_count = EVENT_INGEST_MAX_ACTIONS + 1;
    if (event_ingest_validate(&fields)) return fail("more than the max actions must be rejected");

    /* An id is echoed back in event-ack; an empty one makes the audit trail
     * ambiguous, so it is rejected rather than defaulted. */
    static const event_ingest_action_t empty_id[1] = {{"", "A", NULL, NULL}};
    fields.actions = empty_id;
    fields.action_count = 1;
    if (event_ingest_validate(&fields)) return fail("an action without an id must be rejected");

    static const event_ingest_action_t empty_label[1] = {{"a", "", NULL, NULL}};
    fields.actions = empty_label;
    fields.action_count = 1;
    if (event_ingest_validate(&fields)) return fail("an action without a label must be rejected");

    /* A present-but-unknown kind/risk is a malformed payload; an absent one
     * is a legitimate "not declared". */
    static const event_ingest_action_t bad_kind[1] = {{"a", "A", "tertiary", NULL}};
    fields.actions = bad_kind;
    fields.action_count = 1;
    if (event_ingest_validate(&fields)) return fail("an unknown action kind must be rejected");

    static const event_ingest_action_t bad_risk[1] = {{"a", "A", NULL, "critical"}};
    fields.actions = bad_risk;
    fields.action_count = 1;
    if (event_ingest_validate(&fields)) return fail("an unknown action risk must be rejected");

    static const event_ingest_action_t absent_both[1] = {{"a", "A", NULL, NULL}};
    fields.actions = absent_both;
    fields.action_count = 1;
    if (!event_ingest_validate(&fields)) {
        return fail("an action with neither kind nor risk must be accepted");
    }

    fields.actions = NULL;
    fields.action_count = 1;
    if (event_ingest_validate(&fields)) return fail("a non-zero action count with no array must be rejected");
    return 0;
}

static int test_unknown_severity_is_rejected(void) {
    event_ingest_fields_t fields = valid_approval();
    fields.severity = "notice"; /* the exact typo the Hub prompt once used */
    if (event_ingest_validate(&fields)) return fail("an unknown severity must be rejected");

    fields.severity = NULL;
    if (event_ingest_validate(&fields)) return fail("a missing severity must be rejected");

    fields.severity = "silent";
    if (!event_ingest_validate(&fields)) return fail("a known severity must be accepted");
    return 0;
}

static int test_dedupe_key_falls_back_to_event_id(void) {
    event_ingest_fields_t fields = valid_approval();
    fields.dedupe_key = NULL;
    if (strcmp(event_ingest_dedupe_key(&fields), "evt_1") != 0) {
        return fail("a missing dedupeKey must fall back to the event id");
    }
    fields.dedupe_key = "";
    if (strcmp(event_ingest_dedupe_key(&fields), "evt_1") != 0) {
        return fail("an empty dedupeKey must fall back to the event id");
    }
    fields.dedupe_key = "approval:1";
    if (strcmp(event_ingest_dedupe_key(&fields), "approval:1") != 0) {
        return fail("a declared dedupeKey must win");
    }
    return 0;
}

/* An invalid event must never reach the matrix: DROP is a decision ("we chose
 * not to show it"), which is not the same as "we could not trust it". */
static int test_decide_rejects_invalid_before_the_matrix(void) {
    event_device_state_flags_t standby = {0};
    event_action_t action = EVENT_ACTION_COUNT;

    event_ingest_fields_t bad = valid_approval();
    bad.ttl_sec = 0; /* breaks the approval audit contract */
    if (event_ingest_decide(&bad, &standby, &action)) {
        return fail("decide must reject an invalid event");
    }
    if (action != EVENT_ACTION_COUNT) {
        return fail("decide must not write an action for an invalid event");
    }
    return 0;
}

/* The valid path must produce exactly what the section 4.2 matrix says. */
static int test_decide_uses_the_admission_matrix(void) {
    event_ingest_fields_t fields = valid_approval();
    fields.severity = "interrupt";

    event_device_state_flags_t speaking = {.speaking = true};
    event_action_t action = EVENT_ACTION_COUNT;
    if (!event_ingest_decide(&fields, &speaking, &action)) {
        return fail("a valid interrupt event must decide");
    }
    if (action != EVENT_ACTION_INTERRUPT_SPEAK) {
        fprintf(stderr, "FAIL: interrupt while speaking decided %s\n", event_action_name(action));
        return 1;
    }

    event_device_state_flags_t quiet = {.quiet_hours = true};
    if (!event_ingest_decide(&fields, &quiet, &action)) {
        return fail("a valid interrupt event must decide during quiet hours");
    }
    if (action != EVENT_ACTION_DISPLAY_AND_BUZZ) {
        return fail("interrupt during quiet hours must downgrade, never break");
    }
    return 0;
}

static int test_display_text_fallbacks(void) {
    event_ingest_fields_t fields = valid_approval();
    if (strcmp(event_ingest_display_title(&fields), "Need your OK") != 0) {
        return fail("the title must be shown as-is");
    }
    if (strcmp(event_ingest_display_body(&fields), "Delete report-final-v2.xlsx?") != 0) {
        return fail("the summary must be shown as the body");
    }
    fields.summary = NULL;
    if (strcmp(event_ingest_display_body(&fields), "") != 0) {
        return fail("a missing summary must yield an empty body, not NULL");
    }
    if (strcmp(event_ingest_display_title(NULL), "") != 0 ||
        strcmp(event_ingest_display_body(NULL), "") != 0) {
        return fail("display helpers must never return NULL");
    }
    return 0;
}

/* While the device is busy, only actions allowed to preempt may proceed. */
static int test_defers_only_while_busy_and_only_when_not_preempting(void) {
    /* Idle: nothing defers. */
    for (int a = 0; a < (int)EVENT_ACTION_COUNT; ++a) {
        if (event_ingest_defers_while_busy((event_action_t)a, false)) {
            fprintf(stderr, "FAIL: %s must not defer while idle\n", event_action_name((event_action_t)a));
            return 1;
        }
    }

    /* Busy: the preempting actions and DROP proceed; the rest wait. */
    const event_action_t proceed[] = {
        EVENT_ACTION_DROP,
        EVENT_ACTION_INTERRUPT_SPEAK,
        EVENT_ACTION_INTERRUPT_LISTEN,
    };
    for (size_t i = 0; i < sizeof(proceed) / sizeof(proceed[0]); ++i) {
        if (event_ingest_defers_while_busy(proceed[i], true)) {
            fprintf(stderr, "FAIL: %s must proceed while busy\n", event_action_name(proceed[i]));
            return 1;
        }
    }
    const event_action_t wait[] = {
        EVENT_ACTION_DISPLAY,
        EVENT_ACTION_DISPLAY_AND_BUZZ,
        EVENT_ACTION_SPEAK,
        EVENT_ACTION_QUEUE_SPEAK,
    };
    for (size_t i = 0; i < sizeof(wait) / sizeof(wait[0]); ++i) {
        if (!event_ingest_defers_while_busy(wait[i], true)) {
            fprintf(stderr, "FAIL: %s must defer while busy\n", event_action_name(wait[i]));
            return 1;
        }
    }
    return 0;
}

static int test_dedupe_ring_suppresses_repeats(void) {
    event_ingest_dedupe_t ring;
    event_ingest_dedupe_reset(&ring);
    if (event_ingest_dedupe_seen(&ring, "approval:1")) {
        return fail("the first sighting of a key must not be suppressed");
    }
    if (!event_ingest_dedupe_seen(&ring, "approval:1")) {
        return fail("a repeated key must be suppressed");
    }
    if (event_ingest_dedupe_seen(&ring, "approval:2")) {
        return fail("a different key must not be suppressed");
    }
    event_ingest_dedupe_reset(&ring);
    if (event_ingest_dedupe_seen(&ring, "approval:1")) {
        return fail("reset must forget recorded keys");
    }
    return 0;
}

/* Bounded: the oldest key is evicted rather than pinning memory forever.  A
 * suppressed-then-forgotten key may show again, which is the acceptable
 * failure mode -- the alternative is unbounded growth on a device. */
static int test_dedupe_ring_is_bounded(void) {
    event_ingest_dedupe_t ring;
    event_ingest_dedupe_reset(&ring);
    for (int i = 0; i < EVENT_INGEST_DEDUPE_CAPACITY; ++i) {
        char key[32];
        snprintf(key, sizeof(key), "evt-%d", i);
        if (event_ingest_dedupe_seen(&ring, key)) {
            fprintf(stderr, "FAIL: %s was suppressed before it was recorded\n", key);
            return 1;
        }
    }
    /* Still inside the window: the newest key is remembered. */
    if (!event_ingest_dedupe_seen(&ring, "evt-7")) {
        return fail("a key inside the window must still be suppressed");
    }
    /* Push one more to evict the oldest ("evt-0"). */
    if (event_ingest_dedupe_seen(&ring, "evt-new")) {
        return fail("a fresh key must not be suppressed");
    }
    /* Check the surviving key *before* the evicted one: a miss is recorded,
     * so probing the evicted key first would itself evict its neighbour. */
    if (!event_ingest_dedupe_seen(&ring, "evt-1")) {
        return fail("a key still inside the window must stay suppressed");
    }
    if (event_ingest_dedupe_seen(&ring, "evt-0")) {
        return fail("the evicted key must be forgotten");
    }
    return 0;
}

static int test_dedupe_ring_ignores_unusable_keys(void) {
    event_ingest_dedupe_t ring;
    event_ingest_dedupe_reset(&ring);
    /* An empty key would suppress every later event, so it is never recorded
     * and never matches. */
    if (event_ingest_dedupe_seen(&ring, "") || event_ingest_dedupe_seen(&ring, "")) {
        return fail("an empty key must never be suppressed");
    }
    /* An over-long key is not truncated into a possible collision. */
    char long_key[EVENT_INGEST_MAX_DEDUPE_KEY_LEN + 8];
    memset(long_key, 'k', sizeof(long_key) - 1);
    long_key[sizeof(long_key) - 1] = '\0';
    if (event_ingest_dedupe_seen(&ring, long_key) || event_ingest_dedupe_seen(&ring, long_key)) {
        return fail("an over-long key must never be suppressed");
    }
    if (event_ingest_dedupe_seen(&ring, NULL)) {
        return fail("a NULL key must never be suppressed");
    }
    /* The ring must still work after being fed unusable keys. */
    if (event_ingest_dedupe_seen(&ring, "ok") || !event_ingest_dedupe_seen(&ring, "ok")) {
        return fail("the ring must stay usable after unusable keys");
    }
    return 0;
}

int main(void) {
    if (test_valid_approval_passes()) return 1;
    if (test_category_is_a_closed_set()) return 1;
    if (test_approval_audit_contract_is_enforced()) return 1;
    if (test_non_approval_needs_no_ttl()) return 1;
    if (test_title_is_required()) return 1;
    if (test_ttl_bounds()) return 1;
    if (test_action_bounds_and_fields()) return 1;
    if (test_unknown_severity_is_rejected()) return 1;
    if (test_dedupe_key_falls_back_to_event_id()) return 1;
    if (test_decide_rejects_invalid_before_the_matrix()) return 1;
    if (test_decide_uses_the_admission_matrix()) return 1;
    if (test_display_text_fallbacks()) return 1;
    if (test_defers_only_while_busy_and_only_when_not_preempting()) return 1;
    if (test_dedupe_ring_suppresses_repeats()) return 1;
    if (test_dedupe_ring_is_bounded()) return 1;
    if (test_dedupe_ring_ignores_unusable_keys()) return 1;
    printf("event ingest: 16 groups passed\n");
    return 0;
}
