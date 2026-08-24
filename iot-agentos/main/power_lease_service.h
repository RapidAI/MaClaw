#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

/* Internal implementation of the Device Power Lease API.  This service is
 * deliberately profile-neutral: it owns only business eligibility for the
 * proven DISPLAY_OFF state, never rails, GPIOs, display geometry, or wake
 * sources. */
device_status_t power_lease_service_init(void);
/* Stops new foreground claims while preserving already-issued handles so their
 * owners can still release during a bounded composition-root drain. */
void power_lease_service_close_admission(void);
/* Finishes a closed generation once every previously issued lease is released.
 * On timeout admission remains closed and a later init is refused. */
device_status_t power_lease_service_deinit(uint32_t timeout_ms);
device_status_t power_lease_service_acquire(device_power_lease_owner_t owner,
                                            device_power_lease_t *out_lease);
void power_lease_service_release(device_power_lease_t lease);
/*
 * DISPLAY_OFF uses a small PREPARE -> COMMIT admission fence.  PREPARE
 * atomically proves that no foreground owner exists and closes new lease
 * admission until the caller either commits the physical panel transaction or
 * abandons it.  That prevents an audio/meeting/provisioning lease from
 * arriving between a best-effort eligibility read and panel-off commit.
 *
 * This is intentionally limited to the already-proven DISPLAY_OFF state.  It
 * is not an MCU light/deep-sleep transaction and does not configure wake
 * sources.  The returned generation is private to the Power Service and must
 * always be completed with `end_display_off_commit`, including on errors.
 */
device_status_t power_lease_service_begin_display_off_commit(uint32_t *out_generation);
bool power_lease_service_display_off_commit_is_current(uint32_t generation);
void power_lease_service_end_display_off_commit(uint32_t generation);

/*
 * Future MCU sleep uses an independent admission fence.  It shares the
 * existing foreground-lease invariant with DISPLAY_OFF, but must never reuse
 * the display-only generation: the later PREPARE chain will also own wake
 * configuration, Audio/Connectivity quiesce and rollback.  This service
 * remains policy-only and does not know GPIOs, rails or ESP sleep APIs.
 */
device_status_t power_lease_service_begin_system_sleep_prepare(
    device_power_state_t target_state, uint32_t *out_generation);
bool power_lease_service_system_sleep_prepare_is_current(
    device_power_state_t target_state, uint32_t generation);
void power_lease_service_end_system_sleep_prepare(uint32_t generation);
bool power_lease_service_get_snapshot(device_power_lease_snapshot_t *out_snapshot);

/* Compile-time-only lifecycle proof for the private DISPLAY_OFF fence.  The
 * normal image returns success without changing service state. */
device_status_t power_lease_service_run_display_off_commit_lifecycle_test(void);
