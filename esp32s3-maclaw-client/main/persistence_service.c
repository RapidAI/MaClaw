#include "persistence_service.h"

#include <stdint.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "nvs.h"

static SemaphoreHandle_t s_lock;

esp_err_t persistence_service_init(SemaphoreHandle_t transaction_mutex) {
    if (!transaction_mutex) return ESP_ERR_INVALID_ARG;
    if (s_lock && s_lock != transaction_mutex) return ESP_ERR_INVALID_STATE;
    s_lock = transaction_mutex;
    return ESP_OK;
}

bool persistence_service_is_initialized(void) {
    return s_lock != NULL;
}

static bool valid_name(const char *value) {
    return value && value[0];
}

static bool lock(void) {
    return s_lock && xSemaphoreTake(s_lock, pdMS_TO_TICKS(3000)) == pdTRUE;
}

static void unlock(void) {
    if (s_lock) xSemaphoreGive(s_lock);
}

esp_err_t persistence_service_read_blob(const char *name_space, const char *key,
                                        void *out_value, size_t *inout_size) {
    if (!valid_name(name_space) || !valid_name(key) || !inout_size) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READONLY, &nvs);
    if (err == ESP_OK) {
        err = nvs_get_blob(nvs, key, out_value, inout_size);
        nvs_close(nvs);
    }
    unlock();
    return err;
}

esp_err_t persistence_service_write_blob(const char *name_space, const char *key,
                                         const void *value, size_t size) {
    if (!valid_name(name_space) || !valid_name(key) || !value || !size) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READWRITE, &nvs);
    if (err == ESP_OK) {
        err = nvs_set_blob(nvs, key, value, size);
        if (err == ESP_OK) err = nvs_commit(nvs);
        nvs_close(nvs);
    }
    unlock();
    return err;
}

esp_err_t persistence_service_read_i64(const char *name_space, const char *key,
                                       int64_t *out_value) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READONLY, &nvs);
    if (err == ESP_OK) {
        err = nvs_get_i64(nvs, key, out_value);
        nvs_close(nvs);
    }
    unlock();
    return err;
}

esp_err_t persistence_service_read_i32(const char *name_space, const char *key,
                                       int32_t *out_value) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READONLY, &nvs);
    if (err == ESP_OK) {
        err = nvs_get_i32(nvs, key, out_value);
        nvs_close(nvs);
    }
    unlock();
    return err;
}

esp_err_t persistence_service_read_u8(const char *name_space, const char *key,
                                      uint8_t *out_value) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READONLY, &nvs);
    if (err == ESP_OK) {
        err = nvs_get_u8(nvs, key, out_value);
        nvs_close(nvs);
    }
    unlock();
    return err;
}

esp_err_t persistence_service_read_string(const char *name_space, const char *key,
                                          char *out_value, size_t *inout_size) {
    if (!valid_name(name_space) || !valid_name(key) || !out_value || !inout_size) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READONLY, &nvs);
    if (err == ESP_OK) {
        err = nvs_get_str(nvs, key, out_value, inout_size);
        nvs_close(nvs);
    }
    unlock();
    return err;
}
