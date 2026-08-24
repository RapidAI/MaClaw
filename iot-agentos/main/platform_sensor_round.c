#include "platform_sensor_profile.h"

#include "round_peripheral_service.h"
#include "esp_err.h"

device_status_t platform_sensor_profile_get_motion_sample(
    device_motion_sample_t *out_sample) {
    if (!out_sample) return DEVICE_STATUS_INVALID_ARGUMENT;
    switch (round_peripheral_service_get_motion_sample(out_sample)) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_NOT_SUPPORTED: return DEVICE_STATUS_UNAVAILABLE;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_IO_ERROR;
    }
}
