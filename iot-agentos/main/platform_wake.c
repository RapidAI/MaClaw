#include "platform_wake.h"

#include <string.h>

#include "platform_wake_profile.h"

static bool matrix_is_valid(const platform_wake_profile_matrix_t *matrix) {
    if (!matrix || matrix->verified_display_off_sources == 0) return false;
    const device_wake_source_flags_t all_sources =
        matrix->verified_display_off_sources |
        matrix->light_sleep_candidate_sources |
        matrix->deep_sleep_candidate_sources;
    return (all_sources & ~DEVICE_WAKE_SOURCE_KNOWN_MASK) == 0;
}

device_status_t platform_wake_get_depth_capability(
    device_power_state_t target_state,
    device_wake_depth_capability_t *out_capability) {
    if (!out_capability) return DEVICE_STATUS_INVALID_ARGUMENT;

    platform_wake_profile_matrix_t matrix = {0};
    if (!platform_wake_profile_get_matrix(&matrix) || !matrix_is_valid(&matrix)) {
        return DEVICE_STATUS_UNAVAILABLE;
    }

    device_wake_depth_capability_t result = {
        .struct_size = sizeof(result),
        .abi_version = DEVICE_WAKE_CAPABILITY_ABI_VERSION,
        .target_state = target_state,
    };
    switch (target_state) {
        case DEVICE_POWER_STATE_DISPLAY_OFF:
            result.candidate_sources = matrix.verified_display_off_sources;
            result.verified_sources = matrix.verified_display_off_sources;
            break;
        case DEVICE_POWER_STATE_LIGHT_SLEEP:
            result.candidate_sources = matrix.light_sleep_candidate_sources;
            break;
        case DEVICE_POWER_STATE_DEEP_SLEEP:
            result.candidate_sources = matrix.deep_sleep_candidate_sources;
            break;
        case DEVICE_POWER_STATE_ACTIVE:
        case DEVICE_POWER_STATE_MODEM_SLEEP:
        default:
            return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    *out_capability = result;
    return DEVICE_STATUS_OK;
}

device_status_t platform_wake_authorize_verified_sleep_sources(
    device_power_state_t target_state,
    device_wake_source_flags_t requested_sources) {
    if (requested_sources == 0 ||
        (requested_sources & ~DEVICE_WAKE_SOURCE_KNOWN_MASK) != 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    device_wake_depth_capability_t capability = {0};
    const device_status_t status = platform_wake_get_depth_capability(
        target_state, &capability);
    if (status != DEVICE_STATUS_OK) return status;
    if (capability.verified_sources == 0 ||
        (requested_sources & ~capability.verified_sources) != 0) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    return DEVICE_STATUS_OK;
}
