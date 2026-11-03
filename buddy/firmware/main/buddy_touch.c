#include "buddy_touch.h"

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "esp_timer.h"

#include "buddy_display.h"
#include "buddy_gesture.h"
#include "buddy_i2c.h"
#include "buddy_protocol.h"
#include "buddy_sound.h"
#include "buddy_ui.h"

#include "sdkconfig.h"

/* Short press cycles the task and tells the host. Hold toggles mute.
 * A press that begins while the backlight is dim or off only wakes it. */
static void gesture_contact(buddy_gesture_t *g, bool contact, int64_t now_ms) {
    const bool resting = buddy_display_resting();
    const buddy_gesture_result_t event = buddy_gesture_contact(g, contact, now_ms, resting);
    if (contact) buddy_display_note_activity();
    if (event == BUDDY_GESTURE_HOLD) {
        const bool master = buddy_sound_toggle_master();
        buddy_protocol_send_sound();
        buddy_protocol_send_log("info", master ? "sound on" : "sound off");
    } else if (event == BUDDY_GESTURE_TAP) {
        buddy_ui_on_tap();
        buddy_protocol_send_input("tap", buddy_ui_active_index());
    } else if (event == BUDDY_GESTURE_DOUBLE) {
        buddy_protocol_send_input("confirm", buddy_ui_active_index());
    } else if (event == BUDDY_GESTURE_SWIPE) {
        buddy_ui_on_swipe();
    }
}

#if CONFIG_BUDDY_PANEL_NV3023_240
/* The 240x240 module has one front key and no I2C touch panel: GPIO 11/12 are
 * its 4G modem UART (TX 12 / RX 11). The key is GPIO0, active low, the same
 * strap the module uses as its single button. */
#define BUDDY_KEY_GPIO GPIO_NUM_0

static bool s_key;

bool buddy_touch_available(void) { return false; }
bool buddy_key_available(void) { return s_key; }

static void key_task(void *arg) {
    (void)arg;
    buddy_gesture_t gesture = {0};
    for (;;) {
        const bool contact = gpio_get_level(BUDDY_KEY_GPIO) == 0;
        gesture_contact(&gesture, contact, esp_timer_get_time() / 1000);
        vTaskDelay(pdMS_TO_TICKS(20));
    }
}

esp_err_t buddy_touch_init(void) {
    const gpio_config_t key = {
        .pin_bit_mask = 1ULL << BUDDY_KEY_GPIO,
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    const esp_err_t err = gpio_config(&key);
    if (err != ESP_OK) return err;
    if (xTaskCreate(key_task, "buddy_key", 3072, NULL, 3, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    s_key = true;
    return ESP_OK;
}

#else

#define TOUCH_ADDR 0x15
#define TOUCH_REG_GESTURE 0x01
#define TOUCH_REG_STATUS 0x02
#define TOUCH_REG_POINT 0x03
#define TOUCH_GESTURE_LEFT 0x03
#define TOUCH_GESTURE_RIGHT 0x04

static i2c_master_dev_handle_t s_dev;
static bool s_available;

bool buddy_touch_available(void) { return s_available; }
bool buddy_key_available(void) { return false; }

static bool read_reg(uint8_t reg, uint8_t *buf, size_t len) {
    if (!s_dev) return false;
    if (i2c_master_transmit(s_dev, &reg, 1, 50) != ESP_OK) return false;
    return i2c_master_receive(s_dev, buf, len, 50) == ESP_OK;
}

static void touch_task(void *arg) {
    (void)arg;
    buddy_gesture_t gesture = {0};
    uint8_t gesture_at_down = 0;
    bool gesture_known = false;
    for (;;) {
        /* Gesture, finger count, and the point are one block. Separate reads
         * can tear and turn one press into a tap plus a false swipe. */
        uint8_t raw[8] = {0};
        const bool got = read_reg(TOUCH_REG_GESTURE, raw, sizeof(raw));
        const uint8_t fingers = got ? raw[1] : 0;
        const bool contact = got && fingers > 0 && fingers < 6;
        if (contact && !gesture.down) {
            gesture_at_down = raw[0];
            gesture_known = true;
        }
        if (contact) {
            const int x = ((raw[2] & 0x0Fu) << 8) | raw[3];
            const int y = ((raw[4] & 0x0Fu) << 8) | raw[5];
            buddy_gesture_track(&gesture, x, y);
        } else if (gesture.down) {
            /* 0x01 keeps the previous slide until this press replaces it.
             * A missed read must not inherit that and pin the carousel. */
            if (gesture_known && got && raw[0] != gesture_at_down &&
                (raw[0] == TOUCH_GESTURE_LEFT || raw[0] == TOUCH_GESTURE_RIGHT)) {
                buddy_gesture_mark_swipe(&gesture);
            }
            gesture_known = false;
        }
        gesture_contact(&gesture, contact, esp_timer_get_time() / 1000);
        vTaskDelay(pdMS_TO_TICKS(60));
    }
}

esp_err_t buddy_touch_init(void) {
    i2c_master_bus_handle_t bus = buddy_i2c_bus();
    if (!bus) return ESP_ERR_INVALID_STATE;

    const i2c_device_config_t dev_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = TOUCH_ADDR,
        // iot-agentos drives this same CST816 on this board at 100 kHz; at
        // 400 kHz the part never acknowledged here.
        .scl_speed_hz = 100000,
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
