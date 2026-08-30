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
/* Future System Sleep observer fence.  Battery Policy is a synchronous
 * telemetry consumer, so PREPARE closes new snapshot reads and drains an
 * already-admitted provider read before profile Power may alter ADC/charger
 * rails.  It neither requests sleep nor accesses board wiring. */
device_status_t battery_policy_service_prepare_system_sleep(uint32_t timeout_ms);
void battery_policy_service_abort_system_sleep_prepare(void);
bool battery_policy_service_get_snapshot(device_battery_policy_snapshot_t *out_snapshot);
bool battery_policy_service_allows_optional_work(void);
bool battery_policy_service_allows_high_power_work(void);

/* Returns the temporary safety-limited backlight level for the current
 * policy.  The requested configuration value remains owned by Display /
 * Configuration; this helper only limits the physical presentation while a
 * low-battery policy is active. */
uint8_t battery_policy_service_limit_backlight_percent(uint8_t requested_percent);

typedef device_status_t (*battery_policy_service_checkpoint_fn)(
    uint32_t timeout_ms, void *context);

/* Composition root injects the durable checkpoint transaction.  The callback
 * is value-only and is never invoked while the policy lock is held. */
device_status_t battery_policy_service_set_emergency_checkpoint_callback(
    battery_policy_service_checkpoint_fn callback, void *context);
/* Run the one-shot checkpoint if the current policy is PROTECT. */
device_status_t battery_policy_service_run_emergency_checkpoint(uint32_t timeout_ms);

/* A low-voltage emergency path may request exactly one bounded durable
 * checkpoint per policy generation.  The service itself does not perform NVS
 * I/O; the composition root supplies the checkpoint result and acknowledges
 * it only after a successful Persistence transaction. */
bool battery_policy_service_try_begin_emergency_checkpoint(void);
void battery_policy_service_complete_emergency_checkpoint(bool success);
