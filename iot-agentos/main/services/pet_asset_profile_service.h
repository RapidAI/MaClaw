#pragma once

/*
 * Runtime Gateway pet-profile coordinator.
 *
 * Owns the business decision for one already-parsed profile update: whether a
 * matching cold-start install may continue, when a newer selection supersedes
 * it, transient retry accounting, and terminal failure classification. JSON
 * parsing, Gateway polling/ACK order, HTTP, media, Display and Storage stay
 * with their existing owners behind value-only callbacks.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/pet_asset_service.h"

typedef struct {
    uint32_t struct_size;
    bool (*startup_profile_matches)(const char *revision, const char *skin,
                                    void *context);
    bool (*startup_pending)(void *context);
    void (*set_startup_pending)(bool pending, void *context);
    device_status_t (*apply_asset)(const pet_asset_descriptor_t *descriptor,
                                   void *context);
    device_status_t (*clear_asset)(void *context);
    bool (*status_permanently_invalid)(device_status_t status, void *context);
    uint32_t (*note_transient_failure)(const char *message_id, void *context);
    bool (*retry_exhausted)(uint32_t retry_limit, void *context);
    void (*reset_retries)(void *context);
    void *context;
} pet_asset_profile_service_host_t;

typedef struct {
    bool handled;
    bool permanently_invalid;
    bool deferred_to_startup;
    bool superseded_startup;
    uint32_t retry_count;
    device_status_t status;
} pet_asset_profile_service_result_t;

/* Applies one normalized pet-profile. `descriptor` is NULL for the native
 * fallback/clear profile. A matching retained startup descriptor is treated
 * as successfully handled without duplicating its download. */
pet_asset_profile_service_result_t pet_asset_profile_service_apply(
    const pet_asset_profile_service_host_t *host,
    const pet_asset_descriptor_t *descriptor, const char *skin,
    const char *message_id, uint32_t retry_limit);
