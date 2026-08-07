#pragma once

/* Internal board-adapter contract.  It deliberately has no ESP-IDF types. */
#include "device_api.h"

bool board_profile_get(device_profile_t *out_profile);
