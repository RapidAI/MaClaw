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
#include "driver/i2s_std.h"
#include "esp_check.h"
#include "esp_lcd_io_i2c.h"
#include "esp_lcd_touch.h"
#include "esp_lcd_touch_cst9217.h"
#include "esp_timer.h"

#include "device_api.h"

#define WAVESHARE_PERIPHERAL_PMIC_ADDRESS      0x34
#define WAVESHARE_PERIPHERAL_TOUCH_RESET_GPIO  GPIO_NUM_2
#define WAVESHARE_PERIPHERAL_TOUCH_IRQ_GPIO    GPIO_NUM_11
#define WAVESHARE_PERIPHERAL_ACTIVATE_KEY_GPIO GPIO_NUM_0
#define WAVESHARE_PERIPHERAL_TOUCH_WIDTH       466
#define WAVESHARE_PERIPHERAL_TOUCH_HEIGHT      466
#define WAVESHARE_PERIPHERAL_QMI8658_ADDRESS   0x6B
#define WAVESHARE_PERIPHERAL_QMI8658_WHO_AM_I  0x05

/* Codec and I2S wiring are physical facts shared by the PMIC/touch bus on
 * this profile.  The round-board audio state machine continues to own PCM,
 * wake-word and capture/playback session semantics. */
#define WAVESHARE_AUDIO_I2C_SCL GPIO_NUM_14
#define WAVESHARE_AUDIO_I2C_SDA GPIO_NUM_15
#define WAVESHARE_AUDIO_MCLK GPIO_NUM_16
#define WAVESHARE_AUDIO_BCLK GPIO_NUM_9
#define WAVESHARE_AUDIO_WS GPIO_NUM_45
#define WAVESHARE_AUDIO_DOUT GPIO_NUM_8
#define WAVESHARE_AUDIO_DIN GPIO_NUM_10
#define WAVESHARE_AUDIO_PA_ENABLE GPIO_NUM_46
#define WAVESHARE_AUDIO_ES7210_ADDRESS 0x40
#define WAVESHARE_AUDIO_ES8311_ADDRESS 0x18
#define WAVESHARE_AUDIO_ES8311_DAC_MUTE_REG 0x31
#define WAVESHARE_AUDIO_ES8311_DAC_VOLUME_REG 0x32
#define WAVESHARE_AUDIO_OUTPUT_VOLUME_DEFAULT 70
#define WAVESHARE_AUDIO_RATE 16000
#define WAVESHARE_AUDIO_MCLK_MULTIPLE I2S_MCLK_MULTIPLE_256

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
    ESP_RETURN_ON_ERROR(waveshare_qmi8658_init(bus), "waveshare_peripheral", "IMU init");
    return ESP_OK;
}

static void waveshare_peripheral_deinit(void) {
    if (s_waveshare_cst9217_touch) {
        (void)esp_lcd_touch_del(s_waveshare_cst9217_touch);
        s_waveshare_cst9217_touch = NULL;
    }
    if (s_waveshare_cst9217_io) {
        (void)esp_lcd_panel_io_del(s_waveshare_cst9217_io);
        s_waveshare_cst9217_io = NULL;
    }
    if (s_waveshare_axp2101) {
        (void)i2c_master_bus_rm_device(s_waveshare_axp2101);
        s_waveshare_axp2101 = NULL;
    }
    if (s_waveshare_qmi8658) {
        (void)i2c_master_bus_rm_device(s_waveshare_qmi8658);
        s_waveshare_qmi8658 = NULL;
    }
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

/* Normalize the CST9217 result to the shared round-panel gesture adapter.
 * CST9217 does not expose a usable native double-tap register here, so its
 * gesture byte remains zero and the common timing classifier owns that policy. */
static esp_err_t round_touch_adapter_init(i2c_master_bus_handle_t bus) {
    return waveshare_peripheral_init(bus);
}

/* This adapter also brings up the board PMIC and IMU on the shared bus.
 * Treat its failure as a board bring-up failure, preserving the previous
 * Waveshare startup contract. */
static bool round_touch_adapter_init_is_required(void) {
    return true;
}

static void round_touch_adapter_deinit(void) {
    waveshare_peripheral_deinit();
}

static bool round_touch_adapter_read(bool *pressed, uint8_t *gesture) {
    if (gesture) *gesture = 0;
    return waveshare_peripheral_touch_read(pressed);
}

/* CST9217 on this profile exposes no stable controller-native double-tap
 * indication. The common timing classifier remains the sole producer. */
static bool round_touch_adapter_is_native_double_tap(uint8_t gesture) {
    (void)gesture;
    return false;
}

static bool round_touch_adapter_ready(void) {
    return waveshare_peripheral_touch_ready();
}


/* The 1.75C boot/activation key is active-low GPIO0. Its pull and polarity are
 * a profile-local physical contract; the shared round scanner only combines
 * its normalized pressed state with touch and classifies gestures uniformly. */
static esp_err_t round_input_adapter_init_activate_key(void) {
    const gpio_config_t config = {
        .pin_bit_mask = 1ULL << WAVESHARE_PERIPHERAL_ACTIVATE_KEY_GPIO,
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    return gpio_config(&config);
}

/* The shared scanner owns debounce, gesture timing and stop/join semantics.
 * Its task stack and priority are profile runtime choices. */
static BaseType_t round_input_adapter_start_scan_task(
    TaskFunction_t entry, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreate(entry, "maclaw_round_input", 3072, NULL, 4, out_task);
}
static bool round_input_adapter_activate_key_pressed(void) {
    return gpio_get_level(WAVESHARE_PERIPHERAL_ACTIVATE_KEY_GPIO) == 0;
}


/* The 1.75C has no boot-time transport-selector gesture.  Preserve this as a
 * profile-private fact while the shared scanner classifies ordinary input. */
static board_input_source_t round_input_adapter_resolve_source(bool key_pressed,
                                                                bool touch_pressed) {
    (void)key_pressed;
    return touch_pressed ? BOARD_INPUT_SOURCE_TOUCH : BOARD_INPUT_SOURCE_OTHER_KEY;
}

static bool round_input_adapter_consume_boot_gesture(board_input_action_t action,
                                                      board_input_source_t source) {
    (void)action;
    (void)source;
    return false;
}

static void round_input_adapter_begin_boot_window(void) {}

static bool round_input_adapter_wait_for_boot_network_toggle(uint32_t window_ms) {
    (void)window_ms;
    return false;
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
