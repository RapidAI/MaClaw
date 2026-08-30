#include "services/pet_asset_startup_service.h"

static bool host_valid(const pet_asset_startup_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) && host->snapshot &&
           host->stop_requested && host->clear_applied &&
           host->prepare_for_display && host->revision_installed &&
           host->capture_gateway_lease && host->gateway_lease_current &&
           host->generation_admitted && host->download &&
           host->prepare_cache_mirror && host->install_full &&
           host->cache_in_background && host->release_frames &&
           host->finish_generation;
}

device_status_t pet_asset_startup_service_apply(
    const pet_asset_startup_service_host_t *host) {
    if (!host_valid(host)) return DEVICE_STATUS_INVALID_ARGUMENT;

    startup_pet_asset_state_snapshot_t state = {0};
    if (!host->snapshot(&state, host->context) || !state.pending) {
        return host->stop_requested(host->context) ? DEVICE_STATUS_BUSY : DEVICE_STATUS_OK;
    }
    const uint32_t generation = state.generation;
    if (host->stop_requested(host->context)) return DEVICE_STATUS_BUSY;

    if (!state.present) {
        /* The renderer callback performs the final generation check at its
         * own ownership boundary.  This prevents an old "asset withdrawn"
         * worker from clearing artwork that a newer Hub descriptor has just
         * installed or queued. */
        const device_status_t status = host->clear_applied(generation, host->context);
        host->finish_generation(generation, host->context);
        return status;
    }

    pet_asset_descriptor_t descriptor = {0};
    if (!host->prepare_for_display(&state.descriptor, &descriptor, host->context)) {
        host->finish_generation(generation, host->context);
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (host->revision_installed(&descriptor, host->context)) {
        host->finish_generation(generation, host->context);
        return DEVICE_STATUS_OK;
    }

    gateway_capability_lease_t lease = {0};
    if (!host->capture_gateway_lease(&lease, host->context)) {
        return DEVICE_STATUS_BUSY;
    }

    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES] = {0};
    uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES] = {0};
    device_status_t status = DEVICE_STATUS_OK;

    if (!host->generation_admitted(generation, host->context)) {
        status = DEVICE_STATUS_BUSY;
        goto done;
    }
    status = host->download(&descriptor, &lease, generation, frames, host->context);
    if (status != DEVICE_STATUS_OK) goto done;
    if (!host->generation_admitted(generation, host->context) ||
        !host->gateway_lease_current(&lease, host->context)) {
        status = DEVICE_STATUS_BUSY;
        goto done;
    }

    /* Keep Storage/cache preparation distinct from renderer consumption. A
     * generation may be revoked while the optional mirror decision is being
     * made; revalidate before entering the full install callback. */
    const bool prepare_cache_mirror = host->prepare_cache_mirror(host->context);
    if (!host->generation_admitted(generation, host->context) ||
        !host->gateway_lease_current(&lease, host->context)) {
        status = DEVICE_STATUS_BUSY;
        goto done;
    }
    int installed_frame_count = 0;
    int installed_frame_ms = 0;
    status = host->install_full(&descriptor, frames, prepare_cache_mirror,
                                cache_frames, generation, &installed_frame_count,
                                &installed_frame_ms, host->context);
    /* The renderer callback may consume a complete frame set while a
     * Connectivity restart revokes this startup generation.  A successful
     * install is not enough evidence to continue the transaction: fail closed
     * before handing a mirror to the background Storage worker. */
    if (status == DEVICE_STATUS_OK &&
        (!host->generation_admitted(generation, host->context) ||
         !host->gateway_lease_current(&lease, host->context))) {
        status = DEVICE_STATUS_BUSY;
    }
    if (status == DEVICE_STATUS_OK &&
        installed_frame_count == descriptor.frame_count && cache_frames[0] &&
        host->generation_admitted(generation, host->context) &&
        host->gateway_lease_current(&lease, host->context)) {
        host->cache_in_background(&descriptor, cache_frames, &lease, host->context);
    }

done:
    host->release_frames(frames, host->context);
    host->release_frames(cache_frames, host->context);
    host->finish_generation(generation, host->context);
    return status;
}
