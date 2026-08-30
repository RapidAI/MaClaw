#pragma once

/*
 * Deferred configuration-portal coordinator.
 *
 * The service owns the FreeRTOS worker, immutable Connectivity Task Registry
 * identity and reversible System Sleep fence.  Its host supplies only the two
 * value observations/actions that remain composition-root business policy.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    bool (*meeting_active)(void *context);
    void (*start_setup_portal)(void *context);
    void *context;
} deferred_setup_worker_service_host_t;

device_status_t deferred_setup_worker_service_init(
    const deferred_setup_worker_service_host_t *host);

/* Coalesces duplicate requests while a delayed portal coordinator already
 * exists. A terminal stop closes future admission for this boot. */
device_status_t deferred_setup_worker_service_start(void);
device_status_t deferred_setup_worker_service_stop(uint32_t timeout_ms);

/* PREPARE closes admission and retires only the generation that existed when
 * it began. ABORT may restore that generation, never an idle request. */
device_status_t deferred_setup_worker_service_prepare_system_sleep(
    uint32_t timeout_ms);
void deferred_setup_worker_service_abort_system_sleep_prepare(void);

/* Terminal counterpart for a Connectivity fault-domain restart. It shares
 * bounded retirement mechanics with System Sleep but intentionally has no
 * ABORT path: COMMIT leaves portal-worker admission closed. */
device_status_t deferred_setup_worker_service_prepare_network_restart(
    uint32_t timeout_ms);
device_status_t deferred_setup_worker_service_commit_prepared_network_restart(void);
