#pragma once

/*
 * Internal physical-sensor SPI.
 *
 * Fall Detection owns sampling policy, thresholds, confirmation windows and
 * user-facing behavior.  This port exposes only a normalized, timestamped
 * motion sample from the selected profile adapter.  It never exposes an IMU
 * bus/device handle, register map, interrupt, GPIO or a background-task
 * lifecycle contract.
 */

#include "device_api.h"

device_status_t platform_sensor_get_motion_sample(device_motion_sample_t *out_sample);
