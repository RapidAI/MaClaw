#include "device_profile_validation.h"

static bool device_profile_primary_source_is_valid(device_input_source_t source) {
    return source == DEVICE_INPUT_SOURCE_TOUCH ||
           source == DEVICE_INPUT_SOURCE_PRIMARY_CONTROL;
}

bool device_profile_input_source_is_wake_eligible(device_input_source_t source) {
    return source == DEVICE_INPUT_SOURCE_TOUCH ||
           source == DEVICE_INPUT_SOURCE_PRIMARY_CONTROL ||
           source == DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL;
}

bool device_input_action_is_valid(device_input_action_t action) {
    return action >= DEVICE_INPUT_PRIMARY && action <= DEVICE_INPUT_CONTACT_DOWN;
}

bool device_input_source_is_valid(device_input_source_t source) {
    return source == DEVICE_INPUT_SOURCE_TOUCH ||
           source == DEVICE_INPUT_SOURCE_PRIMARY_CONTROL ||
           source == DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL;
}

bool device_input_action_source_is_valid(device_input_action_t action,
                                         device_input_source_t source) {
    if (!device_input_action_is_valid(action) ||
        !device_input_source_is_valid(source)) {
        return false;
    }
    /* Contact-down is the sole raw physical edge shared policy consumes. It
     * may only originate from a touch surface; key scanners publish completed
     * normalized gestures instead of an ambiguous press edge. */
    return action != DEVICE_INPUT_CONTACT_DOWN || source == DEVICE_INPUT_SOURCE_TOUCH;
}

bool device_profile_is_valid(const device_profile_t *profile) {
    if (!profile || profile->struct_size != sizeof(*profile) ||
        profile->abi_version != DEVICE_PROFILE_ABI_VERSION ||
        !profile->id || !profile->id[0] ||
        !profile->primary_interaction_label || !profile->primary_interaction_label[0] ||
        !profile->volume_interaction_hint || !profile->volume_interaction_hint[0] ||
        profile->display_width == 0 || profile->display_height == 0 ||
        (profile->capabilities & ~DEVICE_CAPABILITY_KNOWN_MASK) != 0 ||
        (profile->capabilities & DEVICE_CAPABILITY_REQUIRED_BASELINE) !=
            DEVICE_CAPABILITY_REQUIRED_BASELINE) {
        return false;
    }

    if (!device_profile_primary_source_is_valid(profile->primary_interaction_source)) {
        return false;
    }
    if (profile->primary_interaction_source == DEVICE_INPUT_SOURCE_TOUCH &&
        (profile->capabilities & DEVICE_CAPABILITY_TOUCH_INPUT) == 0) {
        return false;
    }

    const device_input_source_flags_t valid_wake_sources =
        profile->display_wake_sources & DEVICE_INPUT_SOURCE_WAKE_MASK;
    if (valid_wake_sources == 0 ||
        valid_wake_sources != profile->display_wake_sources ||
        (valid_wake_sources &
         DEVICE_INPUT_SOURCE_FLAG(profile->primary_interaction_source)) == 0) {
        return false;
    }

    /* The public viewport is what shared scenes target. A round renderer
     * cannot describe a rectangular logical safe area as an ordinary panel. */
    if ((profile->capabilities & DEVICE_CAPABILITY_ROUND_DISPLAY) != 0 &&
        profile->display_width != profile->display_height) {
        return false;
    }
    return true;
}
