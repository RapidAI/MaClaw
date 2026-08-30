#include "services/pet_asset_integrity_service.h"

#include <stdbool.h>

#include "services/pet_asset_service.h"

static bool host_valid(const pet_asset_integrity_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) && host->compute_sha256;
}

device_status_t pet_asset_integrity_service_verify_frame(
    const pet_asset_integrity_service_host_t *host,
    const uint8_t *frame, uint32_t frame_bytes,
    const char expected_sha256[65]) {
    if (!host_valid(host) || !frame || frame_bytes == 0 || !expected_sha256) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    uint8_t digest[32] = {0};
    const device_status_t status = host->compute_sha256(
        frame, frame_bytes, digest, host->context);
    if (status != DEVICE_STATUS_OK) return status;
    return pet_asset_service_sha256_matches_hex(digest, expected_sha256)
               ? DEVICE_STATUS_OK
               : DEVICE_STATUS_INVALID_ARGUMENT;
}
