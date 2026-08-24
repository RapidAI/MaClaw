#include "alarm_manager.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "esp_err.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "services/alarm_service.h"

/*
 * Thin facade over services/alarm_service.c.
 *
 * This translation unit deliberately retains the Device Tool Registry cJSON
 * adapter, the tool-admission counter and the System Sleep participant
 * contract: the HAL boundary gate requires alarm_manager_execute_tool,
 * alarm_manager_prepare_system_sleep/abort, s_tool_admissions,
 * s_system_sleep_preparing and a cooperative xTaskNotifyGive here, and
 * power_service.c calls the prepare/abort pair directly.  All domain,
 * repository, scheduler and feedback behaviour lives in alarm_service.
 */

/* Tool admission is separate from the store mutex.  Deinit closes admission
 * before joining the worker, then waits for callers that observed the old
 * service instance before it may destroy scheduler state. */
static portMUX_TYPE s_lifecycle_lock = portMUX_INITIALIZER_UNLOCKED;
static uint32_t s_tool_admissions;
/* Separate reversible System Sleep admission from terminal deinit. The alarm
 * worker and deadline registration stay alive: a later profile Power commit
 * will need their next-deadline facts, while rollback must resume ordinary
 * scheduling without recreating a task or touching persisted alarms. */
static volatile bool s_system_sleep_preparing;

static TickType_t stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t stop_remaining_ticks(TickType_t started, TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

static bool alarm_manager_sleep_prepare_query(void) {
    return s_system_sleep_preparing;
}

static bool admit_tool(void) {
    bool admitted = false;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (alarm_service_is_ready() && !s_system_sleep_preparing) {
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

static void format_local_time(int64_t epoch_ms, char out[24]) {
    time_t seconds = (time_t)(epoch_ms / 1000);
    struct tm local = {0};
    localtime_r(&seconds, &local);
    strftime(out, 24, "%Y-%m-%d %H:%M", &local);
}

static cJSON *alarm_json(const alarm_service_item_t *item, size_t index) {
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

void alarm_manager_set_ring_callback(alarm_manager_ring_callback_t callback,
                                     void *arg) {
    alarm_service_set_ring_callback((alarm_service_ring_callback_t)callback, arg);
}

device_status_t alarm_manager_init(void) {
    alarm_service_set_sleep_prepare_query(alarm_manager_sleep_prepare_query);
    return alarm_service_init();
}

device_status_t alarm_manager_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    device_status_t status = alarm_service_deinit_close_and_join(timeout_ms);
    if (status != DEVICE_STATUS_OK) return status;
    /* Existing tool calls may have crossed their admission check before the
     * stop boundary. Wait for them to leave before the deadline/store
     * teardown so a tool cannot hold service state across destruction. */
    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        uint32_t admissions = s_tool_admissions;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (admissions == 0) break;
        if (stop_remaining_ticks(started, budget) == 0) {
            alarm_service_deinit_abort();
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    const uint32_t remaining_ms =
        (uint32_t)pdTICKS_TO_MS(stop_remaining_ticks(started, budget));
    return alarm_service_deinit_finish(remaining_ms);
}

device_status_t alarm_manager_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    device_status_t status = alarm_service_lifecycle_acquire(timeout_ms);
    if (status == DEVICE_STATUS_UNAVAILABLE) return DEVICE_STATUS_UNAVAILABLE;
    if (status != DEVICE_STATUS_OK) return DEVICE_STATUS_TIMEOUT;

    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool already_preparing = s_system_sleep_preparing;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (!alarm_service_is_ready() || already_preparing) {
        alarm_service_lifecycle_release();
        return DEVICE_STATUS_BUSY;
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_system_sleep_preparing = true;
    taskEXIT_CRITICAL(&s_lifecycle_lock);

    /* Existing tool calls may have crossed their admission check before the
     * transaction marker. Wait for them to leave before examining durable
     * alarm ownership, so a tool cannot concurrently add/remove an alarm
     * behind the later profile wake configuration. */
    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        const uint32_t admissions = s_tool_admissions;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (admissions == 0) break;
        if (stop_remaining_ticks(started, budget) == 0) {
            alarm_service_lifecycle_release();
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(1);
    }

    TickType_t remaining = stop_remaining_ticks(started, budget);
    bool active_alarm = false;
    status = remaining == 0
                 ? DEVICE_STATUS_TIMEOUT
                 : alarm_service_active_alarm_present(
                       (uint32_t)pdTICKS_TO_MS(remaining), &active_alarm);
    /* A persisted active owner represents a ringing/recovery policy whose
     * eventual device wake semantics have not yet been materialized as an RTC
     * source. Refuse the transaction rather than stranding it below a future
     * sleep commit. Queued future alarms remain intact for the later explicit
     * alarm-to-profile-wake mapping. */
    if (status != DEVICE_STATUS_OK || active_alarm) {
        /* Keep tool/scheduler admission closed until the outer transaction
         * performs ABORT. In particular, a ringing alarm must not race a
         * sibling participant that is still preparing. */
        alarm_service_lifecycle_release();
        return status != DEVICE_STATUS_OK ? DEVICE_STATUS_TIMEOUT
                                          : DEVICE_STATUS_BUSY;
    }
    alarm_service_lifecycle_release();
    return DEVICE_STATUS_OK;
}

void alarm_manager_abort_system_sleep_prepare(void) {
    if (alarm_service_lifecycle_acquire(3000) != DEVICE_STATUS_OK) return;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool was_preparing = s_system_sleep_preparing;
    s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    /* Wake a pending or newly-due alarm after rollback; it will re-read the
     * durable queue under the store lock, so no stale pre-prepare deadline is
     * used. */
    uintptr_t task = alarm_service_scheduler_task_token();
    if (was_preparing && task) xTaskNotifyGive((TaskHandle_t)task);
    alarm_service_lifecycle_release();
}

bool alarm_manager_is_ringing(void) {
    return alarm_service_is_ringing();
}

bool alarm_manager_is_initialized(void) {
    return alarm_service_is_ready();
}

void alarm_manager_dismiss(void) {
    alarm_service_dismiss();
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
        if (key_bytes >= ALARM_SERVICE_RESULT_CACHE_KEY_BYTES || !ascii) {
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
    if (alarm_service_tool_store_lock(3000) != DEVICE_STATUS_OK) {
        cJSON_Delete(result);
        release_tool();
        return ESP_ERR_TIMEOUT;
    }
    if (alarm_service_stop_requested()) {
        alarm_service_tool_store_unlock();
        cJSON_Delete(result);
        release_tool();
        return ESP_ERR_INVALID_STATE;
    }
    // Read-only list calls are safe to execute again and may return more JSON
    // than the bounded persistent result cache can hold. Cache only operations
    // that mutate state, where replay protection is required.
    if (cacheable && idempotency_key && idempotency_key[0]) {
        int32_t cached_status = 0;
        char cached_detail[96] = {0};
        char cached_json[ALARM_SERVICE_RESULT_CACHE_JSON_BYTES] = {0};
        if (alarm_service_replay_find(idempotency_key, &cached_status,
                                      cached_detail, sizeof(cached_detail),
                                      cached_json, sizeof(cached_json))) {
            esp_err_t cached_err = (esp_err_t)cached_status;
            if (cached_err == ESP_OK) {
                cJSON_Delete(result);
                result = cJSON_Parse(cached_json);
                if (!result) result = cJSON_CreateObject();
            } else {
                snprintf(error, error_size, "%s", cached_detail);
            }
            alarm_service_tool_store_unlock();
            if (cached_err != ESP_OK) cJSON_Delete(result);
            else *out_result = result;
            release_tool();
            return cached_err;
        }
    }
    void *store_before = NULL;
    if (cacheable) {
        store_before = alarm_service_store_snapshot();
        if (!store_before) {
            alarm_service_tool_store_unlock();
            cJSON_Delete(result);
            release_tool();
            return ESP_ERR_NO_MEM;
        }
    }
    bool store_dirty = false;
    bool dismiss_active_after_commit = false;
    bool rollback_store = false;
    if (!strcmp(name, "alarm_create")) {
        cJSON *trigger = cJSON_GetObjectItemCaseSensitive(arguments, "triggerAtEpochMs");
        cJSON *label = cJSON_GetObjectItemCaseSensitive(arguments, "label");
        int64_t trigger_ms = cJSON_IsNumber(trigger) ? (int64_t)trigger->valuedouble : 0;
        if (!cJSON_IsNumber(trigger) || trigger->valuedouble != (double)trigger_ms) {
            snprintf(error, error_size, "triggerAtEpochMs must be an integer");
            err = ESP_ERR_INVALID_ARG;
        } else if (label && !cJSON_IsString(label)) {
            snprintf(error, error_size, "label must be a string");
            err = ESP_ERR_INVALID_ARG;
        } else {
            alarm_service_item_t created = {0};
            uint32_t display_index = 0;
            uint32_t count = 0;
            device_status_t create_status = alarm_service_create(
                trigger_ms, cJSON_IsString(label) ? label->valuestring : NULL,
                &created, &display_index, &count, error, error_size);
            if (create_status == DEVICE_STATUS_OK) {
                store_dirty = true;
                cJSON_AddItemToObject(result, "alarm",
                                      alarm_json(&created, display_index));
                cJSON_AddNumberToObject(result, "count", count);
            } else {
                err = create_status == DEVICE_STATUS_RESOURCE_EXHAUSTED
                          ? ESP_ERR_NO_MEM
                          : ESP_ERR_INVALID_ARG;
            }
        }
    } else if (!strcmp(name, "alarm_clear_all")) {
        uint32_t cleared = 0;
        bool dismiss_active = false;
        (void)alarm_service_clear_all(&cleared, &dismiss_active);
        dismiss_active_after_commit = dismiss_active;
        store_dirty = true;
        cJSON_AddNumberToObject(result, "cleared", cleared);
    } else if (!strcmp(name, "alarm_clear")) {
        cJSON *index_json = cJSON_GetObjectItemCaseSensitive(arguments, "index");
        int index = cJSON_IsNumber(index_json) ? index_json->valueint : 0;
        if (!cJSON_IsNumber(index_json) || index_json->valuedouble != (double)index) {
            snprintf(error, error_size, "index must be an integer");
            err = ESP_ERR_INVALID_ARG;
        } else {
            alarm_service_item_t cleared_item = {0};
            uint32_t display_index = 0;
            uint32_t count = 0;
            bool dismiss_active = false;
            device_status_t clear_status = alarm_service_clear(
                index, &cleared_item, &display_index, &count, &dismiss_active,
                error, error_size);
            if (clear_status != DEVICE_STATUS_OK) {
                err = ESP_ERR_INVALID_ARG;
            } else {
                cJSON_AddItemToObject(result, "clearedAlarm",
                                      alarm_json(&cleared_item, display_index));
                store_dirty = true;
                dismiss_active_after_commit = dismiss_active;
                cJSON_AddNumberToObject(result, "count", count);
            }
        }
    } else if (!strcmp(name, "alarm_list")) {
        cJSON *alarms = cJSON_AddArrayToObject(result, "alarms");
        uint32_t total = alarm_service_list_count();
        for (uint32_t i = 0; alarms && i < total; ++i) {
            alarm_service_item_t item = {0};
            bool is_active = false;
            if (!alarm_service_list_entry(i, &item, &is_active)) continue;
            cJSON *entry = alarm_json(&item, i);
            if (is_active) cJSON_AddBoolToObject(entry, "active", true);
            cJSON_AddItemToArray(alarms, entry);
        }
        cJSON_AddNumberToObject(result, "count", total);
    } else {
        snprintf(error, error_size, "unsupported client tool: %s", name);
        err = ESP_ERR_NOT_SUPPORTED;
    }
    bool deterministic_result = err == ESP_OK || err == ESP_ERR_INVALID_ARG || err == ESP_ERR_NO_MEM;
    if (cacheable && deterministic_result && idempotency_key && idempotency_key[0]) {
        // The mutation and replay record are one NVS commit. A reset can see
        // the old pair or the new pair, never a new alarm without its dedupe
        // result (which would allow the gateway retry to create it twice).
        char *encoded = err == ESP_OK ? cJSON_PrintUnformatted(result) : NULL;
        device_status_t record_status = alarm_service_replay_record(
            idempotency_key, (int32_t)err, error, encoded);
        free(encoded);
        if (record_status != DEVICE_STATUS_OK) {
            err = ESP_ERR_NO_MEM;
            rollback_store = true;
            snprintf(error, error_size, "tool result exceeds persistent replay capacity");
        }
        store_dirty = true;
    }
    if (cacheable && store_dirty && !rollback_store) {
        int32_t persist_err = alarm_service_persist();
        if (persist_err != 0) {
            alarm_service_store_rollback(store_before);
            err = (esp_err_t)persist_err;
            snprintf(error, error_size, "cannot persist alarm change: %s",
                     esp_err_to_name(err));
        } else if (dismiss_active_after_commit) {
            alarm_service_request_dismiss();
        }
    } else if (cacheable && rollback_store && store_before) {
        alarm_service_store_rollback(store_before);
    }
    /* The store lock is still owned here.  Re-arm before releasing it so a
     * just persisted earlier alarm cannot be missed between the durable
     * commit and dispatcher update. */
    if (cacheable && store_dirty) alarm_service_rearm_deadline();
    alarm_service_store_snapshot_free(store_before);
    alarm_service_tool_store_unlock();
    if (err != ESP_OK) {
        cJSON_Delete(result);
        release_tool();
        return err;
    }
    if (cacheable && store_dirty) alarm_service_publish_scheduled();
    if (cacheable && store_dirty) {
        /* A new earlier alarm needs immediate dispatcher re-evaluation; an
         * expired alarm is delivered by this same notification. */
        uintptr_t task = alarm_service_scheduler_task_token();
        if (task) xTaskNotifyGive((TaskHandle_t)task);
    }
    *out_result = result;
    release_tool();
    return ESP_OK;
}
