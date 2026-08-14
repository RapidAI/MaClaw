#pragma once

/*
 * Internal physical-storage SPI.
 *
 * Persistence, resource-pressure and product features own their respective
 * data policy. This port owns SPIFFS/VFS mount facts: partition lookup,
 * factory-blank proof and the only automatic-format decision. It exposes no
 * driver handle, cache-disable primitive or writer/task lifecycle.
 */

#include <stdbool.h>

#include "device_api.h"

bool platform_storage_allows_optional_flash_work(void);
device_status_t platform_storage_mount(void);
/* Unmount only after Storage Service has closed admission and its composition
 * root has joined every VFS user. The physical port never force-closes caller
 * FILE handles; it fails closed on an unmount error. */
device_status_t platform_storage_unmount(void);
bool platform_storage_is_mounted(void);
const char *platform_storage_label(void);
