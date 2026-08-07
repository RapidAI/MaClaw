#include <stdbool.h>
#include <stddef.h>

#include "driver/gpio.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

/*
 * Bread Compact button pin discovery utility.
 *
 * This deliberately performs read-only sampling: it does not call gpio_config,
 * attach interrupts, enable pulls, or drive any pin. That makes it safe to
 * inspect pins which may be connected to board peripherals while avoiding all
 * flash (GPIO26..32), octal PSRAM (GPIO33..37), USB/JTAG, LCD and I2S pins.
 *
 * GPIO0/38/39 are known candidates. The other entries are conservative exposed
 * GPIO candidates that could carry a board revision's second side key.
 */
static const gpio_num_t k_candidates[] = {
    GPIO_NUM_0, GPIO_NUM_1, GPIO_NUM_2, GPIO_NUM_3,
    GPIO_NUM_8, GPIO_NUM_9, GPIO_NUM_10, GPIO_NUM_11,
    GPIO_NUM_12, GPIO_NUM_13, GPIO_NUM_14, GPIO_NUM_17,
    GPIO_NUM_18, GPIO_NUM_38, GPIO_NUM_39,
};

static const char *TAG = "bread_button_diag";

void app_main(void) {
    int levels[sizeof(k_candidates) / sizeof(k_candidates[0])];
    ESP_LOGI(TAG, "BUTTON_DIAG_READY: press each physical button one at a time");
    ESP_LOGI(TAG, "sampling is read-only; GPIO26..37, LCD, I2S, USB/JTAG are excluded");

    for (size_t i = 0; i < sizeof(k_candidates) / sizeof(k_candidates[0]); ++i) {
        levels[i] = gpio_get_level(k_candidates[i]);
        ESP_LOGI(TAG, "BUTTON_DIAG_BASELINE GPIO%d=%d", k_candidates[i], levels[i]);
    }

    while (true) {
        for (size_t i = 0; i < sizeof(k_candidates) / sizeof(k_candidates[0]); ++i) {
            int next = gpio_get_level(k_candidates[i]);
            if (next != levels[i]) {
                ESP_LOGI(TAG, "BUTTON_DIAG_EDGE GPIO%d %d->%d", k_candidates[i], levels[i], next);
                levels[i] = next;
            }
        }
        vTaskDelay(pdMS_TO_TICKS(5));
    }
}
