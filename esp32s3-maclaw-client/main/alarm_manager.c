#include "alarm_manager.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "board_port.h"
#include "app_ui.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "nvs.h"

#define ALARM_MAX_COUNT 16
#define ALARM_LABEL_BYTES 48
#define ALARM_STORE_MAGIC_V1 0x414c4d31u
#define ALARM_STORE_MAGIC 0x414c4d32u
#define ALARM_RING_SECONDS 60
#define ALARM_SNOOZE_SECONDS (5 * 60)
#define ALARM_MAX_ATTEMPTS 3
#define ALARM_RESULT_CACHE_COUNT 8
#define ALARM_RESULT_CACHE_KEY_BYTES 64
#define ALARM_RESULT_CACHE_JSON_BYTES 512

typedef struct {
    uint32_t id;
    int64_t trigger_at_ms;
    char label[ALARM_LABEL_BYTES + 1];
} alarm_item_t;

typedef struct {
    char key[ALARM_RESULT_CACHE_KEY_BYTES];
    int32_t status;
    char detail[96];
    char result_json[ALARM_RESULT_CACHE_JSON_BYTES];
} alarm_cached_result_t;

typedef struct {
    uint32_t magic;
    uint32_t next_id;
    uint32_t count;
    alarm_item_t items[ALARM_MAX_COUNT];
    uint32_t cache_next;
    alarm_cached_result_t cache[ALARM_RESULT_CACHE_COUNT];
    bool active_valid;
    alarm_item_t active_alarm;
} alarm_store_t;

// On-device migration source for firmware that predates persisted active
// alarm ownership. Keep this layout stable; changing the current blob without
// accepting V1 would silently discard every user's existing alarms.
typedef struct {
    uint32_t magic;
    uint32_t next_id;
    uint32_t count;
    alarm_item_t items[ALARM_MAX_COUNT];
    uint32_t cache_next;
    alarm_cached_result_t cache[ALARM_RESULT_CACHE_COUNT];
} alarm_store_v1_t;

static const char *TAG = "alarm_manager";
static alarm_store_t s_store = {.magic = ALARM_STORE_MAGIC, .next_id = 1};
static SemaphoreHandle_t s_lock;
static TaskHandle_t s_task;
static volatile bool s_ringing;
static volatile bool s_dismiss_requested;
static portMUX_TYPE s_state_lock = portMUX_INITIALIZER_UNLOCKED;

static esp_err_t persist_locked(void);

static void publish_scheduled_state(void) {
    bool scheduled = false;
    if (s_lock && xSemaphoreTake(s_lock, portMAX_DELAY) == pdTRUE) {
        scheduled = s_store.count > 0 || s_store.active_valid;
        xSemaphoreGive(s_lock);
    }
    app_ui_set_alarm_scheduled(scheduled);
}

static bool dismiss_requested(void) {
    taskENTER_CRITICAL(&s_state_lock);
    bool requested = s_dismiss_requested;
    taskEXIT_CRITICAL(&s_state_lock);
    return requested;
}

static void set_ring_state(bool ringing, bool dismiss) {
    taskENTER_CRITICAL(&s_state_lock);
    s_ringing = ringing;
    s_dismiss_requested = dismiss;
    taskEXIT_CRITICAL(&s_state_lock);
}

static bool complete_active_alarm(uint32_t alarm_id) {
    bool complete = false;
    if (xSemaphoreTake(s_lock, portMAX_DELAY) != pdTRUE) return false;
    if (!s_store.active_valid || s_store.active_alarm.id != alarm_id) {
        // A committed tool clear may have removed the alarm while this task
        // was leaving the ring loop. Never overwrite a newer owner.
        complete = true;
    } else {
        alarm_item_t active_before = s_store.active_alarm;
        s_store.active_valid = false;
        memset(&s_store.active_alarm, 0, sizeof(s_store.active_alarm));
        esp_err_t persist_err = persist_locked();
        if (persist_err == ESP_OK) {
            complete = true;
        } else {
            // Keep durable and in-memory ownership aligned. The task retries
            // this completion marker before it can dispatch another alarm, so
            // a reboot cannot resurrect an alarm that already finished.
            s_store.active_valid = true;
            s_store.active_alarm = active_before;
            ESP_LOGE(TAG, "cannot persist completed alarm: %s",
                     esp_err_to_name(persist_err));
        }
    }
    xSemaphoreGive(s_lock);
    return complete;
}

static bool active_alarm_locked(alarm_item_t *out_alarm) {
    // Callers hold s_lock. The active alarm is part of the same persisted
    // transaction as the queued list, so an index can never refer to a torn
    // mixture of two independently locked snapshots.
    bool active = s_store.active_valid;
    if (active && out_alarm) *out_alarm = s_store.active_alarm;
    return active;
}

static int compare_alarm(const void *left, const void *right) {
    const alarm_item_t *a = left, *b = right;
    if (a->trigger_at_ms < b->trigger_at_ms) return -1;
    if (a->trigger_at_ms > b->trigger_at_ms) return 1;
    return a->id < b->id ? -1 : a->id > b->id;
}

static void sort_alarms(void) {
    qsort(s_store.items, s_store.count, sizeof(s_store.items[0]), compare_alarm);
}

static esp_err_t persist_locked(void) {
    nvs_handle_t nvs;
    esp_err_t err = nvs_open("alarms", NVS_READWRITE, &nvs);
    if (err != ESP_OK) return err;
    err = nvs_set_blob(nvs, "store", &s_store, sizeof(s_store));
    if (err == ESP_OK) err = nvs_commit(nvs);
    nvs_close(nvs);
    return err;
}

static void remove_index_locked(size_t index) {
    if (index >= s_store.count) return;
    if (index + 1 < s_store.count) {
        memmove(&s_store.items[index], &s_store.items[index + 1],
                (s_store.count - index - 1) * sizeof(s_store.items[0]));
    }
    --s_store.count;
}

static void format_local_time(int64_t epoch_ms, char out[24]) {
    time_t seconds = (time_t)(epoch_ms / 1000);
    struct tm local = {0};
    localtime_r(&seconds, &local);
    strftime(out, 24, "%Y-%m-%d %H:%M", &local);
}

static cJSON *alarm_json(const alarm_item_t *item, size_t index) {
    cJSON *object = cJSON_CreateObject();
    char display[24];
    format_local_time(item->trigger_at_ms, display);
    cJSON_AddNumberToObject(object, "index", (double)(index + 1));
    cJSON_AddNumberToObject(object, "id", item->id);
    cJSON_AddNumberToObject(object, "triggerAtEpochMs", (double)item->trigger_at_ms);
    cJSON_AddStringToObject(object, "displayTime", display);
    if (item->label[0]) cJSON_AddStringToObject(object, "label", item->label);
    return object;
}

static void alarm_task(void *arg) {
    (void)arg;
    for (;;) {
        alarm_item_t current = {0};
        bool due = false;
        int64_t now_ms = (int64_t)time(NULL) * 1000;
        if (xSemaphoreTake(s_lock, portMAX_DELAY) == pdTRUE) {
            if (s_store.count > 0 && now_ms >= s_store.items[0].trigger_at_ms) {
                current = s_store.items[0];
                // Publish ownership before removing it from the persisted
                // queue. Tool list/clear calls serialize on s_lock and will
                // therefore always see the alarm in exactly one place.
                s_store.active_valid = true;
                s_store.active_alarm = current;
                remove_index_locked(0);
                // Active state is represented separately so list/clear
                // indices remain stable while this alarm rings or snoozes.
                esp_err_t persist_err = persist_locked();
                if (persist_err == ESP_OK) {
                    set_ring_state(false, false);
                    due = true;
                } else {
                    // Keep the alarm queued when durable ownership cannot be
                    // established. Retrying a little later is safer than
                    // ringing once and losing it on the next reboot.
                    memmove(&s_store.items[1], &s_store.items[0],
                            s_store.count * sizeof(s_store.items[0]));
                    s_store.items[0] = current;
                    ++s_store.count;
                    s_store.active_valid = false;
                    memset(&s_store.active_alarm, 0, sizeof(s_store.active_alarm));
                    ESP_LOGE(TAG, "cannot persist active alarm: %s",
                             esp_err_to_name(persist_err));
                }
            }
            xSemaphoreGive(s_lock);
        }
        if (!due) {
            vTaskDelay(pdMS_TO_TICKS(250));
            continue;
        }

        bool dismissed = false;
        for (unsigned attempt = 1; attempt <= ALARM_MAX_ATTEMPTS; ++attempt) {
            taskENTER_CRITICAL(&s_state_lock);
            s_ringing = true;
            taskEXIT_CRITICAL(&s_state_lock);
            int64_t ring_started = esp_timer_get_time();
            unsigned frame = 0;
            while (!dismiss_requested() &&
                   esp_timer_get_time() - ring_started < (int64_t)ALARM_RING_SECONDS * 1000000) {
                char display[24];
                format_local_time(current.trigger_at_ms, display);
                app_ui_set_alarm_visual(true, frame++, display, current.label,
                                        attempt, ALARM_MAX_ATTEMPTS);
                (void)board_port_play_alarm_burst();
                vTaskDelay(pdMS_TO_TICKS(120));
            }
            taskENTER_CRITICAL(&s_state_lock);
            s_ringing = false;
            bool was_dismissed = s_dismiss_requested;
            taskEXIT_CRITICAL(&s_state_lock);
            app_ui_set_alarm_visual(false, 0, NULL, NULL, attempt, ALARM_MAX_ATTEMPTS);
            if (was_dismissed) {
                dismissed = true;
                break;
            }
            if (attempt < ALARM_MAX_ATTEMPTS) {
                int64_t snooze_started = esp_timer_get_time();
                while (!dismiss_requested() &&
                       esp_timer_get_time() - snooze_started < (int64_t)ALARM_SNOOZE_SECONDS * 1000000) {
                    vTaskDelay(pdMS_TO_TICKS(250));
                }
                if (dismiss_requested()) {
                    dismissed = true;
                    break;
                }
            }
        }
        while (!complete_active_alarm(current.id)) {
            vTaskDelay(pdMS_TO_TICKS(1000));
        }
        publish_scheduled_state();
        set_ring_state(false, false);
        ESP_LOGI(TAG, "alarm %lu finished (%s)", (unsigned long)current.id,
                 dismissed ? "dismissed" : "attempts exhausted");
    }
}

esp_err_t alarm_manager_init(void) {
    if (s_lock || s_task) return ESP_ERR_INVALID_STATE;
    s_lock = xSemaphoreCreateMutex();
    if (!s_lock) return ESP_ERR_NO_MEM;
    nvs_handle_t nvs;
    bool store_needs_persist = false;
    if (nvs_open("alarms", NVS_READONLY, &nvs) == ESP_OK) {
        size_t size = 0;
        esp_err_t size_err = nvs_get_blob(nvs, "store", NULL, &size);
        if (size_err == ESP_OK && size == sizeof(alarm_store_t)) {
            alarm_store_t loaded = {0};
            if (nvs_get_blob(nvs, "store", &loaded, &size) == ESP_OK &&
                loaded.magic == ALARM_STORE_MAGIC && loaded.count <= ALARM_MAX_COUNT) {
                s_store = loaded;
            }
        } else if (size_err == ESP_OK && size == sizeof(alarm_store_v1_t)) {
            alarm_store_v1_t loaded = {0};
            if (nvs_get_blob(nvs, "store", &loaded, &size) == ESP_OK &&
                loaded.magic == ALARM_STORE_MAGIC_V1 && loaded.count <= ALARM_MAX_COUNT) {
                s_store.magic = ALARM_STORE_MAGIC;
                s_store.next_id = loaded.next_id;
                s_store.count = loaded.count;
                 memcpy(s_store.items, loaded.items, sizeof(loaded.items));
                 s_store.cache_next = loaded.cache_next;
                 memcpy(s_store.cache, loaded.cache, sizeof(loaded.cache));
                 store_needs_persist = true;
            }
        }
        nvs_close(nvs);
    }
    if (s_store.next_id == 0) s_store.next_id = 1;
    sort_alarms();
    // A reboot during ringing/snooze restarts the policy from attempt one.
    // The item stays authoritative and visible until completion is persisted.
    if (s_store.active_valid) {
        alarm_item_t recovered = s_store.active_alarm;
        if (s_store.count >= ALARM_MAX_COUNT) {
            ESP_LOGE(TAG, "cannot recover active alarm: capacity exhausted");
            return ESP_ERR_INVALID_SIZE;
        }
        s_store.items[s_store.count++] = recovered;
        sort_alarms();
        s_store.active_valid = false;
        memset(&s_store.active_alarm, 0, sizeof(s_store.active_alarm));
        store_needs_persist = true;
    }
    if (store_needs_persist) {
        esp_err_t migration_err = persist_locked();
        if (migration_err != ESP_OK) {
            ESP_LOGE(TAG, "cannot persist migrated alarm store: %s",
                     esp_err_to_name(migration_err));
            return migration_err;
        }
    }
    esp_err_t task_err = xTaskCreate(alarm_task, "maclaw_alarm", 5120, NULL, 7, &s_task) == pdPASS
                             ? ESP_OK : ESP_ERR_NO_MEM;
    if (task_err == ESP_OK) {
        publish_scheduled_state();
        ESP_LOGI(TAG, "alarm scheduler ready: queued=%u active=%s",
                 (unsigned)s_store.count, s_store.active_valid ? "yes" : "no");
    } else {
        vSemaphoreDelete(s_lock);
        s_lock = NULL;
    }
    return task_err;
}

bool alarm_manager_is_ringing(void) {
    taskENTER_CRITICAL(&s_state_lock);
    bool ringing = s_ringing;
    taskEXIT_CRITICAL(&s_state_lock);
    return ringing;
}

void alarm_manager_dismiss(void) {
    // The UI calls this only for the enclosure-specific control while the
    // alarm is ringing. Keep this path lock-free with respect to NVS/tool
    // operations: a touch/key down must never be lost because a tool call held
    // s_lock for longer than the old 20 ms timeout.
    taskENTER_CRITICAL(&s_state_lock);
    if (s_ringing) {
        s_dismiss_requested = true;
    }
    taskEXIT_CRITICAL(&s_state_lock);
}

esp_err_t alarm_manager_execute_tool(const char *name, cJSON *arguments,
                                     const char *idempotency_key,
                                     cJSON **out_result, char *error, size_t error_size) {
    if (!name || !out_result) return ESP_ERR_INVALID_ARG;
    *out_result = NULL;
    if (!cJSON_IsObject(arguments)) {
        snprintf(error, error_size, "arguments must be a JSON object");
        return ESP_ERR_INVALID_ARG;
    }
    // Mutating calls are replay-protected by a fixed NVS record. Reject keys
    // that cannot be stored losslessly instead of silently truncating them and
    // allowing a later retry to execute the state change again.
    bool cacheable = strcmp(name, "alarm_create") == 0 ||
                     strcmp(name, "alarm_clear_all") == 0 ||
                     strcmp(name, "alarm_clear") == 0;
    if (cacheable && idempotency_key && idempotency_key[0]) {
        size_t key_bytes = strlen(idempotency_key);
        bool ascii = true;
        for (size_t i = 0; i < key_bytes; ++i) {
            if ((unsigned char)idempotency_key[i] > 0x7f) {
                ascii = false;
                break;
            }
        }
        if (key_bytes >= ALARM_RESULT_CACHE_KEY_BYTES || !ascii) {
            snprintf(error, error_size, "idempotencyKey must be at most 63 ASCII characters");
            return ESP_ERR_INVALID_ARG;
        }
    }
    cJSON *result = cJSON_CreateObject();
    if (!result) return ESP_ERR_NO_MEM;
    esp_err_t err = ESP_OK;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(3000)) != pdTRUE) {
        cJSON_Delete(result);
        return ESP_ERR_TIMEOUT;
    }
    // Read-only list calls are safe to execute again and may return more JSON
    // than the bounded persistent result cache can hold. Cache only operations
    // that mutate state, where replay protection is required.
    if (cacheable && idempotency_key && idempotency_key[0]) {
        for (size_t i = 0; i < ALARM_RESULT_CACHE_COUNT; ++i) {
            alarm_cached_result_t *cached = &s_store.cache[i];
            if (!strcmp(cached->key, idempotency_key)) {
                esp_err_t cached_status = (esp_err_t)cached->status;
                if (cached_status == ESP_OK) {
                    cJSON_Delete(result);
                    result = cJSON_Parse(cached->result_json);
                    if (!result) result = cJSON_CreateObject();
                } else {
                    snprintf(error, error_size, "%s", cached->detail);
                }
                xSemaphoreGive(s_lock);
                if (cached_status != ESP_OK) cJSON_Delete(result);
                else *out_result = result;
                return cached_status;
            }
        }
    }
    alarm_store_t *store_before = NULL;
    if (cacheable) {
        store_before = malloc(sizeof(*store_before));
        if (!store_before) {
            xSemaphoreGive(s_lock);
            cJSON_Delete(result);
            return ESP_ERR_NO_MEM;
        }
        *store_before = s_store;
    }
    bool store_dirty = false;
    bool dismiss_active_after_commit = false;
    bool rollback_store = false;
    if (!strcmp(name, "alarm_create")) {
        cJSON *trigger = cJSON_GetObjectItemCaseSensitive(arguments, "triggerAtEpochMs");
        cJSON *label = cJSON_GetObjectItemCaseSensitive(arguments, "label");
        int64_t trigger_ms = cJSON_IsNumber(trigger) ? (int64_t)trigger->valuedouble : 0;
        int64_t now_ms = (int64_t)time(NULL) * 1000;
        if (!cJSON_IsNumber(trigger) || trigger->valuedouble != (double)trigger_ms) {
            snprintf(error, error_size, "triggerAtEpochMs must be an integer");
            err = ESP_ERR_INVALID_ARG;
        } else if (label && !cJSON_IsString(label)) {
            snprintf(error, error_size, "label must be a string");
            err = ESP_ERR_INVALID_ARG;
        } else if (cJSON_IsString(label) && strlen(label->valuestring) > ALARM_LABEL_BYTES) {
            snprintf(error, error_size, "label must be at most %d UTF-8 bytes", ALARM_LABEL_BYTES);
            err = ESP_ERR_INVALID_ARG;
        } else if (trigger_ms <= now_ms) {
            snprintf(error, error_size, "triggerAtEpochMs must be in the future");
            err = ESP_ERR_INVALID_ARG;
        } else if (s_store.count + (s_store.active_valid ? 1u : 0u) >=
                   ALARM_MAX_COUNT) {
            snprintf(error, error_size, "alarm capacity is %d", ALARM_MAX_COUNT);
            err = ESP_ERR_NO_MEM;
        } else {
            alarm_item_t *item = &s_store.items[s_store.count++];
            memset(item, 0, sizeof(*item));
            item->id = s_store.next_id++;
            uint32_t created_id = item->id;
            item->trigger_at_ms = trigger_ms;
            if (cJSON_IsString(label)) strlcpy(item->label, label->valuestring, sizeof(item->label));
            sort_alarms();
            store_dirty = true;
            for (size_t i = 0; i < s_store.count; ++i) {
                if (s_store.items[i].id == created_id) {
                    cJSON_AddItemToObject(
                        result, "alarm",
                        alarm_json(&s_store.items[i],
                                   i + (s_store.active_valid ? 1u : 0u)));
                    break;
                }
            }
            cJSON_AddNumberToObject(result, "count",
                                   s_store.count + (s_store.active_valid ? 1u : 0u));
        }
    } else if (!strcmp(name, "alarm_clear_all")) {
        size_t stored_count = s_store.count;
        bool active_alarm = active_alarm_locked(NULL);
        size_t cleared = stored_count + (active_alarm ? 1u : 0u);
        s_store.count = 0;
        if (active_alarm) {
            s_store.active_valid = false;
            memset(&s_store.active_alarm, 0, sizeof(s_store.active_alarm));
            dismiss_active_after_commit = true;
        }
        store_dirty = true;
        cJSON_AddNumberToObject(result, "cleared", cleared);
    } else if (!strcmp(name, "alarm_clear")) {
        cJSON *index_json = cJSON_GetObjectItemCaseSensitive(arguments, "index");
        int index = cJSON_IsNumber(index_json) ? index_json->valueint : 0;
        if (!cJSON_IsNumber(index_json) || index_json->valuedouble != (double)index) {
            snprintf(error, error_size, "index must be an integer");
            err = ESP_ERR_INVALID_ARG;
        } else {
            alarm_item_t active_item = {0};
            bool active_alarm = active_alarm_locked(&active_item);
            size_t visible_count = s_store.count + (active_alarm ? 1u : 0u);
            if (index < 1 || index > (int)visible_count) {
                snprintf(error, error_size, "index must be between 1 and %u",
                         (unsigned)visible_count);
                err = ESP_ERR_INVALID_ARG;
            } else if (active_alarm && index == 1) {
                alarm_item_t cleared_item = active_item;
                cJSON_AddItemToObject(result, "clearedAlarm", alarm_json(&cleared_item, 0));
                s_store.active_valid = false;
                memset(&s_store.active_alarm, 0, sizeof(s_store.active_alarm));
                store_dirty = true;
                dismiss_active_after_commit = true;
                cJSON_AddNumberToObject(result, "count", s_store.count);
            } else {
                size_t store_index = (size_t)index - 1u - (active_alarm ? 1u : 0u);
                alarm_item_t cleared_item = s_store.items[store_index];
                cJSON_AddItemToObject(result, "clearedAlarm", alarm_json(&cleared_item, index - 1));
                remove_index_locked(store_index);
                store_dirty = true;
                cJSON_AddNumberToObject(result, "count", visible_count - 1u);
            }
        }
    } else if (!strcmp(name, "alarm_list")) {
        cJSON *alarms = cJSON_AddArrayToObject(result, "alarms");
        alarm_item_t active_item = {0};
        bool active_alarm = active_alarm_locked(&active_item);
        if (alarms && active_alarm) {
            cJSON *active = alarm_json(&active_item, 0);
            cJSON_AddBoolToObject(active, "active", true);
            cJSON_AddItemToArray(alarms, active);
        }
        for (size_t i = 0; alarms && i < s_store.count; ++i) {
            cJSON_AddItemToArray(alarms, alarm_json(&s_store.items[i], i + (active_alarm ? 1u : 0u)));
        }
        cJSON_AddNumberToObject(result, "count", s_store.count + (active_alarm ? 1u : 0u));
    } else {
        snprintf(error, error_size, "unsupported client tool: %s", name);
        err = ESP_ERR_NOT_SUPPORTED;
    }
    bool deterministic_result = err == ESP_OK || err == ESP_ERR_INVALID_ARG || err == ESP_ERR_NO_MEM;
    if (cacheable && deterministic_result && idempotency_key && idempotency_key[0]) {
        // The mutation and replay record are one NVS commit. A reset can see
        // the old pair or the new pair, never a new alarm without its dedupe
        // result (which would allow the gateway retry to create it twice).
        alarm_cached_result_t *cached =
            &s_store.cache[s_store.cache_next++ % ALARM_RESULT_CACHE_COUNT];
        memset(cached, 0, sizeof(*cached));
        strlcpy(cached->key, idempotency_key, sizeof(cached->key));
        cached->status = (int32_t)err;
        if (err == ESP_OK) {
            char *encoded = cJSON_PrintUnformatted(result);
            if (!encoded || strlen(encoded) >= sizeof(cached->result_json)) {
                free(encoded);
                err = ESP_ERR_NO_MEM;
                rollback_store = true;
                snprintf(error, error_size, "tool result exceeds persistent replay capacity");
            } else {
                strlcpy(cached->result_json, encoded, sizeof(cached->result_json));
                free(encoded);
            }
        } else {
            strlcpy(cached->detail, error, sizeof(cached->detail));
        }
        store_dirty = true;
    }
    if (cacheable && store_dirty && !rollback_store) {
        esp_err_t persist_err = persist_locked();
        if (persist_err != ESP_OK) {
            s_store = *store_before;
            err = persist_err;
            snprintf(error, error_size, "cannot persist alarm change: %s",
                     esp_err_to_name(persist_err));
        } else if (dismiss_active_after_commit) {
            taskENTER_CRITICAL(&s_state_lock);
            s_dismiss_requested = true;
            taskEXIT_CRITICAL(&s_state_lock);
        }
    } else if (cacheable && rollback_store && store_before) {
        s_store = *store_before;
    }
    free(store_before);
    xSemaphoreGive(s_lock);
    if (err != ESP_OK) {
        cJSON_Delete(result);
        return err;
    }
    if (cacheable && store_dirty) publish_scheduled_state();
    *out_result = result;
    return ESP_OK;
}
