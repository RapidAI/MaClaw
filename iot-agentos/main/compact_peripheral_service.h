#pragma once

/* Private compact-board peripheral HAL implementation boundary.  It owns the
 * selected optional battery/charge and motion adapter.  The common renderer
 * observes only normalized power snapshots, motion samples and bounded
 * background-worker lifecycle operations. */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "esp_err.h"

esp_err_t compact_peripheral_service_initialize(void);
esp_err_t compact_peripheral_service_stop_background_tasks(uint32_t timeout_ms);
bool compact_peripheral_service_get_power_status(unsigned *level_percent, bool *charging);
esp_err_t compact_peripheral_service_get_motion_sample(device_motion_sample_t *out_sample);
