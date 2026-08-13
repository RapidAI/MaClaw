#include "platform_input.h"

#include "board_port.h"

static device_status_t status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

device_status_t platform_input_start(platform_input_publish_cb_t on_input,
                                     void *context) {
    if (!on_input) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_init(on_input, context));
}

device_status_t platform_input_stop(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_stop_input(timeout_ms));
}

void platform_input_set_command_cancel_enabled(bool enabled) {
    board_port_set_command_cancel_enabled(enabled);
}
