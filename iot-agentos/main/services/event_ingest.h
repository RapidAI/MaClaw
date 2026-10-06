#pragma once

/*
 * Event ingest validation (plan N1-2, device-side wiring).
 *
 * The Hub is the authority that decides whether an event may be delivered
 * (hub/internal/im/device_event_push.go).  This module is the device's own
 * gate: it re-checks the fields it is about to act on, so a firmware never
 * renders a card from a payload the wire contract would have rejected.  The
 * two checks overlap on purpose -- a compromised or buggy producer must not be
 * able to reach the screen by skipping the Hub, and a future direct-LAN path
 * (D4) has no Hub validator in front of it at all.
 *
 * Why this is a pure value module and not a branch inside the dispatcher:
 *   - the rules it encodes are *safety* rules, not plumbing.  "approval means
 *     the three audit guarantees are present" and "an unknown severity must be
 *     rejected, never defaulted" are the kind of thing that is invisible in a
 *     code review of a 1400-line dispatcher and cheap to verify on the host;
 *   - the dispatcher already owns ack/cursor ordering; mixing JSON field
 *     policy into it is how the two get entangled.
 *
 * The caller (main.c) does the JSON extraction and hands plain values in, so
 * this header stays free of ESP-IDF and any JSON library and compiles on the
 * host.
 */

#include <stdbool.h>
#include <stddef.h>
#include <string.h>

#include "services/event_presentation_policy.h"

/* Mirrors the wire enum (corelib/im/device_event.go).  A closed set: the
 * firmware switches on it, so an unknown category must be rejected rather than
 * carried around as an opaque string. */
typedef enum {
    EVENT_CATEGORY_APPROVAL = 0,
    EVENT_CATEGORY_TASK_DONE,
    EVENT_CATEGORY_SCHEDULE,
    EVENT_CATEGORY_VE,
    EVENT_CATEGORY_SYSTEM,
    EVENT_CATEGORY_COUNT
} event_category_t;

/* Protocol bounds, mirroring corelib/im.  Duplicated here because the device
 * image does not link the Go contract; a mismatch is caught by the check
 * script rather than at runtime. */
#define EVENT_INGEST_MAX_ACTIONS 4
#define EVENT_INGEST_MAX_TTL_SEC 3600
#define EVENT_INGEST_MAX_DEDUPE_KEY_LEN 128

typedef struct {
    const char *id;    /* required, non-empty */
    const char *label; /* required, non-empty */
    const char *kind;  /* "primary"/"secondary"; NULL means not declared */
    const char *risk;  /* "low"/"medium"/"high"; NULL means not declared */
} event_ingest_action_t;

/*
 * The fields the device acts on.  Text is bounded by the Hub before it gets
 * here; this struct deliberately carries no length, because the rule this
 * module enforces is *presence and closed-set membership*, not truncation
 * (truncating an identity field would break event-ack correlation, and
 * truncating free text is the Hub's declared-budget job).
 */
typedef struct {
    const char *event_id;
    const char *category;   /* wire string, parsed below */
    const char *severity;   /* wire string, parsed below */
    const char *title;
    const char *summary;    /* NULL when absent */
    const char *dedupe_key; /* NULL when absent; falls back to event_id */
    bool requires_ack;
    bool persist;
    long ttl_sec; /* 0 means "not declared" */
    const event_ingest_action_t *actions;
    int action_count;
} event_ingest_fields_t;

static inline const char *event_category_name(event_category_t category) {
    static const char *const names[EVENT_CATEGORY_COUNT] = {
        "approval", "task_done", "schedule", "ve", "system",
    };
    if ((unsigned)category >= (unsigned)EVENT_CATEGORY_COUNT) return "?";
    return names[(unsigned)category];
}

/* Closed-set parse.  Returns false for NULL/empty/unknown so the caller can
 * reject the whole event instead of guessing a category for it. */
static inline bool event_category_parse(const char *text, event_category_t *out) {
    if (!text || text[0] == '\0') return false;
    static const struct {
        const char *wire;
        event_category_t value;
    } kCategories[EVENT_CATEGORY_COUNT] = {
        {"approval", EVENT_CATEGORY_APPROVAL},
        {"task_done", EVENT_CATEGORY_TASK_DONE},
        {"schedule", EVENT_CATEGORY_SCHEDULE},
        {"ve", EVENT_CATEGORY_VE},
        {"system", EVENT_CATEGORY_SYSTEM},
    };
    for (size_t i = 0; i < (size_t)EVENT_CATEGORY_COUNT; ++i) {
        if (strcmp(text, kCategories[i].wire) == 0) {
            if (out) *out = kCategories[i].value;
            return true;
        }
    }
    return false;
}

static inline bool event_ingest_action_kind_valid(const char *kind) {
    return kind && (strcmp(kind, "primary") == 0 || strcmp(kind, "secondary") == 0);
}

static inline bool event_ingest_action_risk_valid(const char *risk) {
    return risk && (strcmp(risk, "low") == 0 || strcmp(risk, "medium") == 0 ||
                    strcmp(risk, "high") == 0);
}

/*
 * Validate the fields the device is about to render.
 *
 * Fails closed: an event the device cannot render faithfully is rejected
 * rather than shown half-formed, because a half-rendered approval card is
 * worse than no card at all -- the user would be asked to decide on an object
 * the card never named.
 *
 * The approval audit contract (section 4.1) is enforced here too, not merely
 * trusted to the Hub.  All three failures it guards are silent in production:
 * without requiresAck nothing notices the card never arrived, without persist
 * a reboot drops a pending high-risk decision, and without a ttl the prompt
 * hangs forever.
 */
static inline bool event_ingest_validate(const event_ingest_fields_t *fields) {
    if (!fields) return false;
    if (!fields->event_id || fields->event_id[0] == '\0') return false;

    event_category_t category = EVENT_CATEGORY_COUNT;
    if (!event_category_parse(fields->category, &category)) return false;

    event_severity_t severity = EVENT_SEVERITY_COUNT;
    if (!event_severity_parse(fields->severity, &severity)) return false;

    /* Nothing to draw: a titleless card would occupy the screen and say
     * nothing, which reads as a device fault rather than an event. */
    if (!fields->title || fields->title[0] == '\0') return false;

    /* 0 means absent; any declared window must be inside the protocol range.
     * A negative or oversized ttl is a malformed payload, not a policy
     * choice, so it is rejected instead of clamped. */
    if (fields->ttl_sec < 0) return false;
    if (fields->ttl_sec > EVENT_INGEST_MAX_TTL_SEC) return false;

    /* An over-long dedupe key is rejected rather than truncated: truncation
     * would let two distinct events collide into one suppression, which is
     * the opposite of what the key is for. */
    if (fields->dedupe_key &&
        strlen(fields->dedupe_key) > EVENT_INGEST_MAX_DEDUPE_KEY_LEN) {
        return false;
    }

    if (fields->action_count < 0 ||
        fields->action_count > EVENT_INGEST_MAX_ACTIONS) return false;
    if (fields->action_count > 0 && !fields->actions) return false;
    for (int i = 0; i < fields->action_count; ++i) {
        const event_ingest_action_t *action = &fields->actions[i];
        if (!action->id || action->id[0] == '\0') return false;
        if (!action->label || action->label[0] == '\0') return false;
        /* An absent kind/risk is valid ("not declared"); a present but
         * unknown one is not.  Keeping "absent" distinct is what lets D5-A
         * stay a positive test on risk=high. */
        if (action->kind && !event_ingest_action_kind_valid(action->kind)) return false;
        if (action->risk && !event_ingest_action_risk_valid(action->risk)) return false;
    }

    if (category == EVENT_CATEGORY_APPROVAL) {
        if (fields->ttl_sec <= 0 || !fields->requires_ack || !fields->persist) {
            return false;
        }
    }
    return true;
}

/* Idempotency key with the same fallback the Hub applies: a producer that
 * does not distinguish a logical request from one attempt still gets the
 * "same key presented once" guarantee.  Never returns NULL. */
static inline const char *event_ingest_dedupe_key(const event_ingest_fields_t *fields) {
    if (!fields) return "";
    if (fields->dedupe_key && fields->dedupe_key[0] != '\0') return fields->dedupe_key;
    if (fields->event_id && fields->event_id[0] != '\0') return fields->event_id;
    return "";
}

/*
 * Validate, parse the severity and run the section 4.2 admission matrix in one
 * step.  Returns false when the event must be dropped without presentation;
 * on success `out_action` holds the abstract action the caller renders.
 *
 * An invalid event is not "decided as silent" -- it never reaches the matrix.
 * That distinction matters because DROP has a rendering meaning ("decided not
 * to show") while an invalid payload has none.
 */
static inline bool event_ingest_decide(const event_ingest_fields_t *fields,
                                       const event_device_state_flags_t *state,
                                       event_action_t *out_action) {
    if (!event_ingest_validate(fields)) return false;
    event_severity_t severity = EVENT_SEVERITY_COUNT;
    if (!event_severity_parse(fields->severity, &severity)) return false;
    const event_action_t action = event_presentation_decide_from_flags(severity, state);
    if (out_action) *out_action = action;
    return true;
}

/* What the card shows.  Title is always the event title; the body prefers the
 * summary and is empty when none was declared, so a producer that only sent a
 * title still gets a usable card.  Never returns NULL. */
static inline const char *event_ingest_display_title(const event_ingest_fields_t *fields) {
    if (!fields || !fields->title) return "";
    return fields->title;
}

static inline const char *event_ingest_display_body(const event_ingest_fields_t *fields) {
    if (!fields || !fields->summary) return "";
    return fields->summary;
}

/*
 * Whether this action must wait for the current turn to end.
 *
 * The matrix already encodes *whether* an event may preempt (`INTERRUPT_*`).
 * What is left is what to do with everything else while the device is busy:
 * `QUEUE_SPEAK` literally means "announce once the current turn is over", and
 * showing a card mid-reply would replace what the user is reading.  So while
 * busy, only the preempting actions and DROP proceed; the rest defer and the
 * dispatcher keeps the message pending for a later poll.
 *
 * Deferring is the honest answer for "cannot present yet" -- dropping it would
 * lose an event the user was meant to see, and presenting it would destroy
 * whatever the current turn put on screen.
 */
static inline bool event_ingest_defers_while_busy(event_action_t action, bool device_busy) {
    if (!device_busy) return false;
    if (action == EVENT_ACTION_DROP) return false; /* consumed; nothing to show */
    if (event_action_preempts_playback(action)) return false;
    if (event_action_preempts_capture(action)) return false;
    return true;
}

/*
 * Session-scoped idempotency ring (section 4.1: the same `dedupeKey` is
 * presented once).
 *
 * Bounded and non-persistent on purpose.  The ack path already stops the Hub
 * from redelivering a message, so this only has to catch a duplicate that
 * arrives within one boot -- a retry race, or the same logical event produced
 * twice.  Surviving a reboot is a different question with a different owner:
 * a persist=true event is *meant* to reappear after a reboot, so a persistent
 * ring would suppress exactly the replay the contract requires.  Cross-boot
 * bookkeeping belongs with N1-6's snapshot lifecycle, not here.
 */
#define EVENT_INGEST_DEDUPE_CAPACITY 8

typedef struct {
    char keys[EVENT_INGEST_DEDUPE_CAPACITY][EVENT_INGEST_MAX_DEDUPE_KEY_LEN + 1];
    int count; /* valid entries, <= EVENT_INGEST_DEDUPE_CAPACITY */
    int next;  /* ring cursor */
} event_ingest_dedupe_t;

static inline void event_ingest_dedupe_reset(event_ingest_dedupe_t *ring) {
    if (!ring) return;
    memset(ring, 0, sizeof(*ring));
}

/*
 * Returns true when `key` was already recorded (the caller suppresses the
 * event).  Otherwise records it and returns false.  An empty or over-long key
 * is never recorded and never matches: an empty key would suppress everything
 * after the first event, and a truncated key could collide two real events
 * into one.
 */
static inline bool event_ingest_dedupe_seen(event_ingest_dedupe_t *ring, const char *key) {
    if (!ring || !key || key[0] == '\0') return false;
    const size_t len = strlen(key);
    if (len > EVENT_INGEST_MAX_DEDUPE_KEY_LEN) return false;
    for (int i = 0; i < ring->count; ++i) {
        if (strcmp(ring->keys[i], key) == 0) return true;
    }
    const int slot = ring->next;
    memcpy(ring->keys[slot], key, len + 1);
    ring->next = (slot + 1) % EVENT_INGEST_DEDUPE_CAPACITY;
    if (ring->count < EVENT_INGEST_DEDUPE_CAPACITY) {
        ring->count += 1;
    }
    return false;
}

