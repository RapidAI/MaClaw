#pragma once

/*
 * Selected physical Power profile seam.
 *
 * Platform Power owns the stable Device-status API and the Display Service
 * bridge.  This private family contract supplies the latest normalized
 * battery observation and the profile-local beginning of a future verified
 * MCU-sleep transaction.  It deliberately does not expose ADC, PMIC, charger
 * GPIO, I2C, task, board-port, or ESP sleep types, so a new hardware profile
 * implements its electrical adapter without teaching Power Service about
 * board identity.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

bool platform_power_profile_get_telemetry(uint8_t *out_level_percent,
                                          bool *out_charging);

/*
 * Profile-private Power half of the system-sleep transaction.
 *
 * `verified_sources` is already filtered by Wake Service and is a value-only
 * description. The selected profile must atomically arm only those already
 * HIL-proven electrical sources, put any required peripheral rails into their
 * pre-sleep state, and retain enough private state for abort/resume. It must
 * return UNAVAILABLE until that complete electrical sequence is implemented
 * and proven. Returning an error leaves no armed wake source or altered rail.
 *
 * This is preparation only: it must never call an MCU sleep API. Power
 * Service owns global participant ordering, final lease recheck and the later
 * commit. `abort` is idempotent and may be called after any failed prepare.
 */
device_status_t platform_power_profile_prepare_verified_sleep(
    device_power_state_t target_state,
    device_wake_source_flags_t verified_sources,
    uint32_t timeout_ms);
device_status_t platform_power_profile_abort_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms);

/*
 * Profile-private COMMIT/RESUME half of a verified MCU-sleep transaction.
 *
 * The selected profile may return OK from COMMIT only after every prepared
 * electrical wake source is armed and the physical sleep entry has returned
 * because of a wake. `entry_timeout_ms` bounds only pre-entry arming/handoff;
 * it must never impose a maximum sleep duration. Power Service then invokes
 * RESUME with a fresh post-wake recovery budget before reopening shared
 * participants. `resume` must restore the profile-local rails, clocks and
 * input paths required by the already selected power depth; it is idempotent
 * after a failed/partial COMMIT. Until a profile has electrical HIL evidence
 * for that complete sequence, COMMIT must return UNAVAILABLE.
 *
 * This boundary deliberately carries only common value types. ESP sleep APIs,
 * RTC/GPIO/touch-controller mechanics, PMIC rails, modem and task objects
 * remain in the selected board adapter below the Platform Power facade.
 */
device_status_t platform_power_profile_commit_verified_sleep(
    device_power_state_t target_state,
    device_wake_source_flags_t verified_sources,
    uint32_t entry_timeout_ms);
device_status_t platform_power_profile_resume_verified_sleep(
    device_power_state_t target_state, uint32_t timeout_ms);
