#include "platform_lifecycle.h"

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

device_status_t platform_lifecycle_stop_board_background_tasks(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_stop_background_tasks(timeout_ms));
}
