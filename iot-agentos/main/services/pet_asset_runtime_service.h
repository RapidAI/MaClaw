#pragma once

/*
 * Runtime pet-asset transaction coordinator.
 *
 * Owns the ordering for an online pet-profile update: capability capture,
 * optional-work admission, stale-cache reclamation, download, complete
 * display install, and cache hand-off. The host retains every physical owner
 * (Gateway state, HTTP/PSA/media lane, Display, Storage and allocator).
 * This contract therefore transports only values and source-buffer pointers.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/gateway_capability_projection.h"
#include "services/pet_asset_service.h"

typedef struct {
    uint32_t struct_size;
    bool (*revision_installed)(const pet_asset_descriptor_t *descriptor,
                               void *context);
    bool (*capture_gateway_lease)(gateway_capability_lease_t *out_lease,
                                  void *context);
    bool (*gateway_lease_current)(const gateway_capability_lease_t *lease,
                                  void *context);
    /* Value-only cancellation/admission probe. Physical HTTP ownership stays
     * behind the download callback; this is sampled before each phase. */
    bool (*transaction_admitted)(void *context);
    void (*begin_optional_media_work)(void *context);
    void (*finish_optional_media_work)(void *context);
    bool (*capacity_available)(const pet_asset_descriptor_t *descriptor,
                               void *context);
    bool (*drop_stale_cache)(const pet_asset_descriptor_t *descriptor,
                             void *context);
    device_status_t (*download)(
        const pet_asset_descriptor_t *descriptor,
        const gateway_capability_lease_t *lease,
        uint8_t *out_frames[PET_ASSET_SERVICE_MAX_FRAMES], void *context);
    bool (*prepare_cache_mirror)(void *context);
    device_status_t (*install_full)(
        const pet_asset_descriptor_t *descriptor,
        uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES], bool prepare_cache_mirror,
        uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES],
        int *out_installed_frame_count, int *out_installed_frame_ms,
        void *context);
    void (*cache_in_background)(
        const pet_asset_descriptor_t *descriptor,
        uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES],
        const gateway_capability_lease_t *lease, void *context);
    void (*release_frames)(uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
                           void *context);
    void *context;
} pet_asset_runtime_service_host_t;

/* Runs one complete latest-wins runtime asset transaction. Source and cache
 * frame ownership is always returned to the host release callback unless the
 * cache host explicitly takes it during `cache_in_background`. */
device_status_t pet_asset_runtime_service_apply(
    const pet_asset_runtime_service_host_t *host,
    const pet_asset_descriptor_t *descriptor);
