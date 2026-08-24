#pragma once

/*
 * Waveshare ESP32-S3 Touch AMOLED 1.75C peripheral contract.
 *
 * This header is intentionally included by the circular board implementation
 * only after its shared I2C bus exists.  It owns PMIC, CST9217 and QMI8658
 * controller details; callers receive only normalized power, touch and motion
 * facts.  It is header-local during the transitional single-TU renderer so no
 * driver handle can escape through a shared board API.
 */

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "esp_check.h"
#include "esp_lcd_io_i2c.h"
#include "esp_lcd_touch.h"
#include "esp_lcd_touch_cst9217.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "device_api.h"
#include "provisioning_failure_injection.h"

#define WAVESHARE_PERIPHERAL_PMIC_ADDRESS      0x34
#define WAVESHARE_PERIPHERAL_TOUCH_RESET_GPIO  GPIO_NUM_2
#define WAVESHARE_PERIPHERAL_TOUCH_IRQ_GPIO    GPIO_NUM_11
#define WAVESHARE_PERIPHERAL_TOUCH_WIDTH       466
#define WAVESHARE_PERIPHERAL_TOUCH_HEIGHT      466
#define WAVESHARE_PERIPHERAL_QMI8658_ADDRESS   0x6B
#define WAVESHARE_PERIPHERAL_QMI8658_WHO_AM_I  0x05

static i2c_master_dev_handle_t s_waveshare_axp2101;
static i2c_master_dev_handle_t s_waveshare_qmi8658;
static esp_lcd_panel_io_handle_t s_waveshare_cst9217_io;
static esp_lcd_touch_handle_t s_waveshare_cst9217_touch;

static esp_err_t waveshare_axp2101_write(uint8_t reg, uint8_t value) {
    const uint8_t bytes[2] = {reg, value};
    return i2c_master_transmit(s_waveshare_axp2101, bytes, sizeof(bytes), 1000);
}

static esp_err_t waveshare_axp2101_read(uint8_t reg, uint8_t *value) {
    return i2c_master_transmit_receive(s_waveshare_axp2101, &reg, sizeof(reg), value, 1, 1000);
}

static esp_err_t waveshare_axp2101_init(i2c_master_bus_handle_t bus) {
    const i2c_device_config_t cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = WAVESHARE_PERIPHERAL_PMIC_ADDRESS,
        .scl_speed_hz = 100000,
    };
    ESP_RETURN_ON_ERROR(i2c_master_bus_add_device(bus, &cfg, &s_waveshare_axp2101),
                        "waveshare_peripheral", "add AXP2101");
    static const uint8_t init[][2] = {
        {0x80, 0x01}, {0x90, 0x01}, {0x91, 0x00}, {0x82, 0x12},
        {0x92, 0x1c}, {0x64, 0x02}, {0x61, 0x02}, {0x62, 0x0a},
        {0x63, 0x01}, {0x22, 0x06}, {0x27, 0x10},
    };
    for (size_t i = 0; i < sizeof(init) / sizeof(init[0]); ++i) {
        ESP_RETURN_ON_ERROR(waveshare_axp2101_write(init[i][0], init[i][1]),
                            "waveshare_peripheral", "AXP2101 reg %02x", init[i][0]);
    }
    return ESP_OK;
}

static esp_err_t waveshare_cst9217_init(i2c_master_bus_handle_t bus) {
    const esp_lcd_touch_config_t touch_cfg = {
        .x_max = WAVESHARE_PERIPHERAL_TOUCH_WIDTH - 1,
        .y_max = WAVESHARE_PERIPHERAL_TOUCH_HEIGHT - 1,
        .rst_gpio_num = WAVESHARE_PERIPHERAL_TOUCH_RESET_GPIO,
        .int_gpio_num = WAVESHARE_PERIPHERAL_TOUCH_IRQ_GPIO,
        .levels = {.reset = 0, .interrupt = 0},
        .flags = {.swap_xy = 0, .mirror_x = 1, .mirror_y = 1},
    };
    esp_lcd_panel_io_i2c_config_t io_cfg = ESP_LCD_TOUCH_IO_I2C_CST9217_CONFIG();
    io_cfg.scl_speed_hz = 400000;
    ESP_RETURN_ON_ERROR(esp_lcd_new_panel_io_i2c(bus, &io_cfg, &s_waveshare_cst9217_io),
                        "waveshare_peripheral", "create CST9217 I2C IO");
    return esp_lcd_touch_new_i2c_cst9217(s_waveshare_cst9217_io, &touch_cfg,
                                         &s_waveshare_cst9217_touch);
}

#define WAVESHARE_QMI8658_REG_WHO_AM_I 0x00
#define WAVESHARE_QMI8658_REG_CTRL1     0x02
#define WAVESHARE_QMI8658_REG_CTRL2     0x03
#define WAVESHARE_QMI8658_REG_CTRL3     0x04
#define WAVESHARE_QMI8658_REG_CTRL7     0x08
#define WAVESHARE_QMI8658_REG_DATA      0x35

static esp_err_t waveshare_qmi8658_write(uint8_t reg, uint8_t value) {
    const uint8_t bytes[2] = {reg, value};
    return i2c_master_transmit(s_waveshare_qmi8658, bytes, sizeof(bytes), 1000);
}

static esp_err_t waveshare_qmi8658_read(uint8_t reg, uint8_t *data, size_t length) {
    return i2c_master_transmit_receive(s_waveshare_qmi8658, &reg, sizeof(reg), data, length, 1000);
}

static esp_err_t waveshare_qmi8658_init(i2c_master_bus_handle_t bus) {
    /* This is a compile-time-only profile test seam.  It models an absent or
     * non-responsive optional IMU before a private I2C handle is acquired;
     * production images compile it to false and provide no runtime control. */
    if (provisioning_failure_injection_waveshare_qmi8658_init_fails()) {
        ESP_LOGW("waveshare_peripheral",
                 "test injection: forcing optional QMI8658 initialization failure");
        return ESP_ERR_NOT_FOUND;
    }
    const i2c_device_config_t cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = WAVESHARE_PERIPHERAL_QMI8658_ADDRESS,
        .scl_speed_hz = 400000,
    };
    ESP_RETURN_ON_ERROR(i2c_master_bus_add_device(bus, &cfg, &s_waveshare_qmi8658),
                        "waveshare_peripheral", "add QMI8658");
    uint8_t who_am_i = 0;
    ESP_RETURN_ON_ERROR(waveshare_qmi8658_read(WAVESHARE_QMI8658_REG_WHO_AM_I,
                                                &who_am_i, 1),
                        "waveshare_peripheral", "read QMI8658 identity");
    if (who_am_i != WAVESHARE_PERIPHERAL_QMI8658_WHO_AM_I) return ESP_ERR_NOT_FOUND;
    ESP_RETURN_ON_ERROR(waveshare_qmi8658_write(WAVESHARE_QMI8658_REG_CTRL1, 0x40),
                        "waveshare_peripheral", "QMI8658 CTRL1");
    ESP_RETURN_ON_ERROR(waveshare_qmi8658_write(WAVESHARE_QMI8658_REG_CTRL2, 0x26),
                        "waveshare_peripheral", "QMI8658 CTRL2");
    ESP_RETURN_ON_ERROR(waveshare_qmi8658_write(WAVESHARE_QMI8658_REG_CTRL3, 0x66),
                        "waveshare_peripheral", "QMI8658 CTRL3");
    ESP_RETURN_ON_ERROR(waveshare_qmi8658_write(WAVESHARE_QMI8658_REG_CTRL7, 0x03),
                        "waveshare_peripheral", "QMI8658 CTRL7");
    return ESP_OK;
}

static esp_err_t waveshare_peripheral_init(i2c_master_bus_handle_t bus) {
    if (!bus) return ESP_ERR_INVALID_ARG;
    ESP_RETURN_ON_ERROR(waveshare_axp2101_init(bus), "waveshare_peripheral", "PMIC init");
    ESP_RETURN_ON_ERROR(waveshare_cst9217_init(bus), "waveshare_peripheral", "touch init");
    /* The QMI8658 contributes only the optional Motion HAL.  A missing,
     * mismatched or transiently unavailable IMU must not turn a usable
     * touch/display/audio device into a startup failure: higher layers already
     * project no sample as Motion unavailable and Fall Detection remains an
     * optional consumer.  PMIC and touch stay fail-closed because they are
     * required by this physical profile's common-device baseline. */
    esp_err_t imu_err = waveshare_qmi8658_init(bus);
    if (imu_err != ESP_OK) {
        ESP_LOGW("waveshare_peripheral", "optional QMI8658 unavailable: %s; Motion HAL disabled",
                 esp_err_to_name(imu_err));
        if (s_waveshare_qmi8658) {
            (void)i2c_master_bus_rm_device(s_waveshare_qmi8658);
            s_waveshare_qmi8658 = NULL;
        }
    }
    return ESP_OK;
}

static esp_err_t waveshare_peripheral_deinit(void) {
    esp_err_t result = ESP_OK;
    if (s_waveshare_cst9217_touch) {
        const esp_err_t err = esp_lcd_touch_del(s_waveshare_cst9217_touch);
        if (result == ESP_OK && err != ESP_OK) result = err;
        s_waveshare_cst9217_touch = NULL;
    }
    if (s_waveshare_cst9217_io) {
        const esp_err_t err = esp_lcd_panel_io_del(s_waveshare_cst9217_io);
        if (result == ESP_OK && err != ESP_OK) result = err;
        s_waveshare_cst9217_io = NULL;
    }
    if (s_waveshare_axp2101) {
        const esp_err_t err = i2c_master_bus_rm_device(s_waveshare_axp2101);
        if (result == ESP_OK && err != ESP_OK) result = err;
        s_waveshare_axp2101 = NULL;
    }
    if (s_waveshare_qmi8658) {
        const esp_err_t err = i2c_master_bus_rm_device(s_waveshare_qmi8658);
        if (result == ESP_OK && err != ESP_OK) result = err;
        s_waveshare_qmi8658 = NULL;
    }
    return result;
}

static bool waveshare_peripheral_touch_read(bool *pressed) {
    if (pressed) *pressed = false;
    if (!s_waveshare_cst9217_touch) return false;
    if (esp_lcd_touch_read_data(s_waveshare_cst9217_touch) != ESP_OK) return false;
    esp_lcd_touch_point_data_t point = {0};
    uint8_t points = 0;
    const bool down = esp_lcd_touch_get_data(s_waveshare_cst9217_touch, &point, &points, 1) == ESP_OK;
    if (pressed) *pressed = down && points != 0;
    return true;
}

static bool waveshare_peripheral_touch_ready(void) {
    return s_waveshare_cst9217_touch != NULL;
}

/* CST9217 does not expose a usable native double-tap register here, so its
 * gesture byte remains zero and the common timing classifier owns that policy. */
static esp_err_t round_peripheral_adapter_initialize(i2c_master_bus_handle_t bus) {
    return waveshare_peripheral_init(bus);
}

static esp_err_t round_peripheral_adapter_release(void) {
    return waveshare_peripheral_deinit();
}

static bool round_peripheral_adapter_touch_read(bool *pressed, uint8_t *gesture) {
    if (gesture) *gesture = 0;
    return waveshare_peripheral_touch_read(pressed);
}

/* CST9217 on this profile exposes no stable controller-native double-tap
 * indication. The common timing classifier remains the sole producer. */
static bool round_peripheral_adapter_touch_is_native_double_tap(uint8_t gesture) {
    (void)gesture;
    return false;
}

static bool round_peripheral_adapter_touch_ready(void) {
    return waveshare_peripheral_touch_ready();
}


static bool waveshare_peripheral_power_get(unsigned *level_percent, bool *charging) {
    if (!s_waveshare_axp2101) return false;
    uint8_t capacity = 0;
    uint8_t state = 0;
    if (waveshare_axp2101_read(0xA4, &capacity) != ESP_OK ||
        waveshare_axp2101_read(0x00, &state) != ESP_OK) return false;
    if (level_percent) *level_percent = capacity > 100 ? 100 : capacity;
    if (charging) *charging = ((state >> 5) & 0x03u) == 1u;
    return true;
}

static int16_t waveshare_qmi8658_decode_i16(const uint8_t *data) {
    return (int16_t)((uint16_t)data[0] | ((uint16_t)data[1] << 8));
}

static esp_err_t waveshare_peripheral_motion_get(device_motion_sample_t *out_sample) {
    if (!out_sample) return ESP_ERR_INVALID_ARG;
    if (!s_waveshare_qmi8658) return ESP_ERR_NOT_FOUND;
    /* Keep runtime fault semantics at the private adapter seam. The first
     * normalised Motion read is the startup availability probe, so the test
     * artifact lets it pass and then injects exactly one retryable I/O failure
     * for the retained shared consumer. */
    if (provisioning_failure_injection_waveshare_qmi8658_motion_read_fails_once()) {
        ESP_LOGW("waveshare_peripheral",
                 "test injection: forcing one optional QMI8658 motion read failure");
        return ESP_ERR_TIMEOUT;
    }
    uint8_t data[12] = {0};
    ESP_RETURN_ON_ERROR(waveshare_qmi8658_read(WAVESHARE_QMI8658_REG_DATA, data, sizeof(data)),
                        "waveshare_peripheral", "read QMI8658 motion sample");
    const int32_t acceleration_mg_per_lsb_num = 8000;
    const int32_t angular_rate_mdps_per_lsb_num = 1024000;
    out_sample->timestamp_us = (uint64_t)esp_timer_get_time();
    out_sample->acceleration_mg_x = (int32_t)waveshare_qmi8658_decode_i16(&data[0]) * acceleration_mg_per_lsb_num / 32768;
    out_sample->acceleration_mg_y = (int32_t)waveshare_qmi8658_decode_i16(&data[2]) * acceleration_mg_per_lsb_num / 32768;
    out_sample->acceleration_mg_z = (int32_t)waveshare_qmi8658_decode_i16(&data[4]) * acceleration_mg_per_lsb_num / 32768;
    out_sample->angular_rate_mdps_x = (int32_t)waveshare_qmi8658_decode_i16(&data[6]) * angular_rate_mdps_per_lsb_num / 32768;
    out_sample->angular_rate_mdps_y = (int32_t)waveshare_qmi8658_decode_i16(&data[8]) * angular_rate_mdps_per_lsb_num / 32768;
    out_sample->angular_rate_mdps_z = (int32_t)waveshare_qmi8658_decode_i16(&data[10]) * angular_rate_mdps_per_lsb_num / 32768;
    return ESP_OK;
}

/* The shared round board facade asks every profile the same normalized
 * question.  Only this adapter knows that AXP2101 and QMI8658 answer it. */
static bool round_peripheral_adapter_get_power_status(unsigned *level_percent,
                                                       bool *charging) {
    return waveshare_peripheral_power_get(level_percent, charging);
}

static esp_err_t round_peripheral_adapter_get_motion_sample(
    device_motion_sample_t *out_sample) {
    return waveshare_peripheral_motion_get(out_sample);
}
