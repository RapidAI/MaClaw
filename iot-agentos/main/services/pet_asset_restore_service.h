#pragma once

/* Cold-start pet-cache restore transaction. Storage, crypto-provider,
 * allocator, Display, task and board ownership remain host callbacks. */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/pet_asset_service.h"

typedef struct {
    uint32_t struct_size;
    bool (*storage_restore_allowed)(void *context);
    bool (*read_descriptor)(pet_asset_descriptor_t *out_descriptor, void *context);
    /* On success, ownership of `*out_frame` transfers to this service until
     * the terminal release_frames callback. */
    device_status_t (*load_verified_frame)(const pet_asset_descriptor_t *descriptor,
                                           uint32_t frame_index,
                                           uint8_t **out_frame, void *context);
    device_status_t (*install_full)(const pet_asset_descriptor_t *descriptor,
                                    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES],
                                    int *out_installed_frame_count,
                                    int *out_installed_frame_ms, void *context);
    void (*release_frames)(uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES], void *context);
    void (*clear_cache)(void *context);
    void (*apply_cached_profile)(void *context);
    void *context;
} pet_asset_restore_service_host_t;

/* Restores a fully verified committed cache. Malformed, incomplete, and
 * un-installable caches are cleared before return. */
device_status_t pet_asset_restore_service_restore(
    const pet_asset_restore_service_host_t *host);
