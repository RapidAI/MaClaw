#pragma once

/*
 * Pending approval decision (plan N1-6, device side).
 *
 * N1-2 made the device *show* an event card.  This module makes the card
 * *answerable*: it owns the one card that is waiting for a decision, turns the
 * user's gesture into the action the card actually offered, and decides which
 * ack the device still owes the Hub.
 *
 * Why this is a pure value module:
 *   - the interesting rules are safety rules, not plumbing.  "a tap may only
 *     approve an action the card declared", "an expired card must never be
 *     approved late", "a decision is reported once and retried until the Hub
 *     accepts it" are exactly the kind of thing that is invisible inside a
 *     1400-line dispatcher and cheap to verify on the host;
 *   - the state machine has real edge cases (decide-then-expire, expire-then-
 *     decide, a second card arriving before the first ack landed) that deserve
 *     table tests rather than field reports.
 *
 * The caller owns the clock (monotonic milliseconds) and the transport, so
 * this header stays free of ESP-IDF, cJSON and any board detail and compiles
 * on the host.
 *
 * Deliberately NOT here: which physical gesture means "confirm".  That mapping
 * belongs to the presentation layer's input binding, which already owns the
 * "one foreground surface owns the input" policy.  This module takes an
 * abstract primary/secondary input so the mapping can change per board without
 * touching the safety rules.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

#include "services/event_ingest.h"

/* Protocol bounds, mirroring corelib/im (DeviceEventMaxIDLen /
 * DeviceEventMaxActionIDLen / DeviceEventMaxActions).  Duplicated because the
 * device image does not link the Go contract; a mismatch is caught by the
 * check script rather than at runtime. */
#define EVENT_DECISION_MAX_EVENT_ID_LEN 64
#define EVENT_DECISION_MAX_ACTION_ID_LEN 32
#define EVENT_DECISION_MAX_ACTIONS EVENT_INGEST_MAX_ACTIONS

/* The ack wire statuses (corelib/im/device_event.go).  Spelled here as macros
 * so the check script can compare them value by value against the Go contract
 * the way it already does for the event categories. */
#define EVENT_DECISION_STATUS_RECEIVED "received"
#define EVENT_DECISION_STATUS_APPROVED "approved"
#define EVENT_DECISION_STATUS_REJECTED "rejected"
#define EVENT_DECISION_STATUS_EXPIRED "expired"

/* How the decision was made (corelib/im/device_event.go).  `timeout` is not a
 * user action; it exists so the audit record can say "nobody answered" without
 * inventing a decider. */
#define EVENT_DECISION_DECIDED_BY_BUTTON "button"
#define EVENT_DECISION_DECIDED_BY_TIMEOUT "timeout"

/* The abstract gesture the presentation layer resolved.  Keeping this out of
 * app_intent_service.h is what lets this header stay host-compilable: the
 * binding does the two-line translation. */
typedef enum {
    EVENT_DECISION_INPUT_PRIMARY = 0, /* the "confirm" gesture */
    EVENT_DECISION_INPUT_SECONDARY,   /* the "decline" gesture */
    EVENT_DECISION_INPUT_COUNT
} event_decision_input_t;

/* Which ack the device still owes the Hub. */
typedef enum {
    EVENT_DECISION_ACK_NONE = 0, /* nothing outstanding */
    EVENT_DECISION_ACK_RECEIPT,  /* `requiresAck`: the card reached the screen */
    EVENT_DECISION_ACK_DECISION, /* the user answered */
    EVENT_DECISION_ACK_TIMEOUT,  /* the window closed unanswered */
    EVENT_DECISION_ACK_COUNT
} event_decision_ack_kind_t;

typedef struct {
    char id[EVENT_DECISION_MAX_ACTION_ID_LEN + 1];
    /* The affirmative action, derived from section 4.1's `kind`: the GUI builds
     * `approve` as primary and `reject` as secondary.  An action that declared
     * no kind is not affirmative -- "absent != low" applies here too, and the
     * fail-closed reading of an unlabelled button is "this does not approve
     * anything". */
    bool affirmative;
} event_decision_action_t;

typedef struct {
    char event_id[EVENT_DECISION_MAX_EVENT_ID_LEN + 1];
    event_decision_action_t actions[EVENT_DECISION_MAX_ACTIONS];
    int action_count;
    bool requires_ack;
    /* Absolute deadline in the caller's monotonic milliseconds.  0 means the
     * event declared no window, so the card never expires on its own. */
    int64_t expires_at_ms;

    /* The card was presented, so a `received` receipt is owed. */
    bool presented;
    bool receipt_delivered;

    /* A decision was taken.  Kept even after the card is gone, because the ack
     * is retried until the Hub accepts it. */
    bool decided;
    bool decided_approved;
    char decided_action_id[EVENT_DECISION_MAX_ACTION_ID_LEN + 1];
    bool decision_delivered;

    /* The window closed unanswered, so an `expired` report is owed. */
    bool timed_out;
    bool timeout_delivered;
} event_decision_t;

static inline void event_decision_copy_text(char *dst, size_t capacity, const char *src) {
    if (!dst || capacity == 0) return;
    dst[0] = '\0';
    if (!src) return;
    size_t index = 0;
    while (index + 1 < capacity && src[index] != '\0') {
        dst[index] = src[index];
        index++;
    }
    dst[index] = '\0';
}

static inline void event_decision_reset(event_decision_t *pending) {
    if (!pending) return;
    memset(pending, 0, sizeof(*pending));
}

/* An action is affirmative only when the producer said so explicitly. */
static inline bool event_decision_action_is_affirmative(const event_ingest_action_t *action) {
    return action && action->kind && strcmp(action->kind, "primary") == 0;
}

static inline int event_decision_affirmative_index(const event_decision_t *pending) {
    if (!pending) return -1;
    for (int index = 0; index < pending->action_count; index++) {
        if (pending->actions[index].affirmative) return index;
    }
    return -1;
}

/* The decline target is the first action that is not the affirmative one.  A
 * card with a single affirmative button therefore offers no way to decline,
 * and a secondary gesture falls through to the ordinary input policy instead
 * of inventing a rejection the user was never offered. */
static inline int event_decision_decline_index(const event_decision_t *pending) {
    if (!pending) return -1;
    for (int index = 0; index < pending->action_count; index++) {
        if (!pending->actions[index].affirmative) return index;
    }
    return -1;
}

/* True when the card still has an answer to give (or an ack to hand over).
 * Used to decide whether a new card may replace this one: overwriting a card
 * whose decision has not reached the Hub yet would silently drop the user's
 * answer, so the caller keeps the incoming message pending instead. */
static inline bool event_decision_busy(const event_decision_t *pending) {
    if (!pending) return false;
    if (pending->decided && !pending->decision_delivered) return true;
    if (pending->timed_out && !pending->timeout_delivered) return true;
    return false;
}

/* True while the card is on screen and still answerable. */
static inline bool event_decision_active(const event_decision_t *pending, int64_t now_ms) {
    if (!pending || pending->event_id[0] == '\0') return false;
    if (pending->decided || pending->timed_out) return false;
    if (pending->expires_at_ms > 0 && now_ms >= pending->expires_at_ms) return false;
    return true;
}

/* Takes over the pending slot with a new card.  Refuses while an undelivered
 * decision is outstanding (see event_decision_busy). */
static inline bool event_decision_begin(event_decision_t *pending,
                                        const event_ingest_fields_t *fields,
                                        int64_t now_ms) {
    if (!pending || !fields) return false;
    if (!fields->event_id || fields->event_id[0] == '\0') return false;
    if (!fields->actions || fields->action_count <= 0) return false;
    if (event_decision_busy(pending)) return false;

    event_decision_reset(pending);
    event_decision_copy_text(pending->event_id, sizeof(pending->event_id), fields->event_id);
    pending->requires_ack = fields->requires_ack;
    pending->presented = true;
    if (fields->ttl_sec > 0) {
        pending->expires_at_ms = now_ms + (int64_t)fields->ttl_sec * 1000;
    }
    int count = fields->action_count;
    if (count > EVENT_DECISION_MAX_ACTIONS) count = EVENT_DECISION_MAX_ACTIONS;
    for (int index = 0; index < count; index++) {
        const event_ingest_action_t *source = &fields->actions[index];
        if (!source->id || source->id[0] == '\0') continue;
        event_decision_copy_text(pending->actions[pending->action_count].id,
                                 sizeof(pending->actions[pending->action_count].id),
                                 source->id);
        pending->actions[pending->action_count].affirmative =
            event_decision_action_is_affirmative(source);
        pending->action_count++;
    }
    if (pending->action_count == 0) {
        event_decision_reset(pending);
        return false;
    }
    return true;
}

/* Turns a gesture into a decision, returning whether the input was consumed.
 *
 * Expiry is checked first and wins: a card whose window has closed must never
 * be approved late, because "timed out means not approved" (plan D5) is a
 * safety property, not a display preference.  The input then falls through to
 * the ordinary policy, which is also what happens when the card does not offer
 * the requested action -- the device only ever steals input for something the
 * user was actually offered. */
static inline bool event_decision_apply_input(event_decision_t *pending,
                                              event_decision_input_t input,
                                              int64_t now_ms) {
    if (!event_decision_active(pending, now_ms)) return false;
    int index = input == EVENT_DECISION_INPUT_PRIMARY
                    ? event_decision_affirmative_index(pending)
                    : event_decision_decline_index(pending);
    if (index < 0) return false;

    pending->decided = true;
    pending->decided_approved = pending->actions[index].affirmative;
    event_decision_copy_text(pending->decided_action_id,
                             sizeof(pending->decided_action_id),
                             pending->actions[index].id);
    return true;
}

/* Marks the window closed.  Returns true when this call is the one that closed
 * it, so the caller can repaint once rather than on every poll. */
static inline bool event_decision_note_window(event_decision_t *pending, int64_t now_ms) {
    if (!pending || pending->event_id[0] == '\0') return false;
    if (pending->decided || pending->timed_out) return false;
    if (pending->expires_at_ms <= 0 || now_ms < pending->expires_at_ms) return false;
    pending->timed_out = true;
    return true;
}

/* Which ack the device owes right now.
 *
 * A decision outranks a receipt: the Hub only needs to learn that the card
 * arrived, and a stale receipt sent after the answer would be noise.  A
 * timeout outranks a receipt for the same reason, and outranks nothing else
 * because a decision can no longer be taken once the window closed. */
static inline event_decision_ack_kind_t event_decision_next_ack(const event_decision_t *pending,
                                                               int64_t now_ms) {
    if (!pending || pending->event_id[0] == '\0') return EVENT_DECISION_ACK_NONE;
    if (pending->decided) {
        return pending->decision_delivered ? EVENT_DECISION_ACK_NONE
                                           : EVENT_DECISION_ACK_DECISION;
    }
    if (pending->timed_out) {
        return pending->timeout_delivered ? EVENT_DECISION_ACK_NONE
                                          : EVENT_DECISION_ACK_TIMEOUT;
    }
    if (pending->expires_at_ms > 0 && now_ms >= pending->expires_at_ms) {
        /* The caller has not run event_decision_note_window yet; report the
         * timeout rather than a receipt for a card that is already over. */
        return EVENT_DECISION_ACK_TIMEOUT;
    }
    if (pending->requires_ack && pending->presented && !pending->receipt_delivered) {
        return EVENT_DECISION_ACK_RECEIPT;
    }
    return EVENT_DECISION_ACK_NONE;
}

static inline const char *event_decision_ack_status(const event_decision_t *pending,
                                                    event_decision_ack_kind_t kind) {
    switch (kind) {
        case EVENT_DECISION_ACK_RECEIPT:
            return EVENT_DECISION_STATUS_RECEIVED;
        case EVENT_DECISION_ACK_DECISION:
            return pending && pending->decided_approved ? EVENT_DECISION_STATUS_APPROVED
                                                        : EVENT_DECISION_STATUS_REJECTED;
        case EVENT_DECISION_ACK_TIMEOUT:
            return EVENT_DECISION_STATUS_EXPIRED;
        default:
            return NULL;
    }
}

/* The action the user pressed.  NULL for a receipt or a timeout: the wire
 * contract rejects a receipt carrying a decision and an expiry carrying an
 * actionId, so an empty answer here is the only correct one. */
static inline const char *event_decision_ack_action_id(const event_decision_t *pending,
                                                       event_decision_ack_kind_t kind) {
    if (kind != EVENT_DECISION_ACK_DECISION || !pending) return NULL;
    if (pending->decided_action_id[0] == '\0') return NULL;
    return pending->decided_action_id;
}

static inline const char *event_decision_ack_decided_by(event_decision_ack_kind_t kind) {
    if (kind == EVENT_DECISION_ACK_DECISION) return EVENT_DECISION_DECIDED_BY_BUTTON;
    if (kind == EVENT_DECISION_ACK_TIMEOUT) return EVENT_DECISION_DECIDED_BY_TIMEOUT;
    return NULL;
}

/* Records that the Hub accepted this ack.  A decision that has landed frees
 * the slot, so the next card can take it.
 *
 * A delivered timeout also marks the card timed out.  next_ack can report a
 * timeout before anyone ran note_window, and without this the two would
 * disagree: the ack would be marked delivered while the card still looked
 * merely "expired but undecided", so every poll would send it again. */
static inline void event_decision_mark_ack_delivered(event_decision_t *pending,
                                                     event_decision_ack_kind_t kind) {
    if (!pending) return;
    switch (kind) {
        case EVENT_DECISION_ACK_RECEIPT:
            pending->receipt_delivered = true;
            break;
        case EVENT_DECISION_ACK_DECISION:
            pending->decision_delivered = true;
            break;
        case EVENT_DECISION_ACK_TIMEOUT:
            pending->timed_out = true;
            pending->timeout_delivered = true;
            break;
        default:
            break;
    }
}

/* True when the card is fully settled: nothing left on screen to answer and
 * nothing left to send.  The caller may clear it at its leisure.
 *
 * An *answerable* card is never settled even when it owes no ack.  A card with
 * requiresAck=false owes the Hub nothing, but it is still on screen waiting
 * for the user, and reporting it settled would disarm the very gesture it
 * exists to collect. */
static inline bool event_decision_settled(const event_decision_t *pending, int64_t now_ms) {
    if (!pending || pending->event_id[0] == '\0') return true;
    if (event_decision_active(pending, now_ms)) return false;
    if (event_decision_busy(pending)) return false;
    return event_decision_next_ack(pending, now_ms) == EVENT_DECISION_ACK_NONE;
}

/* Which outcome the card reached, so the caller can word it.
 *
 * Kept as a closed set rather than as text, for two reasons.  The wording is
 * device copy and belongs with the other UI strings, and this header stays
 * ASCII-only and host-compilable.  But the *semantics* belong here: a timeout
 * is its own outcome and never a rejection (plan D5 -- "not approved" is not
 * the same as "refused", and the user must not be told they declined
 * something they never saw).  A caller that had to infer the outcome from
 * `decided`/`timed_out` would get exactly that wrong. */
typedef enum {
    EVENT_DECISION_OUTCOME_NONE = 0, /* still waiting for an answer */
    EVENT_DECISION_OUTCOME_APPROVED,
    EVENT_DECISION_OUTCOME_DECLINED,
    EVENT_DECISION_OUTCOME_TIMED_OUT,
    EVENT_DECISION_OUTCOME_COUNT
} event_decision_outcome_t;

static inline event_decision_outcome_t event_decision_outcome(const event_decision_t *pending) {
    if (!pending) return EVENT_DECISION_OUTCOME_NONE;
    if (pending->decided) {
        return pending->decided_approved ? EVENT_DECISION_OUTCOME_APPROVED
                                         : EVENT_DECISION_OUTCOME_DECLINED;
    }
    if (pending->timed_out) return EVENT_DECISION_OUTCOME_TIMED_OUT;
    return EVENT_DECISION_OUTCOME_NONE;
}
