#pragma once

/* Internal Motion domain service: Device API sees normalized samples; this is
 * the sole shared owner of the physical Sensor-port transition. */

#include "device_api.h"

device_status_t motion_service_get_sample(device_motion_sample_t *out_sample);
