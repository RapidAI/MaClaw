#pragma once

/*
 * Pet Asset cache storage adapter.
 *
 * Owns the named cache object set and its VFS transaction rules.  The caller
 * retains descriptor validation, SHA provider, allocation, renderer, task and
 * board/display policy.  This adapter deliberately exposes bytes and callbacks
 * only: no physical storage, allocator, scheduler or profile identity leaks upward.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "services/pet_asset_service.h"

typedef bool (*pet_asset_cache_cancelled_fn)(void *context);
typedef void (*pet_asset_cache_yield_fn)(void *context);

typedef struct {
    pet_asset_cache_cancelled_fn cancelled;
    pet_asset_cache_yield_fn yield;
    void *context;
} pet_asset_cache_storage_options_t;

/* Metadata is published only after every frame is fully flushed and synced.
 * On failure/cancel there is intentionally no valid commit record, so restore
 * must reject the interrupted generation. */
bool pet_asset_cache_storage_write(const pet_asset_descriptor_t *descriptor,
                                   uint8_t *const frames[PET_ASSET_SERVICE_MAX_FRAMES],
                                   const pet_asset_cache_storage_options_t *options);

/* Reads a complete cache commit record and one exact-size frame object.  The
 * caller supplies its allocation and retains cryptographic verification; this
 * adapter only owns named-object lookup, completeness and VFS error handling. */
bool pet_asset_cache_storage_read_descriptor(pet_asset_descriptor_t *out_descriptor);
bool pet_asset_cache_storage_read_frame(const pet_asset_descriptor_t *descriptor,
                                        uint32_t frame_index, uint8_t *out,
                                        size_t out_capacity);

/* Removes only current and legacy pet cache objects; never unrelated storage
 * such as meeting recordings. */
void pet_asset_cache_storage_clear(void);

/* Returns true only when cache objects were removed because no complete cache
 * exists for `new_revision`. */
bool pet_asset_cache_storage_drop_if_stale(const char *new_revision);
