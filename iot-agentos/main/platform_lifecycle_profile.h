#pragma once

/* Selected physical lifecycle profile seam.  Lifecycle Service owns startup
 * rollback ordering and deadlines; this private bridge owns only the selected
 * renderer's bounded background-task shutdown during a failed boot. */

#include <stdint.h>

#include "device_api.h"

device_status_t platform_lifecycle_profile_stop_board_background_tasks(uint32_t timeout_ms);
