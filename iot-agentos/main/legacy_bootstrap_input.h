#pragma once

/*
 * Private legacy bootstrap/input seam.
 *
 * Renderer source owners still construct boot-lifetime panel/audio/peripheral
 * state and validate the prerequisite state before starting an input scanner.
 * Platform Bootstrap/Input require only those two narrow lifecycle operations,
 * not the broad board_port compatibility facade.
 */

#include <stdint.h>

#include "device_api.h"
#include "esp_err.h"

typedef void (*legacy_input_scanner_publish_cb_t)(device_input_action_t action,
                                                  device_input_source_t source,
                                                  void *context);

esp_err_t legacy_bootstrap_input_initialize(void);
esp_err_t legacy_bootstrap_input_start_scanner(legacy_input_scanner_publish_cb_t on_input,
                                               void *context);
esp_err_t legacy_bootstrap_input_stop_scanner(uint32_t timeout_ms);

/* Renderer source owners implement this narrow contract directly.  Keeping
 * the compatibility names out of this header prevents Platform Bootstrap and
 * Platform Input from depending on the broad board_port facade while the
 * boot-lifetime renderer ownership remains unchanged. */
