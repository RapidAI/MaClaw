#pragma once

/*
 * Internal Storage policy service.
 *
 * Persistence owns durable data transactions and Resource Pressure owns
 * capacity admission.  This narrow service owns the remaining profile-level
 * question of whether optional, rebuildable flash work is safe for the active
 * display/PSRAM topology, so Device API never calls a board port directly.
 */

#include <stdbool.h>

bool storage_service_allows_optional_flash_work(void);
