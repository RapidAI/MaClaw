#pragma once

/*
 * Private selected-profile Wake matrix.  It contains only normalized value
 * types: GPIO/RTC/touch-controller details belong in the profile translation
 * unit that supplies this table.  Candidate entries are deliberately not
 * effective capabilities; see device_wake_depth_capability_t.
 */
#include "device_api.h"

typedef struct {
    device_wake_source_flags_t verified_display_off_sources;
    device_wake_source_flags_t light_sleep_candidate_sources;
    device_wake_source_flags_t deep_sleep_candidate_sources;
} platform_wake_profile_matrix_t;

bool platform_wake_profile_get_matrix(platform_wake_profile_matrix_t *out_matrix);
