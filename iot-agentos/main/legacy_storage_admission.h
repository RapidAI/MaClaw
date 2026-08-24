#pragma once

/*
 * Private legacy Storage admission seam.
 *
 * Platform Storage owns the physical volume transaction.  During renderer
 * decomposition it asks the selected display/cache implementation only
 * whether best-effort, rebuildable Flash work is safe.  This must not make a
 * profile bridge depend on the broad board_port compatibility facade.
 */

#include <stdbool.h>

bool legacy_storage_admission_allows_optional_flash_work(void);

/* Renderer source owners implement this narrow symbol directly.  Keeping the
 * compatibility name out of this header prevents a Platform-only storage
 * question from being coupled back to the broad board_port facade. */
