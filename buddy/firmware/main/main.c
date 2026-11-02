#include <string.h>

#include "esp_err.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs_flash.h"

#include "sdkconfig.h"

#include "buddy_display.h"
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

    const esp_err_t touch_err = buddy_touch_init();
    const esp_err_t sound_err = buddy_sound_init();

    ESP_ERROR_CHECK(buddy_protocol_start());

    if (touch_err != ESP_OK) {
#if CONFIG_BUDDY_PANEL_NV3023_240
        buddy_protocol_send_log("warn", "front key unavailable");
#else
        buddy_protocol_send_log("warn", "touch controller unavailable");
#endif
    }
    if (sound_err != ESP_OK) {
        buddy_protocol_send_log("warn", "sound codec unavailable");
    }
    if (!pet_buffer) {
        buddy_protocol_send_log("warn", "pet asset buffer unavailable");
    }
    buddy_protocol_send_log("info", "buddy firmware started");

    if (xTaskCreate(ui_task, "buddy_ui", 6144, NULL, 5, NULL) != pdPASS) {
        buddy_protocol_send_log("error", "ui task creation failed");
    }
}
