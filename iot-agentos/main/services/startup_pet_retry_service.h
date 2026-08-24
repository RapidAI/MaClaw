#pragma once

/*
 * Deferred startup-pet retry coordinator.
 *
 * The service owns only the one-shot retry timer and its callback-admission
 * fence.  Download work, HTTP cancellation, capability leases and renderer
 * installation remain with the composition root.  A timer callback records a
 * value-only due fact; a normal worker consumes it later, so small ESP timer
 * task stacks never allocate frames or create download tasks.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

device_status_t startup_pet_retry_service_init(void);

/* Arms one coalesced retry. Calls during a System Sleep PREPARE are rejected;
 * the caller must wait for the transaction's ABORT before retrying. */
device_status_t startup_pet_retry_service_schedule(uint64_t delay_us);

/* Returns and clears the callback's coalesced due fact. */
bool startup_pet_retry_service_take_due(void);

/* Terminal close for a full startup rollback. It cannot be reopened in this
 * boot generation; System Sleep must use the reversible PREPARE API below. */
device_status_t startup_pet_retry_service_stop(uint32_t timeout_ms);

/* Reversible lifecycle fence for the startup-pet domain. PREPARE stops a
 * scheduled callback and waits for an already-admitted callback to drain. */
device_status_t startup_pet_retry_service_prepare_system_sleep(uint32_t timeout_ms);
void startup_pet_retry_service_abort_system_sleep_prepare(void);
