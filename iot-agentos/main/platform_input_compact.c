#include "platform_input_profile.h"

#include "compact_input_service.h"
#include "legacy_bootstrap_input.h"

static device_status_t compact_status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

device_status_t platform_input_profile_start(platform_input_profile_publish_cb_t on_input,
                                             void *context) {
    if (!on_input) return DEVICE_STATUS_INVALID_ARGUMENT;
    return compact_status_from_esp_err(legacy_bootstrap_input_start_scanner(on_input, context));
}

device_status_t platform_input_profile_stop(uint32_t timeout_ms) {
    return compact_status_from_esp_err(legacy_bootstrap_input_stop_scanner(timeout_ms));
}

void platform_input_profile_set_command_cancel_enabled(bool enabled) {
    compact_input_service_set_command_cancel_enabled(enabled);
}
