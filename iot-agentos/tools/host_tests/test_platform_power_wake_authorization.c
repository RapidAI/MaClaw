#include <stdio.h>

#include "platform_power.h"
#include "platform_power_profile.h"
#include "platform_wake_profile.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static platform_wake_profile_matrix_t s_matrix;
static unsigned s_prepare_calls;
static unsigned s_abort_calls;
static unsigned s_commit_calls;
static unsigned s_resume_calls;

bool platform_wake_profile_get_matrix(platform_wake_profile_matrix_t *out_matrix) {
    if (!out_matrix) return false;
    *out_matrix = s_matrix;
    return true;
}

device_status_t display_service_enter_display_off(void) {
    return DEVICE_STATUS_OK;
}
device_status_t display_service_wake_display(void) {
    return DEVICE_STATUS_OK;
}
bool display_service_display_is_off(void) {
    return false;
}

bool platform_power_profile_get_telemetry(uint8_t *out_level_percent,
                                          bool *out_charging) {
    (void)out_level_percent;
    (void)out_charging;
    return false;
}
device_status_t platform_power_profile_prepare_verified_sleep(
    device_power_state_t target_state,
    device_wake_source_flags_t verified_sources,
    uint32_t timeout_ms) {
    CHECK(target_state == DEVICE_POWER_STATE_LIGHT_SLEEP);
    CHECK(verified_sources == DEVICE_WAKE_SOURCE_TIMER);
    CHECK(timeout_ms != 0);
    ++s_prepare_calls;
    return DEVICE_STATUS_UNAVAILABLE;
}
device_status_t platform_power_profile_abort_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms) {
    CHECK(target_state == DEVICE_POWER_STATE_LIGHT_SLEEP);
    CHECK(timeout_ms != 0);
    ++s_abort_calls;
    return DEVICE_STATUS_OK;
}
device_status_t platform_power_profile_commit_verified_sleep(
    device_power_state_t target_state,
    device_wake_source_flags_t verified_sources,
    uint32_t timeout_ms) {
    CHECK(target_state == DEVICE_POWER_STATE_LIGHT_SLEEP);
    CHECK(verified_sources == DEVICE_WAKE_SOURCE_TIMER);
    CHECK(timeout_ms != 0);
    ++s_commit_calls;
    return DEVICE_STATUS_UNAVAILABLE;
}
device_status_t platform_power_profile_resume_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms) {
    CHECK(target_state == DEVICE_POWER_STATE_LIGHT_SLEEP);
    CHECK(timeout_ms != 0);
    ++s_resume_calls;
    return DEVICE_STATUS_OK;
}

int main(void) {
    s_matrix = (platform_wake_profile_matrix_t){
        .verified_display_off_sources = DEVICE_WAKE_SOURCE_TOUCH,
        .light_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER |
                                         DEVICE_WAKE_SOURCE_PRIMARY_CONTROL,
        .deep_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER,
    };

    /* Light/Deep candidates have no effective authorization. The facade must
     * never pass a caller-provided candidate mask into the selected profile. */
    CHECK(platform_power_prepare_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, DEVICE_WAKE_SOURCE_TIMER, 10) ==
          DEVICE_STATUS_UNAVAILABLE);
    CHECK(s_prepare_calls == 0);
    CHECK(platform_power_commit_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, DEVICE_WAKE_SOURCE_TIMER, 10) ==
          DEVICE_STATUS_UNAVAILABLE);
    CHECK(s_commit_calls == 0);

    s_matrix.light_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER;
    /* A future verified source must still be a subset of the requested depth,
     * rather than any currently proved DISPLAY_OFF source. */
    CHECK(platform_power_prepare_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, DEVICE_WAKE_SOURCE_TOUCH, 10) ==
          DEVICE_STATUS_UNAVAILABLE);
    CHECK(s_prepare_calls == 0);

    CHECK(platform_power_prepare_verified_sleep(
              DEVICE_POWER_STATE_DISPLAY_OFF, DEVICE_WAKE_SOURCE_TOUCH, 10) ==
          DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(platform_power_prepare_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, 0, 10) ==
          DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(platform_power_prepare_verified_sleep(
              DEVICE_POWER_STATE_LIGHT_SLEEP, DEVICE_WAKE_SOURCE_TIMER | (1u << 31), 10) ==
          DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(s_prepare_calls == 0);

    /* Rollback/recovery remain reachable after a partial profile attempt. They
     * do not re-authorize a source, so a changed matrix cannot strand a
     * profile-private prepared state. */
    CHECK(platform_power_abort_verified_sleep(DEVICE_POWER_STATE_LIGHT_SLEEP, 10) ==
          DEVICE_STATUS_OK);
    CHECK(platform_power_resume_verified_sleep(DEVICE_POWER_STATE_LIGHT_SLEEP, 10) ==
          DEVICE_STATUS_OK);
    CHECK(s_abort_calls == 1);
    CHECK(s_resume_calls == 1);
    CHECK(platform_power_abort_verified_sleep(DEVICE_POWER_STATE_DISPLAY_OFF, 10) ==
          DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(platform_power_resume_verified_sleep(DEVICE_POWER_STATE_LIGHT_SLEEP, 0) ==
          DEVICE_STATUS_INVALID_ARGUMENT);

    puts("PASS Platform Power rejects unverified Wake candidates before profile entry");
    return 0;
}
