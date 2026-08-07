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
        .capabilities = DEVICE_CAPABILITY_DISPLAY |
                        DEVICE_CAPABILITY_TOUCH_INPUT |
                        DEVICE_CAPABILITY_PRIMARY_CONTROL |
                        DEVICE_CAPABILITY_OUTPUT_VOLUME |
                        DEVICE_CAPABILITY_AUDIO_CAPTURE |
                        DEVICE_CAPABILITY_AUDIO_PLAYBACK |
                        DEVICE_CAPABILITY_OFFLINE_WAKE_WORD |
                        DEVICE_CAPABILITY_PERSISTENT_STORAGE |
                        DEVICE_CAPABILITY_BATTERY_TELEMETRY |
                        DEVICE_CAPABILITY_DISPLAY_OFF |
                        DEVICE_CAPABILITY_ROUND_DISPLAY |
                        DEVICE_CAPABILITY_MOTION_SENSOR,
        .primary_interaction_source = DEVICE_INPUT_SOURCE_TOUCH,
        .primary_interaction_label = "触摸屏",
    };
    return true;
}
