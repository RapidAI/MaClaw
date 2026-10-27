#include "buddy_state.h"

#include <string.h>

#include "esp_timer.h"
#include "nvs.h"

#include "buddy_protocol_generated.h"

static SemaphoreHandle_t s_lock;
static buddy_view_t s_view;

static int64_t now_ms(void) {
    return esp_timer_get_time() / 1000;
}

void buddy_state_init(void) {
    if (!s_lock) {
        s_lock = xSemaphoreCreateMutex();
    }
    memset(&s_view, 0, sizeof(s_view));
    // Start offline: without a host frame the standby page must already say
    // "not connected" instead of claiming a live link for the first 10s.
    s_view.last_host_ms = now_ms() - BUDDY_OFFLINE_MS;
    s_view.online = false;
    s_view.missing_glyphs = 0;
}

void buddy_state_lock(void) {
    if (s_lock) {
        xSemaphoreTake(s_lock, portMAX_DELAY);
    }
}

void buddy_state_unlock(void) {
    if (s_lock) {
        xSemaphoreGive(s_lock);
    }
}

void buddy_state_get(buddy_view_t *out) {
    if (!out) return;
    buddy_state_lock();
    *out = s_view;
    buddy_state_unlock();
}

void buddy_state_note_host(void) {
    buddy_state_lock();
    s_view.last_host_ms = now_ms();
    s_view.online = true;
    buddy_state_unlock();
}

void buddy_state_apply_state(const buddy_counts_t *counts, const buddy_task_t *current,
                             int current_count, bool stale, uint64_t seq) {
    if (!counts) return;
    buddy_state_lock();
    s_view.counts = *counts;
    if (current_count < 0) current_count = 0;
    if (current_count > BUDDY_MAX_TASKS) current_count = BUDDY_MAX_TASKS;
    s_view.current_count = current_count;
    for (int i = 0; i < current_count; ++i) {
        s_view.current[i] = current[i];
    }
    s_view.stale = stale;
    s_view.has_state = true;
    s_view.state_seq = seq;
    s_view.prompt_active = false;
    s_view.last_host_ms = now_ms();
    s_view.online = true;
    buddy_state_unlock();
}

void buddy_state_apply_weather(const char *summary, int temperature_c, const char *location,
                               bool stale) {
    buddy_state_lock();
    strlcpy(s_view.weather_summary, summary ? summary : "", sizeof(s_view.weather_summary));
    strlcpy(s_view.weather_location, location ? location : "", sizeof(s_view.weather_location));
    s_view.weather_temp = temperature_c;
    s_view.weather_stale = stale;
    s_view.has_weather = s_view.weather_summary[0] != '\0';
    s_view.last_host_ms = now_ms();
    s_view.online = true;
    buddy_state_unlock();
}

void buddy_state_apply_clock(const char *date, const char *weekday) {
    buddy_state_lock();
    strlcpy(s_view.date, date ? date : "", sizeof(s_view.date));
    strlcpy(s_view.weekday, weekday ? weekday : "", sizeof(s_view.weekday));
    s_view.has_clock = s_view.date[0] != '\0';
    s_view.last_host_ms = now_ms();
    s_view.online = true;
    buddy_state_unlock();
}

void buddy_state_apply_celebrate(const char *title, int seconds) {
    buddy_state_lock();
    strlcpy(s_view.celebrate_title, title ? title : "", sizeof(s_view.celebrate_title));
    s_view.celebrate_active = s_view.celebrate_title[0] != '\0';
    s_view.celebrate_start_ms = now_ms();
    if (seconds < 2) seconds = 2;
    if (seconds > 10) seconds = 10;
    s_view.celebrate_seconds = seconds;
    s_view.last_host_ms = now_ms();
    s_view.online = true;
    buddy_state_unlock();
}

void buddy_state_set_prompt(const char *text) {
    buddy_state_lock();
    strlcpy(s_view.prompt, text ? text : "", sizeof(s_view.prompt));
    s_view.prompt_active = true;
    buddy_state_unlock();
}

void buddy_state_clear_prompt(void) {
    buddy_state_lock();
    s_view.prompt_active = false;
    buddy_state_unlock();
}

void buddy_state_add_missing(int count) {
    if (count <= 0) return;
    buddy_state_lock();
    s_view.missing_glyphs += count;
    buddy_state_unlock();
}

void buddy_state_tick(void) {
    const int64_t now = now_ms();
    buddy_state_lock();
    s_view.online = (now - s_view.last_host_ms) < BUDDY_OFFLINE_MS;
    if (s_view.has_state && (now - s_view.last_host_ms) >= BUDDY_STALE_MS) {
        s_view.stale = true;
    }
    if (s_view.has_weather && (now - s_view.last_host_ms) >= BUDDY_STALE_MS) {
        s_view.weather_stale = true;
    }
    if (s_view.celebrate_active &&
        (now - s_view.celebrate_start_ms) >= (int64_t)s_view.celebrate_seconds * 1000) {
        s_view.celebrate_active = false;
    }
    buddy_state_unlock();
}

typedef struct {
    char summary[BUDDY_LABEL_BYTES + 1];
    int temperature_c;
    char location[BUDDY_LABEL_BYTES + 1];
} buddy_weather_nvs_t;

bool buddy_state_weather_load(void) {
    nvs_handle_t h;
    if (nvs_open("buddy", NVS_READONLY, &h) != ESP_OK) return false;
    buddy_weather_nvs_t saved;
    size_t len = sizeof(saved);
    esp_err_t err = nvs_get_blob(h, "weather", &saved, &len);
    nvs_close(h);
    if (err != ESP_OK || len != sizeof(saved) || saved.summary[0] == '\0') return false;
    saved.summary[sizeof(saved.summary) - 1] = '\0';
    saved.location[sizeof(saved.location) - 1] = '\0';
    buddy_state_lock();
    strlcpy(s_view.weather_summary, saved.summary, sizeof(s_view.weather_summary));
    strlcpy(s_view.weather_location, saved.location, sizeof(s_view.weather_location));
    s_view.weather_temp = saved.temperature_c;
    s_view.has_weather = true;
    s_view.weather_stale = true;
    buddy_state_unlock();
    return true;
}

void buddy_state_weather_save(void) {
    buddy_weather_nvs_t saved;
    buddy_state_lock();
    memset(&saved, 0, sizeof(saved));
    strlcpy(saved.summary, s_view.weather_summary, sizeof(saved.summary));
    strlcpy(saved.location, s_view.weather_location, sizeof(saved.location));
    saved.temperature_c = s_view.weather_temp;
    buddy_state_unlock();
    if (saved.summary[0] == '\0') return;

    nvs_handle_t h;
    if (nvs_open("buddy", NVS_READWRITE, &h) != ESP_OK) return;
    (void)nvs_set_blob(h, "weather", &saved, sizeof(saved));
    (void)nvs_commit(h);
    nvs_close(h);
}

uint32_t buddy_state_uptime_s(void) {
    return (uint32_t)(esp_timer_get_time() / 1000000);
}
