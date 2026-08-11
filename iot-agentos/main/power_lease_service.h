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
bool power_lease_service_allows_display_off(void);
bool power_lease_service_get_snapshot(device_power_lease_snapshot_t *out_snapshot);
