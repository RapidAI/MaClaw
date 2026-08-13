#include "motion_service.h"

#include "platform_sensor.h"

device_status_t motion_service_get_sample(device_motion_sample_t *out_sample) {
    if (!out_sample) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!device_profile_has_capability(DEVICE_CAPABILITY_MOTION_SENSOR)) {
        return DEVICE_STATUS_UNAVAILABLE;
    }
    device_motion_sample_t sample = {
        .struct_size = sizeof(sample),
        .abi_version = DEVICE_MOTION_SAMPLE_ABI_VERSION,
    };
    const device_status_t status = platform_sensor_get_motion_sample(&sample);
    if (status == DEVICE_STATUS_OK) *out_sample = sample;
    return status;
}
