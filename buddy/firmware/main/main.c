#include <stdio.h>
#include <string.h>

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "esp_err.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs_flash.h"

#include "sdkconfig.h"

#include "buddy_display.h"
#include "buddy_i2c.h"
#include "buddy_pet.h"
#include "buddy_protocol.h"
#include "buddy_sound.h"
#include "buddy_state.h"
#include "buddy_touch.h"
#include "buddy_ui.h"

static void ui_task(void *arg) {
    (void)arg;
    int prev_page = -1;
    for (;;) {
        buddy_state_tick();

        buddy_view_t view;
        buddy_state_get(&view);
        const int page = buddy_ui_page(&view);
        // A running task or an error page is not idle. Counting those frames
        // as activity keeps the backlight up; the idle timer starts once the
        // glass returns to standby.
        if (page == BUDDY_PAGE_TASKS || page == BUDDY_PAGE_PROMPT) {
            buddy_display_note_activity();
        }
        buddy_display_tick();

        // One sound per meaningful page transition. Boot (prev_page < 0) is
        // silent, and returning from the celebration to the task page does not
        // replay the running cue on top of the completion cue.
        if (prev_page >= 0) {
            if (page == BUDDY_PAGE_TASKS && prev_page == BUDDY_PAGE_STANDBY) {
                buddy_sound_event(BUDDY_SOUND_RUNNING);
            } else if (page == BUDDY_PAGE_CELEBRATE && prev_page != BUDDY_PAGE_CELEBRATE) {
                buddy_sound_event(BUDDY_SOUND_COMPLETE);
            } else if (page == BUDDY_PAGE_STANDBY && prev_page == BUDDY_PAGE_TASKS) {
                buddy_sound_event(BUDDY_SOUND_STANDBY);
            }
        }
        prev_page = page;

        // One chime when a new row needs a look. The same id stays quiet.
        // Clearing the row arms the next appearance. This page does not hold
        // the backlight; accepting the state frame already lit it once.
        static char chimed_id[24];
        static char chimed_title[BUDDY_TITLE_BYTES + 1];
        if (view.attention.title[0]) {
            const bool same = view.attention.id[0]
                                  ? strcmp(chimed_id, view.attention.id) == 0
                                  : chimed_id[0] == '\0' &&
                                        strcmp(chimed_title, view.attention.title) == 0;
            if (view.online && !same) {
                buddy_sound_event(BUDDY_SOUND_ATTENTION);
                strlcpy(chimed_id, view.attention.id, sizeof(chimed_id));
                strlcpy(chimed_title, view.attention.title, sizeof(chimed_title));
            }
        } else {
            chimed_id[0] = '\0';
            chimed_title[0] = '\0';
        }

        buddy_ui_render(&view, esp_timer_get_time() / 1000);
        vTaskDelay(pdMS_TO_TICKS(50));
    }
}

#if !CONFIG_BUDDY_PANEL_NV3023_240
// The EchoEar panel reset (GPIO9) is physically shared with the touch
// controller, so a bus sample taken before display init and one taken after
// every client is up separate "the parts were never powered" from "our own
// init knocked them off". Nothing can be reported before the protocol link
// exists, so both samples are captured first and reported together.
#endif

// A seven-address list cannot distinguish "our addresses are wrong" from "no
// device is powered on this bus", and both produce the same two boot
// warnings. Scanning the whole 7-bit range answers it in ~200 ms: any ACK at
// all means the bus is electrically alive and only the addresses are wrong.
static void scan_i2c(char *line, size_t cap) {
#if !CONFIG_BUDDY_PANEL_NV3023_240
    i2c_master_bus_handle_t bus = buddy_i2c_bus();
    line[0] = '\0';
    if (!bus) {
        snprintf(line, cap, "bus unavailable");
        return;
    }
    size_t used = 0;
    int found = 0;
    for (uint8_t addr = 0x08; addr <= 0x77 && used + 4 < cap; ++addr) {
        if (i2c_master_probe(bus, addr, 50) != ESP_OK) continue;
        ++found;
        const int n = snprintf(line + used, cap - used, "%s%02x", found > 1 ? " " : "", addr);
        if (n <= 0) break;
        used += (size_t)n;
    }
    if (found == 0) snprintf(line, cap, "no device answered");
#else
    (void)line;
    (void)cap;
#endif
}

// i2c_master_probe() always clocks at 100 kHz regardless of any device
// config, so it cannot tell "no such part" from "100 kHz does not settle on
// this board's pull-ups". A real device handle at 10 kHz can: if that one
// ACKs, the wiring is fine and the bus is merely too fast.
#if !CONFIG_BUDDY_PANEL_NV3023_240
static bool probe_slow(uint8_t addr, int hz) {
    i2c_master_bus_handle_t bus = buddy_i2c_bus();
    if (!bus) return false;
    i2c_master_dev_handle_t dev = NULL;
    const i2c_device_config_t cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = addr,
        .scl_speed_hz = hz,
    };
    if (i2c_master_bus_add_device(bus, &cfg, &dev) != ESP_OK) return false;
    // A register read, not a write: a present part must ACK the address byte,
    // and a read-only register never turns an absent part into a false pass.
    const uint8_t reg = 0x00;
    uint8_t value = 0;
    const esp_err_t err = i2c_master_transmit_receive(dev, &reg, 1, &value, 1, 200);
    i2c_master_bus_rm_device(dev);
    return err == ESP_OK;
}
#endif

static void report_i2c(const char *tag, const char *line, int scl, int sda) {
#if !CONFIG_BUDDY_PANEL_NV3023_240
    char msg[160];
    snprintf(msg, sizeof(msg), "i2c %s: %s", tag, line);
    buddy_protocol_send_log("info", msg);
    snprintf(msg, sizeof(msg), "i2c %s pins: scl11=%d sda12=%d", tag, scl, sda);
    buddy_protocol_send_log("info", msg);
#else
    (void)tag;
    (void)line;
    (void)scl;
    (void)sda;
#endif
}

void app_main(void) {
    esp_err_t err = nvs_flash_init();
    if (err == ESP_ERR_NVS_NO_FREE_PAGES || err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        err = nvs_flash_init();
    }
    ESP_ERROR_CHECK(err);

    buddy_state_init();
    // A previous run's weather is better than an empty standby screen while
    // the host is still starting.
    buddy_state_weather_load();

    const bool pet_buffer = buddy_pet_init();
    ESP_ERROR_CHECK(buddy_display_init());
    buddy_ui_init();

    // Sampled before any I2C client is claimed: the display init has just
    // toggled GPIO9, the reset line it shares with the touch controller.
    int boot_scl = -1;
    int boot_sda = -1;
    char boot_scan[96] = "";
    scan_i2c(boot_scan, sizeof(boot_scan));
    boot_scl = gpio_get_level(GPIO_NUM_11);
    boot_sda = gpio_get_level(GPIO_NUM_12);

    const esp_err_t touch_err = buddy_touch_init();
    const esp_err_t sound_err = buddy_sound_init();

    ESP_ERROR_CHECK(buddy_protocol_start());

    // Both I2C peripherals report the same boot warning on this board, so the
    // message carries the actual esp_err_t: "touch controller unavailable" alone
    // could not tell a missing part from a bus that never came up.
    char reason[64];
    if (touch_err != ESP_OK) {
#if CONFIG_BUDDY_PANEL_NV3023_240
        snprintf(reason, sizeof(reason), "front key unavailable: %s",
                 esp_err_to_name(touch_err));
#else
        snprintf(reason, sizeof(reason), "touch controller unavailable: %s",
                 esp_err_to_name(touch_err));
#endif
        buddy_protocol_send_log("warn", reason);
    }
    if (sound_err != ESP_OK) {
        snprintf(reason, sizeof(reason), "sound codec unavailable: %s",
                 esp_err_to_name(sound_err));
        buddy_protocol_send_log("warn", reason);
    }
    if (!pet_buffer) {
        buddy_protocol_send_log("warn", "pet asset buffer unavailable");
    }
    buddy_protocol_send_log("info", "buddy firmware started");
    // Second sample, now that both I2C clients ran their own init on the
    // same bus. A part that ACKs here but not at boot means power-up timing;
    // NACK in both samples means the part is not on the bus at all.
    char late_scan[96] = "";
    scan_i2c(late_scan, sizeof(late_scan));
    report_i2c("early", boot_scan, boot_scl, boot_sda);
    report_i2c("late", late_scan, gpio_get_level(GPIO_NUM_11), gpio_get_level(GPIO_NUM_12));
#if !CONFIG_BUDDY_PANEL_NV3023_240
    // Final discriminator: the parts the reference design puts on this bus,
    // re-probed at 10 kHz. ACK here but not at 100 kHz means the wiring is
    // intact and the bus timing is the problem; NACK at both speeds means
    // nothing is driving those addresses.
    char slow[64];
    snprintf(slow, sizeof(slow), "i2c slow 10k: 15=%s 18=%s 40=%s",
             probe_slow(0x15, 10000) ? "ack" : "nack",
             probe_slow(0x18, 10000) ? "ack" : "nack",
             probe_slow(0x40, 10000) ? "ack" : "nack");
    buddy_protocol_send_log("info", slow);
#endif

    if (xTaskCreate(ui_task, "buddy_ui", 6144, NULL, 5, NULL) != pdPASS) {
        buddy_protocol_send_log("error", "ui task creation failed");
    }
}
