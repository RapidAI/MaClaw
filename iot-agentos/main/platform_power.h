#pragma once

/*
 * Internal physical DISPLAY_OFF SPI.
 *
 * Power Service owns deadlines, leases and serialized policy.  This port owns
 * only the Power-to-Display-Service bridge for an already-authorized
 * panel/backlight transaction, observed display-off state and normalized
 * read-only battery telemetry. Display Service is the single execution owner
 * of renderer/panel calls. It intentionally does not expose power rails,
 * PMIC/ADC handles, MCU sleep, wake-source configuration, GPIOs or any
 * light/deep-sleep lifecycle claim.
 */

#include <stdbool.h>

#include "device_api.h"

bool platform_power_enter_display_off(void);
bool platform_power_wake_display(void);
bool platform_power_display_is_off(void);
bool platform_power_get_telemetry(device_power_telemetry_t *out_telemetry);
