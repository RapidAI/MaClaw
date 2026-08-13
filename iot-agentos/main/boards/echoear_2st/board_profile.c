#include "board_profile.h"

bool board_profile_get(device_profile_t *out_profile) {
    if (!out_profile) return false;
    *out_profile = (device_profile_t){
        .struct_size = sizeof(device_profile_t),
        .abi_version = DEVICE_PROFILE_ABI_VERSION,
        .id = "echoear-2st-r8",
        .display_width = 360,
        .display_height = 360,
        /* Required business behaviour is shared with Bread; round/touch are
         * presentation and input adaptations, not a separate product tier. */
        .capabilities = DEVICE_CAPABILITY_REQUIRED_BASELINE |
                        DEVICE_CAPABILITY_TOUCH_INPUT |
                        DEVICE_CAPABILITY_ROUND_DISPLAY,
        .primary_interaction_source = DEVICE_INPUT_SOURCE_TOUCH,
        .primary_interaction_label = "屏幕",
    };
    return true;
}
