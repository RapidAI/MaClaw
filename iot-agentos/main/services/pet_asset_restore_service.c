#include "services/pet_asset_restore_service.h"

static bool host_valid(const pet_asset_restore_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) &&
           host->storage_restore_allowed && host->read_descriptor &&
           host->load_verified_frame && host->install_full && host->release_frames &&
           host->clear_cache && host->apply_cached_profile;
}

device_status_t pet_asset_restore_service_restore(
    const pet_asset_restore_service_host_t *host) {
    if (!host_valid(host)) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!host->storage_restore_allowed(host->context)) return DEVICE_STATUS_UNAVAILABLE;

    pet_asset_descriptor_t descriptor = {0};
    if (!host->read_descriptor(&descriptor, host->context) || descriptor.frame_count < 1 ||
        descriptor.frame_count > (int)PET_ASSET_SERVICE_MAX_FRAMES) {
        host->clear_cache(host->context);
        return DEVICE_STATUS_NOT_FOUND;
    }

    uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES] = {0};
    device_status_t status = DEVICE_STATUS_OK;
    for (int index = 0; index < descriptor.frame_count; ++index) {
        status = host->load_verified_frame(&descriptor, (uint32_t)index,
                                           &frames[index], host->context);
        if (status != DEVICE_STATUS_OK || !frames[index]) {
            if (status == DEVICE_STATUS_OK) status = DEVICE_STATUS_INTERNAL_ERROR;
            goto invalid_cache;
        }
    }

    int installed_frame_count = 0;
    int installed_frame_ms = 0;
    status = host->install_full(&descriptor, frames, &installed_frame_count,
                                &installed_frame_ms, host->context);
    if (status != DEVICE_STATUS_OK || installed_frame_count < 1 || installed_frame_ms < 1) {
        if (status == DEVICE_STATUS_OK) status = DEVICE_STATUS_INTERNAL_ERROR;
        goto invalid_cache;
    }
    host->release_frames(frames, host->context);
    host->apply_cached_profile(host->context);
    return DEVICE_STATUS_OK;

invalid_cache:
    host->release_frames(frames, host->context);
    host->clear_cache(host->context);
    return status;
}
