#pragma once

/*
 * Pet asset frame-integrity transaction.
 *
 * The service owns the value-level ordering and status semantics for a
 * SHA-256 check.  The cryptographic provider remains a composition-root
 * callback so this contract does not expose PSA, an allocator, HTTP, RTOS or
 * a board adapter.
 */

#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    device_status_t (*compute_sha256)(const uint8_t *frame,
                                      uint32_t frame_bytes,
                                      uint8_t out_digest[32],
                                      void *context);
    void *context;
} pet_asset_integrity_service_host_t;

/* Computes and compares one frame digest.  Provider failures are returned as
 * supplied; malformed input or a digest mismatch is never retryable content
 * evidence and returns DEVICE_STATUS_INVALID_ARGUMENT. */
device_status_t pet_asset_integrity_service_verify_frame(
    const pet_asset_integrity_service_host_t *host,
    const uint8_t *frame, uint32_t frame_bytes,
    const char expected_sha256[65]);
