#include "platform_storage.h"

#include <dirent.h>
#include <errno.h>
#include <stdio.h>
#include <sys/stat.h>

#include "board_port.h"
#include "esp_log.h"
#include "esp_partition.h"
#include "esp_spiffs.h"

#define PLATFORM_STORAGE_BASE_PATH "/storage"
#define PLATFORM_STORAGE_LABEL "storage"
#define PLATFORM_STORAGE_MAX_FILES 16

static const char *TAG = "maclaw_storage";
static bool s_mounted;
static bool s_mount_owned;

bool platform_storage_allows_optional_flash_work(void) {
    return board_port_allows_optional_flash_work();
}

static bool platform_storage_partition_is_factory_blank(void) {
    const esp_partition_t *partition = esp_partition_find_first(
        ESP_PARTITION_TYPE_DATA, ESP_PARTITION_SUBTYPE_DATA_SPIFFS,
        PLATFORM_STORAGE_LABEL);
    if (!partition || partition->size == 0) return false;

    /* Never infer blank media from one erased sector: recoverable SPIFFS data
     * can survive later in the partition after interrupted metadata writes. */
    uint8_t sample[1024];
    for (size_t offset = 0; offset < partition->size; offset += sizeof(sample)) {
        size_t count = partition->size - offset;
        if (count > sizeof(sample)) count = sizeof(sample);
        if (esp_partition_read(partition, offset, sample, count) != ESP_OK) return false;
        for (size_t i = 0; i < count; ++i) {
            if (sample[i] != 0xff) return false;
        }
    }
    return true;
}

static void platform_storage_log_inventory(void) {
    DIR *dir = opendir(PLATFORM_STORAGE_BASE_PATH);
    if (dir) {
        struct dirent *entry;
        while ((entry = readdir(dir)) != NULL) {
            if (entry->d_name[0] == '.') continue;
            char path[sizeof(PLATFORM_STORAGE_BASE_PATH) + 256];
            snprintf(path, sizeof(path), "%s/%s", PLATFORM_STORAGE_BASE_PATH, entry->d_name);
            struct stat info;
            if (stat(path, &info) == 0) {
                ESP_LOGI(TAG, "storage file: %s size=%ld", entry->d_name, (long)info.st_size);
            } else {
                ESP_LOGW(TAG, "storage file: %s stat failed errno=%d", entry->d_name, errno);
            }
        }
        closedir(dir);
    } else {
        ESP_LOGW(TAG, "storage inventory: opendir failed errno=%d", errno);
    }

    size_t total = 0;
    size_t used = 0;
    if (esp_spiffs_info(PLATFORM_STORAGE_LABEL, &total, &used) == ESP_OK && used <= total) {
        ESP_LOGI(TAG, "storage mounted: total=%u used=%u free=%u", (unsigned)total,
                 (unsigned)used, (unsigned)(total - used));
    }
}

device_status_t platform_storage_mount(void) {
    if (s_mounted) return DEVICE_STATUS_OK;

    esp_vfs_spiffs_conf_t config = {
        .base_path = PLATFORM_STORAGE_BASE_PATH,
        .partition_label = PLATFORM_STORAGE_LABEL,
        .max_files = PLATFORM_STORAGE_MAX_FILES,
        .format_if_mount_failed = false,
    };
    esp_err_t err = esp_vfs_spiffs_register(&config);
    if (err != ESP_OK && platform_storage_partition_is_factory_blank()) {
        ESP_LOGW(TAG, "factory-blank storage detected; formatting once");
        config.format_if_mount_failed = true;
        err = esp_vfs_spiffs_register(&config);
    }
    if (err == ESP_OK || err == ESP_ERR_INVALID_STATE) {
        s_mounted = true;
        s_mount_owned = err == ESP_OK;
        platform_storage_log_inventory();
        return DEVICE_STATUS_OK;
    }
    ESP_LOGE(TAG, "storage mount failed; preserving existing contents: %s", esp_err_to_name(err));
    return err == ESP_ERR_NO_MEM ? DEVICE_STATUS_RESOURCE_EXHAUSTED : DEVICE_STATUS_IO_ERROR;
}

device_status_t platform_storage_unmount(void) {
    if (!s_mounted) return DEVICE_STATUS_OK;
    /* An already-mounted volume is usable, but this port cannot unregister a
     * VFS it did not create. The HAL boundary should make this case unreachable
     * in production; retaining ownership still prevents a future duplicate
     * mount path from tearing down somebody else's volume. */
    if (!s_mount_owned) {
        s_mounted = false;
        return DEVICE_STATUS_OK;
    }
    const esp_err_t err = esp_vfs_spiffs_unregister(PLATFORM_STORAGE_LABEL);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "storage unmount failed: %s", esp_err_to_name(err));
        return err == ESP_ERR_NO_MEM ? DEVICE_STATUS_RESOURCE_EXHAUSTED :
                                        DEVICE_STATUS_IO_ERROR;
    }
    s_mounted = false;
    s_mount_owned = false;
    ESP_LOGI(TAG, "storage VFS unmounted");
    return DEVICE_STATUS_OK;
}

bool platform_storage_is_mounted(void) { return s_mounted; }

const char *platform_storage_label(void) { return PLATFORM_STORAGE_LABEL; }
