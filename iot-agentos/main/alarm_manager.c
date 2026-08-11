#include "alarm_manager.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "device_api.h"
#include "app_ui.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "nvs.h"
#include "persistence_service.h"
#include "wake_deadline_service.h"

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
/* Alarm tools, display dismissal and the deadline callback can all have
 * sampled service state before rollback closes it.  Keep this mutex as a
 * permanent lifecycle shell so an old waiter cannot obtain a freed FreeRTOS
 * object. */
static StaticSemaphore_t s_lock_storage;
static SemaphoreHandle_t s_deinit_lock;
static StaticSemaphore_t s_deinit_lock_storage;
static TaskHandle_t s_task;
static SemaphoreHandle_t s_stopped;
static wake_deadline_handle_t s_deadline = WAKE_DEADLINE_HANDLE_INVALID;
static volatile bool s_ringing;
static volatile bool s_dismiss_requested;
static volatile bool s_stop_requested;
static volatile bool s_initialized;
static portMUX_TYPE s_state_lock = portMUX_INITIALIZER_UNLOCKED;
/* Tool admission is separate from the store mutex.  Deinit closes admission
 * before joining the worker, then waits for callers that observed the old
 * service instance before it may destroy s_lock. */
static portMUX_TYPE s_lifecycle_lock = portMUX_INITIALIZER_UNLOCKED;
static uint32_t s_tool_admissions;
static alarm_manager_ring_callback_t s_ring_callback;
static void *s_ring_callback_arg;

static esp_err_t persist_locked(void);

static TickType_t stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t stop_remaining_ticks(TickType_t started, TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

/* Callers own s_lock.  The common dispatcher replaces the legacy 250 ms
 * polling loop: only the earliest queued alarm owns a wall-clock deadline. */
static void arm_next_deadline_locked(void) {
    if (!s_deadline) return;
    if (!s_store.count) {
        wake_deadline_service_cancel(s_deadline);
        return;
    }
    esp_err_t err = wake_deadline_service_arm(s_deadline, s_store.items[0].trigger_at_ms);
    if (err != ESP_OK) ESP_LOGW(TAG, "cannot arm alarm deadline: %s", esp_err_to_name(err));
}

static void alarm_deadline_callback(void *arg) {
    (void)arg;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    TaskHandle_t task = s_initialized && !s_stop_requested ? s_task : NULL;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (task) xTaskNotifyGive(task);
}

static bool stop_requested(void) {
    return s_stop_requested;
}

static bool admit_tool(void) {
    bool admitted = false;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_lock && s_initialized && !s_stop_requested) {
        ++s_tool_admissions;
        admitted = true;
    }
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return admitted;
}

static void release_tool(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_tool_admissions > 0) --s_tool_admissions;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

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
    return persistence_service_write_blob("alarms", "store",
                                          &s_store, sizeof(s_store));
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
        (void)ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
        if (stop_requested()) break;
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
            arm_next_deadline_locked();
            xSemaphoreGive(s_lock);
        }
        if (!due || stop_requested()) {
            continue;
        }

        device_power_lease_t ring_lease = DEVICE_POWER_LEASE_INVALID;
        device_status_t lease_status = device_power_lease_acquire(
            DEVICE_POWER_LEASE_OWNER_ALARM, &ring_lease);
        if (lease_status != DEVICE_STATUS_OK) {
            /* A ringing alarm remains safety-critical even if all fixed lease
             * slots are unexpectedly exhausted.  Continue the existing alarm
             * policy and leave a diagnostic; normal images reserve ample
             * slots for the bounded foreground domains. */
            ESP_LOGE(TAG, "cannot acquire alarm power lease: status=%d", lease_status);
        }
        bool dismissed = false;
        for (unsigned attempt = 1; attempt <= ALARM_MAX_ATTEMPTS && !stop_requested(); ++attempt) {
            taskENTER_CRITICAL(&s_state_lock);
            s_ringing = true;
            alarm_manager_ring_callback_t ring_callback = s_ring_callback;
            void *ring_callback_arg = s_ring_callback_arg;
            taskEXIT_CRITICAL(&s_state_lock);
            if (ring_callback) ring_callback(ring_callback_arg);
            int64_t ring_started = esp_timer_get_time();
            unsigned frame = 0;
            while (!stop_requested() && !dismiss_requested() &&
                   esp_timer_get_time() - ring_started < (int64_t)ALARM_RING_SECONDS * 1000000) {
                char display[24];
                format_local_time(current.trigger_at_ms, display);
                app_ui_set_alarm_visual(true, frame++, display, current.label,
                                        attempt, ALARM_MAX_ATTEMPTS);
                (void)device_audio_play_alarm_burst();
                vTaskDelay(pdMS_TO_TICKS(120));
            }
            taskENTER_CRITICAL(&s_state_lock);
            s_ringing = false;
            bool was_dismissed = s_dismiss_requested;
            taskEXIT_CRITICAL(&s_state_lock);
            app_ui_set_alarm_visual(false, 0, NULL, NULL, attempt, ALARM_MAX_ATTEMPTS);
            if (stop_requested() || was_dismissed) {
                dismissed = true;
                break;
            }
            if (attempt < ALARM_MAX_ATTEMPTS) {
                int64_t snooze_started = esp_timer_get_time();
                while (!stop_requested() && !dismiss_requested() &&
                       esp_timer_get_time() - snooze_started < (int64_t)ALARM_SNOOZE_SECONDS * 1000000) {
                    vTaskDelay(pdMS_TO_TICKS(250));
                }
                if (stop_requested() || dismiss_requested()) {
                    dismissed = true;
                    break;
                }
            }
        }
        /* Stopping is not a dismissal: leave the already-persisted active
         * record intact so init can recover the reminder after restart. */
        while (!stop_requested() && !complete_active_alarm(current.id)) {
            vTaskDelay(pdMS_TO_TICKS(1000));
        }
        if (!stop_requested() && xSemaphoreTake(s_lock, portMAX_DELAY) == pdTRUE) {
            arm_next_deadline_locked();
            xSemaphoreGive(s_lock);
        }
        publish_scheduled_state();
        set_ring_state(false, false);
        device_power_lease_release(ring_lease);
        if (!stop_requested()) {
            ESP_LOGI(TAG, "alarm %lu finished (%s)", (unsigned long)current.id,
                     dismissed ? "dismissed" : "attempts exhausted");
        }
    }
    set_ring_state(false, false);
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_task = NULL;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    /* The completion semaphore is the final worker→deinit handoff.  Do not
     * access service-owned deadline/store state after giving it. */
    if (s_stopped) xSemaphoreGive(s_stopped);
    vTaskDelete(NULL);
}

esp_err_t alarm_manager_init(void) {
    if (!persistence_service_is_initialized()) return ESP_ERR_INVALID_STATE;
    if (!s_lock) s_lock = xSemaphoreCreateMutexStatic(&s_lock_storage);
    if (!s_deinit_lock) s_deinit_lock = xSemaphoreCreateMutexStatic(&s_deinit_lock_storage);
    if (!s_lock || !s_deinit_lock) return ESP_ERR_NO_MEM;
    if (xSemaphoreTake(s_deinit_lock, pdMS_TO_TICKS(3000)) != pdTRUE) return ESP_ERR_TIMEOUT;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool already_ready = s_initialized;
    const bool closing = s_stop_requested || s_task || s_stopped;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (already_ready) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_OK;
    }
    if (closing) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_ERR_INVALID_STATE;
    }
    s_stopped = xSemaphoreCreateBinary();
    if (!s_stopped) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_ERR_NO_MEM;
    }
    bool store_needs_persist = false;
    size_t size = 0;
    esp_err_t size_err = persistence_service_read_blob("alarms", "store", NULL, &size);
    if (size_err != ESP_OK && size_err != ESP_ERR_NVS_NOT_FOUND) {
        ESP_LOGE(TAG, "cannot inspect alarm store: %s", esp_err_to_name(size_err));
        vSemaphoreDelete(s_stopped);
        s_stopped = NULL;
        xSemaphoreGive(s_deinit_lock);
        return size_err;
    }
    if (size_err == ESP_OK && size == sizeof(alarm_store_t)) {
        alarm_store_t loaded = {0};
        size_t loaded_size = sizeof(loaded);
        esp_err_t read_err = persistence_service_read_blob("alarms", "store",
                                                            &loaded, &loaded_size);
        if (read_err != ESP_OK) {
            ESP_LOGE(TAG, "cannot load alarm store: %s", esp_err_to_name(read_err));
            vSemaphoreDelete(s_stopped);
            s_stopped = NULL;
            xSemaphoreGive(s_deinit_lock);
            return read_err;
        }
        if (loaded.magic == ALARM_STORE_MAGIC && loaded.count <= ALARM_MAX_COUNT) {
            s_store = loaded;
        } else {
            ESP_LOGW(TAG, "ignoring invalid alarm store");
        }
    } else if (size_err == ESP_OK && size == sizeof(alarm_store_v1_t)) {
        alarm_store_v1_t loaded = {0};
        size_t loaded_size = sizeof(loaded);
        esp_err_t read_err = persistence_service_read_blob("alarms", "store",
                                                            &loaded, &loaded_size);
        if (read_err != ESP_OK) {
            ESP_LOGE(TAG, "cannot load legacy alarm store: %s", esp_err_to_name(read_err));
            vSemaphoreDelete(s_stopped);
            s_stopped = NULL;
            xSemaphoreGive(s_deinit_lock);
            return read_err;
        }
        if (loaded.magic == ALARM_STORE_MAGIC_V1 && loaded.count <= ALARM_MAX_COUNT) {
            s_store.magic = ALARM_STORE_MAGIC;
            s_store.next_id = loaded.next_id;
            s_store.count = loaded.count;
            memcpy(s_store.items, loaded.items, sizeof(loaded.items));
            s_store.cache_next = loaded.cache_next;
            memcpy(s_store.cache, loaded.cache, sizeof(loaded.cache));
            store_needs_persist = true;
        } else {
            ESP_LOGW(TAG, "ignoring invalid legacy alarm store");
        }
    } else if (size_err == ESP_OK) {
        ESP_LOGW(TAG, "ignoring incompatible alarm store (%u bytes)", (unsigned)size);
    }
    if (s_store.next_id == 0) s_store.next_id = 1;
    sort_alarms();
    // A reboot during ringing/snooze restarts the policy from attempt one.
    // The item stays authoritative and visible until completion is persisted.
    if (s_store.active_valid) {
        alarm_item_t recovered = s_store.active_alarm;
        if (s_store.count >= ALARM_MAX_COUNT) {
            ESP_LOGE(TAG, "cannot recover active alarm: capacity exhausted");
            vSemaphoreDelete(s_stopped);
            s_stopped = NULL;
            xSemaphoreGive(s_deinit_lock);
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
            vSemaphoreDelete(s_stopped);
            s_stopped = NULL;
            xSemaphoreGive(s_deinit_lock);
            return migration_err;
        }
    }
    esp_err_t deadline_err = wake_deadline_service_register(alarm_deadline_callback, NULL,
                                                             &s_deadline);
    if (deadline_err != ESP_OK) {
        vSemaphoreDelete(s_stopped);
        s_stopped = NULL;
        xSemaphoreGive(s_deinit_lock);
        return deadline_err;
    }
    esp_err_t task_err = xTaskCreate(alarm_task, "maclaw_alarm", 5120, NULL, 7, &s_task) == pdPASS
                             ? ESP_OK : ESP_ERR_NO_MEM;
    if (task_err == ESP_OK) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        s_initialized = true;
        s_stop_requested = false;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (xSemaphoreTake(s_lock, portMAX_DELAY) == pdTRUE) {
            arm_next_deadline_locked();
            xSemaphoreGive(s_lock);
        }
        if (!stop_requested()) publish_scheduled_state();
        ESP_LOGI(TAG, "alarm scheduler ready: queued=%u active=%s",
                 (unsigned)s_store.count, s_store.active_valid ? "yes" : "no");
    } else {
        /* A worker was never started.  Still close the registered callback
         * with an explicit, bounded hand-off rather than the legacy 1-second
         * convenience wrapper. */
        (void)wake_deadline_service_unregister_with_timeout(s_deadline, 1000);
        s_deadline = WAKE_DEADLINE_HANDLE_INVALID;
        vSemaphoreDelete(s_stopped);
        s_stopped = NULL;
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (task_err != ESP_OK) s_stop_requested = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    xSemaphoreGive(s_deinit_lock);
    return task_err;
}

esp_err_t alarm_manager_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    if (!s_lock || !s_deinit_lock) return ESP_OK;
    if (s_task && xTaskGetCurrentTaskHandle() == s_task) return ESP_ERR_INVALID_STATE;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    if (xSemaphoreTake(s_deinit_lock, budget) != pdTRUE) return ESP_ERR_TIMEOUT;
    TickType_t remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_ERR_TIMEOUT;
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (!s_initialized && !s_stop_requested) {
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        xSemaphoreGive(s_lock);
        xSemaphoreGive(s_deinit_lock);
        return ESP_OK;
    }
    s_stop_requested = true;
    s_initialized = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    /* The parent deadline dispatcher is drained only after Alarm.  Stop its
     * admission first, then release our slot while it is still accepting
     * unregister calls; reversing this order would leave a stale client
     * callback in the permanent dispatcher lock shell. */
    if (s_deadline) wake_deadline_service_cancel(s_deadline);
    TaskHandle_t task = s_task;
    xSemaphoreGive(s_lock);
    if (task) {
        xTaskNotifyGive(task);
        remaining = stop_remaining_ticks(started, budget);
        if (remaining == 0 || xSemaphoreTake(s_stopped, remaining) != pdTRUE) {
            xSemaphoreGive(s_deinit_lock);
            return ESP_ERR_TIMEOUT;
        }
    } else if (s_stopped) {
        remaining = stop_remaining_ticks(started, budget);
        if (remaining == 0 || xSemaphoreTake(s_stopped, remaining) != pdTRUE) {
            xSemaphoreGive(s_deinit_lock);
            return ESP_ERR_TIMEOUT;
        }
    }
    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        uint32_t admissions = s_tool_admissions;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (admissions == 0) break;
        if (stop_remaining_ticks(started, budget) == 0) {
            xSemaphoreGive(s_deinit_lock);
            return ESP_ERR_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    /* A tool call may have been waiting while stop was requested.  Re-acquire
     * the mutex before destruction so it observes the stop boundary first. */
    remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_ERR_TIMEOUT;
    }
    if (s_deadline) {
        /* A callback can have been copied out by the shared dispatcher just
         * before the alarm worker was joined.  Drain that callback inside the
         * same parent deadline before clearing the callback-owned state.  The
         * alarm callback only takes s_lifecycle_lock, not s_lock, so holding
         * this store mutex cannot deadlock the drain. */
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
    /* The ring worker has joined, so its last callback copy is complete.
     * Keep callback ownership under the same state lock used for invocation.
     */
    taskENTER_CRITICAL(&s_state_lock);
    s_ring_callback = NULL;
    s_ring_callback_arg = NULL;
    taskEXIT_CRITICAL(&s_state_lock);
    xSemaphoreGive(s_lock);
    vSemaphoreDelete(s_stopped);
    s_stopped = NULL;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_stop_requested = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    xSemaphoreGive(s_deinit_lock);
    set_ring_state(false, false);
    ESP_LOGI(TAG, "alarm scheduler stopped");
    return ESP_OK;
}

void alarm_manager_set_ring_callback(alarm_manager_ring_callback_t callback,
                                     void *arg) {
    /* Lifecycle registers this static application callback after init.  Do
     * not let a late startup/recovery caller repopulate it once deinit has
     * closed the scheduler; the worker owns the only invocation path. */
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool ready = s_initialized && !s_stop_requested;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (!ready) return;
    taskENTER_CRITICAL(&s_state_lock);
    s_ring_callback = callback;
    s_ring_callback_arg = arg;
    taskEXIT_CRITICAL(&s_state_lock);
}

bool alarm_manager_is_ringing(void) {
    taskENTER_CRITICAL(&s_state_lock);
    bool ringing = s_ringing;
    taskEXIT_CRITICAL(&s_state_lock);
    return ringing;
}

bool alarm_manager_is_initialized(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    bool initialized = s_lock != NULL && s_task != NULL && s_initialized && !s_stop_requested;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return initialized;
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
    if (!name || !out_result || !admit_tool()) return ESP_ERR_INVALID_STATE;
    *out_result = NULL;
    if (!cJSON_IsObject(arguments)) {
        snprintf(error, error_size, "arguments must be a JSON object");
        release_tool();
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
            release_tool();
            return ESP_ERR_INVALID_ARG;
        }
    }
    cJSON *result = cJSON_CreateObject();
    if (!result) {
        release_tool();
        return ESP_ERR_NO_MEM;
    }
    esp_err_t err = ESP_OK;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(3000)) != pdTRUE) {
        cJSON_Delete(result);
        release_tool();
        return ESP_ERR_TIMEOUT;
    }
    if (s_stop_requested) {
        xSemaphoreGive(s_lock);
        cJSON_Delete(result);
        release_tool();
        return ESP_ERR_INVALID_STATE;
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
                release_tool();
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
            release_tool();
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
    /* s_lock is still owned here.  Re-arm before releasing it so a just
     * persisted earlier alarm cannot be missed between the durable commit and
     * dispatcher update. */
    if (cacheable && store_dirty) arm_next_deadline_locked();
    free(store_before);
    xSemaphoreGive(s_lock);
    if (err != ESP_OK) {
        cJSON_Delete(result);
        release_tool();
        return err;
    }
    if (cacheable && store_dirty) publish_scheduled_state();
    if (cacheable && store_dirty) {
        /* A new earlier alarm needs immediate dispatcher re-evaluation; an
         * expired alarm is delivered by this same notification. */
        taskENTER_CRITICAL(&s_lifecycle_lock);
        TaskHandle_t task = s_initialized && !s_stop_requested ? s_task : NULL;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (task) xTaskNotifyGive(task);
    }
    *out_result = result;
    release_tool();
    return ESP_OK;
}
