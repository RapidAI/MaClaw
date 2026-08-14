#pragma once

/*
 * Internal Storage policy service.
 *
 * Persistence owns durable data transactions and Resource Pressure owns
 * capacity admission. Storage Service owns the durable-volume lifecycle and
 * profile-level optional-flash suitability, so Device API never calls a board
 * port or SPIFFS implementation directly.
 */

#include <stdbool.h>

#include "device_api.h"

device_status_t storage_service_init(void);
/* The composition root must first stop every VFS consumer (meeting, pet cache
 * and restore work). This service closes admission before unmounting so late
 * optional-flash callers fail closed instead of reopening a retired VFS. */
device_status_t storage_service_deinit(void);
bool storage_service_is_available(void);
const char *storage_service_label(void);
bool storage_service_allows_optional_flash_work(void);
