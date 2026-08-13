#pragma once

/*
 * Private circular Peripheral HAL boundary.
 *
 * Round Audio owns the lifetime of the shared codec I2C bus.  Touch, PMIC
 * and IMU controllers connected to that bus belong to this independent
 * source owner, however: a board's peripheral implementation must not be
 * compiled into its codec transport merely because both use one bus.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "esp_err.h"

/* Starts the shared peripheral observation domain before Input begins.  The
 * service coordinates the private codec-bus preflight; callers see only a
 * bounded semantic readiness result. */
esp_err_t round_peripheral_service_prepare(unsigned output_volume,
                                           uint32_t timeout_ms);

bool round_peripheral_service_touch_read(bool *pressed, uint8_t *gesture);
bool round_peripheral_service_touch_is_native_double_tap(uint8_t gesture);
bool round_peripheral_service_touch_ready(void);

bool round_peripheral_service_get_power_status(unsigned *level_percent,
                                                bool *charging);
esp_err_t round_peripheral_service_get_motion_sample(device_motion_sample_t *out_sample);
