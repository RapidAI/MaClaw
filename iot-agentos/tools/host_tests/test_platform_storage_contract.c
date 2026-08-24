#include <stdio.h>
#include <string.h>

#include "platform_storage.h"
#include "esp_partition.h"
#include "esp_spiffs.h"

#define CHECK(condition) do { \
    if (!(condition)) { \
        fprintf(stderr, "CHECK failed at %s:%d: %s\n", __FILE__, __LINE__, #condition); \
        return 1; \
    } \
} while (0)

static esp_err_t s_register_result;
static esp_err_t s_format_register_result;
static esp_err_t s_unregister_result;
static bool s_partition_present;
static bool s_partition_blank;
static unsigned s_register_calls;
static unsigned s_unregister_calls;
static bool s_optional_allowed;
static const esp_partition_t s_partition = {.size = 16};

const esp_partition_t *esp_partition_find_first(int type, int subtype,
                                                 const char *label) {
    (void)type; (void)subtype; (void)label;
    return s_partition_present ? &s_partition : NULL;
}
esp_err_t esp_partition_read(const esp_partition_t *partition, size_t offset,
                             void *buffer, size_t size) {
    (void)partition; (void)offset;
    memset(buffer, s_partition_blank ? 0xff : 0x00, size);
    return ESP_OK;
}
esp_err_t esp_vfs_spiffs_register(const esp_vfs_spiffs_conf_t *config) {
    ++s_register_calls;
    return config->format_if_mount_failed ? s_format_register_result : s_register_result;
}
esp_err_t esp_vfs_spiffs_unregister(const char *partition_label) {
    (void)partition_label;
    ++s_unregister_calls;
    return s_unregister_result;
}
esp_err_t esp_spiffs_info(const char *partition_label, size_t *total, size_t *used) {
    (void)partition_label;
    *total = 16;
    *used = 0;
    return ESP_OK;
}
bool platform_storage_profile_allows_optional_flash_work(void) {
    return s_optional_allowed;
}

static void reset_platform(void) {
    s_register_result = ESP_OK;
    s_format_register_result = ESP_OK;
    s_unregister_result = ESP_OK;
    s_partition_present = false;
    s_partition_blank = false;
    s_register_calls = 0;
    s_unregister_calls = 0;
    s_optional_allowed = true;
}

int main(void) {
    reset_platform();
    CHECK(platform_storage_mount() == DEVICE_STATUS_OK);
    CHECK(platform_storage_is_mounted());
    CHECK(platform_storage_mount() == DEVICE_STATUS_OK);
    CHECK(s_register_calls == 1);
    CHECK(platform_storage_allows_optional_flash_work());
    CHECK(platform_storage_unmount() == DEVICE_STATUS_OK);
    CHECK(!platform_storage_is_mounted());
    CHECK(s_unregister_calls == 1);

    reset_platform();
    s_register_result = ESP_ERR_INVALID_STATE;
    CHECK(platform_storage_mount() == DEVICE_STATUS_BUSY);
    CHECK(!platform_storage_is_mounted());
    CHECK(s_register_calls == 1);
    CHECK(platform_storage_unmount() == DEVICE_STATUS_OK);
    CHECK(s_unregister_calls == 0);

    reset_platform();
    s_partition_present = true;
    s_partition_blank = true;
    s_register_result = ESP_ERR_INVALID_STATE;
    CHECK(platform_storage_mount() == DEVICE_STATUS_BUSY);
    CHECK(!platform_storage_is_mounted());
    CHECK(s_register_calls == 1);
    CHECK(platform_storage_unmount() == DEVICE_STATUS_OK);
    CHECK(s_unregister_calls == 0);

    reset_platform();
    s_partition_present = true;
    s_partition_blank = true;
    s_register_result = ESP_ERR_TIMEOUT;
    CHECK(platform_storage_mount() == DEVICE_STATUS_OK);
    CHECK(s_register_calls == 2);
    CHECK(platform_storage_is_mounted());
    CHECK(platform_storage_unmount() == DEVICE_STATUS_OK);

    reset_platform();
    s_partition_present = true;
    s_partition_blank = false;
    s_register_result = ESP_ERR_TIMEOUT;
    CHECK(platform_storage_mount() == DEVICE_STATUS_IO_ERROR);
    CHECK(s_register_calls == 1);
    CHECK(!platform_storage_is_mounted());

    reset_platform();
    s_register_result = ESP_ERR_NO_MEM;
    CHECK(platform_storage_mount() == DEVICE_STATUS_RESOURCE_EXHAUSTED);
    CHECK(!platform_storage_is_mounted());

    reset_platform();
    CHECK(platform_storage_mount() == DEVICE_STATUS_OK);
    s_unregister_result = ESP_ERR_TIMEOUT;
    CHECK(platform_storage_unmount() == DEVICE_STATUS_IO_ERROR);
    CHECK(platform_storage_is_mounted());
    s_unregister_result = ESP_OK;
    CHECK(platform_storage_unmount() == DEVICE_STATUS_OK);
    CHECK(!platform_storage_is_mounted());

    puts("PASS Platform Storage owns only verified SPIFFS mount transactions");
    return 0;
}
