#pragma once

/*
 * Cold-start pet-asset transaction coordinator.
 *
 * Owns the latest-wins ordering for a retained startup descriptor: absence
 * clearing, display adaptation, Gateway capability capture, verified download,
 * late admission, full installation, cache hand-off, and terminal generation
 * completion. The host owns all physical operations and state stores, so this
 * value-only contract exposes no SDK, RTOS, JSON, allocator or renderer type.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/gateway_capability_projection.h"
#include "services/pet_asset_service.h"
#include "services/startup_pet_asset_state_service.h"

typedef struct {
    uint32_t struct_size;
    bool (*snapshot)(startup_pet_asset_state_snapshot_t *out_state, void *context);
    bool (*stop_requested)(void *context);
    /* Clears retained artwork only if this captured startup generation is
     * still admitted at the physical renderer boundary.  A newer handshake
     * can replace an absent descriptor while an older worker is waking, so a
     * pre-check inside this value coordinator alone is not sufficient. */
    device_status_t (*clear_applied)(uint32_t generation, void *context);
    bool (*prepare_for_display)(const pet_asset_descriptor_t *source,
                                pet_asset_descriptor_t *out_display,
                                void *context);
    bool (*revision_installed)(const pet_asset_descriptor_t *descriptor,
                               void *context);
    bool (*capture_gateway_lease)(gateway_capability_lease_t *out_lease,
                                  void *context);
    bool (*gateway_lease_current)(const gateway_capability_lease_t *lease,
                                  void *context);
    bool (*generation_admitted)(uint32_t generation, void *context);
    device_status_t (*download)(
        const pet_asset_descriptor_t *descriptor,
        const gateway_capability_lease_t *lease, uint32_t generation,
        uint8_t *out_frames[PET_ASSET_SERVICE_MAX_FRAMES], void *context);
    bool (*prepare_cache_mirror)(void *context);
    device_status_t (*install_full)(
        const pet_asset_descriptor_t *descriptor,
        uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES], bool prepare_cache_mirror,
        uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES], uint32_t generation,
        int *out_installed_frame_count, int *out_installed_frame_ms,
        void *context);
    void (*cache_in_background)(
        const pet_asset_descriptor_t *descriptor,
        uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES],
        const gateway_capability_lease_t *lease, void *context);
    void (*release_frames)(uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
                           void *context);
    void (*finish_generation)(uint32_t generation, void *context);
    void *context;
} pet_asset_startup_service_host_t;

/* Runs one startup descriptor transaction. A withdrawn Gateway lease leaves
 * the retained generation pending, matching the cold-start policy that only a
 * later authenticated descriptor can re-authorize network artwork. All other
 * terminal outcomes finish only the captured generation. */
device_status_t pet_asset_startup_service_apply(
    const pet_asset_startup_service_host_t *host);
