#pragma once

#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

#define ESP_PARTITION_TYPE_DATA 1
#define ESP_PARTITION_SUBTYPE_DATA_SPIFFS 2

typedef struct {
    size_t size;
} esp_partition_t;

const esp_partition_t *esp_partition_find_first(int type, int subtype,
                                                 const char *label);
esp_err_t esp_partition_read(const esp_partition_t *partition, size_t offset,
                             void *buffer, size_t size);
