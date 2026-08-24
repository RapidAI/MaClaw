#pragma once

/* Internal physical bootstrap boundary.
 *
 * The composition root establishes selected-profile panel/audio/peripheral
 * ownership before any Input scanner is published.  This is intentionally a
 * one-time boot transaction, not a runtime deinit/restart API: Input Service
 * owns only normalized scanner lifecycle after this contract succeeds.
 */

#include "device_api.h"

device_status_t platform_bootstrap_initialize(void);
