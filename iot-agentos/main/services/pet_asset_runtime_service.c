#include "services/pet_asset_runtime_service.h"

static bool host_valid(const pet_asset_runtime_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) &&
           host->revision_installed && host->capture_gateway_lease &&
           host->gateway_lease_current && host->transaction_admitted &&
           host->begin_optional_media_work &&
           host->finish_optional_media_work && host->capacity_available &&
           host->drop_stale_cache && host->download && host->prepare_cache_mirror &&
           host->install_full && host->cache_in_background && host->release_frames;
}

device_status_t pet_asset_runtime_service_apply(
    const pet_asset_runtime_service_host_t *host,
    const pet_asset_descriptor_t *descriptor) {
    if (!host_valid(host) || !descriptor || descriptor->frame_count < 1 ||
        descriptor->frame_count > (int)PET_ASSET_SERVICE_MAX_FRAMES) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    if (host->revision_installed(descriptor, host->context)) return DEVICE_STATUS_OK;

    gateway_capability_lease_t lease = {0};
    if (!host->capture_gateway_lease(&lease, host->context)) return DEVICE_STATUS_BUSY;

    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES] = {0};
    uint8_t *cache_frames[PET_ASSET_SERVICE_MAX_FRAMES] = {0};
    device_status_t status = DEVICE_STATUS_OK;
    host->begin_optional_media_work(host->context);

    /* Optional-media admission is a separate ownership boundary from the
     * initial Gateway lease capture.  A concurrent Connectivity restart may
     * revoke that lease while the media lane is being opened; do not perform
     * cache reclamation or start a download against the retired generation. */
    if (!host->transaction_admitted(host->context) ||
        !host->gateway_lease_current(&lease, host->context)) {
        status = DEVICE_STATUS_BUSY;
        goto done;
    }

    if (!host->capacity_available(descriptor, host->context)) {
        (void)host->drop_stale_cache(descriptor, host->context);
        if (!host->capacity_available(descriptor, host->context)) {
            status = DEVICE_STATUS_RESOURCE_EXHAUSTED;
            goto done;
        }
    }
    /* Stale-cache reclamation can block on flash and outlive the old
     * capability generation.  Revalidate before entering the HTTP lane so a
     * late cache-drop completion cannot authorize work for a revoked lease. */
    if (!host->transaction_admitted(host->context) ||
        !host->gateway_lease_current(&lease, host->context)) {
        status = DEVICE_STATUS_BUSY;
        goto done;
    }
    status = host->download(descriptor, &lease, frames, host->context);
    if (status != DEVICE_STATUS_OK) goto done;
    if (!host->transaction_admitted(host->context) ||
        !host->gateway_lease_current(&lease, host->context)) {
        status = DEVICE_STATUS_BUSY;
        goto done;
    }

    /* Cache-mirror preparation can itself cross a Storage/Connectivity
     * boundary. Keep its decision separate from renderer install and fence
     * the lease again before consuming source frames. */
    const bool prepare_cache_mirror = host->prepare_cache_mirror(host->context);
    if (!host->transaction_admitted(host->context) ||
        !host->gateway_lease_current(&lease, host->context)) {
        status = DEVICE_STATUS_BUSY;
        goto done;
    }
    int installed_frames = 0;
    int installed_frame_ms = 0;
    status = host->install_full(descriptor, frames, prepare_cache_mirror,
                                cache_frames, &installed_frames,
                                &installed_frame_ms, host->context);
    /* Install consumes the renderer-owned source set and can block on a
     * profile-local buffer/DMA boundary.  A lease withdrawal during that
     * callback must not be reported as a successful runtime transaction or
     * allow the optional cache mirror to advance the retired generation. */
    if (status == DEVICE_STATUS_OK &&
        (!host->transaction_admitted(host->context) ||
         !host->gateway_lease_current(&lease, host->context))) {
        status = DEVICE_STATUS_BUSY;
    }
    if (status == DEVICE_STATUS_OK &&
        installed_frames == descriptor->frame_count && cache_frames[0] &&
        host->transaction_admitted(host->context) &&
        host->gateway_lease_current(&lease, host->context)) {
        host->cache_in_background(descriptor, cache_frames, &lease, host->context);
    }

done:
    host->release_frames(frames, host->context);
    host->release_frames(cache_frames, host->context);
    host->finish_optional_media_work(host->context);
    return status;
}
