#pragma once

#include "driver/i2c_master.h"

/** Shared I2C master bus for the touch controller (0x15) and the ES8311
 * codec (0x18), both on SCL 11 / SDA 12. Created once; NULL on failure. */
i2c_master_bus_handle_t buddy_i2c_bus(void);
