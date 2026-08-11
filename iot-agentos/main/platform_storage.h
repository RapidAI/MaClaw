#pragma once

/*
 * Internal physical-storage SPI.
 *
 * Persistence, resource-pressure and product features own their respective
 * data policy.  This port only reports whether a profile permits optional,
 * rebuildable Flash work while its display/PSRAM topology is active.  It
 * exposes no SPIFFS/VFS/NVS handle, cache-disable primitive, partition layout
 * or writer/task lifecycle.
 */

#include <stdbool.h>

bool platform_storage_allows_optional_flash_work(void);
