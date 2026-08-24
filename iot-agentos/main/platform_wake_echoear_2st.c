#include "platform_wake_profile.h"

bool platform_wake_profile_get_matrix(platform_wake_profile_matrix_t *out_matrix) {
    if (!out_matrix) return false;
    *out_matrix = (platform_wake_profile_matrix_t){
        .verified_display_off_sources = DEVICE_WAKE_SOURCE_TOUCH |
                                        DEVICE_WAKE_SOURCE_AUXILIARY_CONTROL,
        /* The current touch path is polling based, so touch is intentionally
         * absent below. GPIO0 remains a strapping-pin candidate only. */
        .light_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER |
                                         DEVICE_WAKE_SOURCE_AUXILIARY_CONTROL,
        .deep_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER |
                                        DEVICE_WAKE_SOURCE_AUXILIARY_CONTROL,
    };
    return true;
}
