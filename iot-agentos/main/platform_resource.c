#include "platform_resource.h"

#include <limits.h>

#include "esp_heap_caps.h"
#include "esp_spiffs.h"

bool platform_resource_sample(const char *storage_label, bool storage_available,
                              device_resource_pressure_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;

    device_resource_pressure_snapshot_t snapshot = {
        .struct_size = sizeof(snapshot),
        .abi_version = DEVICE_RESOURCE_PRESSURE_ABI_VERSION,
        .internal_free_bytes = heap_caps_get_free_size(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT),
        .internal_largest_free_bytes = heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT),
        .external_free_bytes = heap_caps_get_free_size(MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT),
        .external_largest_free_bytes = heap_caps_get_largest_free_block(MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT),
        .storage_available = storage_available && storage_label && storage_label[0],
    };

    if (snapshot.storage_available) {
        size_t total = 0;
        size_t used = 0;
        if (esp_spiffs_info(storage_label, &total, &used) == ESP_OK && used <= total) {
            snapshot.storage_total_bytes = total > UINT32_MAX ? UINT32_MAX : (uint32_t)total;
            const size_t free_bytes = total - used;
            snapshot.storage_free_bytes = free_bytes > UINT32_MAX ? UINT32_MAX : (uint32_t)free_bytes;
        } else {
            /* A mounted volume without coherent capacity cannot admit optional durable work. */
            snapshot.storage_available = false;
        }
    }

    *out_snapshot = snapshot;
    return true;
}