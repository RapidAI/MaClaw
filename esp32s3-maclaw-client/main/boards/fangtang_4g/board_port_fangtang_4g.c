/*
 * Fangtang-4G board-adapter transition unit.
 *
 * CMake selects this translation unit only for the Fangtang profile.  It is
 * intentionally a one-owner bridge while the legacy Bread/Fangtang combined
 * adapter is dismantled: including the legacy implementation keeps every
 * physical side effect in exactly one translation unit, so there is no second
 * display, audio, GPIO scanner, or power-sampling owner during the cutover.
 *
 * Move Fangtang-specific NV3023, GPIO0, ML307 presentation and battery code
 * from ../../board_port_bread_compact.c here in independently verified
 * increments.  Do not add Fangtang-only business policy to this file; shared
 * policy continues above the Device API boundary.
 */

#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
#error "Fangtang adapter may only be compiled for CONFIG_MACLAW_BOARD_FANGTANG_4G"
#endif

/* The compact legacy adapter still owns shared direct-I2S and common scene
 * mechanics during this transition.  Do not let it instantiate Fangtang's
 * modem-enable operation: the physical guard/power sequence has moved below
 * into this profile adapter, while the application continues to use only the
 * hardware-neutral Device API. */
#define MACLAW_FANGTANG_EXTERNAL_CELLULAR_PREPARATION 1
#define MACLAW_FANGTANG_EXTERNAL_BOOT_SELECTOR 1
#include "../../board_port_bread_compact.c"
#undef MACLAW_FANGTANG_EXTERNAL_CELLULAR_PREPARATION
#undef MACLAW_FANGTANG_EXTERNAL_BOOT_SELECTOR

/* Runs synchronously before the legacy compact scanner is created.  The
 * selector therefore owns GPIO0 for this bounded interval and hands it off at
 * a quiescent point, rather than making a second scanner race normal input. */
static bool s_fangtang_boot_toggle_latched;

void fangtang_board_run_boot_network_selector(void) {
    const uint32_t window_ms = 1800;
    bool released = gpio_get_level(BUTTON_ACTIVATE) != 0;
    bool previous_released = released;
    int64_t pressed_at = 0;
    int64_t first_release_at = 0;
    const int64_t deadline = esp_timer_get_time() + (int64_t)window_ms * 1000;
    s_fangtang_boot_toggle_latched = false;
    ESP_LOGI("fangtang_port", "GPIO0 startup network selector active for %u ms",
             (unsigned)window_ms);
    while (esp_timer_get_time() < deadline && !s_fangtang_boot_toggle_latched) {
        int64_t now = esp_timer_get_time();
        released = gpio_get_level(BUTTON_ACTIVATE) != 0;
        if (previous_released && !released) {
            pressed_at = now;
        } else if (!previous_released && released && pressed_at) {
            const int64_t duration = now - pressed_at;
            pressed_at = 0;
            if (duration < 2500000) {
                if (first_release_at && now - first_release_at <= 500000) {
                    s_fangtang_boot_toggle_latched = true;
                    break;
                }
                first_release_at = now;
            } else {
                first_release_at = 0;
            }
        }
        if (first_release_at && now - first_release_at > 500000) {
            first_release_at = 0;
        }
        previous_released = released;
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    ESP_LOGI("fangtang_port", "GPIO0 startup network selector closed: %s",
             s_fangtang_boot_toggle_latched ? "toggle" : "unchanged");
}

bool board_port_wait_for_boot_network_toggle(uint32_t window_ms) {
    (void)window_ms;
    const bool requested = s_fangtang_boot_toggle_latched;
    s_fangtang_boot_toggle_latched = false;
    return requested;
}

esp_err_t board_port_prepare_cellular_transport(void) {
    if (CONFIG_MACLAW_FANGTANG_MODEM_UART_TX_GPIO < 0 ||
        CONFIG_MACLAW_FANGTANG_MODEM_UART_RX_GPIO < 0) {
        return ESP_ERR_INVALID_ARG;
    }
    if (CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO >= 0) {
        gpio_config_t guard = {
            .pin_bit_mask = 1ULL << CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO,
            .mode = GPIO_MODE_OUTPUT,
            .pull_down_en = GPIO_PULLDOWN_ENABLE,
        };
        esp_err_t err = gpio_config(&guard);
        if (err != ESP_OK) return err;
        err = gpio_set_level(CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO,
                             CONFIG_MACLAW_FANGTANG_MODEM_GUARD_LEVEL);
        if (err != ESP_OK) return err;
    }
    if (CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO >= 0) {
        gpio_config_t power = {
            .pin_bit_mask = 1ULL << CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO,
            .mode = GPIO_MODE_OUTPUT,
        };
        esp_err_t err = gpio_config(&power);
        if (err != ESP_OK) return err;
        err = gpio_set_level(CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO,
                             CONFIG_MACLAW_FANGTANG_MODEM_POWER_ACTIVE_LEVEL);
        if (err != ESP_OK) return err;
        vTaskDelay(pdMS_TO_TICKS(500));
    }
    return ESP_OK;
}
