#include "platform_nvs.h"

#include "esp_err.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "nvs.h"
#include "nvs_flash.h"

static const char *const TAG = "platform_nvs";
static SemaphoreHandle_t s_transaction_lock;
static bool s_initialized;

static device_status_t status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_ERR_NVS_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        default: return DEVICE_STATUS_IO_ERROR;
    }
}

static bool lock_transaction(void) {
    return s_initialized && s_transaction_lock &&
           xSemaphoreTake(s_transaction_lock, pdMS_TO_TICKS(3000)) == pdTRUE;
}

static void unlock_transaction(void) {
    if (s_transaction_lock) xSemaphoreGive(s_transaction_lock);
}

device_status_t platform_nvs_init(void) {
    if (!s_transaction_lock) {
        s_transaction_lock = xSemaphoreCreateMutex();
        if (!s_transaction_lock) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    if (s_initialized) return DEVICE_STATUS_OK;

    const esp_err_t err = nvs_flash_init();
    /* ESP-IDF's sample path erases the full partition for these errors.  That
     * would silently discard Wi-Fi credentials, pairing, alarms and recovery
     * state, so preserve the partition and leave boot in diagnostics instead. */
    if (err == ESP_ERR_NVS_NO_FREE_PAGES || err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_LOGE(TAG, "NVS unavailable (%s); preserving user data", esp_err_to_name(err));
        return DEVICE_STATUS_IO_ERROR;
    }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "NVS initialization failed: %s", esp_err_to_name(err));
        return status_from_esp_err(err);
    }
    s_initialized = true;
    return DEVICE_STATUS_OK;
}

device_status_t platform_nvs_deinit(void) {
    if (!s_initialized) return DEVICE_STATUS_OK;
    if (!s_transaction_lock ||
        xSemaphoreTake(s_transaction_lock, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    nvs_flash_deinit();
    s_initialized = false;
    xSemaphoreGive(s_transaction_lock);
    return DEVICE_STATUS_OK;
}

bool platform_nvs_is_initialized(void) {
    return s_initialized;
}

device_status_t platform_nvs_read_blob(const char *name_space, const char *key,
                                       void *out_value, size_t *inout_size) {
    if (!lock_transaction()) return DEVICE_STATUS_BUSY;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READONLY, &nvs);
    if (err == ESP_OK) {
        err = nvs_get_blob(nvs, key, out_value, inout_size);
        nvs_close(nvs);
    }
    unlock_transaction();
    return status_from_esp_err(err);
}

device_status_t platform_nvs_write_blob(const char *name_space, const char *key,
                                        const void *value, size_t size) {
    if (!lock_transaction()) return DEVICE_STATUS_BUSY;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READWRITE, &nvs);
    if (err == ESP_OK) {
        err = nvs_set_blob(nvs, key, value, size);
        if (err == ESP_OK) err = nvs_commit(nvs);
        nvs_close(nvs);
    }
    unlock_transaction();
    return status_from_esp_err(err);
}

device_status_t platform_nvs_read_i64(const char *name_space, const char *key,
                                      int64_t *out_value) {
    if (!lock_transaction()) return DEVICE_STATUS_BUSY;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READONLY, &nvs);
    if (err == ESP_OK) {
        err = nvs_get_i64(nvs, key, out_value);
        nvs_close(nvs);
    }
    unlock_transaction();
    return status_from_esp_err(err);
}

device_status_t platform_nvs_read_i32(const char *name_space, const char *key,
                                      int32_t *out_value) {
    if (!lock_transaction()) return DEVICE_STATUS_BUSY;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READONLY, &nvs);
    if (err == ESP_OK) {
        err = nvs_get_i32(nvs, key, out_value);
        nvs_close(nvs);
    }
    unlock_transaction();
    return status_from_esp_err(err);
}

device_status_t platform_nvs_read_u8(const char *name_space, const char *key,
                                     uint8_t *out_value) {
    if (!lock_transaction()) return DEVICE_STATUS_BUSY;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READONLY, &nvs);
    if (err == ESP_OK) {
        err = nvs_get_u8(nvs, key, out_value);
        nvs_close(nvs);
    }
    unlock_transaction();
    return status_from_esp_err(err);
}

device_status_t platform_nvs_write_u8(const char *name_space, const char *key,
                                      uint8_t value) {
    if (!lock_transaction()) return DEVICE_STATUS_BUSY;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READWRITE, &nvs);
    if (err == ESP_OK) {
        err = nvs_set_u8(nvs, key, value);
        if (err == ESP_OK) err = nvs_commit(nvs);
        nvs_close(nvs);
    }
    unlock_transaction();
    return status_from_esp_err(err);
}

device_status_t platform_nvs_read_string(const char *name_space, const char *key,
                                         char *out_value, size_t *inout_size) {
    if (!lock_transaction()) return DEVICE_STATUS_BUSY;
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(name_space, NVS_READONLY, &nvs);
    if (err == ESP_OK) {
        err = nvs_get_str(nvs, key, out_value, inout_size);
        nvs_close(nvs);
    }
    unlock_transaction();
    return status_from_esp_err(err);
}
