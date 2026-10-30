#include "buddy_touch.h"

#include "driver/i2c_master.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "buddy_display.h"
#include "buddy_i2c.h"

#include "sdkconfig.h"

#if CONFIG_BUDDY_PANEL_NV3023_240
/* The 240x240 module has one front key and no I2C touch panel: GPIO 11/12 are
 * its 4G modem UART (TX 12 / RX 11). Probing 0x15 there would put an I2C master
 * on another peripheral's pins, so this profile reports "no touch" and leaves
 * the bus alone. */
bool buddy_touch_available(void) { return false; }

esp_err_t buddy_touch_init(void) { return ESP_ERR_NOT_SUPPORTED; }

#else

#define TOUCH_ADDR 0x15
#define TOUCH_REG_STATUS 0x02
#define TOUCH_REG_POINT 0x03

static i2c_master_dev_handle_t s_dev;
static bool s_available;

bool buddy_touch_available(void) { return s_available; }

static bool read_reg(uint8_t reg, uint8_t *buf, size_t len) {
    if (!s_dev) return false;
    if (i2c_master_transmit(s_dev, &reg, 1, 50) != ESP_OK) return false;
    return i2c_master_receive(s_dev, buf, len, 50) == ESP_OK;
}

static void touch_task(void *arg) {
    (void)arg;
    uint8_t point[6];
    for (;;) {
        uint8_t fingers = 0;
        if (read_reg(TOUCH_REG_STATUS, &fingers, 1) && fingers > 0 && fingers < 6) {
            if (read_reg(TOUCH_REG_POINT, point, sizeof(point))) {
                buddy_display_note_activity();
            }
        }
        vTaskDelay(pdMS_TO_TICKS(60));
    }
}

esp_err_t buddy_touch_init(void) {
    i2c_master_bus_handle_t bus = buddy_i2c_bus();
    if (!bus) return ESP_ERR_INVALID_STATE;

    const i2c_device_config_t dev_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = TOUCH_ADDR,
        .scl_speed_hz = 400000,
    };
    esp_err_t err = i2c_master_bus_add_device(bus, &dev_cfg, &s_dev);
    if (err != ESP_OK) return err;

    uint8_t probe = 0;
    if (!read_reg(TOUCH_REG_STATUS, &probe, 1)) {
        // Controller absent or wired differently; keep the bus but stop here.
        return ESP_ERR_NOT_FOUND;
    }

    if (xTaskCreate(touch_task, "buddy_touch", 3072, NULL, 3, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    s_available = true;
    return ESP_OK;
}

#endif  // CONFIG_BUDDY_PANEL_NV3023_240
