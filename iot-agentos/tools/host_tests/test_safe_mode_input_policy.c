#include <stdio.h>

#include "presentation/safe_mode_input_policy.h"

static int s_failures;

static void expect_route(const char *name,
                         bool alarm_initialized,
                         bool alarm_ringing,
                         bool primary,
                         bool safe_mode,
                         safe_mode_input_route_t expected) {
    const safe_mode_input_route_t actual = safe_mode_input_policy_route(
        alarm_initialized, alarm_ringing, primary, safe_mode);
    if (actual != expected) {
        fprintf(stderr, "FAIL %s: expected %d got %d\n", name, (int)expected, (int)actual);
        ++s_failures;
    } else {
        printf("PASS %s\n", name);
    }
}

int main(void) {
    /* Normal operation must not change established ordinary-input routing. */
    expect_route("normal ordinary input continues", false, false, true, false,
                 SAFE_MODE_INPUT_ROUTE_CONTINUE);

    /* An alarm is a retained local safety function even while SAFE_MODE is
     * active. Primary touch/key dismisses it before ordinary admission. */
    expect_route("safe mode primary input dismisses ringing alarm", true, true, true, true,
                 SAFE_MODE_INPUT_ROUTE_DISMISS_ALARM);
    expect_route("normal primary input dismisses ringing alarm", true, true, true, false,
                 SAFE_MODE_INPUT_ROUTE_DISMISS_ALARM);
    expect_route("non-primary input cannot dismiss ringing alarm", true, true, false, true,
                 SAFE_MODE_INPUT_ROUTE_IGNORE_RINGING_ALARM);

    /* With no ringing local alarm, every ordinary source/action remains
     * blocked in SAFE_MODE; Input Binding consults this result before its
     * fall-prompt and command-capture-stop foreground policies, so callers
     * cannot fall through to voice, meeting, pairing, provisioning, or
     * configuration policy. */
    expect_route("safe mode blocks primary ordinary input", true, false, true, true,
                 SAFE_MODE_INPUT_ROUTE_IGNORE_SAFE_MODE);
    expect_route("safe mode blocks secondary ordinary input", true, false, false, true,
                 SAFE_MODE_INPUT_ROUTE_IGNORE_SAFE_MODE);
    expect_route("safe mode blocks input before alarm initialization", false, false, true, true,
                 SAFE_MODE_INPUT_ROUTE_IGNORE_SAFE_MODE);

    return s_failures == 0 ? 0 : 1;
}
