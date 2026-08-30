#pragma once

/*
 * Pet asset download transaction.
 *
 * Owns the ordered, retrying traversal of a verified asset descriptor.  The
 * host retains physical HTTP, cryptographic-provider, media-arbitration and
 * display ownership behind value-only callbacks.  Consequently this shared
 * contract exposes neither an ESP HTTP client, PSA object, allocator nor
 * FreeRTOS primitive.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/gateway_capability_projection.h"
#include "services/pet_asset_service.h"

typedef struct {
    uint32_t struct_size;
    /* These probes are always invoked outside a host-owned media lane. */
    bool (*transaction_admitted)(bool startup_transaction, void *context);
    bool (*gateway_lease_current)(const gateway_capability_lease_t *lease,
                                  void *context);
    /* Performs exactly one bounded media request. On DEVICE_STATUS_OK the
     * callback transfers the body ownership to the service. `out_http_status`
     * is the received HTTP status even when it is non-200. */
    device_status_t (*request_frame)(const char *url, uint32_t expected_bytes,
                                     bool startup_transaction,
                                     const gateway_capability_lease_t *lease,
                                     uint8_t **out_frame, uint32_t *out_length,
                                     int32_t *out_http_status, void *context);
    /* Owns the physical SHA provider. Invalid bytes or digest mismatch must
     * return a non-OK value and are never retried as transport failures. */
    device_status_t (*verify_frame_sha256)(const uint8_t *frame,
                                           uint32_t frame_bytes,
                                           const char expected_sha256[65],
                                           void *context);
    void (*release_frame)(uint8_t *frame, void *context);
    /* Called after a retryable failed attempt. It may block using the host's
     * own task mechanism; false cancels the transaction. */
    bool (*wait_before_retry)(uint32_t delay_ms, bool startup_transaction,
                              void *context);
    /* Called between failed complete startup-pack attempts. Like the
     * per-frame wait, this is host-owned so this value-only service never
     * learns the scheduler or task-cancellation mechanism. */
    bool (*wait_before_pack_retry)(uint32_t delay_ms, void *context);
    /* Optional startup-only first-frame preview. Its failure is deliberately
     * non-fatal: a later complete install remains the desired outcome. */
    device_status_t (*install_first_frame_preview)(
        const pet_asset_descriptor_t *descriptor, const uint8_t *frame,
        const gateway_capability_lease_t *lease, void *context);
    void *context;
} pet_asset_download_service_host_t;

/* Downloads all authored display frames in order. On success, ownership of
 * every non-NULL output frame transfers to the caller. On failure, this
 * service releases all frames it acquired through the host callback. */
device_status_t pet_asset_download_service_fetch(
    const pet_asset_download_service_host_t *host,
    const pet_asset_descriptor_t *descriptor, bool startup_transaction,
    const gateway_capability_lease_t *gateway_lease,
    uint8_t *out_frames[PET_ASSET_SERVICE_MAX_FRAMES]);

/* Performs the bounded startup transaction around `fetch`: each pass may
 * still retry individual frames, while only transient transport failures
 * cause a fresh complete-pack pass. Descriptor/content errors, lease or
 * admission cancellation, and provider failures return immediately. */
device_status_t pet_asset_download_service_fetch_startup_pack(
    const pet_asset_download_service_host_t *host,
    const pet_asset_descriptor_t *descriptor,
    const gateway_capability_lease_t *gateway_lease,
    uint8_t *out_frames[PET_ASSET_SERVICE_MAX_FRAMES]);
