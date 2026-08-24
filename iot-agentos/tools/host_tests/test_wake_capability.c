#include <stdio.h>
#include <string.h>

#include "device_api.h"
#include "platform_wake.h"
#include "platform_wake_profile.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static platform_wake_profile_matrix_t s_matrix;
static bool s_profile_available = true;

bool platform_wake_profile_get_matrix(platform_wake_profile_matrix_t *out_matrix) {
    if (!s_profile_available || !out_matrix) return false;
    *out_matrix = s_matrix;
    return true;
}

static int check_depth(device_power_state_t depth,
                       device_wake_source_flags_t candidate,
                       device_wake_source_flags_t verified) {
    device_wake_depth_capability_t actual = {0};
    CHECK(platform_wake_get_depth_capability(depth, &actual) == DEVICE_STATUS_OK);
    CHECK(actual.struct_size == sizeof(actual));
    CHECK(actual.abi_version == DEVICE_WAKE_CAPABILITY_ABI_VERSION);
    CHECK(actual.target_state == depth);
    CHECK(actual.candidate_sources == candidate);
    CHECK(actual.verified_sources == verified);
    return 0;
}

int main(void) {
    s_matrix = (platform_wake_profile_matrix_t){
        .verified_display_off_sources = DEVICE_WAKE_SOURCE_TOUCH |
                                        DEVICE_WAKE_SOURCE_PRIMARY_CONTROL,
        .light_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER |
                                         DEVICE_WAKE_SOURCE_TOUCH,
        .deep_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER |
                                        DEVICE_WAKE_SOURCE_AUXILIARY_CONTROL,
    };

    CHECK(check_depth(DEVICE_POWER_STATE_DISPLAY_OFF,
                      DEVICE_WAKE_SOURCE_TOUCH | DEVICE_WAKE_SOURCE_PRIMARY_CONTROL,
                      DEVICE_WAKE_SOURCE_TOUCH | DEVICE_WAKE_SOURCE_PRIMARY_CONTROL) == 0);
    /* Candidate entries serve engineering/HIL planning only. They must never
     * become effective verified authorization merely by appearing in a profile
     * table. */
    CHECK(check_depth(DEVICE_POWER_STATE_LIGHT_SLEEP,
                      DEVICE_WAKE_SOURCE_TIMER | DEVICE_WAKE_SOURCE_TOUCH, 0) == 0);
    CHECK(check_depth(DEVICE_POWER_STATE_DEEP_SLEEP,
                      DEVICE_WAKE_SOURCE_TIMER | DEVICE_WAKE_SOURCE_AUXILIARY_CONTROL, 0) == 0);

    device_wake_depth_capability_t result = {0};
    CHECK(platform_wake_get_depth_capability(DEVICE_POWER_STATE_ACTIVE, &result) ==
          DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(platform_wake_get_depth_capability(DEVICE_POWER_STATE_MODEM_SLEEP, &result) ==
          DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(platform_wake_get_depth_capability(DEVICE_POWER_STATE_LIGHT_SLEEP, NULL) ==
          DEVICE_STATUS_INVALID_ARGUMENT);

    s_matrix.verified_display_off_sources = 0;
    CHECK(platform_wake_get_depth_capability(DEVICE_POWER_STATE_DISPLAY_OFF, &result) ==
          DEVICE_STATUS_UNAVAILABLE);
    s_matrix.verified_display_off_sources = DEVICE_WAKE_SOURCE_TOUCH;
    s_matrix.light_sleep_candidate_sources = DEVICE_WAKE_SOURCE_KNOWN_MASK | (1u << 31);
    CHECK(platform_wake_get_depth_capability(DEVICE_POWER_STATE_LIGHT_SLEEP, &result) ==
          DEVICE_STATUS_UNAVAILABLE);
    s_profile_available = false;
    CHECK(platform_wake_get_depth_capability(DEVICE_POWER_STATE_DISPLAY_OFF, &result) ==
          DEVICE_STATUS_UNAVAILABLE);

    puts("PASS wake capability candidate/verified separation");
    return 0;
}
