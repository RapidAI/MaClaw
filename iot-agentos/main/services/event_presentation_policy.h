#pragma once

/*
 * Event presentation policy (plan N1-2, admission matrix in section 4.2).
 *
 * Decides *what a pushed event should do* from two inputs: the event's
 * `severity` and what the device is currently doing.  The decision is a pure
 * value function, so the whole matrix - including the two hard rules that
 * exist to protect the user rather than the product - is exercised by the
 * host regression without a device, a clock or an audio backend.
 *
 * Why this is a separate module and not a branch inside the dispatcher:
 *   - the expensive mistakes here are *interrupting the user mid-sentence*
 *     and *waking them at 3am*.  Both are decisions, not plumbing, and both
 *     are invisible in a code review of a 1400-line dispatcher;
 *   - the device has no local TTS: audible delivery needs the server.  The
 *     policy therefore returns an abstract action (`SPEAK`, `QUEUE_SPEAK`,
 *     ...) that stays valid once N3-5 lands, instead of hard-coding today's
 *     "there is no audio path yet" into the admission rules.
 *
 * Severity is a closed set mirroring the wire contract (`corelib/im`):
 * unknown values must be rejected, never defaulted - a typo that silently
 * became `silent` would drop a high-risk approval on the floor.
 */

#include <stdbool.h>
#include <stddef.h>
#include <string.h>

/* Mirrors the wire enum.  `soft` is deliberately spelled the same as the
 * contract: an earlier revision of the Hub prompt said `notice`, and a
 * mismatched severity name is exactly the failure this closed set catches. */
typedef enum {
    EVENT_SEVERITY_SILENT = 0, /* screen only, never makes a sound */
    EVENT_SEVERITY_SOFT,       /* announce when idle, queue when busy */
    EVENT_SEVERITY_INTERRUPT,  /* may preempt playback or capture */
    EVENT_SEVERITY_COUNT
} event_severity_t;

/* What the caller should do.  Kept abstract: `SPEAK` means "this event is
 * allowed to be audible now", not "start the audio pipeline" - the device
 * has no TTS of its own, so the caller still has to obtain audio. */
typedef enum {
    EVENT_ACTION_DROP = 0,         /* discard silently; nothing on screen */
    EVENT_ACTION_DISPLAY,          /* show it, make no sound */
    EVENT_ACTION_DISPLAY_AND_BUZZ, /* show it and vibrate (quiet-hours downgrade) */
    EVENT_ACTION_SPEAK,            /* announce immediately */
    EVENT_ACTION_QUEUE_SPEAK,      /* announce once the current turn is over */
    EVENT_ACTION_INTERRUPT_SPEAK,  /* cut off the current playback, then announce */
    EVENT_ACTION_INTERRUPT_LISTEN, /* stop capturing, then announce */
    EVENT_ACTION_COUNT
} event_action_t;

/* The five columns of section 4.2.  They are exclusive by construction; the caller
 * observes independent flags and `event_device_state_dominant` resolves them
 * with a documented precedence. */
typedef enum {
    EVENT_STATE_STANDBY = 0,       /* nothing else is happening */
    EVENT_STATE_SPEAKING,          /* TTS/result playback in progress */
    EVENT_STATE_LISTENING,         /* capturing the user's turn */
    EVENT_STATE_QUIET_HOURS,       /* inside the sleep schedule window */
    EVENT_STATE_MEETING_RECORDING, /* meeting recorder is running */
    EVENT_STATE_COUNT
} event_device_state_t;

/* Raw observations, one per service the caller can query.  Flags rather than
 * a single state so the caller does not have to re-implement precedence. */
typedef struct {
    bool speaking;
    bool listening;
    bool quiet_hours;
    bool meeting_recording;
} event_device_state_flags_t;

static inline const char *event_severity_name(event_severity_t severity) {
    static const char *const names[EVENT_SEVERITY_COUNT] = {
        "silent", "soft", "interrupt",
    };
    if ((unsigned)severity >= (unsigned)EVENT_SEVERITY_COUNT) return "?";
    return names[(unsigned)severity];
}

static inline const char *event_action_name(event_action_t action) {
    static const char *const names[EVENT_ACTION_COUNT] = {
        "drop", "display", "display+buzz", "speak", "queue-speak",
        "interrupt-speak", "interrupt-listen",
    };
    if ((unsigned)action >= (unsigned)EVENT_ACTION_COUNT) return "?";
    return names[(unsigned)action];
}

static inline const char *event_device_state_name(event_device_state_t state) {
    static const char *const names[EVENT_STATE_COUNT] = {
        "standby", "speaking", "listening", "quiet-hours", "meeting",
    };
    if ((unsigned)state >= (unsigned)EVENT_STATE_COUNT) return "?";
    return names[(unsigned)state];
}

/* Closed-set parse.  Returns false for NULL/empty/unknown so the caller can
 * reject the whole event instead of guessing a severity for it. */
static inline bool event_severity_parse(const char *text, event_severity_t *out) {
    if (!text || text[0] == '\0') return false;
    if (strcmp(text, "silent") == 0) {
        if (out) *out = EVENT_SEVERITY_SILENT;
        return true;
    }
    if (strcmp(text, "soft") == 0) {
        if (out) *out = EVENT_SEVERITY_SOFT;
        return true;
    }
    if (strcmp(text, "interrupt") == 0) {
        if (out) *out = EVENT_SEVERITY_INTERRUPT;
        return true;
    }
    return false;
}

/*
 * Resolve independent observations into the one column that decides the row.
 *
 * Precedence, most protective first:
 *   1. meeting recording - the hard rule: never disturb a recording, not even
 *      for `interrupt`.  It outranks quiet hours because a buzz during a
 *      recording is audible on the recording.
 *   2. quiet hours - the other hard rule.  Beats both activity states because
 *      the user's intent (I am asleep) is stronger than "something is playing".
 *   3. listening - beats speaking because a capture in progress is holding the
 *      user's own words; cutting it off destroys input that cannot be redone,
 *      while cutting off our own playback only costs a re-listen.
 *   4. speaking.
 *   5. standby.
 */
static inline event_device_state_t event_device_state_dominant(
    const event_device_state_flags_t *flags) {
    if (!flags) return EVENT_STATE_STANDBY;
    if (flags->meeting_recording) return EVENT_STATE_MEETING_RECORDING;
    if (flags->quiet_hours) return EVENT_STATE_QUIET_HOURS;
    if (flags->listening) return EVENT_STATE_LISTENING;
    if (flags->speaking) return EVENT_STATE_SPEAKING;
    return EVENT_STATE_STANDBY;
}

/*
 * section 4.2 admission matrix, verbatim.  A table rather than a switch so the rows
 * can be read against the plan document side by side.
 *
 * Out-of-range inputs degrade to the least intrusive outcome rather than
 * being treated as `interrupt`: an unrecognised state must never be the
 * reason the device starts talking.
 */
static inline event_action_t event_presentation_decide(event_severity_t severity,
                                                       event_device_state_t state) {
    static const event_action_t matrix[EVENT_SEVERITY_COUNT][EVENT_STATE_COUNT] = {
        /* silent */
        {
            EVENT_ACTION_DISPLAY,          /* standby */
            EVENT_ACTION_DISPLAY,          /* speaking */
            EVENT_ACTION_DISPLAY,          /* listening */
            EVENT_ACTION_DROP,             /* quiet hours */
            EVENT_ACTION_DISPLAY,          /* meeting */
        },
        /* soft */
        {
            EVENT_ACTION_SPEAK,            /* standby */
            EVENT_ACTION_QUEUE_SPEAK,      /* speaking */
            EVENT_ACTION_QUEUE_SPEAK,      /* listening */
            EVENT_ACTION_DROP,             /* quiet hours */
            EVENT_ACTION_DISPLAY,          /* meeting */
        },
        /* interrupt */
        {
            EVENT_ACTION_SPEAK,            /* standby: nothing to preempt */
            EVENT_ACTION_INTERRUPT_SPEAK,  /* speaking */
            EVENT_ACTION_INTERRUPT_LISTEN, /* listening */
            EVENT_ACTION_DISPLAY_AND_BUZZ, /* quiet hours: downgrade, never break */
            EVENT_ACTION_DISPLAY,          /* meeting: never break */
        },
    };
    if ((unsigned)severity >= (unsigned)EVENT_SEVERITY_COUNT) return EVENT_ACTION_DROP;
    if ((unsigned)state >= (unsigned)EVENT_STATE_COUNT) return EVENT_ACTION_DISPLAY;
    return matrix[(unsigned)severity][(unsigned)state];
}

/* Convenience: observe flags -> decide in one call. */
static inline event_action_t event_presentation_decide_from_flags(
    event_severity_t severity, const event_device_state_flags_t *flags) {
    return event_presentation_decide(severity, event_device_state_dominant(flags));
}

/* ---- action predicates (keep callers from re-deriving them) ---- */

/* The action reaches the user's ears (once audio is available).
 * `interrupt-listen` belongs here: it stops the capture *in order to* speak,
 * so it is not a silent action. */
static inline bool event_action_is_audible(event_action_t action) {
    return action == EVENT_ACTION_SPEAK || action == EVENT_ACTION_QUEUE_SPEAK ||
           action == EVENT_ACTION_INTERRUPT_SPEAK ||
           action == EVENT_ACTION_INTERRUPT_LISTEN;
}

/* The action preempts our own playback. */
static inline bool event_action_preempts_playback(event_action_t action) {
    return action == EVENT_ACTION_INTERRUPT_SPEAK;
}

/* The action preempts the user's in-flight capture. */
static inline bool event_action_preempts_capture(event_action_t action) {
    return action == EVENT_ACTION_INTERRUPT_LISTEN;
}

/* Everything except DROP leaves something on screen - including the actions
 * that are audible, so a user who cannot hear still sees the event. */
static inline bool event_action_should_display(event_action_t action) {
    return action != EVENT_ACTION_DROP;
}

static inline bool event_action_should_buzz(event_action_t action) {
    return action == EVENT_ACTION_DISPLAY_AND_BUZZ;
}

/*
 * What to say: the plan fixes the order as `summary`, falling back to
 * `title` (section 4.1).  One field is chosen instead of concatenating both because
 * the Hub already writes `summary` as the spoken sentence; reading both would
 * say the same thing twice.
 *
 * Never returns NULL so the caller can pass the result straight to a
 * length-taking API.
 */
static inline const char *event_presentation_speech_text(const char *title,
                                                         const char *summary) {
    if (summary && summary[0] != '\0') return summary;
    if (title && title[0] != '\0') return title;
    return "";
}

/* ---- device-side glue (implemented in event_presentation_policy.c) ---- */

/*
 * Snapshot the live device state from the owning services (foreground
 * coordinator, sleep schedule, meeting recorder).  Lives in the .c so this
 * header keeps compiling on the host with no ESP-IDF in scope.
 */
void event_presentation_current_flags(event_device_state_flags_t *out_flags);
