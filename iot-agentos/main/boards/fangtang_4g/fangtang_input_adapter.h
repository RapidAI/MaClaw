/* Fangtang-4G physical input profile.
 *
 * The shared compact scanner owns normalized gestures and publishes only
 * Device Input actions.  This header owns the electrical contract of the
 * one physical control and the bounded startup selector timing, so adding a
 * different single-key product does not require hard-coding its GPIO/timing
 * into shared business or input-service code.
 */
#pragma once

#include "driver/gpio.h"
#include "esp_err.h"

#define FANGTANG_INPUT_ACTIVATE_GPIO GPIO_NUM_0
#define FANGTANG_INPUT_RELEASED_LEVEL 1
#define FANGTANG_INPUT_DEBOUNCE_US 25000LL
#define FANGTANG_INPUT_LONG_PRESS_US 2500000LL
#define FANGTANG_INPUT_DOUBLE_CLICK_US 500000LL

#define FANGTANG_INPUT_BOOT_SELECTOR_WINDOW_MS 1800u
#define COMPACT_INPUT_RESPONSE_PAGING_USES_VOLUME_KEYS false

static inline bool fangtang_input_activate_is_released(int level) {
    return level == FANGTANG_INPUT_RELEASED_LEVEL;
}

static inline esp_err_t fangtang_input_init(void) {
    const gpio_config_t key = {
        .pin_bit_mask = 1ULL << FANGTANG_INPUT_ACTIVATE_GPIO,
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
    };
    return gpio_config(&key);
}

/* Must stay source-compatible with Bread's selected compact input adapter.
 * The shared scanner sees only an active contact plus optional volume-key
 * facts, never the Fangtang GPIO0 polarity or its missing controls. */
typedef struct {
    bool activate_released;
    bool volume_up_released;
    bool volume_down_released;
} compact_input_raw_state_t;

static inline esp_err_t compact_input_adapter_init(void) {
    return fangtang_input_init();
}

static inline void compact_input_adapter_read_raw(compact_input_raw_state_t *out_state) {
    if (!out_state) return;
    out_state->activate_released = fangtang_input_activate_is_released(
        gpio_get_level(FANGTANG_INPUT_ACTIVATE_GPIO));
    out_state->volume_up_released = true;
    out_state->volume_down_released = true;
}

static inline bool compact_input_adapter_has_volume_keys(void) {
    return false;
}

static inline int64_t compact_input_adapter_activate_debounce_us(void) {
    return FANGTANG_INPUT_DEBOUNCE_US;
}

static inline int64_t compact_input_adapter_volume_debounce_us(void) {
    return 0;
}

static inline int64_t compact_input_adapter_long_press_us(void) {
    return FANGTANG_INPUT_LONG_PRESS_US;
}

static inline int64_t compact_input_adapter_double_click_us(void) {
    return FANGTANG_INPUT_DOUBLE_CLICK_US;
}

static inline const char *compact_input_adapter_name(void) {
    return "fangtang-primary-control";
}

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

/* Gesture classification and stop/join remain in the shared input scanner.
 * Fangtang owns only the scanner worker's scheduling footprint. */
static inline BaseType_t compact_input_adapter_start_scan_task(
    TaskFunction_t entry, TaskHandle_t *out_task) {
    if (!entry || !out_task) return pdFAIL;
    return xTaskCreate(entry, "maclaw_fangtang_input", 3072, NULL, 4, out_task);
}

/* The one-key Fangtang enclosure reserves GPIO0 for a bounded, pre-scanner
 * double-click transport selector.  Its electrical timing and implementation
 * remain in the Fangtang profile translation unit; shared startup only asks
 * the selected input adapter to run any such pre-scan ownership window. */
void compact_input_adapter_run_startup_selector(void);
bool compact_input_adapter_consume_startup_selector_result(uint32_t window_ms);

/* Response paging is an input-capability fact. Shared response policy chooses
 * the page count and return action; this profile declares its available control. */
static inline bool compact_input_adapter_response_paging_uses_volume_keys(void) {
    return COMPACT_INPUT_RESPONSE_PAGING_USES_VOLUME_KEYS;
}
