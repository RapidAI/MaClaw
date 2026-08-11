#include "sleep_schedule_service.h"

#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <time.h>

#include "device_api.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "nvs.h"
#include "persistence_service.h"
#include "wake_deadline_service.h"

#define SLEEP_SCHEDULE_NAMESPACE "sleep_sched"
#define SLEEP_SCHEDULE_STORE_MAGIC 0x53534331u /* SSC1 */
#define SLEEP_SCHEDULE_STORE_VERSION 1u
#define SLEEP_SCHEDULE_REPLAY_COUNT 4u
#define SLEEP_SCHEDULE_RESULT_BYTES 448u
#define SLEEP_SCHEDULE_ERROR_BYTES 112u
#define SLEEP_SCHEDULE_MIN_OVERRIDE_SECONDS 60u
#define SLEEP_SCHEDULE_MAX_OVERRIDE_SECONDS (12u * 60u * 60u)

typedef struct {
    char key[SLEEP_SCHEDULE_IDEMPOTENCY_KEY_CAPACITY];
    int32_t status;
    char detail[SLEEP_SCHEDULE_ERROR_BYTES];
    char result_json[SLEEP_SCHEDULE_RESULT_BYTES];
} sleep_schedule_replay_t;

typedef struct {
    uint32_t magic;
    uint32_t version;
    sleep_schedule_t schedule;
    int64_t manual_override_until_epoch;
    uint32_t replay_next;
    sleep_schedule_replay_t replay[SLEEP_SCHEDULE_REPLAY_COUNT];
} sleep_schedule_store_t;

typedef struct {
    bool active_window;
    bool override_active;
    bool display_off_requested;
    int64_t next_transition_epoch;
} schedule_evaluation_t;

static const char *TAG = "sleep_schedule";
static SemaphoreHandle_t s_lock;
/* The public schedule APIs can be entered by Gateway/UI/clock tasks while a
 * startup rollback begins.  Keep their mutex as a static lifecycle shell so
 * an already-waiting caller never obtains a freed FreeRTOS object. */
static StaticSemaphore_t s_lock_storage;
static SemaphoreHandle_t s_deinit_lock;
static StaticSemaphore_t s_deinit_lock_storage;
static TaskHandle_t s_task;
static SemaphoreHandle_t s_stopped;
static wake_deadline_handle_t s_deadline = WAKE_DEADLINE_HANDLE_INVALID;
static sleep_schedule_store_t s_store;
/* Rollback data is held only while s_lock is owned.  Keeping it static avoids
 * copying the replay journal onto the Gateway poll task stack for every write
 * tool call, which is the same class of pressure that previously overflowed
 * Fangtang's app_main stack during initialization. */
static sleep_schedule_store_t s_store_rollback;
static volatile bool s_initialized;
static volatile bool s_stop_requested;
static uint32_t s_tool_admissions;
static bool s_manual_wake_pending;
static portMUX_TYPE s_pending_lock = portMUX_INITIALIZER_UNLOCKED;

/* The public stop is one lifecycle transaction; none of its admission lock,
 * worker join, borrower drain or final slot cleanup may restart the caller's
 * timeout budget. */
static TickType_t stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t stop_remaining_ticks(TickType_t started, TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

static bool admit_tool(void) {
    taskENTER_CRITICAL(&s_pending_lock);
    const bool admitted = s_initialized && !s_stop_requested;
    if (admitted) ++s_tool_admissions;
    taskEXIT_CRITICAL(&s_pending_lock);
    return admitted;
}

static void release_tool(void) {
    taskENTER_CRITICAL(&s_pending_lock);
    if (s_tool_admissions) --s_tool_admissions;
    taskEXIT_CRITICAL(&s_pending_lock);
}

/* This service runs its initialization from app_main().  Keep the persisted
 * object in static storage: a full store includes the bounded replay journal
 * and can exceed the remaining startup-task stack on the Fangtang profile. */
static void reset_store(void) {
    memset(&s_store, 0, sizeof(s_store));
    s_store.magic = SLEEP_SCHEDULE_STORE_MAGIC;
    s_store.version = SLEEP_SCHEDULE_STORE_VERSION;
}

static bool clock_is_trusted(int64_t epoch) {
    return epoch >= 1672531200; /* 2023-01-01 UTC */
}

static esp_err_t persist_locked(void) {
    return persistence_service_write_blob(SLEEP_SCHEDULE_NAMESPACE, "store",
                                          &s_store, sizeof(s_store));
}

static bool valid_timezone(const char *timezone) {
    /* Current product time policy is China Standard Time.  Do not accept an
     * arbitrary POSIX TZ string until its parser/DST contract is bounded. */
    return timezone && !strcmp(timezone, "CST-8");
}

static bool trusted_now(int64_t *out_epoch) {
    int64_t now = (int64_t)time(NULL);
    if (!clock_is_trusted(now)) return false;
    if (out_epoch) *out_epoch = now;
    return true;
}

static int64_t make_local_epoch(const struct tm *civil, int hour, int minute,
                                int day_delta) {
    if (!civil || hour < 0 || hour > 23 || minute < 0 || minute > 59) return 0;
    struct tm value = *civil;
    value.tm_hour = hour;
    value.tm_min = minute;
    value.tm_sec = 0;
    value.tm_mday += day_delta;
    value.tm_isdst = -1;
    time_t converted = mktime(&value);
    return converted < 0 ? 0 : (int64_t)converted;
}

static bool periodic_window_for_start_day(const sleep_schedule_t *schedule,
                                          const struct tm *today, int day_delta,
                                          int64_t now_epoch,
                                          int64_t *out_start, int64_t *out_end) {
    if (!schedule || !today || !schedule->weekday_mask) return false;
    struct tm start_day = *today;
    start_day.tm_mday += day_delta;
    start_day.tm_hour = 12; /* mktime normalizes DST changes from a safe time. */
    start_day.tm_min = 0;
    start_day.tm_sec = 0;
    start_day.tm_isdst = -1;
    time_t normalized = mktime(&start_day);
    if (normalized < 0) return false;
    struct tm normalized_day = {0};
    localtime_r(&normalized, &normalized_day);
    unsigned weekday_bit = normalized_day.tm_wday == 0 ? 6u
                                                        : (unsigned)normalized_day.tm_wday - 1u;
    if (!(schedule->weekday_mask & (1u << weekday_bit))) return false;
    const int start_hour = schedule->start_minute_of_day / 60u;
    const int start_minute = schedule->start_minute_of_day % 60u;
    const int end_hour = schedule->end_minute_of_day / 60u;
    const int end_minute = schedule->end_minute_of_day % 60u;
    int64_t start = make_local_epoch(&normalized_day, start_hour, start_minute, 0);
    int end_delta = schedule->end_minute_of_day <= schedule->start_minute_of_day ? 1 : 0;
    int64_t end = make_local_epoch(&normalized_day, end_hour, end_minute, end_delta);
    if (!start || !end || end <= start || now_epoch < start - 8 * 24 * 60 * 60) return false;
    if (out_start) *out_start = start;
    if (out_end) *out_end = end;
    return true;
}

static schedule_evaluation_t evaluate_locked(int64_t now_epoch) {
    schedule_evaluation_t result = {0};
    const sleep_schedule_t *schedule = &s_store.schedule;
    if (!schedule->enabled || !clock_is_trusted(now_epoch)) return result;
    if (s_store.manual_override_until_epoch > now_epoch) {
        result.override_active = true;
        result.next_transition_epoch = s_store.manual_override_until_epoch;
    }
    if (schedule->mode == SLEEP_SCHEDULE_MODE_ONCE) {
        int64_t start_epoch = schedule->once_start_epoch_ms / 1000;
        int64_t end_epoch = schedule->once_end_epoch_ms / 1000;
        result.active_window = now_epoch >= start_epoch && now_epoch < end_epoch;
        if (!result.next_transition_epoch) {
            result.next_transition_epoch = now_epoch < start_epoch ? start_epoch : end_epoch;
        }
    } else if (schedule->mode == SLEEP_SCHEDULE_MODE_PERIODIC) {
        time_t now_time = (time_t)now_epoch;
        struct tm local = {0};
        localtime_r(&now_time, &local);
        int64_t nearest_future = 0;
        /* Include yesterday to catch a cross-midnight interval.  Seven future
         * start days are sufficient because weekdays are a 7-bit cycle. */
        for (int delta = -1; delta <= 7; ++delta) {
            int64_t start = 0, end = 0;
            if (!periodic_window_for_start_day(schedule, &local, delta, now_epoch,
                                               &start, &end)) continue;
            if (now_epoch >= start && now_epoch < end) {
                result.active_window = true;
                if (!nearest_future || end < nearest_future) nearest_future = end;
            } else if (start > now_epoch && (!nearest_future || start < nearest_future)) {
                nearest_future = start;
            }
        }
        if (!result.next_transition_epoch ||
            (nearest_future && nearest_future < result.next_transition_epoch)) {
            result.next_transition_epoch = nearest_future;
        }
    }
    result.display_off_requested = result.active_window && !result.override_active;
    return result;
}

static void arm_next_timer_locked(int64_t now_epoch, const schedule_evaluation_t *evaluation) {
    (void)now_epoch;
    if (!s_deadline || !evaluation || !evaluation->next_transition_epoch) {
        if (s_deadline) wake_deadline_service_cancel(s_deadline);
        return;
    }
    /* The dispatcher owns the physical ESP timer.  Schedule policy provides
     * only its next wall-clock transition, which remains re-evaluable after a
     * gateway/SNTP clock correction. */
    esp_err_t err = wake_deadline_service_arm(
        s_deadline, evaluation->next_transition_epoch * 1000LL);
    if (err != ESP_OK) ESP_LOGW(TAG, "cannot arm schedule deadline: %s", esp_err_to_name(err));
}

static void apply_evaluation(const schedule_evaluation_t *evaluation) {
    if (!evaluation) return;
    if (evaluation->display_off_requested) {
        /* The board adapter remains free to reject a foreground scene.  A
         * schedule never closes a recorder, alarm or response by force. */
        (void)device_power_schedule_display_off(1);
    } else if (evaluation->active_window && evaluation->override_active) {
        /* A schedule-owned display-off request was explicitly interrupted by
         * a person.  Leave the panel active until the override expires. */
        device_power_cancel_display_off();
    } else if (!evaluation->active_window) {
        /* End of a schedule window is a scheduled display wake, not a fake
         * touch event.  The Device API keeps this below domain policy. */
        (void)device_power_wake_display_from_schedule();
    }
}

static void deadline_callback(void *arg) {
    (void)arg;
    /* The shared dispatcher can select this callback while rollback is
     * closing the schedule service.  Sample the notification target under
     * the same admission shell used by manual-wake and wall-clock callers;
     * schedule_task clears s_task under that shell before handing completion
     * to deinit. */
    taskENTER_CRITICAL(&s_pending_lock);
    TaskHandle_t task = s_initialized && !s_stop_requested ? s_task : NULL;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (task) xTaskNotifyGive(task);
}

static void schedule_task(void *arg) {
    (void)arg;
    for (;;) {
        (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
        taskENTER_CRITICAL(&s_pending_lock);
        const bool stopping = s_stop_requested;
        taskEXIT_CRITICAL(&s_pending_lock);
        if (stopping) break;
        int64_t now = 0;
        if (!trusted_now(&now) || xSemaphoreTake(s_lock, pdMS_TO_TICKS(3000)) != pdTRUE) continue;
        /* Stop may have won while this task waited for a concurrent tool
         * mutation.  Do not perform a late Persistence write or re-arm the
         * shared deadline after that admission boundary has closed. */
        taskENTER_CRITICAL(&s_pending_lock);
        const bool ready = s_initialized && !s_stop_requested;
        taskEXIT_CRITICAL(&s_pending_lock);
        if (!ready) {
            xSemaphoreGive(s_lock);
            break;
        }
        taskENTER_CRITICAL(&s_pending_lock);
        bool manual_wake = s_manual_wake_pending;
        s_manual_wake_pending = false;
        taskEXIT_CRITICAL(&s_pending_lock);
        schedule_evaluation_t evaluation = evaluate_locked(now);
        if (manual_wake && evaluation.active_window &&
            s_store.schedule.manual_wake_override_seconds) {
            s_store.manual_override_until_epoch =
                now + (int64_t)s_store.schedule.manual_wake_override_seconds;
            evaluation = evaluate_locked(now);
            esp_err_t persist_err = persist_locked();
            if (persist_err != ESP_OK) {
                s_store.manual_override_until_epoch = 0;
                evaluation = evaluate_locked(now);
                ESP_LOGW(TAG, "manual wake override was not persisted: %s",
                         esp_err_to_name(persist_err));
            } else {
                ESP_LOGI(TAG, "manual wake override until epoch=%lld",
                         (long long)s_store.manual_override_until_epoch);
            }
        }
        arm_next_timer_locked(now, &evaluation);
        xSemaphoreGive(s_lock);
        apply_evaluation(&evaluation);
    }
    /* Publish final task state before the completion handoff; deinit may
     * reclaim the binary semaphore immediately after this give. */
    taskENTER_CRITICAL(&s_pending_lock);
    s_task = NULL;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (s_stopped) xSemaphoreGive(s_stopped);
    vTaskDelete(NULL);
}

static bool valid_idempotency_key(const char *key) {
    if (!key || !key[0] || strlen(key) >= SLEEP_SCHEDULE_IDEMPOTENCY_KEY_CAPACITY) return false;
    for (const char *p = key; *p; ++p) if ((unsigned char)*p > 0x7f) return false;
    return true;
}

static void schedule_json(cJSON *result, const sleep_schedule_t *schedule,
                          const schedule_evaluation_t *evaluation) {
    if (!result || !schedule) return;
    cJSON_AddBoolToObject(result, "enabled", schedule->enabled);
    cJSON_AddNumberToObject(result, "revision", schedule->revision);
    cJSON_AddStringToObject(result, "target", "displayOff");
    cJSON_AddStringToObject(result, "timeZone", schedule->timezone);
    if (schedule->mode == SLEEP_SCHEDULE_MODE_ONCE) {
        cJSON_AddStringToObject(result, "mode", "once");
        cJSON_AddNumberToObject(result, "startAtEpochMs", (double)schedule->once_start_epoch_ms);
        cJSON_AddNumberToObject(result, "endAtEpochMs", (double)schedule->once_end_epoch_ms);
    } else if (schedule->mode == SLEEP_SCHEDULE_MODE_PERIODIC) {
        cJSON_AddStringToObject(result, "mode", "periodic");
        cJSON_AddNumberToObject(result, "startMinuteOfDay", schedule->start_minute_of_day);
        cJSON_AddNumberToObject(result, "endMinuteOfDay", schedule->end_minute_of_day);
        cJSON_AddNumberToObject(result, "weekdayMask", schedule->weekday_mask);
    }
    cJSON_AddNumberToObject(result, "manualWakeOverrideSeconds",
                            schedule->manual_wake_override_seconds);
    if (evaluation) {
        cJSON_AddBoolToObject(result, "active", evaluation->active_window);
        cJSON_AddBoolToObject(result, "manualOverrideActive", evaluation->override_active);
        cJSON_AddNumberToObject(result, "nextTransitionEpoch",
                                (double)evaluation->next_transition_epoch);
    }
}

static bool json_integer(cJSON *object, const char *name, int64_t *out) {
    cJSON *item = cJSON_GetObjectItemCaseSensitive(object, name);
    if (!cJSON_IsNumber(item) || item->valuedouble < INT64_MIN || item->valuedouble > INT64_MAX) return false;
    int64_t value = (int64_t)item->valuedouble;
    if (item->valuedouble != (double)value) return false;
    *out = value;
    return true;
}

static bool parse_target(cJSON *arguments, char *error, size_t error_size) {
    cJSON *target = cJSON_GetObjectItemCaseSensitive(arguments, "target");
    if (!target) return true; /* DISPLAY_OFF is the safe explicit default. */
    if (!cJSON_IsString(target) || strcmp(target->valuestring, "displayOff")) {
        snprintf(error, error_size,
                 "target must be displayOff; light/deep sleep is not verified");
        return false;
    }
    return true;
}

static bool parse_override(cJSON *arguments, uint32_t *out, char *error, size_t error_size) {
    cJSON *item = cJSON_GetObjectItemCaseSensitive(arguments, "manualWakeOverrideSeconds");
    if (!item) {
        *out = 0;
        return true;
    }
    int64_t value = 0;
    if (!json_integer(arguments, "manualWakeOverrideSeconds", &value) || value < 0 ||
        value > SLEEP_SCHEDULE_MAX_OVERRIDE_SECONDS ||
        (value && value < SLEEP_SCHEDULE_MIN_OVERRIDE_SECONDS)) {
        snprintf(error, error_size, "manualWakeOverrideSeconds must be 0 or 60..43200");
        return false;
    }
    *out = (uint32_t)value;
    return true;
}

static bool parse_time_of_day(const char *text, uint16_t *out_minute) {
    if (!text || strlen(text) != 5 || text[2] != ':' ||
        text[0] < '0' || text[0] > '2' || text[1] < '0' || text[1] > '9' ||
        text[3] < '0' || text[3] > '5' || text[4] < '0' || text[4] > '9') return false;
    int hour = (text[0] - '0') * 10 + text[1] - '0';
    int minute = (text[3] - '0') * 10 + text[4] - '0';
    if (hour > 23) return false;
    *out_minute = (uint16_t)(hour * 60 + minute);
    return true;
}

static bool parse_schedule(cJSON *arguments, sleep_schedule_t *out,
                           char *error, size_t error_size) {
    if (!arguments || !out || !parse_target(arguments, error, error_size)) return false;
    const char *timezone = "CST-8";
    cJSON *timezone_item = cJSON_GetObjectItemCaseSensitive(arguments, "timeZone");
    if (timezone_item) {
        if (!cJSON_IsString(timezone_item)) {
            snprintf(error, error_size, "timeZone must be CST-8");
            return false;
        }
        timezone = timezone_item->valuestring;
    }
    if (!valid_timezone(timezone)) {
        snprintf(error, error_size, "only CST-8 is supported by the current clock policy");
        return false;
    }
    cJSON *mode = cJSON_GetObjectItemCaseSensitive(arguments, "mode");
    if (!cJSON_IsString(mode)) {
        snprintf(error, error_size, "mode must be once or periodic");
        return false;
    }
    sleep_schedule_t next = {.enabled = true,
                             .target_power_state = DEVICE_POWER_STATE_DISPLAY_OFF};
    strlcpy(next.timezone, timezone, sizeof(next.timezone));
    if (!parse_override(arguments, &next.manual_wake_override_seconds, error, error_size)) return false;
    if (!strcmp(mode->valuestring, "once")) {
        int64_t now = 0;
        if (!trusted_now(&now)) {
            snprintf(error, error_size, "trusted wall clock is unavailable");
            return false;
        }
        if (!json_integer(arguments, "startAtEpochMs", &next.once_start_epoch_ms) ||
            !json_integer(arguments, "endAtEpochMs", &next.once_end_epoch_ms) ||
            next.once_start_epoch_ms <= now * 1000 ||
            next.once_end_epoch_ms <= next.once_start_epoch_ms) {
            snprintf(error, error_size, "once window requires future integer startAtEpochMs < endAtEpochMs");
            return false;
        }
        next.mode = SLEEP_SCHEDULE_MODE_ONCE;
    } else if (!strcmp(mode->valuestring, "periodic")) {
        cJSON *start = cJSON_GetObjectItemCaseSensitive(arguments, "startTime");
        cJSON *end = cJSON_GetObjectItemCaseSensitive(arguments, "endTime");
        int64_t weekday_mask = 0;
        if (!cJSON_IsString(start) || !cJSON_IsString(end) ||
            !parse_time_of_day(start->valuestring, &next.start_minute_of_day) ||
            !parse_time_of_day(end->valuestring, &next.end_minute_of_day) ||
            next.start_minute_of_day == next.end_minute_of_day ||
            !json_integer(arguments, "weekdayMask", &weekday_mask) ||
            weekday_mask < 1 || weekday_mask > 0x7f) {
            snprintf(error, error_size,
                     "periodic requires HH:MM startTime/endTime, distinct times and weekdayMask 1..127");
            return false;
        }
        next.mode = SLEEP_SCHEDULE_MODE_PERIODIC;
        next.weekday_mask = (uint8_t)weekday_mask;
    } else {
        snprintf(error, error_size, "mode must be once or periodic");
        return false;
    }
    *out = next;
    return true;
}

static esp_err_t return_replay_locked(const char *key, cJSON **out_result,
                                      char *error, size_t error_size) {
    for (size_t i = 0; i < SLEEP_SCHEDULE_REPLAY_COUNT; ++i) {
        const sleep_schedule_replay_t *record = &s_store.replay[i];
        if (!record->key[0] || strcmp(record->key, key)) continue;
        esp_err_t status = (esp_err_t)record->status;
        if (status != ESP_OK) {
            snprintf(error, error_size, "%s", record->detail);
            return status;
        }
        *out_result = cJSON_Parse(record->result_json);
        return *out_result ? ESP_OK : ESP_ERR_INVALID_RESPONSE;
    }
    return ESP_ERR_NOT_FOUND;
}

static esp_err_t save_replay_locked(const char *key, esp_err_t status,
                                    cJSON *result, const char *error) {
    sleep_schedule_replay_t *record =
        &s_store.replay[s_store.replay_next++ % SLEEP_SCHEDULE_REPLAY_COUNT];
    memset(record, 0, sizeof(*record));
    strlcpy(record->key, key, sizeof(record->key));
    record->status = (int32_t)status;
    if (status == ESP_OK) {
        char *json = cJSON_PrintUnformatted(result);
        if (!json || strlen(json) >= sizeof(record->result_json)) {
            cJSON_free(json);
            return ESP_ERR_NO_MEM;
        }
        strlcpy(record->result_json, json, sizeof(record->result_json));
        cJSON_free(json);
    } else {
        strlcpy(record->detail, error ? error : "schedule operation failed", sizeof(record->detail));
    }
    return ESP_OK;
}

esp_err_t sleep_schedule_service_init(void) {
    if (!persistence_service_is_initialized()) return ESP_ERR_INVALID_STATE;
    taskENTER_CRITICAL(&s_pending_lock);
    const bool already_initialized = s_initialized;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (already_initialized) return ESP_OK;
    if (!s_lock) s_lock = xSemaphoreCreateMutexStatic(&s_lock_storage);
    if (!s_deinit_lock) s_deinit_lock = xSemaphoreCreateMutexStatic(&s_deinit_lock_storage);
    if (!s_lock || !s_deinit_lock) return ESP_ERR_NO_MEM;
    if (xSemaphoreTake(s_deinit_lock, pdMS_TO_TICKS(3000)) != pdTRUE) return ESP_ERR_TIMEOUT;
    taskENTER_CRITICAL(&s_pending_lock);
    const bool initialized_while_waiting = s_initialized;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (initialized_while_waiting) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_OK;
    }
    taskENTER_CRITICAL(&s_pending_lock);
    const bool closing_or_live = s_stop_requested || s_task || s_stopped;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (closing_or_live) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_stopped = xSemaphoreCreateBinary();
    if (!s_stopped) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_ERR_NO_MEM;
    }
    reset_store();
    size_t size = sizeof(s_store);
    esp_err_t load_err = persistence_service_read_blob(SLEEP_SCHEDULE_NAMESPACE, "store",
                                                        &s_store, &size);
    if (load_err != ESP_OK || size != sizeof(s_store) ||
        s_store.magic != SLEEP_SCHEDULE_STORE_MAGIC ||
        s_store.version != SLEEP_SCHEDULE_STORE_VERSION ||
        (s_store.schedule.enabled && !valid_timezone(s_store.schedule.timezone))) {
        /* Never retain a partial, incompatible, or corrupt blob.  Missing is
         * normal first boot; other load failures fail closed to default state
         * and are visible in the startup log. */
        if (load_err != ESP_ERR_NVS_NOT_FOUND) {
            ESP_LOGW(TAG, "ignoring persisted schedule store: %s", esp_err_to_name(load_err));
        }
        reset_store();
    }
    esp_err_t deadline_err = wake_deadline_service_register(deadline_callback, NULL, &s_deadline);
    if (deadline_err != ESP_OK) {
        vSemaphoreDelete(s_stopped);
        s_stopped = NULL;
        xSemaphoreGive(s_deinit_lock);
        return deadline_err;
    }
    if (xTaskCreate(schedule_task, "maclaw_schedule", 4096, NULL, 5, &s_task) != pdPASS) {
        wake_deadline_service_cancel(s_deadline);
        /* No worker was created, but use the bounded client hand-off rather
         * than the legacy convenience wrapper so init rollback cannot add an
         * unaccounted blocking wait to its lifecycle transaction. */
        (void)wake_deadline_service_unregister_with_timeout(s_deadline, 1000);
        s_deadline = WAKE_DEADLINE_HANDLE_INVALID;
        vSemaphoreDelete(s_stopped);
        s_stopped = NULL;
        xSemaphoreGive(s_deinit_lock);
        return ESP_ERR_NO_MEM;
    }
    taskENTER_CRITICAL(&s_pending_lock);
    s_initialized = true;
    s_stop_requested = false;
    taskEXIT_CRITICAL(&s_pending_lock);
    int64_t now = 0;
    if (trusted_now(&now) && xSemaphoreTake(s_lock, pdMS_TO_TICKS(3000)) == pdTRUE) {
        schedule_evaluation_t evaluation = evaluate_locked(now);
        arm_next_timer_locked(now, &evaluation);
        xSemaphoreGive(s_lock);
        /* Do not seize an arbitrary startup/foreground surface merely because
         * a persisted rest window happens to be active at boot.  The next
         * ambient transition is shortened through the shared UI seam instead. */
    }
    xSemaphoreGive(s_deinit_lock);
    ESP_LOGI(TAG, "service ready: enabled=%s", s_store.schedule.enabled ? "yes" : "no");
    return ESP_OK;
}

esp_err_t sleep_schedule_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_lock || !s_deinit_lock) return ESP_OK;
    taskENTER_CRITICAL(&s_pending_lock);
    const bool caller_is_worker = s_task && xTaskGetCurrentTaskHandle() == s_task;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (caller_is_worker) return ESP_ERR_INVALID_STATE;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    if (xSemaphoreTake(s_deinit_lock, budget) != pdTRUE) return ESP_ERR_TIMEOUT;
    TickType_t remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_pending_lock);
    const bool already_stopped = !s_initialized && !s_stop_requested;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (already_stopped) {
        xSemaphoreGive(s_lock);
        xSemaphoreGive(s_deinit_lock);
        return ESP_OK;
    }
    taskENTER_CRITICAL(&s_pending_lock);
    s_stop_requested = true;
    s_initialized = false;
    if (s_deadline) wake_deadline_service_cancel(s_deadline);
    TaskHandle_t task = s_task;
    taskEXIT_CRITICAL(&s_pending_lock);
    xSemaphoreGive(s_lock);
    if (task) {
        xTaskNotifyGive(task);
        remaining = stop_remaining_ticks(started, budget);
        if (remaining == 0 || xSemaphoreTake(s_stopped, remaining) != pdTRUE) {
            xSemaphoreGive(s_deinit_lock);
            return ESP_ERR_TIMEOUT;
        }
    } else if (s_stopped) {
        /* Complete a prior bounded stop that observed the worker exit after
         * its waiter timed out, instead of silently returning with a stale
         * completion object. */
        remaining = stop_remaining_ticks(started, budget);
        if (remaining == 0 || xSemaphoreTake(s_stopped, remaining) != pdTRUE) {
            xSemaphoreGive(s_deinit_lock);
            return ESP_ERR_TIMEOUT;
        }
    }
    for (;;) {
        taskENTER_CRITICAL(&s_pending_lock);
        const uint32_t admissions = s_tool_admissions;
        taskEXIT_CRITICAL(&s_pending_lock);
        if (admissions == 0) break;
        if (stop_remaining_ticks(started, budget) == 0) {
            xSemaphoreGive(s_deinit_lock);
            return ESP_ERR_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    /* Waiters which sampled the former ready state re-check the closed
     * boundary under this permanent mutex before they touch the deadline or
     * store.  The mutex itself intentionally survives deinit. */
    remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_ERR_TIMEOUT;
    }
    if (s_deadline) {
        /* The common dispatcher runs callback code outside its lock.  Wait for
         * a copied callback before releasing schedule-owned state, sharing
         * this service's single shutdown deadline.  deadline_callback only
         * reads the task handle and never takes s_lock. */
        remaining = stop_remaining_ticks(started, budget);
        const uint32_t remaining_ms = (uint32_t)pdTICKS_TO_MS(remaining);
        if (remaining == 0 || remaining_ms == 0 ||
            wake_deadline_service_unregister_with_timeout(s_deadline, remaining_ms) != ESP_OK) {
            xSemaphoreGive(s_lock);
            xSemaphoreGive(s_deinit_lock);
            return ESP_ERR_TIMEOUT;
        }
    }
    s_deadline = WAKE_DEADLINE_HANDLE_INVALID;
    s_manual_wake_pending = false;
    taskENTER_CRITICAL(&s_pending_lock);
    s_stop_requested = false;
    taskEXIT_CRITICAL(&s_pending_lock);
    xSemaphoreGive(s_lock);
    vSemaphoreDelete(s_stopped);
    s_stopped = NULL;
    xSemaphoreGive(s_deinit_lock);
    ESP_LOGI(TAG, "service stopped");
    return ESP_OK;
}

void sleep_schedule_service_get_status(sleep_schedule_status_t *out_status) {
    if (!out_status) return;
    memset(out_status, 0, sizeof(*out_status));
    if (!s_lock) return;
    int64_t now = 0;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(100)) != pdTRUE) return;
    taskENTER_CRITICAL(&s_pending_lock);
    const bool ready = s_initialized && !s_stop_requested;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (!ready) {
        xSemaphoreGive(s_lock);
        return;
    }
    schedule_evaluation_t evaluation = trusted_now(&now) ? evaluate_locked(now)
                                                          : (schedule_evaluation_t){0};
    out_status->initialized = true;
    out_status->enabled = s_store.schedule.enabled;
    out_status->revision = s_store.schedule.revision;
    out_status->active_window = evaluation.active_window;
    out_status->override_active = evaluation.override_active;
    out_status->display_off_requested = evaluation.display_off_requested;
    out_status->next_transition_epoch = evaluation.next_transition_epoch;
    out_status->manual_override_until_epoch = s_store.manual_override_until_epoch;
    xSemaphoreGive(s_lock);
}

uint32_t sleep_schedule_service_adjust_display_off_delay(uint32_t ambient_delay_ms) {
    if (!ambient_delay_ms || !s_lock) return ambient_delay_ms;
    int64_t now = 0;
    if (!trusted_now(&now) || xSemaphoreTake(s_lock, pdMS_TO_TICKS(20)) != pdTRUE) {
        return ambient_delay_ms;
    }
    taskENTER_CRITICAL(&s_pending_lock);
    const bool ready = s_initialized && !s_stop_requested;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (!ready) {
        xSemaphoreGive(s_lock);
        return ambient_delay_ms;
    }
    schedule_evaluation_t evaluation = evaluate_locked(now);
    xSemaphoreGive(s_lock);
    return evaluation.display_off_requested ? 1u : ambient_delay_ms;
}

void sleep_schedule_service_note_manual_wake(void) {
    /* This is called from the App Interaction Task.  NVS commits may disable
     * cache and must not run on that task's PSRAM stack, so hand the durable
     * override write to the service's internal-stack worker. */
    taskENTER_CRITICAL(&s_pending_lock);
    if (!s_initialized || s_stop_requested || !s_task) {
        taskEXIT_CRITICAL(&s_pending_lock);
        return;
    }
    TaskHandle_t task = s_task;
    s_manual_wake_pending = true;
    taskEXIT_CRITICAL(&s_pending_lock);
    xTaskNotifyGive(task);
}

void sleep_schedule_service_on_wall_clock_updated(void) {
    taskENTER_CRITICAL(&s_pending_lock);
    TaskHandle_t task = s_initialized && !s_stop_requested ? s_task : NULL;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (task) xTaskNotifyGive(task);
}

esp_err_t sleep_schedule_service_execute_tool(const char *name, cJSON *arguments,
                                              const char *idempotency_key,
                                              cJSON **out_result, char *error,
                                              size_t error_size) {
    if (!name || !out_result || !cJSON_IsObject(arguments) || !admit_tool()) return ESP_ERR_INVALID_STATE;
    *out_result = NULL;
    const bool mutation = !strcmp(name, "sleep_schedule_set") ||
                          !strcmp(name, "sleep_schedule_disable");
    if (mutation && !valid_idempotency_key(idempotency_key)) {
        snprintf(error, error_size, "idempotencyKey must be 1..63 ASCII characters");
        release_tool();
        return ESP_ERR_INVALID_ARG;
    }
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(3000)) != pdTRUE) {
        release_tool();
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_pending_lock);
    const bool ready = s_initialized && !s_stop_requested;
    taskEXIT_CRITICAL(&s_pending_lock);
    if (!ready) {
        xSemaphoreGive(s_lock);
        release_tool();
        return ESP_ERR_INVALID_STATE;
    }
    if (mutation) {
        esp_err_t replay = return_replay_locked(idempotency_key, out_result, error, error_size);
        if (replay != ESP_ERR_NOT_FOUND) {
            xSemaphoreGive(s_lock);
            release_tool();
            return replay;
        }
    }
    cJSON *result = cJSON_CreateObject();
    if (!result) {
        xSemaphoreGive(s_lock);
        release_tool();
        return ESP_ERR_NO_MEM;
    }
    esp_err_t status = ESP_OK;
    int64_t now = 0;
    if (!strcmp(name, "sleep_schedule_get")) {
        schedule_evaluation_t evaluation = trusted_now(&now) ? evaluate_locked(now)
                                                              : (schedule_evaluation_t){0};
        schedule_json(result, &s_store.schedule, &evaluation);
        cJSON_AddBoolToObject(result, "clockTrusted", clock_is_trusted(now));
    } else if (!strcmp(name, "sleep_schedule_set")) {
        sleep_schedule_t next = {0};
        if (!parse_schedule(arguments, &next, error, error_size)) {
            status = ESP_ERR_INVALID_ARG;
        } else {
            s_store_rollback = s_store;
            next.revision = s_store.schedule.revision + 1u;
            if (!next.revision) next.revision = 1;
            s_store.schedule = next;
            s_store.manual_override_until_epoch = 0;
            int64_t current = (int64_t)time(NULL);
            schedule_evaluation_t evaluation = evaluate_locked(current);
            schedule_json(result, &s_store.schedule, &evaluation);
            if (save_replay_locked(idempotency_key, status, result, error) != ESP_OK ||
                persist_locked() != ESP_OK) {
                s_store = s_store_rollback;
                status = ESP_FAIL;
                snprintf(error, error_size, "cannot persist sleep schedule");
            } else {
                arm_next_timer_locked(current, &evaluation);
                taskENTER_CRITICAL(&s_pending_lock);
                TaskHandle_t task = s_initialized && !s_stop_requested ? s_task : NULL;
                taskEXIT_CRITICAL(&s_pending_lock);
                if (task) xTaskNotifyGive(task);
            }
        }
    } else if (!strcmp(name, "sleep_schedule_disable")) {
        s_store_rollback = s_store;
        s_store.schedule.enabled = false;
        s_store.schedule.revision++;
        if (!s_store.schedule.revision) s_store.schedule.revision = 1;
        s_store.manual_override_until_epoch = 0;
        schedule_evaluation_t evaluation = {0};
        schedule_json(result, &s_store.schedule, &evaluation);
        if (save_replay_locked(idempotency_key, status, result, error) != ESP_OK ||
            persist_locked() != ESP_OK) {
            s_store = s_store_rollback;
            status = ESP_FAIL;
            snprintf(error, error_size, "cannot persist disabled sleep schedule");
        } else {
            wake_deadline_service_cancel(s_deadline);
            taskENTER_CRITICAL(&s_pending_lock);
            TaskHandle_t task = s_initialized && !s_stop_requested ? s_task : NULL;
            taskEXIT_CRITICAL(&s_pending_lock);
            if (task) xTaskNotifyGive(task);
        }
    } else {
        status = ESP_ERR_NOT_SUPPORTED;
        snprintf(error, error_size, "unsupported client tool: %s", name);
    }
    xSemaphoreGive(s_lock);
    release_tool();
    if (status != ESP_OK) {
        cJSON_Delete(result);
        return status;
    }
    *out_result = result;
    return ESP_OK;
}
