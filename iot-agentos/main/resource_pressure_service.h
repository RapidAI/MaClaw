#pragma once

#include <stdbool.h>

#include "device_api.h"

/*
 * Common resource policy observation.  Hardware ports report facts through
 * standard ESP-IDF allocators/filesystems; this service alone maps them to the
 * product-level NORMAL/PRESSURE/CRITICAL state.  It intentionally has no
 * board IDs or renderer/audio side effects.
 */

device_status_t resource_pressure_service_init(const char *storage_label,
                                               bool storage_available);
/* Closes observation admission before the Storage/VFS owner is released. The
 * static lifecycle lock remains valid for late callers; after success every
 * query fails closed instead of sampling an unmounted storage volume. Init's
 * construction publication shares this same bounded stop deadline. */
device_status_t resource_pressure_service_deinit(uint32_t timeout_ms);
void resource_pressure_service_set_storage_available(bool available);
bool resource_pressure_service_get_snapshot(device_resource_pressure_snapshot_t *out_snapshot);

/* Optional work (remote pet pack fetch/cache/animation) must check this before
 * allocating or opening a new transfer.  Foreground voice, alarm, meeting
 * finalization and persistence never use this gate. */
bool resource_pressure_service_allows_optional_work(void);
bool resource_pressure_service_allows_optional_allocation(
    uint32_t internal_bytes, uint32_t external_bytes, uint32_t storage_bytes);
