#include "platform_wake_profile.h"

bool platform_wake_profile_get_matrix(platform_wake_profile_matrix_t *out_matrix) {
    if (!out_matrix) return false;
    *out_matrix = (platform_wake_profile_matrix_t){
        .verified_display_off_sources = DEVICE_WAKE_SOURCE_PRIMARY_CONTROL,
        /* GPIO0 is a boot strapping pin.  These entries are candidates for a
         * future profile-local electrical self-check/HIL gate, never current
         * Light/Deep Sleep support. */
        .light_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER |
                                         DEVICE_WAKE_SOURCE_PRIMARY_CONTROL,
        .deep_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER |
                                        DEVICE_WAKE_SOURCE_PRIMARY_CONTROL,
    };
    return true;
}
