#include "platform_wake_profile.h"

bool platform_wake_profile_get_matrix(platform_wake_profile_matrix_t *out_matrix) {
    if (!out_matrix) return false;
    *out_matrix = (platform_wake_profile_matrix_t){
        .verified_display_off_sources = DEVICE_WAKE_SOURCE_TOUCH |
                                        DEVICE_WAKE_SOURCE_AUXILIARY_CONTROL,
        /* Touch IRQ and GPIO0 remain candidates until the profile-specific
         * sleep electrical sequence and stale-contact drain are HIL proven. */
        .light_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER |
                                         DEVICE_WAKE_SOURCE_TOUCH |
                                         DEVICE_WAKE_SOURCE_AUXILIARY_CONTROL,
        .deep_sleep_candidate_sources = DEVICE_WAKE_SOURCE_TIMER |
                                        DEVICE_WAKE_SOURCE_AUXILIARY_CONTROL,
    };
    return true;
}
