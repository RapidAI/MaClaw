#pragma once

#include <stdbool.h>

#include "device_api.h"

/*
 * Hardware-independent battery admission policy.  Board ports only publish a
 * calibrated (or unavailable) telemetry snapshot through Device API; this
 * service never reads ADCs, charger GPIOs or board identifiers.
 *
 * This first safe slice deliberately does not force sleep or write a
 * checkpoint: those actions require a verified per-profile wake matrix and a
 * Persistence Service transaction.  It does, however, give every business
 * operation one consistent answer for starting an optional/high-power job.
 */

device_status_t battery_policy_service_init(void);
/* Closes policy snapshot admission before its telemetry provider is released.
 * This synchronous observer owns no task or hardware resource, so a
 * successful return means all later public queries fail closed. Calling this
 * before init is idempotent and does not close a future generation. */
device_status_t battery_policy_service_deinit(uint32_t timeout_ms);
bool battery_policy_service_get_snapshot(device_battery_policy_snapshot_t *out_snapshot);
bool battery_policy_service_allows_optional_work(void);
bool battery_policy_service_allows_high_power_work(void);
