/*
 * Host test of the event presentation policy (plan N1-2).
 *
 * Compiles the public header only: the matrix, the state precedence and the
 * speech-text rule are inline and have no ESP-IDF dependency, so the whole
 * admission contract is exercised here without a device.
 *
 * The expected matrix below is written out a second time on purpose.  A test
 * that recomputed the answer the same way the header does would pass even if
 * both were wrong; restating section 4.2 by hand means an edit to the header table
 * has to be justified against the plan, not merely against itself.
 */

#include <stdio.h>
#include <string.h>

#include "services/event_presentation_policy.h"

static int fail(const char *msg) {
    fprintf(stderr, "FAIL: %s\n", msg);
    return 1;
}

/* section 4.2, transcribed by hand: [severity][state]. */
static const event_action_t kExpected[EVENT_SEVERITY_COUNT][EVENT_STATE_COUNT] = {
    /* silent */
    {
        EVENT_ACTION_DISPLAY,  /* standby */
        EVENT_ACTION_DISPLAY,  /* speaking */
        EVENT_ACTION_DISPLAY,  /* listening */
        EVENT_ACTION_DROP,     /* quiet hours */
        EVENT_ACTION_DISPLAY,  /* meeting */
    },
    /* soft */
    {
        EVENT_ACTION_SPEAK,       /* standby */
        EVENT_ACTION_QUEUE_SPEAK, /* speaking */
        EVENT_ACTION_QUEUE_SPEAK, /* listening */
        EVENT_ACTION_DROP,        /* quiet hours */
        EVENT_ACTION_DISPLAY,     /* meeting */
    },
    /* interrupt */
    {
        EVENT_ACTION_SPEAK,            /* standby */
        EVENT_ACTION_INTERRUPT_SPEAK,  /* speaking */
        EVENT_ACTION_INTERRUPT_LISTEN, /* listening */
        EVENT_ACTION_DISPLAY_AND_BUZZ, /* quiet hours */
        EVENT_ACTION_DISPLAY,          /* meeting */
    },
};

static int test_full_admission_matrix(void) {
    for (int sev = 0; sev < (int)EVENT_SEVERITY_COUNT; ++sev) {
        for (int st = 0; st < (int)EVENT_STATE_COUNT; ++st) {
            const event_action_t got =
                event_presentation_decide((event_severity_t)sev, (event_device_state_t)st);
            if (got != kExpected[sev][st]) {
                fprintf(stderr, "FAIL: matrix[%s][%s] = %s, expected %s\n",
                        event_severity_name((event_severity_t)sev),
                        event_device_state_name((event_device_state_t)st),
                        event_action_name(got), event_action_name(kExpected[sev][st]));
                return 1;
            }
        }
    }
    return 0;
}

/* Hard rule 1: quiet hours must never be broken, not even by `interrupt`. */
static int test_quiet_hours_never_breaks(void) {
    if (event_presentation_decide(EVENT_SEVERITY_SILENT, EVENT_STATE_QUIET_HOURS) !=
        EVENT_ACTION_DROP) {
        return fail("silent during quiet hours must be dropped, not shown");
    }
    if (event_presentation_decide(EVENT_SEVERITY_SOFT, EVENT_STATE_QUIET_HOURS) !=
        EVENT_ACTION_DROP) {
        return fail("soft during quiet hours must be dropped, not spoken");
    }
    const event_action_t interrupt =
        event_presentation_decide(EVENT_SEVERITY_INTERRUPT, EVENT_STATE_QUIET_HOURS);
    if (interrupt != EVENT_ACTION_DISPLAY_AND_BUZZ) {
        return fail("interrupt during quiet hours must downgrade to display+buzz");
    }
    if (event_action_is_audible(interrupt)) {
        return fail("the quiet-hours downgrade must not be audible");
    }
    if (!event_action_should_display(interrupt) || !event_action_should_buzz(interrupt)) {
        return fail("the quiet-hours downgrade must still display and buzz");
    }
    return 0;
}

/* Hard rule 2: never disturb a recording - and never buzz into one either. */
static int test_meeting_never_breaks(void) {
    const event_severity_t all[] = {
        EVENT_SEVERITY_SILENT, EVENT_SEVERITY_SOFT, EVENT_SEVERITY_INTERRUPT,
    };
    for (unsigned i = 0; i < sizeof(all) / sizeof(all[0]); ++i) {
        const event_action_t action =
            event_presentation_decide(all[i], EVENT_STATE_MEETING_RECORDING);
        if (action != EVENT_ACTION_DISPLAY) {
            fprintf(stderr, "FAIL: %s during a recording = %s, expected display\n",
                    event_severity_name(all[i]), event_action_name(action));
            return 1;
        }
        if (event_action_is_audible(action) || event_action_should_buzz(action)) {
            return fail("a recording must not be disturbed by sound or vibration");
        }
    }
    return 0;
}

static int test_state_precedence(void) {
    /* Meeting outranks quiet hours: a buzz is audible on the recording. */
    event_device_state_flags_t flags = {0};
    flags.meeting_recording = true;
    flags.quiet_hours = true;
    if (event_device_state_dominant(&flags) != EVENT_STATE_MEETING_RECORDING) {
        return fail("meeting must outrank quiet hours");
    }

    /* Quiet hours outrank activity: intent beats "something is playing". */
    flags = (event_device_state_flags_t){0};
    flags.quiet_hours = true;
    flags.speaking = true;
    flags.listening = true;
    if (event_device_state_dominant(&flags) != EVENT_STATE_QUIET_HOURS) {
        return fail("quiet hours must outrank speaking/listening");
    }

    /* Listening outranks speaking: capture holds words that cannot be redone. */
    flags = (event_device_state_flags_t){0};
    flags.speaking = true;
    flags.listening = true;
    if (event_device_state_dominant(&flags) != EVENT_STATE_LISTENING) {
        return fail("listening must outrank speaking");
    }

    flags = (event_device_state_flags_t){0};
    flags.speaking = true;
    if (event_device_state_dominant(&flags) != EVENT_STATE_SPEAKING) {
        return fail("speaking must be reported when it is the only activity");
    }

    flags = (event_device_state_flags_t){0};
    if (event_device_state_dominant(&flags) != EVENT_STATE_STANDBY) {
        return fail("no activity must resolve to standby");
    }
    if (event_device_state_dominant(NULL) != EVENT_STATE_STANDBY) {
        return fail("a missing flag block must resolve to standby");
    }
    return 0;
}

static int test_decide_from_flags(void) {
    event_device_state_flags_t flags = {0};
    flags.quiet_hours = true;
    flags.speaking = true;
    /* The busy state must not rescue an event out of the quiet window. */
    if (event_presentation_decide_from_flags(EVENT_SEVERITY_SOFT, &flags) !=
        EVENT_ACTION_DROP) {
        return fail("decide_from_flags must apply precedence before the matrix");
    }
    flags = (event_device_state_flags_t){0};
    flags.listening = true;
    if (event_presentation_decide_from_flags(EVENT_SEVERITY_INTERRUPT, &flags) !=
        EVENT_ACTION_INTERRUPT_LISTEN) {
        return fail("decide_from_flags must reach the listening column");
    }
    return 0;
}

static int test_severity_parse_is_a_closed_set(void) {
    event_severity_t parsed = EVENT_SEVERITY_COUNT;

    if (!event_severity_parse("silent", &parsed) || parsed != EVENT_SEVERITY_SILENT) {
        return fail("`silent` must parse");
    }
    if (!event_severity_parse("soft", &parsed) || parsed != EVENT_SEVERITY_SOFT) {
        return fail("`soft` must parse");
    }
    if (!event_severity_parse("interrupt", &parsed) ||
        parsed != EVENT_SEVERITY_INTERRUPT) {
        return fail("`interrupt` must parse");
    }

    /* The Hub prompt once said `notice`; an unknown value must be rejected,
     * never quietly treated as a known one. */
    const char *const rejected[] = {"notice", "SOFT", "soft ", "", "urgent"};
    for (unsigned i = 0; i < sizeof(rejected) / sizeof(rejected[0]); ++i) {
        parsed = EVENT_SEVERITY_COUNT;
        if (event_severity_parse(rejected[i], &parsed)) {
            fprintf(stderr, "FAIL: %s must be rejected\n", rejected[i]);
            return 1;
        }
        if (parsed != EVENT_SEVERITY_COUNT) {
            return fail("a rejected severity must not write through the out param");
        }
    }
    if (event_severity_parse(NULL, &parsed)) {
        return fail("NULL must be rejected");
    }
    /* The out param is optional. */
    if (!event_severity_parse("soft", NULL)) {
        return fail("parsing without an out param must still succeed");
    }
    return 0;
}

static int test_speech_text_prefers_summary(void) {
    if (strcmp(event_presentation_speech_text("title", "summary"), "summary") != 0) {
        return fail("summary must win over title");
    }
    if (strcmp(event_presentation_speech_text("title", ""), "title") != 0) {
        return fail("an empty summary must fall back to title");
    }
    if (strcmp(event_presentation_speech_text("title", NULL), "title") != 0) {
        return fail("a missing summary must fall back to title");
    }
    if (strcmp(event_presentation_speech_text("", ""), "") != 0) {
        return fail("empty text must produce an empty string");
    }
    if (strcmp(event_presentation_speech_text(NULL, NULL), "") != 0) {
        return fail("missing text must produce an empty string, never NULL");
    }
    return 0;
}

static int test_action_predicates_partition_the_actions(void) {
    int audible = 0;
    int buzz = 0;
    int display = 0;
    for (int a = 0; a < (int)EVENT_ACTION_COUNT; ++a) {
        const event_action_t action = (event_action_t)a;
        if (event_action_is_audible(action)) ++audible;
        if (event_action_should_buzz(action)) ++buzz;
        if (event_action_should_display(action)) ++display;
        /* Preemption is a strict subset of audibility. */
        if ((event_action_preempts_playback(action) ||
             event_action_preempts_capture(action)) &&
            !event_action_is_audible(action)) {
            return fail("a preempting action must also be audible");
        }
    }
    if (audible != 4) return fail("exactly four actions are audible");
    if (buzz != 1) return fail("exactly one action buzzes");
    if (display != (int)EVENT_ACTION_COUNT - 1) {
        return fail("every action except drop must display something");
    }
    if (event_action_should_display(EVENT_ACTION_DROP)) {
        return fail("drop must not display");
    }
    /* `interrupt-listen` is the one action whose audibility is easy to miss:
     * it stops the capture in order to speak, so it must count as audible. */
    if (!event_action_is_audible(EVENT_ACTION_INTERRUPT_LISTEN)) {
        return fail("interrupt-listen must be audible: it announces after preempting");
    }
    if (!event_action_preempts_capture(EVENT_ACTION_INTERRUPT_LISTEN) ||
        event_action_preempts_playback(EVENT_ACTION_INTERRUPT_LISTEN)) {
        return fail("interrupt-listen must preempt capture and only capture");
    }
    if (!event_action_preempts_playback(EVENT_ACTION_INTERRUPT_SPEAK) ||
        event_action_preempts_capture(EVENT_ACTION_INTERRUPT_SPEAK)) {
        return fail("interrupt-speak must preempt playback and only playback");
    }
    return 0;
}

static int test_out_of_range_inputs_degrade_safely(void) {
    /* An unrecognised state must never be the reason the device starts
     * talking, so it degrades to display-only rather than to interrupt. */
    if (event_presentation_decide(EVENT_SEVERITY_INTERRUPT, EVENT_STATE_COUNT) !=
        EVENT_ACTION_DISPLAY) {
        return fail("an unknown state must degrade to display-only");
    }
    /* An unrecognised severity is dropped, never promoted. */
    if (event_presentation_decide(EVENT_SEVERITY_COUNT, EVENT_STATE_STANDBY) !=
        EVENT_ACTION_DROP) {
        return fail("an unknown severity must be dropped");
    }
    if (strcmp(event_severity_name(EVENT_SEVERITY_COUNT), "?") != 0) {
        return fail("an unknown severity must not borrow a name");
    }
    if (strcmp(event_action_name(EVENT_ACTION_COUNT), "?") != 0) {
        return fail("an unknown action must not borrow a name");
    }
    if (strcmp(event_device_state_name(EVENT_STATE_COUNT), "?") != 0) {
        return fail("an unknown state must not borrow a name");
    }
    return 0;
}

/* `silent` is screen-only everywhere it is shown at all: that is its whole
 * contract, and it is what makes it safe for chatty producers. */
static int test_silent_is_never_audible(void) {
    for (int st = 0; st < (int)EVENT_STATE_COUNT; ++st) {
        const event_action_t action =
            event_presentation_decide(EVENT_SEVERITY_SILENT, (event_device_state_t)st);
        if (event_action_is_audible(action)) {
            fprintf(stderr, "FAIL: silent in %s became %s\n",
                    event_device_state_name((event_device_state_t)st),
                    event_action_name(action));
            return 1;
        }
    }
    return 0;
}

int main(void) {
    if (test_full_admission_matrix()) return 1;
    if (test_quiet_hours_never_breaks()) return 1;
    if (test_meeting_never_breaks()) return 1;
    if (test_state_precedence()) return 1;
    if (test_decide_from_flags()) return 1;
    if (test_severity_parse_is_a_closed_set()) return 1;
    if (test_speech_text_prefers_summary()) return 1;
    if (test_action_predicates_partition_the_actions()) return 1;
    if (test_out_of_range_inputs_degrade_safely()) return 1;
    if (test_silent_is_never_audible()) return 1;
    printf("event presentation policy: 10 groups passed\n");
    return 0;
}
