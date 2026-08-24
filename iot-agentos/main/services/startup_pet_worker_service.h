#pragma once

/*
 * Deferred startup-pet worker lifecycle.
 *
 * This service owns only FreeRTOS task lifetime, Task Registry identity and
 * System Sleep fencing. Its host performs the actual download transaction and
 * HTTP cancellation, so the public contract carries no HTTP, renderer,
 * allocator, Gateway or board objects.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    /* Called on the PSRAM-backed worker after its Registry identity and
     * lifecycle handle have been published. Never called while service state
     * is locked. */
    void (*run_transaction)(void *context);
    /* Requests cancellation of the composition-root-owned active HTTP work.
     * It is advisory and bounded; the worker's normal cancellation probes
     * remain authoritative. */
    void (*cancel_active_transaction)(uint32_t timeout_ms, void *context);
    /* Invoked only after a timed-out System Sleep generation has unregistered
     * and is safe to replace. It is not a request to do work in a timer/ISR. */
    void (*restart_after_system_sleep_abort)(void *context);
    void *context;
} startup_pet_worker_service_host_t;

device_status_t startup_pet_worker_service_init(
    const startup_pet_worker_service_host_t *host);

device_status_t startup_pet_worker_service_start(void);
device_status_t startup_pet_worker_service_stop(uint32_t timeout_ms);

device_status_t startup_pet_worker_service_prepare_system_sleep(uint32_t timeout_ms);
void startup_pet_worker_service_abort_system_sleep_prepare(void);

/* Value observations for composition-root cancellation and media policy. */
bool startup_pet_worker_service_stop_requested(void);
bool startup_pet_worker_service_active(void);
bool startup_pet_worker_service_is_current_worker(void);
