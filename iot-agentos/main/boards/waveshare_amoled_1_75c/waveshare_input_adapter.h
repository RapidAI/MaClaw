#pragma once

/* Waveshare's physical activation-key contract.  Do not include the PMIC,
 * touch or IMU adapter here: their I2C lifetime remains owned by Audio. */

#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C
#error "Waveshare input adapter selected for a non-Waveshare build"
#endif

#include "boards/round_input_profile.h"
#include "device_api.h"
#include "driver/gpio.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define WAVESHARE_INPUT_ACTIVATE_KEY_GPIO GPIO_NUM_0

static inline const round_input_profile_t *waveshare_input_profile(void) {
    static const round_input_profile_t profile = {
        .debounce_ms = 25, .scan_poll_ms = 15,
        .double_tap_window_ms = 500, .long_hold_ms = 2500,
        .local_volume_increase_hold_ms = 1200,
        .local_volume_decrease_hold_ms = 1800,
        .touch_regular_min_tap_ms = 30, .touch_cancel_min_tap_ms = 15,
        .touch_double_min_gap_ms = 100, .touch_release_drain_ms = 250,
    };
    return &profile;
}

static inline esp_err_t waveshare_input_initialize_activate_key(void) {
    const gpio_config_t config = {
        .pin_bit_mask = 1ULL << WAVESHARE_INPUT_ACTIVATE_KEY_GPIO,
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    return gpio_config(&config);
}

static inline bool waveshare_input_activate_key_pressed(void) {
    return gpio_get_level(WAVESHARE_INPUT_ACTIVATE_KEY_GPIO) == 0;
}

static inline device_input_source_t waveshare_input_resolve_source(bool key_pressed,
                                                                    bool touch_pressed) {
    (void)key_pressed;
    return touch_pressed ? DEVICE_INPUT_SOURCE_TOUCH : DEVICE_INPUT_SOURCE_AUXILIARY_CONTROL;
}

static inline bool waveshare_input_consume_boot_gesture(device_input_action_t action,
                                                         device_input_source_t source) {
    (void)action;
    (void)source;
    return false;
}

static inline BaseType_t waveshare_input_start_scan_task(TaskFunction_t entry,
                                                          TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreate(entry, "maclaw_waveshare_input", 3072, NULL, 4, out_task);
}
