#pragma once

/*
 * Offline wake-recognizer restart coordinator.
 *
 * FreeRTOS task lifetime, PSRAM stack, immutable Audio Task Registry identity
 * and System Sleep fencing are private to this service.  The composition root
 * provides only value observations and the physical Audio/Gateway actions.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    bool (*restart_allowed)(void *context);
    bool (*foreground_active)(void *context);
    bool (*meeting_active)(void *context);
    bool (*optional_pet_worker_active)(void *context);
    void (*discard_asset_client)(void *context);
    device_status_t (*start_wake_word)(void *context);
    void *context;
} wake_restart_worker_service_host_t;

device_status_t wake_restart_worker_service_init(
    const wake_restart_worker_service_host_t *host);

/* Coalesces duplicate restart requests.  A request made while startup still
 * owns the greeting can be recorded separately and consumed at startup-ready. */
device_status_t wake_restart_worker_service_start(void);
void wake_restart_worker_service_note_startup_teardown(void);
bool wake_restart_worker_service_consume_startup_teardown(void);

/* Terminal stop also closes admission.  `close_admission` is the synchronous
 * SAFE_MODE fence used before its bounded coordinator starts draining tasks. */
device_status_t wake_restart_worker_service_stop(uint32_t timeout_ms);
void wake_restart_worker_service_close_admission(void);

device_status_t wake_restart_worker_service_prepare_system_sleep(uint32_t timeout_ms);
void wake_restart_worker_service_abort_system_sleep_prepare(void);

/* A Connectivity fault-domain restart is terminal for this worker generation,
 * unlike System Sleep. PREPARE closes admission and joins only the generation
 * that existed at entry; COMMIT forgets its ABORT record and leaves admission
 * closed. A later, explicit post-uplink composition bridge is solely
 * responsible for creating a replacement. */
device_status_t wake_restart_worker_service_prepare_network_restart(
    uint32_t timeout_ms);
device_status_t wake_restart_worker_service_commit_prepared_network_restart(void);
