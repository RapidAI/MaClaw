#include <stdio.h>
#include <string.h>

#include "device_api.h"
#include "device_profile_validation.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static device_profile_t valid_compact_profile(void) {
    return (device_profile_t){
        .struct_size = sizeof(device_profile_t),
        .abi_version = DEVICE_PROFILE_ABI_VERSION,
        .id = "host-profile",
        .display_width = 240,
        .display_height = 320,
        .capabilities = DEVICE_CAPABILITY_REQUIRED_BASELINE,
        .primary_interaction_source = DEVICE_INPUT_SOURCE_PRIMARY_CONTROL,
        .primary_interaction_label = "control",
        .volume_interaction_hint = "volume",
        .display_wake_sources =
            DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_PRIMARY_CONTROL),
    };
}

int main(void) {
    device_profile_t profile = valid_compact_profile();
    CHECK(device_profile_is_valid(&profile));

    profile.primary_interaction_source = DEVICE_INPUT_SOURCE_TOUCH;
    CHECK(!device_profile_is_valid(&profile));
    profile.capabilities |= DEVICE_CAPABILITY_TOUCH_INPUT;
    profile.display_wake_sources = DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_TOUCH);
    CHECK(device_profile_is_valid(&profile));

    profile.primary_interaction_source = DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL;
    CHECK(!device_profile_is_valid(&profile));

    profile = valid_compact_profile();
    profile.display_wake_sources =
        DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL);
    CHECK(!device_profile_is_valid(&profile));

    profile = valid_compact_profile();
    profile.display_wake_sources |= 0x80u;
    CHECK(!device_profile_is_valid(&profile));

    profile = valid_compact_profile();
    profile.capabilities |= DEVICE_CAPABILITY_ROUND_DISPLAY;
    CHECK(!device_profile_is_valid(&profile));
    profile.display_height = profile.display_width;
    CHECK(device_profile_is_valid(&profile));

    profile = valid_compact_profile();
    profile.capabilities |= DEVICE_CAPABILITY_TOUCH_INPUT;
    CHECK(device_profile_is_valid(&profile));

    CHECK(device_profile_input_source_is_wake_eligible(DEVICE_INPUT_SOURCE_TOUCH));
    CHECK(device_profile_input_source_is_wake_eligible(
        DEVICE_INPUT_SOURCE_PRIMARY_CONTROL));
    CHECK(device_profile_input_source_is_wake_eligible(
        DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL));
    CHECK(!device_profile_input_source_is_wake_eligible(DEVICE_INPUT_SOURCE_UNKNOWN));

    CHECK(device_input_action_is_valid(DEVICE_INPUT_PRIMARY));
    CHECK(device_input_action_is_valid(DEVICE_INPUT_CONTACT_DOWN));
    CHECK(!device_input_action_is_valid((device_input_action_t)99));
    CHECK(device_input_source_is_valid(DEVICE_INPUT_SOURCE_TOUCH));
    CHECK(!device_input_source_is_valid(DEVICE_INPUT_SOURCE_UNKNOWN));
    CHECK(!device_input_source_is_valid((device_input_source_t)99));
    CHECK(device_input_action_source_is_valid(
        DEVICE_INPUT_PRIMARY, DEVICE_INPUT_SOURCE_PRIMARY_CONTROL));
    CHECK(device_input_action_source_is_valid(
        DEVICE_INPUT_VOLUME_UP, DEVICE_INPUT_SOURCE_TOUCH));
    CHECK(device_input_action_source_is_valid(
        DEVICE_INPUT_CONTACT_DOWN, DEVICE_INPUT_SOURCE_TOUCH));
    CHECK(!device_input_action_source_is_valid(
        DEVICE_INPUT_CONTACT_DOWN, DEVICE_INPUT_SOURCE_PRIMARY_CONTROL));
    CHECK(!device_input_action_source_is_valid(
        (device_input_action_t)99, DEVICE_INPUT_SOURCE_TOUCH));
    CHECK(!device_input_action_source_is_valid(
        DEVICE_INPUT_PRIMARY, DEVICE_INPUT_SOURCE_UNKNOWN));

    puts("PASS Device profile validates shared input/display capability facts");
    return 0;
}
