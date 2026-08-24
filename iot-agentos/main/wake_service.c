#include "wake_service.h"

#include "platform_wake.h"

static bool s_initialized;

device_status_t wake_service_init(void) {
    /* Verify that every selected production profile still publishes the
     * already-proven DISPLAY_OFF restoration contract.  This does not turn
     * any Light/Deep candidate into an effective capability. */
    device_wake_depth_capability_t display_off = {0};
    const device_status_t status = platform_wake_get_depth_capability(
        DEVICE_POWER_STATE_DISPLAY_OFF, &display_off);
    if (status != DEVICE_STATUS_OK || display_off.verified_sources == 0) {
        return status == DEVICE_STATUS_OK ? DEVICE_STATUS_UNAVAILABLE : status;
    }
    s_initialized = true;
    return DEVICE_STATUS_OK;
}

void wake_service_deinit(void) {
    s_initialized = false;
}

device_status_t wake_service_get_depth_capability(
    device_power_state_t target_state,
    device_wake_depth_capability_t *out_capability) {
    if (!s_initialized) return DEVICE_STATUS_UNAVAILABLE;
    return platform_wake_get_depth_capability(target_state, out_capability);
}
