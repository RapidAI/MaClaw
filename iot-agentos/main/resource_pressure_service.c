#include "resource_pressure_service.h"

#include <string.h>

#include "esp_heap_caps.h"
#include "esp_spiffs.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

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
static bool s_initialized;
static bool s_storage_available;
static char s_storage_label[16];
static device_resource_pressure_level_t s_level;

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
    if (!out_snapshot || !s_initialized) return false;
    device_resource_pressure_snapshot_t snapshot = {
        .struct_size = sizeof(snapshot),
        .abi_version = DEVICE_RESOURCE_PRESSURE_ABI_VERSION,
        .internal_free_bytes = heap_caps_get_free_size(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT),
        .internal_largest_free_bytes =
            heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT),
        .external_free_bytes = heap_caps_get_free_size(MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT),
        .external_largest_free_bytes =
            heap_caps_get_largest_free_block(MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT),
    };

    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(50)) != pdTRUE) return false;
    snapshot.storage_available = s_storage_available;
    if (snapshot.storage_available) {
        size_t total = 0, used = 0;
        if (esp_spiffs_info(s_storage_label, &total, &used) == ESP_OK && used <= total) {
            snapshot.storage_total_bytes = total > UINT32_MAX ? UINT32_MAX : (uint32_t)total;
            const size_t free_bytes = total - used;
            snapshot.storage_free_bytes = free_bytes > UINT32_MAX ? UINT32_MAX :
                                                                       (uint32_t)free_bytes;
        } else {
            /* A mounted storage volume which cannot provide a coherent usage
             * observation is unsafe for optional writes; do not hide that as
             * normal capacity. */
            snapshot.storage_available = false;
        }
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
    if (!s_lock) s_lock = xSemaphoreCreateMutexStatic(&s_lock_storage);
    if (!s_lock) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(100)) != pdTRUE) return DEVICE_STATUS_BUSY;
    strlcpy(s_storage_label, storage_label, sizeof(s_storage_label));
    s_storage_available = storage_available;
    s_level = DEVICE_RESOURCE_PRESSURE_NORMAL;
    s_initialized = true;
    xSemaphoreGive(s_lock);
    return DEVICE_STATUS_OK;
}

void resource_pressure_service_set_storage_available(bool available) {
    if (!s_initialized || xSemaphoreTake(s_lock, pdMS_TO_TICKS(50)) != pdTRUE) return;
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
