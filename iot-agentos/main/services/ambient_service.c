#include "services/ambient_service.h"

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "cJSON.h"
#include "esp_err.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "mbedtls/base64.h"

#include "presentation/scene_model.h"
#include "presentation/scene_presenter.h"
#include "task_registry.h"

#define AMBIENT_GLYPH_MAX_PER_MESSAGE 96

_Static_assert(WEATHER_CACHE_SUMMARY_CAPACITY <= SCENE_AMBIENT_WEATHER_CAPACITY,
               "weather summary must fit the ambient scene weather field");
_Static_assert(WEATHER_CACHE_LOCATION_CAPACITY <= SCENE_AMBIENT_LOCATION_CAPACITY,
               "weather location must fit the ambient scene location field");
_Static_assert(SCENE_GLYPH_BITMAP_BYTES == 72u,
               "hub glyph bitmap size must match App UI / presenter");

/* Keep the log tag identical to the original main.c owner so existing
 * ambient/weather trace filters and hardware baseline comparisons stay valid. */
static const char *TAG = "maclaw_client";

#define AMBIENT_ENSURE_MUTEX_WAIT_MS 2000u
#define AMBIENT_LEFTOVER_DRAIN_MS 100u

static portMUX_TYPE s_ambient_lock = portMUX_INITIALIZER_UNLOCKED;
static SemaphoreHandle_t s_lifecycle_mutex;

static weather_cache_snapshot_t s_weather;
static time_t s_display_clock_epoch;
static int64_t s_display_clock_anchor_us;
static bool s_display_clock_valid;
static char s_network_ssid[SCENE_NETWORK_SSID_CAPACITY];
static bool s_network_connected;
static bool s_alarm_scheduled;
static char s_pet_state[SCENE_PET_STATE_CAPACITY];
static char s_pet_skin[SCENE_PET_SKIN_CAPACITY];
static bool s_pet_motion_enabled;

static TaskHandle_t s_ambient_task;
static SemaphoreHandle_t s_ambient_task_stopped;
/* This worker is logically independent of panel/DMA ownership, but it keeps
 * submitting time scenes every second.  Fence it during a future MCU sleep
 * transaction so a late cadence cannot cross the Display safe point. */
/* A task created by FreeRTOS may execute before xTaskCreate returns.  Keep it
 * behind a per-generation start gate until the creator has published both
 * lifecycle handles and registered the POWER owner.  PREPARE treats this
 * brief state as BUSY rather than guessing whether an unpublished submitter
 * needs a later ABORT restart. */
static bool s_ambient_task_starting;
static bool s_system_sleep_preparing;
static bool s_system_sleep_restart_clock;
/* A stopped cadence task may have signalled its completion while it is still
 * removing the immutable Registry entry.  Only that retiring generation may
 * create the ABORT replacement; otherwise a new worker could reuse its
 * completion/Registry identity. */
static bool s_system_sleep_restart_pending;
static bool s_ambient_task_retiring;
/* Completion is not lifecycle retirement.  If the immutable POWER Registry
 * entry cannot be removed, retain a terminal failure so neither normal cadence
 * start nor System Sleep ABORT can create a replacement generation. */
static esp_err_t s_ambient_task_exit_status = ESP_OK;
static bool s_ambient_task_registry_retirement_failed;

static uint32_t remaining_timeout_ms(int64_t deadline_us) {
    const int64_t remaining_us = deadline_us - esp_timer_get_time();
    if (remaining_us <= 0) return 0;
    const uint64_t rounded_ms = ((uint64_t)remaining_us + 999u) / 1000u;
    return rounded_ms > UINT32_MAX ? UINT32_MAX : (uint32_t)rounded_ms;
}

/* Product profiles use 100 Hz ticks.  pdMS_TO_TICKS(1..9) is 0, which turns a
 * join or cadence wait into a poll. */
static TickType_t ticks_for_timeout_ms(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

/* Caller holds s_lifecycle_mutex.  Only recycle when no live worker can still
 * Give this object; wait for that Give before delete so a preempted exit
 * cannot UAF.  A drain timeout leaks rather than deleting under a pending Give. */
static void drain_orphaned_completion_locked(void) {
    SemaphoreHandle_t leftover = NULL;
    taskENTER_CRITICAL(&s_ambient_lock);
    if (s_ambient_task == NULL) {
        leftover = s_ambient_task_stopped;
        s_ambient_task_stopped = NULL;
    }
    taskEXIT_CRITICAL(&s_ambient_lock);
    if (!leftover) return;
    if (xSemaphoreTake(leftover, ticks_for_timeout_ms(AMBIENT_LEFTOVER_DRAIN_MS)) == pdTRUE) {
        vSemaphoreDelete(leftover);
        return;
    }
    ESP_LOGW(TAG, "ambient clock leftover completion retained");
}

static device_status_t status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

static bool snapshot_strings_terminated(const weather_cache_snapshot_t *weather) {
    return weather &&
           memchr(weather->summary, '\0', sizeof(weather->summary)) != NULL &&
           memchr(weather->location, '\0', sizeof(weather->location)) != NULL;
}

static void persist_weather_copy(const weather_cache_snapshot_t *snapshot) {
    device_status_t status = weather_cache_service_save(snapshot);
    if (status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "weather cache save deferred: device status=%d", (int)status);
    }
}

static void refresh_ambient_display(void) {
    time_t system_now = 0;
    time(&system_now);
    int64_t monotonic_us = esp_timer_get_time();
    bool system_clock_ready = system_now >= 1672531200; // 2023-01-01 UTC
    weather_cache_snapshot_t weather;
    bool display_clock_valid;
    time_t display_clock_epoch;
    int64_t display_clock_anchor_us;
    taskENTER_CRITICAL(&s_ambient_lock);
    if (system_clock_ready) {
        time_t predicted = s_display_clock_epoch;
        if (s_display_clock_valid) {
            predicted += (time_t)((monotonic_us - s_display_clock_anchor_us) / 1000000);
        }
        // Accept the initial SNTP value and any later material correction, but
        // otherwise advance only from the local ESP32 monotonic clock.
        if (!s_display_clock_valid || llabs((long long)(system_now - predicted)) > 2) {
            s_display_clock_epoch = system_now;
            s_display_clock_anchor_us = monotonic_us;
            s_display_clock_valid = true;
        }
    }
    display_clock_valid = s_display_clock_valid;
    display_clock_epoch = s_display_clock_epoch;
    display_clock_anchor_us = s_display_clock_anchor_us;
    weather = s_weather;
    taskEXIT_CRITICAL(&s_ambient_lock);
    time_t now = display_clock_valid
                     ? display_clock_epoch + (time_t)((monotonic_us - display_clock_anchor_us) / 1000000)
                     : 0;
    scene_ambient_fields_t fields = {0};
    bool clock_formatted = false;
    if (display_clock_valid) {
        struct tm local = {0};
        if (localtime_r(&now, &local)) {
            static const char *const weekdays[] = {
                "星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"
            };
            unsigned month = (unsigned)(local.tm_mon + 1) % 100u;
            unsigned day = (unsigned)local.tm_mday % 100u;
            snprintf(fields.time, sizeof(fields.time), "%02d:%02d:%02d",
                     local.tm_hour, local.tm_min, local.tm_sec);
            snprintf(fields.date, sizeof(fields.date), "%02u/%02u", month, day);
            if (local.tm_wday >= 0 && local.tm_wday <= 6) {
                strlcpy(fields.weekday, weekdays[local.tm_wday], sizeof(fields.weekday));
            } else {
                strlcpy(fields.weekday, "时间同步中", sizeof(fields.weekday));
            }
            clock_formatted = true;
        }
    }
    if (!clock_formatted) {
        strlcpy(fields.time, "--:--:--", sizeof(fields.time));
        strlcpy(fields.date, "--/--", sizeof(fields.date));
        strlcpy(fields.weekday, "时间同步中", sizeof(fields.weekday));
    }
    int64_t now_ms = (int64_t)now * 1000;
    fields.weather_stale = weather.valid && weather.expires_at_ms > 0 &&
                           now_ms > weather.expires_at_ms;
    strlcpy(fields.location, weather.location, sizeof(fields.location));
    strlcpy(fields.weather, weather.summary, sizeof(fields.weather));
    fields.temperature_c = weather.temperature_c;
    fields.weather_valid = weather.valid;
    scene_presenter_publish_ambient(&fields);
}

static esp_err_t stop_clock_task_locked(uint32_t timeout_ms, const void *expected_task);
static esp_err_t stop_clock_task(uint32_t timeout_ms, const void *expected_task);
static esp_err_t stop_clock_registry_entry(void *context, uint32_t timeout_ms);

static void ambient_task(void *arg) {
    /* The creator owns this gate until it has published s_ambient_task,
     * s_ambient_task_stopped and the Task Registry entry.  Once acquired,
     * this worker is the sole owner and deletes it before any scene submit. */
    SemaphoreHandle_t start_gate = (SemaphoreHandle_t)arg;
    if (!start_gate || xSemaphoreTake(start_gate, portMAX_DELAY) != pdTRUE) {
        /* This is not expected for a binary gate, but do not submit scenes if
         * the creation handshake is corrupt.  The creator retains the normal
         * completion join contract for every successfully-created task. */
        vTaskDelete(NULL);
        return;
    }
    vSemaphoreDelete(start_gate);
    taskENTER_CRITICAL(&s_ambient_lock);
    s_ambient_task_starting = false;
    taskEXIT_CRITICAL(&s_ambient_lock);
    while (true) {
        refresh_ambient_display();
        // Redraw immediately after the next monotonic second boundary rather
        // than drifting with scheduler latency. This keeps the displayed
        // seconds visibly advancing even after the task has been running for
        // a long time.
        int64_t now_us = esp_timer_get_time();
        int64_t wait_us = 1000000 - (now_us % 1000000) + 1000;
        /* The last ~8 ms of a 100 Hz second is 2–9 ms.  Raw pdMS_TO_TICKS
         * becomes 0 and this prio-3 worker busy-spins / double-publishes. */
        if (ulTaskNotifyTake(pdTRUE,
                             ticks_for_timeout_ms((uint32_t)((wait_us + 999) / 1000))) != 0) {
            break;
        }
    }
    TaskHandle_t self = xTaskGetCurrentTaskHandle();
    SemaphoreHandle_t stopped = NULL;
    bool restart_after_system_sleep_abort = false;
    taskENTER_CRITICAL(&s_ambient_lock);
    stopped = s_ambient_task_stopped;
    s_ambient_task_retiring = true;
    taskEXIT_CRITICAL(&s_ambient_lock);
    /* A natural exit still releases its own entry so a future clock cadence
     * can start.  Crucially, this task never takes the Registry mutex
     * unbounded: an owner-wide rollback may be joining it concurrently. If
     * the short bookkeeping attempt loses that race, the retained immutable
     * entry remains fail-closed for the lifecycle owner to remove later. */
    const esp_err_t registry_err = task_registry_unregister_with_timeout(
        TASK_REGISTRY_OWNER_POWER, (void *)self, 10);
    taskENTER_CRITICAL(&s_ambient_lock);
    s_ambient_task_exit_status = registry_err;
    if (s_ambient_task == self) s_ambient_task = NULL;
    s_ambient_task_retiring = false;
    if (registry_err != ESP_OK) {
        s_ambient_task_registry_retirement_failed = true;
    }
    if (s_system_sleep_restart_pending && !s_system_sleep_preparing &&
        !s_ambient_task_registry_retirement_failed && registry_err == ESP_OK) {
        s_system_sleep_restart_pending = false;
        restart_after_system_sleep_abort = true;
    }
    taskEXIT_CRITICAL(&s_ambient_lock);
    if (stopped) xSemaphoreGive(stopped);
    if (restart_after_system_sleep_abort) ambient_service_ensure_clock_task();
    vTaskDelete(NULL);
}

/* Caller holds s_lifecycle_mutex.  expected_task, when non-NULL, is the
 * registry generation to retire: a newer cadence must not be stopped by a
 * stale POWER-owner entry. */
static esp_err_t stop_clock_task_locked(uint32_t timeout_ms, const void *expected_task) {
    if (timeout_ms == 0) return ESP_ERR_TIMEOUT;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    TaskHandle_t task = NULL;
    SemaphoreHandle_t stopped = NULL;
    taskENTER_CRITICAL(&s_ambient_lock);
    task = s_ambient_task;
    stopped = s_ambient_task_stopped;
    taskEXIT_CRITICAL(&s_ambient_lock);
    if (!task) {
        drain_orphaned_completion_locked();
        taskENTER_CRITICAL(&s_ambient_lock);
        const esp_err_t exit_status = s_ambient_task_exit_status;
        taskEXIT_CRITICAL(&s_ambient_lock);
        return exit_status;
    }
    if (expected_task && task != expected_task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;

    xTaskNotifyGive(task);
    /* Notify already happened: a zero remaining budget must still wait one
     * tick rather than polling, so a worker that is about to Give can join. */
    uint32_t remaining_ms = remaining_timeout_ms(deadline_us);
    if (!stopped ||
        xSemaphoreTake(stopped, ticks_for_timeout_ms(remaining_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_ambient_lock);
    const esp_err_t exit_status = s_ambient_task_exit_status;
    if (s_ambient_task_stopped == stopped) s_ambient_task_stopped = NULL;
    taskEXIT_CRITICAL(&s_ambient_lock);
    vSemaphoreDelete(stopped);
    if (exit_status != ESP_OK) return exit_status;
    ESP_LOGI(TAG, "ambient clock task stopped");
    return ESP_OK;
}

static esp_err_t stop_clock_task(uint32_t timeout_ms, const void *expected_task) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_lifecycle_mutex) return ESP_ERR_INVALID_STATE;
    const int64_t deadline_us =
        esp_timer_get_time() + (int64_t)timeout_ms * 1000;
    uint32_t wait_ms = remaining_timeout_ms(deadline_us);
    if (wait_ms == 0) return ESP_ERR_TIMEOUT;
    if (xSemaphoreTake(s_lifecycle_mutex, ticks_for_timeout_ms(wait_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    wait_ms = remaining_timeout_ms(deadline_us);
    esp_err_t err = wait_ms == 0
                        ? ESP_ERR_TIMEOUT
                        : stop_clock_task_locked(wait_ms, expected_task);
    xSemaphoreGive(s_lifecycle_mutex);
    return err;
}

static esp_err_t stop_clock_registry_entry(void *context, uint32_t timeout_ms) {
    return stop_clock_task(timeout_ms, context);
}

device_status_t ambient_service_init(void) {
    if (s_lifecycle_mutex) return DEVICE_STATUS_OK;
    s_lifecycle_mutex = xSemaphoreCreateMutex();
    if (!s_lifecycle_mutex) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    /* CST-8 is the product display zone.  Set it once on the boot task before
     * SNTP / Hub time and the cadence worker can race setenv. */
    setenv("TZ", "CST-8", 1);
    tzset();
    return DEVICE_STATUS_OK;
}

void ambient_service_load(void) {
    weather_cache_snapshot_t snapshot;
    device_status_t status = weather_cache_service_load(&snapshot);
    if (status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "weather cache unavailable: device status=%d", (int)status);
        return;
    }
    if (!snapshot_strings_terminated(&snapshot)) {
        ESP_LOGW(TAG, "ignored invalid weather cache payload");
        return;
    }
    taskENTER_CRITICAL(&s_ambient_lock);
    s_weather = snapshot;
    taskEXIT_CRITICAL(&s_ambient_lock);
}

void ambient_service_apply_weather(const weather_cache_snapshot_t *weather) {
    if (!weather || !weather->valid || !weather->summary[0] ||
        weather->temperature_c < -80 || weather->temperature_c > 80 ||
        !snapshot_strings_terminated(weather)) {
        return;
    }
    weather_cache_snapshot_t copy = *weather;
    taskENTER_CRITICAL(&s_ambient_lock);
    s_weather = copy;
    taskEXIT_CRITICAL(&s_ambient_lock);
}

void ambient_service_persist_weather(void) {
    weather_cache_snapshot_t snapshot;
    taskENTER_CRITICAL(&s_ambient_lock);
    snapshot = s_weather;
    taskEXIT_CRITICAL(&s_ambient_lock);
    if (!snapshot.valid) return;
    persist_weather_copy(&snapshot);
}

void ambient_service_note_wall_clock(int64_t epoch_sec) {
    if (epoch_sec < 1672531200) return; // 2023-01-01 UTC
    int64_t now_us = esp_timer_get_time();
    taskENTER_CRITICAL(&s_ambient_lock);
    s_display_clock_epoch = (time_t)epoch_sec;
    s_display_clock_anchor_us = now_us;
    s_display_clock_valid = true;
    taskEXIT_CRITICAL(&s_ambient_lock);
}

void ambient_service_apply_network(const char *ssid, bool connected) {
    scene_network_fields_t fields = {0};
    strlcpy(fields.ssid, ssid ? ssid : "", sizeof(fields.ssid));
    fields.connected = connected;
    taskENTER_CRITICAL(&s_ambient_lock);
    memcpy(s_network_ssid, fields.ssid, sizeof(s_network_ssid));
    s_network_connected = connected;
    taskEXIT_CRITICAL(&s_ambient_lock);
    scene_presenter_publish_network(&fields);
}

void ambient_service_apply_alarm_scheduled(bool scheduled) {
    taskENTER_CRITICAL(&s_ambient_lock);
    s_alarm_scheduled = scheduled;
    taskEXIT_CRITICAL(&s_ambient_lock);
    scene_presenter_publish_alarm_scheduled(scheduled);
}

void ambient_service_apply_pet_state(const char *state) {
    scene_pet_state_fields_t fields = {0};
    strlcpy(fields.state, state ? state : "idle", sizeof(fields.state));
    taskENTER_CRITICAL(&s_ambient_lock);
    memcpy(s_pet_state, fields.state, sizeof(s_pet_state));
    taskEXIT_CRITICAL(&s_ambient_lock);
    /* Forward the original NULL so App UI's NULL→idle contract stays intact
     * rather than pre-canonicalizing at this layer. */
    scene_presenter_publish_pet_state(state);
}

void ambient_service_apply_pet_profile(const char *skin, bool motion_enabled) {
    scene_pet_profile_fields_t fields = {0};
    strlcpy(fields.skin, skin ? skin : "", sizeof(fields.skin));
    fields.motion_enabled = motion_enabled;
    taskENTER_CRITICAL(&s_ambient_lock);
    memcpy(s_pet_skin, fields.skin, sizeof(s_pet_skin));
    s_pet_motion_enabled = motion_enabled;
    taskEXIT_CRITICAL(&s_ambient_lock);
    scene_presenter_publish_pet_profile(skin, motion_enabled);
}

device_status_t ambient_service_ensure_clock_task(void) {
    if (!s_lifecycle_mutex) return DEVICE_STATUS_UNAVAILABLE;
    if (xSemaphoreTake(s_lifecycle_mutex,
                       ticks_for_timeout_ms(AMBIENT_ENSURE_MUTEX_WAIT_MS)) != pdTRUE) {
        ESP_LOGW(TAG, "cannot start ambient clock task");
        return DEVICE_STATUS_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_ambient_lock);
    bool already_started = s_ambient_task != NULL || s_ambient_task_starting;
    bool system_sleep_preparing = s_system_sleep_preparing ||
                                  s_ambient_task_registry_retirement_failed ||
                                  s_system_sleep_restart_pending;
    if (!already_started && !system_sleep_preparing) {
        /* Claim start admission before allocating or creating anything:
         * system-sleep PREPARE does not take the lifecycle mutex and must be
         * able to reject this in-flight generation fail-closed. */
        s_ambient_task_starting = true;
    }
    taskEXIT_CRITICAL(&s_ambient_lock);
    if (already_started || system_sleep_preparing) {
        xSemaphoreGive(s_lifecycle_mutex);
        return system_sleep_preparing ? DEVICE_STATUS_BUSY : DEVICE_STATUS_OK;
    }
    drain_orphaned_completion_locked();

    SemaphoreHandle_t stopped = xSemaphoreCreateBinary();
    if (!stopped) {
        taskENTER_CRITICAL(&s_ambient_lock);
        s_ambient_task_starting = false;
        taskEXIT_CRITICAL(&s_ambient_lock);
        xSemaphoreGive(s_lifecycle_mutex);
        ESP_LOGE(TAG, "cannot allocate ambient clock completion semaphore");
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    SemaphoreHandle_t start_gate = xSemaphoreCreateBinary();
    if (!start_gate) {
        vSemaphoreDelete(stopped);
        taskENTER_CRITICAL(&s_ambient_lock);
        s_ambient_task_starting = false;
        taskEXIT_CRITICAL(&s_ambient_lock);
        xSemaphoreGive(s_lifecycle_mutex);
        ESP_LOGE(TAG, "cannot allocate ambient clock start gate");
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }

    // Clock cadence must remain independent of animation/render load. A higher
    // priority lets the once-per-second update preempt a slow LCD presentation.
    TaskHandle_t task = NULL;
    BaseType_t created = xTaskCreate(ambient_task, "maclaw_ambient", 3072,
                                     start_gate, 3, &task);
    if (created != pdPASS) {
        vSemaphoreDelete(start_gate);
        vSemaphoreDelete(stopped);
        taskENTER_CRITICAL(&s_ambient_lock);
        s_ambient_task_starting = false;
        taskEXIT_CRITICAL(&s_ambient_lock);
        xSemaphoreGive(s_lifecycle_mutex);
        ESP_LOGE(TAG, "cannot start ambient clock task");
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_ambient_lock);
    s_ambient_task_exit_status = ESP_OK;
    s_ambient_task_registry_retirement_failed = false;
    s_ambient_task_stopped = stopped;
    s_ambient_task = task;
    taskEXIT_CRITICAL(&s_ambient_lock);
    esp_err_t registry_err = task_registry_register(&(task_registry_entry_t){
        .struct_size = sizeof(task_registry_entry_t),
        .owner = TASK_REGISTRY_OWNER_POWER,
        .name = "ambient_clock",
        .context = (void *)task,
        .stop = stop_clock_registry_entry,
    });
    /* Do not let the worker submit its first cadence until publication and
     * registry ownership are complete. From here only the child deletes the
     * gate, so no stop/rollback path can free an object it might still take. */
    xSemaphoreGive(start_gate);
    if (registry_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot register ambient clock lifecycle owner: %s",
                 esp_err_to_name(registry_err));
        (void)stop_clock_task_locked(500, task);
    }
    xSemaphoreGive(s_lifecycle_mutex);
    return status_from_esp_err(registry_err);
}

device_status_t ambient_service_stop_clock_task(uint32_t timeout_ms) {
    return status_from_esp_err(stop_clock_task(timeout_ms, NULL));
}

device_status_t ambient_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!s_lifecycle_mutex) return DEVICE_STATUS_UNAVAILABLE;
    bool restart_clock = false;
    taskENTER_CRITICAL(&s_ambient_lock);
    if (s_system_sleep_preparing || s_ambient_task_starting) {
        taskEXIT_CRITICAL(&s_ambient_lock);
        return DEVICE_STATUS_BUSY;
    }
    s_system_sleep_preparing = true;
    /* A published handle is the only proof of work to restore. A natural exit
     * racing PREPARE must never resurrect a historical cadence worker. */
    s_system_sleep_restart_clock = s_ambient_task != NULL;
    restart_clock = s_system_sleep_restart_clock &&
                    !s_ambient_task_registry_retirement_failed;
    taskEXIT_CRITICAL(&s_ambient_lock);

    if (!restart_clock) return DEVICE_STATUS_OK;
    device_status_t status = ambient_service_stop_clock_task(timeout_ms);
    /* Keep cadence admission closed on a bounded stop failure.  The Power
     * transaction's reverse-order ABORT owns any retained-worker recovery;
     * restarting here could publish a scene while a later participant is
     * still preparing for a possible electrical commit. */
    return status;
}

void ambient_service_abort_system_sleep_prepare(void) {
    bool restart_clock = false;
    taskENTER_CRITICAL(&s_ambient_lock);
    restart_clock = s_system_sleep_restart_clock;
    s_system_sleep_restart_clock = false;
    s_system_sleep_preparing = false;
    /* If bounded stop timed out, the old task is still either running or in
     * final Registry bookkeeping.  It consumes the one-shot marker after it
     * clears its immutable identity; an idle transaction is never invented. */
    if (restart_clock && (s_ambient_task || s_ambient_task_retiring)) {
        s_system_sleep_restart_pending = true;
        restart_clock = false;
    }
    taskEXIT_CRITICAL(&s_ambient_lock);
    if (restart_clock) ambient_service_ensure_clock_task();
}

static const char *hub_json_string(cJSON *root, const char *key) {
    cJSON *node = cJSON_GetObjectItemCaseSensitive(root, key);
    return cJSON_IsString(node) && node->valuestring ? node->valuestring : NULL;
}

static bool hub_json_number(cJSON *root, const char *key, int *value) {
    cJSON *node = root ? cJSON_GetObjectItemCaseSensitive(root, key) : NULL;
    if (!cJSON_IsNumber(node) || !value) return false;
    *value = node->valueint;
    return true;
}

int ambient_service_apply_hub_glyphs(const void *glyphs_json) {
    cJSON *glyphs = (cJSON *)glyphs_json;
    if (!cJSON_IsObject(glyphs)) return 0;
    int accepted = 0;
    cJSON *entry = NULL;
    cJSON_ArrayForEach(entry, glyphs) {
        if (accepted >= AMBIENT_GLYPH_MAX_PER_MESSAGE || !cJSON_IsString(entry) || !entry->string) continue;
        uint32_t codepoint = 0;
        if (!ambient_service_parse_glyph_key(entry->string, &codepoint)) continue;
        uint8_t bitmap[SCENE_GLYPH_BITMAP_BYTES];
        size_t decoded = 0;
        int result = mbedtls_base64_decode(bitmap, sizeof(bitmap), &decoded,
                                           (const unsigned char *)entry->valuestring,
                                           strlen(entry->valuestring));
        if (result != 0 || decoded != sizeof(bitmap)) {
            ESP_LOGW(TAG, "ignored invalid dynamic glyph %s", entry->string);
            continue;
        }
        if (scene_presenter_cache_glyph(codepoint, bitmap)) {
            ++accepted;
            ESP_LOGI(TAG, "dynamic glyph cached: U+%04lX", (unsigned long)codepoint);
        }
    }
    if (accepted) ESP_LOGI(TAG, "dynamic glyph cache updated: received=%d", accepted);
    return accepted;
}

void ambient_service_apply_hub_ambient(const void *ambient_json) {
    cJSON *ambient = (cJSON *)ambient_json;
    if (!cJSON_IsObject(ambient)) return;
    int glyphs_cached = ambient_service_apply_hub_glyphs(
        cJSON_GetObjectItemCaseSensitive(ambient, "glyphs"));
    cJSON *weather = cJSON_GetObjectItemCaseSensitive(ambient, "weather");
    if (!cJSON_IsObject(weather)) return;
    const char *summary = hub_json_string(weather, "summary");
    const char *location = hub_json_string(weather, "location");
    int temperature_c = 0;
    if (!summary || !summary[0] || !hub_json_number(weather, "temperatureC", &temperature_c) ||
        temperature_c < -80 || temperature_c > 80) {
        ESP_LOGW(TAG, "ignored invalid ambient weather payload");
        return;
    }
    weather_cache_snapshot_t snapshot = {
        .temperature_c = temperature_c,
        .valid = true,
    };
    strlcpy(snapshot.summary, summary, sizeof(snapshot.summary));
    strlcpy(snapshot.location, location ? location : "", sizeof(snapshot.location));
    cJSON *expires = cJSON_GetObjectItemCaseSensitive(ambient, "expiresAt");
    snapshot.expires_at_ms = cJSON_IsNumber(expires) ? (int64_t)expires->valuedouble : 0;
    ambient_service_apply_weather(&snapshot);
    /* The long-poll worker intentionally has a PSRAM stack to leave internal
     * memory for TLS/I2S. NVS disables caches during flash operations, where a
     * PSRAM-backed stack is illegal and asserts. Persist only from an
     * internal-stack execution context; the in-memory weather model is already
     * authoritative and a later handshake will safely refresh the cache. */
    if (esp_ptr_internal((const void *)&ambient_json)) {
        ambient_service_persist_weather();
    } else {
        ESP_LOGI(TAG, "ambient weather cache deferred from external-stack poll task");
    }
    ESP_LOGI(TAG, "ambient weather received: summary='%s' temp=%d location='%s' glyphs_cached=%d raw_location=%s",
             snapshot.summary, snapshot.temperature_c, snapshot.location,
             glyphs_cached, location ? "present" : "missing");
}
