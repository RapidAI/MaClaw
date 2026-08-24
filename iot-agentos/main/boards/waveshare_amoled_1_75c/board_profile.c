#include "board_profile.h"

/* Physical facts only.  The business baseline remains common with Bread,
 * EchoEar and Fangtang; the round viewport and touch are adapter capabilities
 * rather than a separate product behaviour. */
bool board_profile_get(device_profile_t *out_profile) {
    if (!out_profile) return false;
    *out_profile = (device_profile_t){
        .struct_size = sizeof(device_profile_t),
        .abi_version = DEVICE_PROFILE_ABI_VERSION,
        .id = "waveshare-s3-touch-amoled-1.75c-v1",
        .display_width = 466,
        .display_height = 466,
        .capabilities = DEVICE_CAPABILITY_REQUIRED_BASELINE |
                        DEVICE_CAPABILITY_TOUCH_INPUT |
                        DEVICE_CAPABILITY_BATTERY_TELEMETRY |
                        DEVICE_CAPABILITY_ROUND_DISPLAY |
                        DEVICE_CAPABILITY_MOTION_SENSOR,
        .primary_interaction_source = DEVICE_INPUT_SOURCE_TOUCH,
        .primary_interaction_label = "触摸屏",
        .volume_interaction_hint = "触摸屏长按调节音量",
        /* The AMOLED touch surface and the hardware activation key both
         * restore DISPLAY_OFF through the common App Intent policy. */
        .display_wake_sources =
            DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_TOUCH) |
            DEVICE_INPUT_SOURCE_FLAG(DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL),
    };
    return true;
}
