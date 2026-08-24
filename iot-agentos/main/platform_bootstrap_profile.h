#pragma once

/* Selected physical Bootstrap profile seam.  Profile bridges retain the
 * legacy renderer transition while Platform Bootstrap remains free of board
 * port, panel, codec, GPIO and RTOS implementation details. */

#include "device_api.h"

device_status_t platform_bootstrap_profile_initialize(void);
