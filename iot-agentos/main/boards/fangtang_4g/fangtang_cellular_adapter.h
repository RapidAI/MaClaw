/* Fangtang-4G ML307 physical transport profile.
 *
 * The common Connectivity Service decides when a cellular uplink is needed
 * and owns request semantics.  This profile owns only the modem's electrical
 * preparation and the concrete ML307 UART/APN binding, so a later 4G board
 * can provide the same neutral operations without copying Fangtang GPIO
 * facts into shared application code.
 */
#pragma once

#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_FANGTANG_4G
#error "Fangtang cellular adapter may only be included by the Fangtang profile"
#endif

#include "driver/gpio.h"
#include "esp_err.h"
#include "nvs.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "ml307_transport.h"
#include <limits.h>

#include "configuration_service.h"
#include "device_api.h"
#include "fangtang_input_adapter.h"
#include "persistence_service.h"

#include <string.h>

/* This changes no modem state when the board Kconfig has no valid UART
 * binding.  GPIO pull/electrical levels and the mandatory settle delay remain
 * profile-private, rather than becoming Connectivity Service policy. */
static inline esp_err_t fangtang_cellular_adapter_prepare_hardware(void) {
    if (CONFIG_MACLAW_FANGTANG_MODEM_UART_TX_GPIO < 0 ||
        CONFIG_MACLAW_FANGTANG_MODEM_UART_RX_GPIO < 0) {
        return ESP_ERR_INVALID_ARG;
    }
    if (CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO >= 0) {
        const gpio_config_t guard = {
            .pin_bit_mask = 1ULL << CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO,
            .mode = GPIO_MODE_OUTPUT,
            .pull_down_en = GPIO_PULLDOWN_ENABLE,
        };
        esp_err_t err = gpio_config(&guard);
        if (err != ESP_OK) return err;
        err = gpio_set_level(CONFIG_MACLAW_FANGTANG_MODEM_GUARD_GPIO,
                             CONFIG_MACLAW_FANGTANG_MODEM_GUARD_LEVEL);
        if (err != ESP_OK) return err;
    }
    if (CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO >= 0) {
        const gpio_config_t power = {
            .pin_bit_mask = 1ULL << CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO,
            .mode = GPIO_MODE_OUTPUT,
        };
        esp_err_t err = gpio_config(&power);
        if (err != ESP_OK) return err;
        err = gpio_set_level(CONFIG_MACLAW_FANGTANG_MODEM_POWER_GPIO,
                             CONFIG_MACLAW_FANGTANG_MODEM_POWER_ACTIVE_LEVEL);
        if (err != ESP_OK) return err;
        vTaskDelay(pdMS_TO_TICKS(500));
    }
    return ESP_OK;
}

static inline esp_err_t fangtang_cellular_adapter_start(uint32_t timeout_ms) {
    if (timeout_ms == 0 || timeout_ms > INT_MAX) return ESP_ERR_INVALID_ARG;
    esp_err_t err = fangtang_cellular_adapter_prepare_hardware();
    if (err != ESP_OK) return err;
    return ml307_transport_start(CONFIG_MACLAW_FANGTANG_MODEM_UART_TX_GPIO,
                                 CONFIG_MACLAW_FANGTANG_MODEM_UART_RX_GPIO,
                                 CONFIG_MACLAW_FANGTANG_MODEM_UART_BAUD,
                                 (int)timeout_ms,
                                 CONFIG_MACLAW_FANGTANG_MODEM_APN);
}

/* Neutral compact-profile transport contract.  Request semantics remain in
 * Connectivity Service; this adapter only binds those value requests to the
 * selected ML307 implementation and handles the required size conversion. */
static inline esp_err_t compact_connectivity_adapter_prepare_cellular_transport(void) {
    return fangtang_cellular_adapter_prepare_hardware();
}

static inline esp_err_t compact_connectivity_adapter_start_cellular_transport(
    uint32_t timeout_ms) {
    return fangtang_cellular_adapter_start(timeout_ms);
}

static inline bool compact_connectivity_adapter_is_cellular_transport_ready(void) {
    return ml307_transport_is_ready();
}

static inline esp_err_t compact_connectivity_adapter_quiesce_cellular_transport(
    uint32_t timeout_ms) {
    return ml307_transport_quiesce(timeout_ms);
}

static inline esp_err_t compact_connectivity_adapter_stream_body_reader(
    void *context, void *buffer, size_t requested, size_t *read_bytes) {
    if (!context || !buffer || !read_bytes || requested > UINT32_MAX) {
        return ESP_ERR_INVALID_ARG;
    }
    const device_connectivity_stream_request_t *request = context;
    uint32_t read = 0;
    device_status_t status = request->body_reader(request->body_reader_context,
                                                   buffer, (uint32_t)requested, &read);
    if (status != DEVICE_STATUS_OK) return device_status_to_platform_error(status);
    *read_bytes = read;
    return ESP_OK;
}

static inline esp_err_t compact_connectivity_adapter_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    if (!request || request->body_len > SIZE_MAX || request->response_capacity > SIZE_MAX) {
        return ESP_ERR_INVALID_ARG;
    }
    size_t response_len = 0;
    esp_err_t err = ml307_transport_http_request(
        request->method, request->url, request->content_type, request->authorization,
        request->extra_header_name, request->extra_header_value, request->body,
        (size_t)request->body_len, request->response, (size_t)request->response_capacity,
        &response_len, request->status_code, request->truncated,
        (int)request->timeout_ms, request->cancellation_owner, request->foreground);
    if (response_len > UINT32_MAX) return ESP_ERR_INVALID_SIZE;
    *request->response_len = (uint32_t)response_len;
    return err;
}

static inline esp_err_t compact_connectivity_adapter_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    if (!request || request->request.body_len > SIZE_MAX ||
        request->request.response_capacity > SIZE_MAX ||
        request->stream_buffer_size > SIZE_MAX) {
        return ESP_ERR_INVALID_ARG;
    }
    size_t response_len = 0;
    esp_err_t err = ml307_transport_http_request_stream(
        request->request.method, request->request.url, request->request.content_type,
        request->request.authorization, request->request.extra_header_name,
        request->request.extra_header_value, (size_t)request->request.body_len,
        compact_connectivity_adapter_stream_body_reader, (void *)request,
        request->stream_buffer, (size_t)request->stream_buffer_size,
        request->request.response, (size_t)request->request.response_capacity,
        &response_len, request->request.status_code, request->request.truncated,
        (int)request->request.timeout_ms, request->request.cancellation_owner,
        request->request.foreground);
    if (response_len > UINT32_MAX) return ESP_ERR_INVALID_SIZE;
    *request->request.response_len = (uint32_t)response_len;
    return err;
}

static inline bool compact_connectivity_adapter_cancel_cellular_foreground_request(void) {
    return ml307_transport_cancel_foreground();
}

static inline bool compact_connectivity_adapter_cancel_cellular_requests_for_owner(
    const void *owner) {
    return owner && ml307_transport_cancel_requests_for_owner(owner);
}

/* Import the legacy Fangtang vendor preference once into Configuration
 * Service's normalized snapshot.  Callers above this adapter never learn the
 * old namespace, integer encoding or this board's default physical uplink. */
static inline bool compact_connectivity_adapter_load_transport_selection(
    bool *out_cellular) {
    if (!out_cellular) return false;
    bool cellular = CONFIG_MACLAW_FANGTANG_DEFAULT_4G;
    bool saved = false;
    if (configuration_service_load_transport_selection(cellular, &cellular,
                                                        &saved) != ESP_OK) {
        return false;
    }
    if (saved) {
        *out_cellular = cellular;
        return true;
    }

    int32_t stock_type = 0;
    const esp_err_t stock_err = persistence_service_read_i32("network", "type",
                                                              &stock_type);
    if (stock_err == ESP_OK && (stock_type == 0 || stock_type == 1)) {
        cellular = stock_type == 1;
        if (configuration_service_set_transport_selection(cellular) != ESP_OK) {
            return false;
        }
    } else if (stock_err != ESP_OK && stock_err != ESP_ERR_NVS_NOT_FOUND) {
        return false;
    }
    *out_cellular = cellular;
    return true;
}

/* The input adapter captures its bounded, pre-scanner selector window.  This
 * adapter alone persists that physical intent as the selected uplink. */
static inline bool compact_connectivity_adapter_apply_startup_transport_toggle(
    uint32_t window_ms, bool current_cellular, bool *out_cellular) {
    if (!out_cellular) return false;
    *out_cellular = current_cellular;
    if (!compact_input_adapter_consume_startup_selector_result(window_ms)) return false;
    const bool selected = !current_cellular;
    if (configuration_service_set_transport_selection(selected) != ESP_OK) return false;
    *out_cellular = selected;
    return true;
}

/* ML307R-DL-MBRH0S01 cannot negotiate the standard Hub's ECDSA-only TLS
 * certificate.  Rewrite only that origin; custom endpoints remain unchanged. */
static inline void compact_connectivity_adapter_adapt_gateway_url(
    char *gateway_url, size_t capacity, bool cellular_active) {
    if (!gateway_url || capacity == 0 || !cellular_active) return;
    if (!strcmp(gateway_url, "https://hub.mypapers.top") ||
        !strcmp(gateway_url, "http://hub.mypapers.top")) {
        strlcpy(gateway_url, "http://hub.mypapers.top:9399", capacity);
    }
}
