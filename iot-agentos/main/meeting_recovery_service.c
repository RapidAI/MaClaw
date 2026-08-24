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
static bool s_system_sleep_preparing;
static uint32_t s_active_calls;
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
    if (s_initialized && !s_stopping && !s_system_sleep_preparing) {
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

/* A pending recording may initially have no server-side ID: its durable
 * placeholder is written before recording begins.  Once either upload
 * progress or a terminal phase is persisted, however, the server-side ID is
 * required to resume safely.  A non-pending snapshot is always the canonical
 * empty tombstone written after local audio has been removed. */
static bool valid_recovery_state(bool pending, int32_t next_chunk, int32_t phase,
                                 const char *recording_id, size_t recording_id_size) {
    if (next_chunk < 0 || phase < 0 || phase > 2 || !recording_id ||
        memchr(recording_id, '\0', recording_id_size) == NULL) {
        return false;
    }
    if (!pending) {
        return recording_id[0] == '\0' && next_chunk == 0 && phase == 0;
    }
    return (next_chunk == 0 && phase == 0) || recording_id[0] != '\0';
}

static bool valid_store(const meeting_recovery_store_t *store) {
    return store && store->magic == MEETING_RECOVERY_STORE_MAGIC &&
           store->version == MEETING_RECOVERY_STORE_VERSION && store->pending <= 1 &&
           valid_recovery_state(store->pending != 0, store->next_chunk, store->phase,
                                store->recording_id, sizeof(store->recording_id));
}

static bool valid_snapshot(const meeting_recovery_snapshot_t *snapshot) {
    return snapshot && valid_recovery_state(snapshot->pending, snapshot->next_chunk,
                                            snapshot->phase, snapshot->recording_id,
                                            sizeof(snapshot->recording_id));
}

/* Legacy field-by-field import is private migration code. The public service
 * reports only stable Device API statuses to meeting/business callers. */
static device_status_t load_legacy(meeting_recovery_snapshot_t *snapshot, bool *out_found) {
    if (!snapshot || !out_found) return DEVICE_STATUS_INVALID_ARGUMENT;
    clear_snapshot(snapshot);
    *out_found = false;
    int32_t value = 0;
    device_status_t status = persistence_service_read_i32(
        MEETING_RECOVERY_NAMESPACE, "meet_next", &value);
    if (status == DEVICE_STATUS_OK) {
        snapshot->next_chunk = value;
        *out_found = true;
    } else if (status != DEVICE_STATUS_NOT_FOUND) {
        return status;
    }
    status = persistence_service_read_i32(MEETING_RECOVERY_NAMESPACE, "meet_phase", &value);
    if (status == DEVICE_STATUS_OK) {
        snapshot->phase = value;
        *out_found = true;
    } else if (status != DEVICE_STATUS_NOT_FOUND) {
        return status;
    }
    size_t size = sizeof(snapshot->recording_id);
    status = persistence_service_read_string(MEETING_RECOVERY_NAMESPACE, "meet_id",
                                             snapshot->recording_id, &size);
    if (status == DEVICE_STATUS_OK) {
        *out_found = true;
    } else if (status != DEVICE_STATUS_NOT_FOUND) {
        return status;
    }
    uint8_t pending = 0;
    status = persistence_service_read_u8(MEETING_RECOVERY_NAMESPACE, "meet_pending", &pending);
    if (status == DEVICE_STATUS_OK) {
        if (pending > 1) return DEVICE_STATUS_INVALID_ARGUMENT;
        snapshot->pending = pending != 0;
        *out_found = true;
    } else if (status != DEVICE_STATUS_NOT_FOUND) {
        return status;
    }
    if (*out_found && !valid_snapshot(snapshot)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return DEVICE_STATUS_OK;
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
    s_system_sleep_preparing = false;
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
    s_system_sleep_preparing = false;
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

device_status_t meeting_recovery_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = meeting_recovery_stop_timeout_ticks(timeout_ms);
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool ready = s_initialized && !s_stopping &&
                       !s_system_sleep_preparing &&
                       !__atomic_load_n(&s_initializing, __ATOMIC_ACQUIRE);
    if (ready) s_system_sleep_preparing = true;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (!ready) return DEVICE_STATUS_BUSY;

    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        const uint32_t active_calls = s_active_calls;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (active_calls == 0) return DEVICE_STATUS_OK;
        if (meeting_recovery_stop_remaining_ticks(started, budget) == 0) {
            /* A caller admitted before PREPARE may still be unwinding a
             * durable checkpoint. Keep new checkpoints closed until Power's
             * mandatory reverse-order ABORT restores this participant. */
            return DEVICE_STATUS_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
}

void meeting_recovery_service_abort_system_sleep_prepare(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_initialized && !s_stopping) s_system_sleep_preparing = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
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
        device_status_t legacy_status = load_legacy(out_snapshot, &legacy_found);
        if (legacy_status != DEVICE_STATUS_OK || !legacy_found) {
            result = legacy_status == DEVICE_STATUS_OK ? DEVICE_STATUS_NOT_FOUND
                                                        : legacy_status;
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
