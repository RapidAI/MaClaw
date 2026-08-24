#include "platform_lifecycle.h"

#include "platform_lifecycle_profile.h"

device_status_t platform_lifecycle_stop_board_background_tasks(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_lifecycle_profile_stop_board_background_tasks(timeout_ms);
}
