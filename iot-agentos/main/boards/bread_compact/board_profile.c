#include "board_profile.h"

bool board_profile_get(device_profile_t *out_profile) {
    if (!out_profile) return false;
    *out_profile = (device_profile_t){
        .struct_size = sizeof(device_profile_t),
        .abi_version = DEVICE_PROFILE_ABI_VERSION,
        .id = "bread-compact-wifi-lcd-v1",
        .display_width = 240,
        .display_height = 320,
        /* All MaClaw AgentOS devices implement the same business baseline.
         * This profile adds only physical controls beyond that shared offer. */
        .capabilities = DEVICE_CAPABILITY_REQUIRED_BASELINE |
                        DEVICE_CAPABILITY_VOLUME_CONTROL,
        .primary_interaction_source = DEVICE_INPUT_SOURCE_PRIMARY_CONTROL,
        .primary_interaction_label = "激活键",
    };
    return true;
}
