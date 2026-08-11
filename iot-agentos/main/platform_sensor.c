#include "platform_sensor.h"

#include "board_port.h"

static device_status_t status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_NOT_SUPPORTED: return DEVICE_STATUS_UNAVAILABLE;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_FAIL: return DEVICE_STATUS_IO_ERROR;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

device_status_t platform_sensor_get_motion_sample(device_motion_sample_t *out_sample) {
    if (!out_sample) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_motion_get_sample(out_sample));
}
