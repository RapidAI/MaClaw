#include "platform_input.h"

#include "platform_input_profile.h"

device_status_t platform_input_start(platform_input_publish_cb_t on_input,
                                     void *context) {
    if (!on_input) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_input_profile_start(on_input, context);
}

device_status_t platform_input_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_input_profile_stop(timeout_ms);
}

void platform_input_set_command_cancel_enabled(bool enabled) {
    platform_input_profile_set_command_cancel_enabled(enabled);
}
