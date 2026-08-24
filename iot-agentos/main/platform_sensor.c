#include "platform_sensor.h"

#include "platform_sensor_profile.h"

device_status_t platform_sensor_get_motion_sample(device_motion_sample_t *out_sample) {
    if (!out_sample) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_sensor_profile_get_motion_sample(out_sample);
}
