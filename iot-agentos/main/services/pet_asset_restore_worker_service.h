#pragma once

/*
 * Bounded boot-time pet-cache restore worker.
 *
 * Owns the temporary FreeRTOS worker and completion join needed to keep the
 * large SPIFFS/renderer call stack out of the main task. The actual restore
 * transaction remains an injected, value-only callback, so Storage, crypto,
 * allocation and Display ownership stay at their existing services/root.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    device_status_t (*run_restore)(void *context);
    void *context;
} pet_asset_restore_worker_service_host_t;

/* Runs one restore transaction on an internal-RAM stack and joins it before
 * returning. This is deliberately a boot-time bounded operation, not a
 * persistent lifecycle participant. */
device_status_t pet_asset_restore_worker_service_run(
    const pet_asset_restore_worker_service_host_t *host);
