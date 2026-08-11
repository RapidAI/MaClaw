#include "resource_pressure_service.h"

#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

#include "platform_resource.h"

/* These are deliberately conservative lower bounds, derived from the current
 * command TLS/capture work needing contiguous internal DMA memory.  Hysteresis
 * prevents a single allocator fluctuation from causing decorative work to
 * flap.  Product-specific reductions belong above this service. */
#define RESOURCE_INTERNAL_PRESSURE_BYTES (16u * 1024u)
#define RESOURCE_INTERNAL_CRITICAL_BYTES (8u * 1024u)
#define RESOURCE_INTERNAL_PRESSURE_CLEAR_BYTES (20u * 1024u)
#define RESOURCE_INTERNAL_CRITICAL_CLEAR_BYTES (12u * 1024u)
#define RESOURCE_EXTERNAL_PRESSURE_BYTES (512u * 1024u)
#define RESOURCE_EXTERNAL_CRITICAL_BYTES (256u * 1024u)
#define RESOURCE_EXTERNAL_PRESSURE_CLEAR_BYTES (768u * 1024u)
#define RESOURCE_EXTERNAL_CRITICAL_CLEAR_BYTES (384u * 1024u)
#define RESOURCE_STORAGE_PRESSURE_BYTES (1024u * 1024u)
#define RESOURCE_STORAGE_CRITICAL_BYTES (256u * 1024u)
#define RESOURCE_STORAGE_PRESSURE_CLEAR_BYTES (1280u * 1024u)
#define RESOURCE_STORAGE_CRITICAL_CLEAR_BYTES (384u * 1024u)

static StaticSemaphore_t s_lock_storage;
static SemaphoreHandle_t s_lock;
static StaticSemaphore_t s_deinit_lock_storage;
static SemaphoreHandle_t s_deinit_lock;
static bool s_initialized;
static bool s_stopping;
/* Publish construction before even the retained static mutexes are visible.
 * Otherwise rollback can observe the locks, see an apparently inactive
 * service, return success, and then let init publish an observer after the
 * Storage/VFS owner has started to unwind. */
static volatile bool s_initializing;
static bool s_storage_available;
static char s_storage_label[16];
static device_resource_pressure_level_t s_level;

/* Deinit owns both the coordinator and sampling locks. Keep one deadline so
 * a blocked coordinator cannot silently grant the sampling lock a second,
 * independent full timeout window. */
static TickType_t stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t stop_remaining_ticks(TickType_t started, TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

static device_resource_pressure_level_t calculate_level(
    const device_resource_pressure_snapshot_t *snapshot,
    device_resource_pressure_level_t previous) {
    const uint32_t internal = snapshot->internal_largest_free_bytes;
    const uint32_t external = snapshot->external_largest_free_bytes;
    const uint32_t storage = snapshot->storage_free_bytes;
    const bool has_storage = snapshot->storage_available;

    /* "storage unavailable" is not equivalent to a board without storage:
     * this service is initialized for the product storage volume.  When a
     * mounted volume stops reporting coherent capacity, optional work must
     * fail closed instead of starting a cache/download that cannot be
     * committed safely. */
    if (!has_storage) {
        return DEVICE_RESOURCE_PRESSURE_CRITICAL;
    }

    if (previous == DEVICE_RESOURCE_PRESSURE_CRITICAL) {
        if (internal < RESOURCE_INTERNAL_CRITICAL_CLEAR_BYTES ||
            external < RESOURCE_EXTERNAL_CRITICAL_CLEAR_BYTES ||
            storage < RESOURCE_STORAGE_CRITICAL_CLEAR_BYTES) {
            return DEVICE_RESOURCE_PRESSURE_CRITICAL;
        }
    }
    if (internal < RESOURCE_INTERNAL_CRITICAL_BYTES ||
        external < RESOURCE_EXTERNAL_CRITICAL_BYTES ||
        storage < RESOURCE_STORAGE_CRITICAL_BYTES) {
        return DEVICE_RESOURCE_PRESSURE_CRITICAL;
    }
    if (previous == DEVICE_RESOURCE_PRESSURE_PRESSURE) {
        if (internal < RESOURCE_INTERNAL_PRESSURE_CLEAR_BYTES ||
            external < RESOURCE_EXTERNAL_PRESSURE_CLEAR_BYTES ||
            storage < RESOURCE_STORAGE_PRESSURE_CLEAR_BYTES) {
            return DEVICE_RESOURCE_PRESSURE_PRESSURE;
        }
    }
    if (internal < RESOURCE_INTERNAL_PRESSURE_BYTES ||
        external < RESOURCE_EXTERNAL_PRESSURE_BYTES ||
        storage < RESOURCE_STORAGE_PRESSURE_BYTES) {
        return DEVICE_RESOURCE_PRESSURE_PRESSURE;
    }
    return DEVICE_RESOURCE_PRESSURE_NORMAL;
}

bool resource_pressure_service_get_snapshot(device_resource_pressure_snapshot_t *out_snapshot) {
    if (!out_snapshot || !s_lock) return false;

    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(50)) != pdTRUE) return false;
    if (!s_initialized || s_stopping) {
        xSemaphoreGive(s_lock);
        return false;
    }

    /* Keep VFS sampling under the lifecycle lock. Deinit closes admission
     * under this same lock before Storage may release the mounted volume. */
    device_resource_pressure_snapshot_t snapshot;
    const bool sampled = platform_resource_sample(
        s_storage_label, s_storage_available, &snapshot);
    if (!sampled) {
        xSemaphoreGive(s_lock);
        return false;
    }
    s_level = calculate_level(&snapshot, s_level);
    snapshot.level = s_level;
    xSemaphoreGive(s_lock);
    *out_snapshot = snapshot;
    return true;
}
device_status_t resource_pressure_service_init(const char *storage_label,
                                               bool storage_available) {
    if (!storage_label || !storage_label[0] || strlen(storage_label) >= sizeof(s_storage_label)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    bool expected = false;
    if (!__atomic_compare_exchange_n(&s_initializing, &expected, true, false,
                                     __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
        return DEVICE_STATUS_BUSY;
    }
    if (!s_lock) s_lock = xSemaphoreCreateMutexStatic(&s_lock_storage);
    if (!s_deinit_lock) s_deinit_lock = xSemaphoreCreateMutexStatic(&s_deinit_lock_storage);
    if (!s_lock || !s_deinit_lock) {
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    if (xSemaphoreTake(s_deinit_lock, pdMS_TO_TICKS(100)) != pdTRUE) {
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_BUSY;
    }
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(100)) != pdTRUE) {
        xSemaphoreGive(s_deinit_lock);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_BUSY;
    }
    strlcpy(s_storage_label, storage_label, sizeof(s_storage_label));
    s_storage_available = storage_available;
    s_level = DEVICE_RESOURCE_PRESSURE_NORMAL;
    s_initialized = true;
    s_stopping = false;
    xSemaphoreGive(s_lock);
    xSemaphoreGive(s_deinit_lock);
    __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
    return DEVICE_STATUS_OK;
}

device_status_t resource_pressure_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = stop_timeout_ticks(timeout_ms);
    /* Claim the same construction gate used by init. Besides waiting for an
     * already-active init, this prevents a new one from beginning between a
     * passive "initializing == false" observation and our admission close. */
    for (;;) {
        bool expected = false;
        if (__atomic_compare_exchange_n(&s_initializing, &expected, true, false,
                                        __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
            break;
        }
        if (stop_remaining_ticks(started, budget) == 0) return DEVICE_STATUS_TIMEOUT;
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    if (!s_lock || !s_deinit_lock) {
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_OK;
    }
    TickType_t remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_deinit_lock, remaining) != pdTRUE) {
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_TIMEOUT;
    }
    remaining = stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_lock, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_lock);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_TIMEOUT;
    }
    if (!s_initialized && !s_stopping) {
        xSemaphoreGive(s_lock);
        xSemaphoreGive(s_deinit_lock);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return DEVICE_STATUS_OK;
    }
    /* The static mutex is deliberately retained. Any caller that sampled an
     * old initialized flag rechecks this boundary after it acquires the lock,
     * so it cannot query a VFS volume whose Storage owner has already left. */
    s_stopping = true;
    s_initialized = false;
    s_storage_available = false;
    s_storage_label[0] = '\0';
    s_level = DEVICE_RESOURCE_PRESSURE_CRITICAL;
    s_stopping = false;
    xSemaphoreGive(s_lock);
    xSemaphoreGive(s_deinit_lock);
    __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
    return DEVICE_STATUS_OK;
}

void resource_pressure_service_set_storage_available(bool available) {
    if (!s_lock || xSemaphoreTake(s_lock, pdMS_TO_TICKS(50)) != pdTRUE) return;
    if (!s_initialized || s_stopping) {
        xSemaphoreGive(s_lock);
        return;
    }
    s_storage_available = available;
    xSemaphoreGive(s_lock);
}

bool resource_pressure_service_allows_optional_work(void) {
    device_resource_pressure_snapshot_t snapshot = {0};
    return resource_pressure_service_get_snapshot(&snapshot) &&
           snapshot.level != DEVICE_RESOURCE_PRESSURE_CRITICAL;
}

bool resource_pressure_service_allows_optional_allocation(
    uint32_t internal_bytes, uint32_t external_bytes, uint32_t storage_bytes) {
    device_resource_pressure_snapshot_t snapshot = {0};
    if (!resource_pressure_service_get_snapshot(&snapshot) ||
        snapshot.level != DEVICE_RESOURCE_PRESSURE_NORMAL) {
        return false;
    }

    /* Preserve the PRESSURE waterline after accepting an optional operation.
     * The comparison uses largest contiguous blocks, because a large TLS/media
     * body cannot be made safe by total free bytes spread across fragments. */
    if (internal_bytes > snapshot.internal_largest_free_bytes ||
        snapshot.internal_largest_free_bytes - internal_bytes <
            RESOURCE_INTERNAL_PRESSURE_BYTES ||
        external_bytes > snapshot.external_largest_free_bytes ||
        snapshot.external_largest_free_bytes - external_bytes <
            RESOURCE_EXTERNAL_PRESSURE_BYTES ||
        !snapshot.storage_available || storage_bytes > snapshot.storage_free_bytes ||
        snapshot.storage_free_bytes - storage_bytes < RESOURCE_STORAGE_PRESSURE_BYTES) {
        return false;
    }
    return true;
}
