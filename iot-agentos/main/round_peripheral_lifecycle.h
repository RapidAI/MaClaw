#pragma once

/* Private Audio-to-Peripheral lifecycle seam for circular boards.
 *
 * The codec owner creates the shared I2C bus, so only that source owner may
 * pass its opaque handle to the PMIC/touch/IMU initializer.  The public
 * Peripheral service exposes normalized observations only. */

#include "driver/i2c_master.h"
#include "esp_err.h"

esp_err_t round_peripheral_lifecycle_attach(i2c_master_bus_handle_t bus);
void round_peripheral_lifecycle_detach(void);
