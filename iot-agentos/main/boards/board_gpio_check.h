#pragma once

/*
 * Shared GPIO validity guards for board profile adapters.
 *
 * Board adapters carry hardcoded pin assignments from their schematics.  A
 * schematic revision or a copy-paste into a new profile otherwise fails deep
 * inside a driver with an opaque error.  These guards fail fast at the
 * adapter boundary with the offending pin number in the log, so a profile
 * authoring mistake is diagnosable without hardware.
 */

#include "driver/gpio.h"
#include "esp_err.h"
#include "esp_log.h"

static inline esp_err_t board_check_gpio(gpio_num_t pin, const char *owner,
                                         const char *role) {
    if (!GPIO_IS_VALID_GPIO(pin)) {
        ESP_LOGE("board_gpio", "%s: %s pin %d is not a valid GPIO on this target",
                 owner ? owner : "board", role ? role : "io", (int)pin);
        return ESP_ERR_INVALID_ARG;
    }
    return ESP_OK;
}

static inline esp_err_t board_check_output_gpio(gpio_num_t pin, const char *owner,
                                                const char *role) {
    if (!GPIO_IS_VALID_OUTPUT_GPIO(pin)) {
        ESP_LOGE("board_gpio",
                 "%s: %s pin %d cannot drive output on this target",
                 owner ? owner : "board", role ? role : "output", (int)pin);
        return ESP_ERR_INVALID_ARG;
    }
    return ESP_OK;
}
