#include <stdio.h>

#include "device_api.h"
#include "platform_wake.h"
#include "platform_wake_profile.h"

#ifndef EXPECTED_DISPLAY_OFF_SOURCES
#error "EXPECTED_DISPLAY_OFF_SOURCES must be supplied by the profile test"
#endif
#ifndef EXPECTED_LIGHT_SLEEP_CANDIDATES
#error "EXPECTED_LIGHT_SLEEP_CANDIDATES must be supplied by the profile test"
#endif
#ifndef EXPECTED_DEEP_SLEEP_CANDIDATES
#error "EXPECTED_DEEP_SLEEP_CANDIDATES must be supplied by the profile test"
#endif

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static int check_depth(device_power_state_t target_state,
                       device_wake_source_flags_t candidates,
                       device_wake_source_flags_t verified) {
    device_wake_depth_capability_t capability = {0};
    CHECK(platform_wake_get_depth_capability(target_state, &capability) ==
          DEVICE_STATUS_OK);
    CHECK(capability.struct_size == sizeof(capability));
    CHECK(capability.abi_version == DEVICE_WAKE_CAPABILITY_ABI_VERSION);
    CHECK(capability.target_state == target_state);
    CHECK(capability.candidate_sources == candidates);
    CHECK(capability.verified_sources == verified);
    return 0;
}

int main(void) {
    CHECK(check_depth(DEVICE_POWER_STATE_DISPLAY_OFF,
                      EXPECTED_DISPLAY_OFF_SOURCES,
                      EXPECTED_DISPLAY_OFF_SOURCES) == 0);
    CHECK(check_depth(DEVICE_POWER_STATE_LIGHT_SLEEP,
                      EXPECTED_LIGHT_SLEEP_CANDIDATES, 0) == 0);
    CHECK(check_depth(DEVICE_POWER_STATE_DEEP_SLEEP,
                      EXPECTED_DEEP_SLEEP_CANDIDATES, 0) == 0);

    /* The physical-profile implementation must reject a null matrix output;
     * this is part of the private adapter contract, not a normal caller path. */
    CHECK(!platform_wake_profile_get_matrix(NULL));

    puts("PASS Wake profile matrix candidate/verified contract");
    return 0;
}
