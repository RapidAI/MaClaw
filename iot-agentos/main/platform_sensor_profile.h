#pragma once

/*
 * Selected physical Sensor profile seam.
 *
 * Platform Sensor exposes a normalized Device API value.  The selected
 * profile family owns whether an IMU exists and all controller-specific I2C,
 * GPIO, calibration and conversion details below this private contract.
 */

#include "device_api.h"

device_status_t platform_sensor_profile_get_motion_sample(
    device_motion_sample_t *out_sample);
