#pragma once

/*
 * Pet Asset Service (A9 descriptor increment).
 *
 * Owns the Hub pet-asset descriptor contract and its representation-neutral
 * arithmetic.  The descriptor is deliberately value-only: it contains no
 * HTTP client, renderer, allocator, FreeRTOS, board, or display-shape detail.
 * The Hub JSON object is borrowed as an opaque pointer so parser types remain
 * an implementation detail of this service rather than leaking into callers.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define PET_ASSET_SERVICE_MAX_DIMENSION 256u
#define PET_ASSET_SERVICE_MAX_FRAMES 8u
#define PET_ASSET_SERVICE_BYTES_PER_PIXEL 3u
#define PET_ASSET_SERVICE_DEFAULT_FRAME_MS 450
#define PET_ASSET_SERVICE_URL_CAPACITY 256u
#define PET_ASSET_SERVICE_REVISION_CAPACITY 40u
#define PET_ASSET_SERVICE_CACHE_METADATA_CAPACITY 1024u

typedef struct {
    char encoding[16];
    char revision[PET_ASSET_SERVICE_REVISION_CAPACITY];
    int width;
    int height;
    int frame_ms;
    int frame_count;
    char urls[PET_ASSET_SERVICE_MAX_FRAMES][PET_ASSET_SERVICE_URL_CAPACITY];
    char sha256[PET_ASSET_SERVICE_MAX_FRAMES][65];
} pet_asset_descriptor_t;

/* Representation-neutral memory requirements for one authored asset. The
 * display adapter supplies its retained-target totals; allocation and current
 * heap-pressure policy remain the responsibility of the composition root. */
typedef struct {
    uint32_t source_bytes;
    uint32_t total_external_bytes;
    uint32_t max_external_allocation_bytes;
} pet_asset_memory_requirements_t;

/* Decodes and validates the complete Hub descriptor. `hub_object` is a
 * borrowed JSON object supplied by the composition root; it is never retained.
 * Media URLs are intentionally restricted to the authenticated Hub media path.
 * `frameMs` defaults only when omitted; a supplied value must be an integral
 * 50..10000 milliseconds so corrupted Hub data never silently changes asset
 * cadence.
 */
bool pet_asset_service_parse_hub_descriptor(const void *hub_object,
                                            pet_asset_descriptor_t *out);

/* Validates frame geometry and returns the exact RGB565A8 source byte count.
 * This is safe for cached metadata as it does not require Hub URLs or hashes.
 */
bool pet_asset_service_frame_bytes(int width, int height, size_t *out_bytes);

/* Combines validated source-frame geometry with a display-provided retained
 * target budget. This is only checked arithmetic and has no Display HAL or
 * allocator dependency. */
bool pet_asset_service_calculate_memory_requirements(
    const pet_asset_descriptor_t *descriptor,
    uint32_t retained_target_bytes,
    uint32_t max_target_allocation_bytes,
    pet_asset_memory_requirements_t *out);

/* Serializes/parses the versioned cache commit record.  These functions have
 * no filesystem ownership: Storage decides when and where bytes are committed
 * atomically.  Parsed cache descriptors intentionally have no media URLs;
 * their hashes/geometry are only for validating locally stored frame files.
 */
bool pet_asset_service_format_cache_metadata(const pet_asset_descriptor_t *descriptor,
                                             char *out, size_t out_capacity,
                                             size_t *out_length);
bool pet_asset_service_parse_cache_metadata(const char *data, size_t length,
                                            pet_asset_descriptor_t *out);

/* Compares a binary SHA-256 result with a validated descriptor/cache digest.
 * The caller retains cryptographic-provider ownership; this service only owns
 * the canonical lower/upper-case-insensitive hexadecimal representation. */
bool pet_asset_service_sha256_matches_hex(const uint8_t digest[32],
                                          const char expected_hex[65]);

/* Applies a display-provided frame-count ceiling to a validated descriptor.
 * The caller owns the display query and may choose the ceiling from a profile
 * budget; this value transformation has no panel or allocator dependency. */
bool pet_asset_service_limit_frame_count(const pet_asset_descriptor_t *source,
                                         uint32_t max_frame_count,
                                         pet_asset_descriptor_t *out);

/* Selects the next memory-pressure fallback from an authored animation while
 * preserving its complete cycle duration. The caller reports how many source
 * frames remain after a consuming renderer attempt; pointer compaction and
 * renderer installation remain outside this value-only service. */
bool pet_asset_service_next_memory_fallback(const pet_asset_descriptor_t *source,
                                            uint32_t attempted_frame_count,
                                            uint32_t available_frame_count,
                                            uint32_t *out_frame_count,
                                            uint32_t *out_frame_ms);
