#include "board_profile.h"

bool board_profile_get(device_profile_t *out_profile) {
    if (!out_profile) return false;
    *out_profile = (device_profile_t){
        .struct_size = sizeof(device_profile_t),
        .abi_version = DEVICE_PROFILE_ABI_VERSION,
        .id = "fangtang-4g-v1",
        .display_width = 240,
        .display_height = 240,
        .capabilities = DEVICE_CAPABILITY_DISPLAY |
                        DEVICE_CAPABILITY_PRIMARY_CONTROL |
                        DEVICE_CAPABILITY_OUTPUT_VOLUME |
                        DEVICE_CAPABILITY_AUDIO_CAPTURE |
                        DEVICE_CAPABILITY_AUDIO_PLAYBACK |
                        DEVICE_CAPABILITY_OFFLINE_WAKE_WORD |
                        DEVICE_CAPABILITY_PERSISTENT_STORAGE |
                        DEVICE_CAPABILITY_BATTERY_TELEMETRY |
                        DEVICE_CAPABILITY_DISPLAY_OFF |
                        DEVICE_CAPABILITY_CELLULAR_TRANSPORT,
        .primary_interaction_source = DEVICE_INPUT_SOURCE_PRIMARY_CONTROL,
        .primary_interaction_label = "激活键",
    };
    return true;
}
