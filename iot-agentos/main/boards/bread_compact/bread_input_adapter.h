/* Bread Compact physical input profile.
 *
 * The shared compact scanner classifies presses and publishes normalized
 * Device Input actions.  This adapter contains only the electrical facts of
 * the three keys and their debounce timing.
 */
#pragma once

#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD
#error "Bread input adapter may only be included by the Bread Compact profile"
#endif

#ifndef MACLAW_COMPACT_INPUT_ADAPTER_IMPLEMENTATION
#error "Bread input adapter is owned exclusively by compact_input_service.c"
#endif

#include "driver/gpio.h"
#include "esp_err.h"

#define BREAD_INPUT_ACTIVATE_GPIO GPIO_NUM_0
#define BREAD_INPUT_VOLUME_UP_GPIO GPIO_NUM_38
/* GPIO37 is reserved by OPI PSRAM; GPIO39 is the factory second user key. */
#define BREAD_INPUT_VOLUME_DOWN_GPIO GPIO_NUM_39

#define BREAD_INPUT_DEBOUNCE_US 25000LL
#define BREAD_INPUT_VOLUME_DEBOUNCE_US 30000LL
#define BREAD_INPUT_LONG_PRESS_US 2500000LL
#define BREAD_INPUT_DOUBLE_CLICK_US 500000LL
#define COMPACT_INPUT_RESPONSE_PAGING_USES_VOLUME_KEYS true

static inline bool bread_input_activate_is_released(int level) {
    return level != 0;
}

static inline esp_err_t bread_input_init(void) {
    const gpio_config_t keys = {
        .pin_bit_mask = (1ULL << BREAD_INPUT_ACTIVATE_GPIO) |
                        (1ULL << BREAD_INPUT_VOLUME_UP_GPIO) |
                        (1ULL << BREAD_INPUT_VOLUME_DOWN_GPIO),
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
    };
    return gpio_config(&keys);
}

/* The shared compact scanner classifies normalized contacts and gestures; it
 * must not learn the number, polarity or GPIO identity of this enclosure's
 * controls.  Keep raw sampling and the one physical initialization contract
 * in the profile adapter. */
static inline esp_err_t compact_input_adapter_init(void) {
    return bread_input_init();
}

static inline void compact_input_adapter_read_raw(compact_input_raw_state_t *out_state) {
    if (!out_state) return;
    out_state->activate_released = bread_input_activate_is_released(
        gpio_get_level(BREAD_INPUT_ACTIVATE_GPIO));
    out_state->volume_up_released = gpio_get_level(BREAD_INPUT_VOLUME_UP_GPIO) != 0;
    out_state->volume_down_released = gpio_get_level(BREAD_INPUT_VOLUME_DOWN_GPIO) != 0;
}

static inline bool compact_input_adapter_has_volume_keys(void) {
    return true;
}

static inline bool compact_input_adapter_activate_is_released_now(void) {
    return bread_input_activate_is_released(gpio_get_level(BREAD_INPUT_ACTIVATE_GPIO));
}

static inline int64_t compact_input_adapter_activate_debounce_us(void) {
    return BREAD_INPUT_DEBOUNCE_US;
}

static inline int64_t compact_input_adapter_volume_debounce_us(void) {
    return BREAD_INPUT_VOLUME_DEBOUNCE_US;
}

static inline int64_t compact_input_adapter_long_press_us(void) {
    return BREAD_INPUT_LONG_PRESS_US;
}

static inline int64_t compact_input_adapter_double_click_us(void) {
    return BREAD_INPUT_DOUBLE_CLICK_US;
}

static inline const char *compact_input_adapter_name(void) {
    return "bread-controls";
}

#include "freertos/task.h"

/* Gesture classification and stop/join remain in the shared input scanner.
 * Bread owns only the scanner worker's scheduling footprint. */
static inline BaseType_t compact_input_adapter_start_scan_task(
    TaskFunction_t entry, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreate(entry, "maclaw_bread_input", 3072, NULL, 4, out_task);
}

/* Bread has no boot-time alternate transport selector.  The common startup
 * sequence still invokes this neutral adapter hook so it never needs a board
 * model conditional merely to preserve Fangtang's bounded GPIO0 window. */
static inline uint32_t compact_input_adapter_startup_selector_window_ms(void) {
    return 0;
}

/* Response paging is an input-capability fact. Shared response policy chooses
 * the page count and return action; this profile declares its available control. */
static inline bool compact_input_adapter_response_paging_uses_volume_keys(void) {
    return COMPACT_INPUT_RESPONSE_PAGING_USES_VOLUME_KEYS;
}
