#include <stdio.h>
#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "resource_pressure_service.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static TickType_t s_ticks;
static bool s_platform_sample_result = true;
static device_resource_pressure_snapshot_t s_platform_snapshot;

size_t strlcpy(char *destination, const char *source, size_t destination_size) {
    const size_t source_length = strlen(source);
    if (destination_size != 0) {
        const size_t copy_length = source_length < destination_size - 1
            ? source_length : destination_size - 1;
        memcpy(destination, source, copy_length);
        destination[copy_length] = '\0';
    }
    return source_length;
}

TickType_t xTaskGetTickCount(void) { return s_ticks; }
void vTaskDelay(TickType_t ticks) { s_ticks += ticks; }
SemaphoreHandle_t xSemaphoreCreateMutexStatic(StaticSemaphore_t *storage) {
    return storage;
}
BaseType_t xSemaphoreTake(SemaphoreHandle_t semaphore, TickType_t timeout) {
    (void)semaphore;
    (void)timeout;
    return pdTRUE;
}
BaseType_t xSemaphoreGive(SemaphoreHandle_t semaphore) {
    (void)semaphore;
    return pdTRUE;
}

bool platform_resource_sample(const char *storage_label, bool storage_available,
                              device_resource_pressure_snapshot_t *out_snapshot) {
    (void)storage_label;
    if (s_platform_sample_result && out_snapshot) {
        *out_snapshot = s_platform_snapshot;
        if (!storage_available) {
            out_snapshot->storage_available = false;
            out_snapshot->storage_total_bytes = 0;
            out_snapshot->storage_free_bytes = 0;
        }
    }
    return s_platform_sample_result;
}

static void make_valid_snapshot(bool storage_available) {
    s_platform_sample_result = true;
    s_platform_snapshot = (device_resource_pressure_snapshot_t){
        .struct_size = sizeof(s_platform_snapshot),
        .abi_version = DEVICE_RESOURCE_PRESSURE_ABI_VERSION,
        .internal_free_bytes = 256u * 1024u,
        .internal_largest_free_bytes = 64u * 1024u,
        .external_free_bytes = 4u * 1024u * 1024u,
        .external_largest_free_bytes = 2u * 1024u * 1024u,
        .storage_available = storage_available,
        .storage_total_bytes = storage_available ? 4u * 1024u * 1024u : 0,
        .storage_free_bytes = storage_available ? 2u * 1024u * 1024u : 0,
    };
}

int main(void) {
    device_resource_pressure_snapshot_t observed = {0};
    device_resource_pressure_snapshot_t sentinel = {
        .struct_size = 0xfeedu,
        .abi_version = 0xbeefu,
    };

    CHECK(resource_pressure_service_init(NULL, true) == DEVICE_STATUS_INVALID_ARGUMENT);
    CHECK(resource_pressure_service_init("storage", true) == DEVICE_STATUS_OK);

    make_valid_snapshot(true);
    CHECK(resource_pressure_service_get_snapshot(&observed));
    CHECK(observed.level == DEVICE_RESOURCE_PRESSURE_NORMAL);
    CHECK(resource_pressure_service_allows_optional_work());
    CHECK(resource_pressure_service_allows_optional_allocation(
        8u * 1024u, 64u * 1024u, 128u * 1024u));

    /* A profile port returning success with a truncated/foreign ABI cannot
     * publish a resource decision or overwrite the caller's previous value. */
    s_platform_snapshot.abi_version++;
    observed = sentinel;
    CHECK(!resource_pressure_service_get_snapshot(&observed));
    CHECK(memcmp(&observed, &sentinel, sizeof(observed)) == 0);
    CHECK(!resource_pressure_service_allows_optional_work());
    s_platform_snapshot.abi_version = DEVICE_RESOURCE_PRESSURE_ABI_VERSION;

    /* Coherent ABI is still insufficient if capacity arithmetic is impossible. */
    s_platform_snapshot.storage_free_bytes = s_platform_snapshot.storage_total_bytes + 1u;
    observed = sentinel;
    CHECK(!resource_pressure_service_get_snapshot(&observed));
    CHECK(memcmp(&observed, &sentinel, sizeof(observed)) == 0);

    make_valid_snapshot(false);
    CHECK(resource_pressure_service_get_snapshot(&observed));
    CHECK(observed.level == DEVICE_RESOURCE_PRESSURE_CRITICAL);
    CHECK(!resource_pressure_service_allows_optional_work());
    CHECK(!resource_pressure_service_allows_optional_allocation(0, 0, 0));

    make_valid_snapshot(true);
    CHECK(resource_pressure_service_get_snapshot(&observed));
    CHECK(observed.level == DEVICE_RESOURCE_PRESSURE_NORMAL);
    resource_pressure_service_set_storage_available(false);
    CHECK(resource_pressure_service_get_snapshot(&observed));
    CHECK(observed.level == DEVICE_RESOURCE_PRESSURE_CRITICAL);

    CHECK(resource_pressure_service_deinit(10) == DEVICE_STATUS_OK);
    observed = sentinel;
    CHECK(!resource_pressure_service_get_snapshot(&observed));
    CHECK(memcmp(&observed, &sentinel, sizeof(observed)) == 0);
    CHECK(!resource_pressure_service_allows_optional_work());
    resource_pressure_service_set_storage_available(true);

    make_valid_snapshot(true);
    CHECK(resource_pressure_service_init("storage", true) == DEVICE_STATUS_OK);
    CHECK(resource_pressure_service_get_snapshot(&observed));
    CHECK(observed.level == DEVICE_RESOURCE_PRESSURE_NORMAL);
    CHECK(resource_pressure_service_deinit(10) == DEVICE_STATUS_OK);

    puts("PASS Resource Pressure validates Platform samples and closes VFS admission");
    return 0;
}
