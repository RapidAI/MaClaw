#include <stdio.h>
#include <string.h>

#include "board_profile.h"
#include "device_api.h"

#ifndef EXPECTED_WIDTH
#error "EXPECTED_WIDTH must be defined"
#endif
#ifndef EXPECTED_HEIGHT
#error "EXPECTED_HEIGHT must be defined"
#endif
#ifndef EXPECTED_CAPABILITIES
#error "EXPECTED_CAPABILITIES must be defined"
#endif
#ifndef EXPECTED_PRIMARY_SOURCE
#error "EXPECTED_PRIMARY_SOURCE must be defined"
#endif
#ifndef EXPECTED_WAKE_SOURCES
#error "EXPECTED_WAKE_SOURCES must be defined"
#endif

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

int main(void) {
    device_profile_t profile = {0};
    CHECK(board_profile_get(NULL) == false);
    CHECK(board_profile_get(&profile));
    CHECK(device_profile_is_valid(&profile));
    CHECK(strcmp(profile.id, EXPECTED_PROFILE_ID_TEXT) == 0);
    CHECK(profile.display_width == EXPECTED_WIDTH);
    CHECK(profile.display_height == EXPECTED_HEIGHT);
    CHECK(profile.capabilities == (device_capability_flags_t)(EXPECTED_CAPABILITIES));
    CHECK(profile.primary_interaction_source == (device_input_source_t)(EXPECTED_PRIMARY_SOURCE));
    CHECK(profile.display_wake_sources ==
          (device_input_source_flags_t)(EXPECTED_WAKE_SOURCES));
    CHECK((profile.capabilities & DEVICE_CAPABILITY_REQUIRED_BASELINE) ==
          DEVICE_CAPABILITY_REQUIRED_BASELINE);
    CHECK((profile.display_wake_sources &
           DEVICE_INPUT_SOURCE_FLAG(profile.primary_interaction_source)) != 0);
    if ((profile.capabilities & DEVICE_CAPABILITY_ROUND_DISPLAY) != 0) {
        CHECK(profile.display_width == profile.display_height);
    }
    if (profile.primary_interaction_source == DEVICE_INPUT_SOURCE_TOUCH) {
        CHECK((profile.capabilities & DEVICE_CAPABILITY_TOUCH_INPUT) != 0);
    }
    puts("PASS official Device profile matches its declared HAL facts");
    return 0;
}
