#include "meeting_recovery_service.h"

#include <string.h>

#include "persistence_service.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "esp_timer.h"

#define MEETING_RECOVERY_NAMESPACE "maclaw"
#define MEETING_RECOVERY_STORE_KEY "meeting_recovery"
#define MEETING_RECOVERY_STORE_MAGIC 0x4d525331u /* MRS1 */
#define MEETING_RECOVERY_STORE_VERSION 1u

typedef struct {
    uint32_t magic;
    uint32_t version;
    uint8_t pending;
    uint8_t reserved[3];
    int32_t next_chunk;
    int32_t phase;
    char recording_id[MEETING_RECOVERY_RECORDING_ID_CAPACITY];
} meeting_recovery_store_t;

static portMUX_TYPE s_lifecycle_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_initialized;
static bool s_stopping;
static volatile bool s_initializing;
static uint32_t s_active_calls;
/* Legacy field-by-field import is private migration code. The public service
 * reports only stable Device API statuses to meeting/business callers. */
static device_status_t device_status_from_legacy_error(esp_err_t status) {
    switch (status) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

static TickType_t meeting_recovery_stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t meeting_recovery_stop_remaining_ticks(TickType_t started,
                                                        TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

static bool admission_enter(void) {
    bool admitted = false;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_initialized && !s_stopping) {
        ++s_active_calls;
        admitted = true;
    }
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return admitted;
}

static void admission_exit(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_active_calls > 0) --s_active_calls;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

static void clear_snapshot(meeting_recovery_snapshot_t *snapshot) {
    memset(snapshot, 0, sizeof(*snapshot));
}

static bool valid_store(const meeting_recovery_store_t *store) {
    return store && store->magic == MEETING_RECOVERY_STORE_MAGIC &&
           store->version == MEETING_RECOVERY_STORE_VERSION && store->pending <= 1 &&
           store->next_chunk >= 0 && store->phase >= 0 && store->phase <= 2 &&
           memchr(store->recording_id, '\0', sizeof(store->recording_id)) != NULL;
}

static bool valid_snapshot(const meeting_recovery_snapshot_t *snapshot) {
    return snapshot && snapshot->next_chunk >= 0 && snapshot->phase >= 0 &&
           snapshot->phase <= 2 &&
           memchr(snapshot->recording_id, '\0', sizeof(snapshot->recording_id)) != NULL;
}

static esp_err_t load_legacy(meeting_recovery_snapshot_t *snapshot, bool *out_found) {
    if (!snapshot || !out_found) return ESP_ERR_INVALID_ARG;
    clear_snapshot(snapshot);
    *out_found = false;
    int32_t value = 0;
    esp_err_t err = device_status_to_platform_error(persistence_service_read_i32(MEETING_RECOVERY_NAMESPACE, "meet_next", &value));
    if (err == ESP_OK) {
        snapshot->next_chunk = value;
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) return err;
    err = device_status_to_platform_error(persistence_service_read_i32(MEETING_RECOVERY_NAMESPACE, "meet_phase", &value));
    if (err == ESP_OK) {
        snapshot->phase = value;
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) return err;
    size_t size = sizeof(snapshot->recording_id);
    err = device_status_to_platform_error(persistence_service_read_string(MEETING_RECOVERY_NAMESPACE, "meet_id",
                                          snapshot->recording_id, &size));
    if (err == ESP_OK) {
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) return err;
    uint8_t pending = 0;
    err = device_status_to_platform_error(persistence_service_read_u8(MEETING_RECOVERY_NAMESPACE, "meet_pending", &pending));
    if (err == ESP_OK) {
        if (pending > 1) return ESP_ERR_INVALID_STATE;
        snapshot->pending = pending != 0;
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) return err;
    if (*out_found && (snapshot->next_chunk < 0 || snapshot->phase < 0 || snapshot->phase > 2)) {
        return ESP_ERR_INVALID_STATE;
    }
    return ESP_OK;
}

device_status_t meeting_recovery_service_init(void) {
    if (!persistence_service_is_initialized()) return DEVICE_STATUS_BUSY;
    bool expected = false;
    if (!__atomic_compare_exchange_n(&s_initializing, &expected, true, false,
                                     __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
        return DEVICE_STATUS_BUSY;
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_initialized && !s_stopping) {
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_OK;
    }
    if (s_stopping) {
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_BUSY;
    }
    s_initialized = true;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
    return DEVICE_STATUS_OK;
}

device_status_t meeting_recovery_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = meeting_recovery_stop_timeout_ticks(timeout_ms);
    while (__atomic_load_n(&s_initializing, __ATOMIC_ACQUIRE)) {
        if (meeting_recovery_stop_remaining_ticks(started, budget) == 0) {
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool already_stopped = !s_initialized && !s_stopping;
    s_initialized = false;
    s_stopping = true;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (already_stopped) return DEVICE_STATUS_OK;
    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        const uint32_t active_calls = s_active_calls;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (active_calls == 0) break;
        if (meeting_recovery_stop_remaining_ticks(started, budget) == 0) {
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_stopping = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return DEVICE_STATUS_OK;
}

bool meeting_recovery_service_is_initialized(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool initialized = s_initialized && !s_stopping &&
                             !__atomic_load_n(&s_initializing, __ATOMIC_ACQUIRE);
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return initialized;
}

device_status_t meeting_recovery_service_load(meeting_recovery_snapshot_t *out_snapshot) {
    if (!out_snapshot) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!admission_enter()) return DEVICE_STATUS_BUSY;

    device_status_t result = DEVICE_STATUS_OK;
    clear_snapshot(out_snapshot);
    meeting_recovery_store_t store = {0};
    size_t size = sizeof(store);
    device_status_t persistence_status = persistence_service_read_blob(
        MEETING_RECOVERY_NAMESPACE, MEETING_RECOVERY_STORE_KEY, &store, &size);
    if (persistence_status == DEVICE_STATUS_NOT_FOUND) {
        bool legacy_found = false;
        esp_err_t legacy_status = load_legacy(out_snapshot, &legacy_found);
        if (legacy_status != ESP_OK || !legacy_found) {
            result = legacy_status == ESP_OK ? DEVICE_STATUS_NOT_FOUND
                                             : device_status_from_legacy_error(legacy_status);
            goto done;
        }
        meeting_recovery_store_t legacy_store = {
            .magic = MEETING_RECOVERY_STORE_MAGIC,
            .version = MEETING_RECOVERY_STORE_VERSION,
            .pending = out_snapshot->pending ? 1u : 0u,
            .next_chunk = out_snapshot->next_chunk,
            .phase = out_snapshot->phase,
        };
        strlcpy(legacy_store.recording_id, out_snapshot->recording_id,
                sizeof(legacy_store.recording_id));
        result = persistence_service_write_blob(MEETING_RECOVERY_NAMESPACE,
                                                MEETING_RECOVERY_STORE_KEY,
                                                &legacy_store, sizeof(legacy_store));
        goto done;
    }
    if (persistence_status != DEVICE_STATUS_OK) {
        result = persistence_status;
        goto done;
    }
    if (size != sizeof(store) || !valid_store(&store)) {
        result = DEVICE_STATUS_INTERNAL_ERROR;
        goto done;
    }
    out_snapshot->pending = store.pending != 0;
    out_snapshot->next_chunk = store.next_chunk;
    out_snapshot->phase = store.phase;
    strlcpy(out_snapshot->recording_id, store.recording_id,
            sizeof(out_snapshot->recording_id));
done:
    admission_exit();
    return result;
}
device_status_t meeting_recovery_service_save(const meeting_recovery_snapshot_t *snapshot) {
    if (!valid_snapshot(snapshot)) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!admission_enter()) return DEVICE_STATUS_BUSY;
    meeting_recovery_store_t store = {
        .magic = MEETING_RECOVERY_STORE_MAGIC,
        .version = MEETING_RECOVERY_STORE_VERSION,
        .pending = snapshot->pending ? 1u : 0u,
        .next_chunk = snapshot->next_chunk,
        .phase = snapshot->phase,
    };
    strlcpy(store.recording_id, snapshot->recording_id, sizeof(store.recording_id));
    device_status_t status = persistence_service_write_blob(MEETING_RECOVERY_NAMESPACE,
                                                            MEETING_RECOVERY_STORE_KEY,
                                                            &store, sizeof(store));
    admission_exit();
    return status;
}