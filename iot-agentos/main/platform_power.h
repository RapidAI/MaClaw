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

device_status_t platform_power_enter_display_off(void);
device_status_t platform_power_wake_display(void);
bool platform_power_display_is_off(void);
bool platform_power_get_telemetry(device_power_telemetry_t *out_telemetry);

/* System-sleep preparation remains a normalized, value-only boundary.  It
 * does not enter LIGHT/DEEP_SLEEP; only a selected profile can arm/cancel its
 * electrical wake/rail sequence below this facade. */
device_status_t platform_power_prepare_verified_sleep(
    device_power_state_t target_state,
    device_wake_source_flags_t verified_sources,
    uint32_t timeout_ms);
device_status_t platform_power_abort_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms);
/* `entry_timeout_ms` bounds only profile-local work before the physical sleep
 * entry is issued (for example, final wake arming or a rail handoff).  It is
 * deliberately not a limit on the time spent asleep: a verified timer/alarm
 * wake can legitimately return hours later.  COMMIT returns only after a
 * profile-private sleep entry has returned on a verified wake. It remains
 * unavailable for every release profile until its complete electrical path
 * has passed HIL. */
device_status_t platform_power_commit_verified_sleep(
    device_power_state_t target_state,
    device_wake_source_flags_t verified_sources,
    uint32_t entry_timeout_ms);
/* `timeout_ms` is a new, bounded post-wake recovery budget. It is not derived
 * from the pre-sleep PREPARE deadline, which has necessarily expired during a
 * long successful sleep. */
device_status_t platform_power_resume_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms);
