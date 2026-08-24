#pragma once

/*
 * Pet cache coordinator.
 *
 * Owns the short-lived internal-stack worker used for SPIFFS mutations, its
 * admission gate and its Storage lifecycle identity.  The public surface is
 * deliberately value-only: callers pass an already validated descriptor and
 * capability lease, never an HTTP/RTOS/storage handle.  The service does not
 * retain caller-owned sources for synchronous work; background submission
 * consumes the supplied frame pointers on every path.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/gateway_capability_projection.h"
#include "services/pet_asset_service.h"

typedef bool (*pet_cache_service_cancelled_fn)(void *context);

typedef struct {
    uint32_t struct_size;
    /* These probes are called with no service lock held.  This is the only
     * allowed service->composition-root direction, so the root never acquires
     * the service lock while holding its own lifecycle lock. */
    bool (*storage_mounted)(void *context);
    bool (*allows_optional_flash_work)(void *context);
    bool (*gateway_lease_current)(const gateway_capability_lease_t *lease,
                                  void *context);
    void *context;
} pet_cache_service_host_t;

device_status_t pet_cache_service_init(const pet_cache_service_host_t *host);

/* Synchronous operations wait for a bounded internal-stack Flash worker.
 * `cancelled` is sampled between page writes and while waiting for admission;
 * it is not retained after this call returns. */
device_status_t pet_cache_service_clear(pet_cache_service_cancelled_fn cancelled,
                                        void *cancel_context);
device_status_t pet_cache_service_drop_if_stale(
    const pet_asset_descriptor_t *descriptor, bool *out_dropped,
    pet_cache_service_cancelled_fn cancelled, void *cancel_context);

/* Takes ownership of every non-NULL frame in `frames`, including rejection
 * paths. The optional cache mirror remains strictly best-effort. */
void pet_cache_service_cache_in_background(
    const pet_asset_descriptor_t *descriptor,
    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
    const gateway_capability_lease_t *gateway_lease);

/* A normal lifecycle stop is terminal for this boot generation. System Sleep
 * uses the separate reversible PREPARE/ABORT pair. */
device_status_t pet_cache_service_stop(uint32_t timeout_ms);
device_status_t pet_cache_service_prepare_system_sleep(uint32_t timeout_ms);
void pet_cache_service_abort_system_sleep_prepare(void);

