#pragma once

/*
 * Private renderer-background lifecycle seam.
 *
 * The selected legacy renderer still owns admission and task handles for its
 * decorative workers while it is being decomposed.  Startup rollback needs
 * only this bounded stop operation; it must not depend on the broad
 * board_port compatibility facade or acquire display/audio/input ownership.
 *
 * This is intentionally a private ESP-IDF-facing contract.  Platform
 * Lifecycle's public facade translates the result through its selected
 * profile bridge and exposes only device_status_t above this boundary.
 */

#include <stdint.h>

#include "esp_err.h"

esp_err_t board_background_lifecycle_stop(uint32_t timeout_ms);
