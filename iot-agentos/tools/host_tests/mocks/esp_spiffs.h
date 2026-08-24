#pragma once

#include <stdbool.h>
#include <stddef.h>

#include "esp_err.h"

typedef struct {
    const char *base_path;
    const char *partition_label;
    size_t max_files;
    bool format_if_mount_failed;
} esp_vfs_spiffs_conf_t;

esp_err_t esp_vfs_spiffs_register(const esp_vfs_spiffs_conf_t *config);
esp_err_t esp_vfs_spiffs_unregister(const char *partition_label);
esp_err_t esp_spiffs_info(const char *partition_label, size_t *total,
                          size_t *used);
