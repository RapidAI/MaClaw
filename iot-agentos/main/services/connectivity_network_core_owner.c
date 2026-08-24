#include "services/connectivity_network_core_owner.h"

#include "esp_err.h"
#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"

static const char *TAG = "maclaw_client";

static bool s_netif_initialized;
static bool s_default_event_loop_created;

static device_status_t status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

device_status_t connectivity_network_core_owner_ensure(void) {
    if (s_netif_initialized && s_default_event_loop_created) {
        return DEVICE_STATUS_OK;
    }
    /* A partial generation cannot safely be overwritten by a second init:
     * ESP-NETIF and the default loop are independent ESP-IDF singletons. */
    if (s_netif_initialized || s_default_event_loop_created) {
        ESP_LOGW(TAG, "network core start rejected: prior partial generation still owns resources");
        return DEVICE_STATUS_BUSY;
    }
    esp_err_t err = esp_netif_init();
    if (err != ESP_OK) return status_from_esp_err(err);
    s_netif_initialized = true;
    err = esp_event_loop_create_default();
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "default event-loop initialization failed: %s", esp_err_to_name(err));
        return status_from_esp_err(err);
    }
    s_default_event_loop_created = true;
    return DEVICE_STATUS_OK;
}

bool connectivity_network_core_owner_ready(void) {
    return s_netif_initialized && s_default_event_loop_created;
}

bool connectivity_network_core_owner_has_resources(void) {
    return s_netif_initialized || s_default_event_loop_created;
}

device_status_t connectivity_network_core_owner_release(void) {
    if (s_default_event_loop_created) {
        const esp_err_t err = esp_event_loop_delete_default();
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "cannot delete default event loop: %s", esp_err_to_name(err));
            return status_from_esp_err(err);
        }
        s_default_event_loop_created = false;
    }
    if (s_netif_initialized) {
        const esp_err_t err = esp_netif_deinit();
        if (err != ESP_OK) {
            ESP_LOGW(TAG, "cannot deinitialize ESP-NETIF: %s", esp_err_to_name(err));
            return status_from_esp_err(err);
        }
        s_netif_initialized = false;
    }
    return DEVICE_STATUS_OK;
}
