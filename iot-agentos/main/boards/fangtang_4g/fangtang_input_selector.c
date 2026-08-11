/* Fangtang's pre-scanner GPIO0 ownership window. */

#include "fangtang_input_adapter.h"

#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

static bool s_fangtang_boot_toggle_latched;

void compact_input_adapter_run_startup_selector(void) {
    const uint32_t window_ms = FANGTANG_INPUT_BOOT_SELECTOR_WINDOW_MS;
    bool released = gpio_get_level(FANGTANG_INPUT_ACTIVATE_GPIO) ==
                    FANGTANG_INPUT_RELEASED_LEVEL;
    bool previous_released = released;
    int64_t pressed_at = 0;
    int64_t first_release_at = 0;
    const int64_t deadline = esp_timer_get_time() + (int64_t)window_ms * 1000;
    s_fangtang_boot_toggle_latched = false;
    ESP_LOGI("fangtang_input", "GPIO0 startup selector active for %u ms", (unsigned)window_ms);
    while (esp_timer_get_time() < deadline && !s_fangtang_boot_toggle_latched) {
        const int64_t now = esp_timer_get_time();
        released = gpio_get_level(FANGTANG_INPUT_ACTIVATE_GPIO) ==
                   FANGTANG_INPUT_RELEASED_LEVEL;
        if (previous_released && !released) {
            pressed_at = now;
        } else if (!previous_released && released && pressed_at) {
            const int64_t duration = now - pressed_at;
            pressed_at = 0;
            if (duration < FANGTANG_INPUT_LONG_PRESS_US) {
                if (first_release_at && now - first_release_at <= FANGTANG_INPUT_DOUBLE_CLICK_US) {
                    s_fangtang_boot_toggle_latched = true;
                    break;
                }
                first_release_at = now;
            } else {
                first_release_at = 0;
            }
        }
        if (first_release_at && now - first_release_at > FANGTANG_INPUT_DOUBLE_CLICK_US) {
            first_release_at = 0;
        }
        previous_released = released;
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    ESP_LOGI("fangtang_input", "GPIO0 startup selector closed: %s",
             s_fangtang_boot_toggle_latched ? "toggle" : "unchanged");
}

bool compact_input_adapter_consume_startup_selector_result(uint32_t window_ms) {
    (void)window_ms;
    const bool requested = s_fangtang_boot_toggle_latched;
    s_fangtang_boot_toggle_latched = false;
    return requested;
}
