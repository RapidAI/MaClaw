#include "services/provisioning_qr_service.h"

#include <stdio.h>
#include <stdlib.h>

#include "esp_log.h"
#include "mbedtls/platform_util.h"
#include "qrcode.h"

static provisioning_qr_service_host_t s_host;
static bool s_initialized;

static bool host_valid(const provisioning_qr_service_host_t *host) {
    return host && host->struct_size == sizeof(*host) && host->publish_modules &&
           host->publish_fallback_message;
}

static void publish_matrix(esp_qrcode_handle_t qrcode, void *user_data) {
    const char *ssid = user_data;
    if (!qrcode || !s_initialized) return;
    const int size = esp_qrcode_get_size(qrcode);
    if (size <= 0 || size > 177) return;
    const size_t module_count = (size_t)size * (size_t)size;
    uint8_t *modules = malloc(module_count);
    if (!modules) return;
    for (int y = 0; y < size; ++y) {
        for (int x = 0; x < size; ++x) {
            modules[(size_t)y * (size_t)size + (size_t)x] =
                esp_qrcode_get_module(qrcode, x, y) ? 1u : 0u;
        }
    }
    (void)s_host.publish_modules(modules, module_count, ssid, s_host.context);
    mbedtls_platform_zeroize(modules, module_count);
    free(modules);
}

device_status_t provisioning_qr_service_init(
    const provisioning_qr_service_host_t *host) {
    if (!host_valid(host)) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (s_initialized) {
        return s_host.publish_modules == host->publish_modules &&
                       s_host.publish_fallback_message == host->publish_fallback_message &&
                       s_host.context == host->context
                   ? DEVICE_STATUS_OK
                   : DEVICE_STATUS_BUSY;
    }
    s_host = *host;
    /* The upstream encoder logs its full input at INFO level. That input is
     * necessarily the ephemeral SoftAP passphrase, so disable this dedicated
     * dependency tag before any request can reach esp_qrcode_generate(). */
    esp_log_level_set("QRCODE", ESP_LOG_NONE);
    s_initialized = true;
    return DEVICE_STATUS_OK;
}

device_status_t provisioning_qr_service_show(const char *ssid,
                                             const char *passphrase) {
    if (!s_initialized) return DEVICE_STATUS_UNAVAILABLE;
    char payload[128];
    const int length = snprintf(payload, sizeof(payload), "WIFI:T:WPA;S:%s;P:%s;;",
                                ssid ? ssid : "", passphrase ? passphrase : "");
    if (length < 0 || length >= (int)sizeof(payload)) {
        mbedtls_platform_zeroize(payload, sizeof(payload));
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    esp_qrcode_config_t config = ESP_QRCODE_CONFIG_DEFAULT();
    config.display_func_with_cb = publish_matrix;
    config.user_data = (void *)ssid;
    config.max_qrcode_version = 5;
    config.qrcode_ecc_level = ESP_QRCODE_ECC_MED;
    if (esp_qrcode_generate(&config, payload) == ESP_OK) {
        mbedtls_platform_zeroize(payload, sizeof(payload));
        return DEVICE_STATUS_OK;
    }
    mbedtls_platform_zeroize(payload, sizeof(payload));
    s_host.publish_fallback_message("设备网络设置", ssid ? ssid : "", s_host.context);
    return DEVICE_STATUS_IO_ERROR;
}
