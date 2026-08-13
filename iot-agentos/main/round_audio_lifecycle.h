#pragma once

/* Private Peripheral-to-Audio lifecycle seam for circular boards.
 *
 * The codec source owner alone can create the shared I2C bus.  Peripheral
 * Service uses this semantic preflight to make touch/PMIC/IMU observations
 * available before Input starts; no GPIO, controller or I2C-handle type leaks
 * across the service boundary.
 */

#include <stdint.h>

#include "esp_err.h"

esp_err_t round_audio_lifecycle_prepare_shared_bus(unsigned output_volume,
                                                    uint32_t timeout_ms);
