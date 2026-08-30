#include "services/pet_asset_download_service.h"

#include <string.h>

#define PET_ASSET_DOWNLOAD_RUNTIME_ATTEMPTS 2u
#define PET_ASSET_DOWNLOAD_STARTUP_ATTEMPTS 3u
#define PET_ASSET_DOWNLOAD_STARTUP_PACK_ATTEMPTS 3u
#define PET_ASSET_DOWNLOAD_STARTUP_PACK_RETRY_DELAY_MS 3000u

static bool host_valid(const pet_asset_download_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) &&
           host->transaction_admitted && host->gateway_lease_current &&
           host->request_frame && host->verify_frame_sha256 && host->release_frame &&
           host->wait_before_retry;
}

static bool transaction_current(const pet_asset_download_service_host_t *host,
                                bool startup_transaction,
                                const gateway_capability_lease_t *gateway_lease) {
    return host->transaction_admitted(startup_transaction, host->context) &&
           host->gateway_lease_current(gateway_lease, host->context);
}

static bool startup_pack_retryable(device_status_t status) {
    /* Timeout is a transient transport result, like a generic I/O failure.
     * Resource pressure is deliberately not retried here: startup admission
     * owns its separate capacity backoff budget. */
    return status == DEVICE_STATUS_IO_ERROR || status == DEVICE_STATUS_TIMEOUT;
}

static void release_all(const pet_asset_download_service_host_t *host,
                        uint8_t *frames[PET_ASSET_SERVICE_MAX_FRAMES]) {
    if (!frames) return;
    for (uint32_t i = 0; i < PET_ASSET_SERVICE_MAX_FRAMES; ++i) {
        if (frames[i]) host->release_frame(frames[i], host->context);
        frames[i] = NULL;
    }
}

device_status_t pet_asset_download_service_fetch(
    const pet_asset_download_service_host_t *host,
    const pet_asset_descriptor_t *descriptor, bool startup_transaction,
    const gateway_capability_lease_t *gateway_lease,
    uint8_t *out_frames[PET_ASSET_SERVICE_MAX_FRAMES]) {
    if (!host_valid(host) || !descriptor || !gateway_lease || !out_frames ||
        descriptor->frame_count < 1 ||
        descriptor->frame_count > (int)PET_ASSET_SERVICE_MAX_FRAMES) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    memset(out_frames, 0, sizeof(uint8_t *) * PET_ASSET_SERVICE_MAX_FRAMES);
    if (!transaction_current(host, startup_transaction, gateway_lease)) {
        return DEVICE_STATUS_BUSY;
    }

    size_t expected_size = 0;
    if (!pet_asset_service_frame_bytes(descriptor->width, descriptor->height,
                                       &expected_size) ||
        expected_size > UINT32_MAX) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    const uint32_t expected_bytes = (uint32_t)expected_size;
    const uint32_t attempts = startup_transaction
                                  ? PET_ASSET_DOWNLOAD_STARTUP_ATTEMPTS
                                  : PET_ASSET_DOWNLOAD_RUNTIME_ATTEMPTS;

    for (int frame_index = 0; frame_index < descriptor->frame_count; ++frame_index) {
        device_status_t final_status = DEVICE_STATUS_IO_ERROR;
        uint8_t *frame = NULL;
        for (uint32_t attempt = 1; attempt <= attempts; ++attempt) {
            if (!transaction_current(host, startup_transaction, gateway_lease)) {
                final_status = DEVICE_STATUS_BUSY;
                break;
            }

            uint32_t frame_length = 0;
            int32_t http_status = 0;
            const device_status_t request_status = host->request_frame(
                descriptor->urls[frame_index], expected_bytes, startup_transaction,
                gateway_lease, &frame, &frame_length, &http_status, host->context);
            if (!transaction_current(host, startup_transaction, gateway_lease)) {
                if (frame) host->release_frame(frame, host->context);
                frame = NULL;
                final_status = DEVICE_STATUS_BUSY;
                break;
            }
            if (request_status == DEVICE_STATUS_OK && http_status == 200 &&
                frame && frame_length == expected_bytes) {
                const device_status_t verify_status = host->verify_frame_sha256(
                    frame, expected_bytes, descriptor->sha256[frame_index], host->context);
                if (verify_status == DEVICE_STATUS_OK) {
                    final_status = DEVICE_STATUS_OK;
                    break;
                }
                host->release_frame(frame, host->context);
                frame = NULL;
                /* A cryptographic mismatch is descriptor/content evidence,
                 * never a transient transport failure. */
                final_status = verify_status;
                break;
            }

            if (frame) host->release_frame(frame, host->context);
            frame = NULL;
            final_status = request_status != DEVICE_STATUS_OK
                               ? request_status
                               : http_status >= 400 && http_status < 500
                                     ? DEVICE_STATUS_INVALID_ARGUMENT
                                     : DEVICE_STATUS_IO_ERROR;
            if (final_status == DEVICE_STATUS_INVALID_ARGUMENT || attempt == attempts) break;
            if (!host->wait_before_retry(250u * attempt, startup_transaction,
                                         host->context)) {
                final_status = DEVICE_STATUS_BUSY;
                break;
            }
        }
        if (final_status != DEVICE_STATUS_OK) {
            release_all(host, out_frames);
            return final_status;
        }
        out_frames[frame_index] = frame;

        if (startup_transaction && frame_index == 0 &&
            host->install_first_frame_preview) {
            (void)host->install_first_frame_preview(descriptor, frame, gateway_lease,
                                                     host->context);
        }
        if (!transaction_current(host, startup_transaction, gateway_lease)) {
            release_all(host, out_frames);
            return DEVICE_STATUS_BUSY;
        }
    }
    return DEVICE_STATUS_OK;
}

device_status_t pet_asset_download_service_fetch_startup_pack(
    const pet_asset_download_service_host_t *host,
    const pet_asset_descriptor_t *descriptor,
    const gateway_capability_lease_t *gateway_lease,
    uint8_t *out_frames[PET_ASSET_SERVICE_MAX_FRAMES]) {
    if (!host_valid(host) || !host->wait_before_pack_retry || !descriptor ||
        !gateway_lease || !out_frames) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }

    device_status_t status = DEVICE_STATUS_IO_ERROR;
    for (uint32_t attempt = 1; attempt <= PET_ASSET_DOWNLOAD_STARTUP_PACK_ATTEMPTS;
         ++attempt) {
        status = pet_asset_download_service_fetch(host, descriptor, true,
                                                  gateway_lease, out_frames);
        if (status == DEVICE_STATUS_OK || !startup_pack_retryable(status) ||
            attempt == PET_ASSET_DOWNLOAD_STARTUP_PACK_ATTEMPTS) {
            return status;
        }
        if (!host->wait_before_pack_retry(
                PET_ASSET_DOWNLOAD_STARTUP_PACK_RETRY_DELAY_MS * attempt,
                host->context)) {
            return DEVICE_STATUS_BUSY;
        }
    }
    return status;
}
