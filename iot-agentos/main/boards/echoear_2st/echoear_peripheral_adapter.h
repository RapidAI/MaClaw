#pragma once

/* EchoEar touch/peripheral implementation.  It shares the I2C bus created by
 * Round Audio but owns its own controller handle and no-op capability facts. */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "driver/i2c_master.h"
#include "esp_err.h"

#define ECHOEAR_TOUCH_CST8XX_ADDRESS 0x15

static i2c_master_dev_handle_t s_echoear_touch;

static esp_err_t round_peripheral_adapter_initialize(i2c_master_bus_handle_t bus) {
    if (!bus) return ESP_ERR_INVALID_ARG;
    if (s_echoear_touch) return ESP_OK;
    const i2c_device_config_t config = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = ECHOEAR_TOUCH_CST8XX_ADDRESS,
        .scl_speed_hz = 100000,
    };
    return i2c_master_bus_add_device(bus, &config, &s_echoear_touch);
}

static void round_peripheral_adapter_release(void) {
    if (s_echoear_touch) {
        (void)i2c_master_bus_rm_device(s_echoear_touch);
        s_echoear_touch = NULL;
    }
}

static bool round_peripheral_adapter_touch_read(bool *pressed, uint8_t *gesture) {
    if (pressed) *pressed = false;
    if (gesture) *gesture = 0;
    if (!s_echoear_touch) return false;
    uint8_t reg = 0x01;
    uint8_t status[2] = {0};
    if (i2c_master_transmit_receive(s_echoear_touch, &reg, 1, status,
                                    sizeof(status), 50) != ESP_OK) return false;
    if (gesture) *gesture = status[0];
    if (pressed) *pressed = (status[1] & 0x0Fu) != 0;
    return true;
}

static bool round_peripheral_adapter_touch_is_native_double_tap(uint8_t gesture) {
    return gesture == 0x0B;
}

static bool round_peripheral_adapter_touch_ready(void) {
    return s_echoear_touch != NULL;
}

static bool round_peripheral_adapter_get_power_status(unsigned *level_percent,
                                                       bool *charging) {
    (void)level_percent;
    (void)charging;
    return false;
}

static esp_err_t round_peripheral_adapter_get_motion_sample(
    device_motion_sample_t *out_sample) {
    if (!out_sample) return ESP_ERR_INVALID_ARG;
    return ESP_ERR_NOT_SUPPORTED;
}
