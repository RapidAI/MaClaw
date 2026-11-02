#pragma once

#include <stdbool.h>

#include "esp_err.h"

/** Best-effort CST816 polling on the shared I2C bus (SCL 11 / SDA 12).
 * A missing controller is not fatal: the device still wakes on host frames. */
esp_err_t buddy_touch_init(void);

/** True when the controller answered a probe and the polling task started.
 * Reported in the device hello frame so the host can show it. */
bool buddy_touch_available(void);

/** True when the square module's front key (GPIO0, active low) is being read.
 * The round panel reports false; it uses the touch controller instead. */
bool buddy_key_available(void);
