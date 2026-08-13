/* Bread Compact connectivity hardware profile.
 *
 * This Wi-Fi-only profile implements the same private cellular-transport
 * contract as a no-op.  Shared board facades therefore forward normalized
 * Connectivity requests without selecting a board or leaking modem details.
 */
#pragma once

#include "sdkconfig.h"

#if !CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD
#error "Bread connectivity adapter may only be included by the Bread Compact profile"
#endif

#ifndef MACLAW_COMPACT_CONNECTIVITY_ADAPTER_IMPLEMENTATION
#error "Bread connectivity adapter is owned exclusively by compact_connectivity_service.c"
#endif

#include "device_api.h"
#include "esp_err.h"

static inline esp_err_t compact_connectivity_adapter_prepare_cellular_transport(void) {
    return ESP_ERR_NOT_SUPPORTED;
}

static inline esp_err_t compact_connectivity_adapter_start_cellular_transport(
    uint32_t timeout_ms) {
    (void)timeout_ms;
    return ESP_ERR_NOT_SUPPORTED;
}

static inline bool compact_connectivity_adapter_is_cellular_transport_ready(void) {
    return false;
}

static inline esp_err_t compact_connectivity_adapter_quiesce_cellular_transport(
    uint32_t timeout_ms) {
    (void)timeout_ms;
    return ESP_ERR_NOT_SUPPORTED;
}

static inline esp_err_t compact_connectivity_adapter_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    (void)request;
    return ESP_ERR_NOT_SUPPORTED;
}

static inline esp_err_t compact_connectivity_adapter_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    (void)request;
    return ESP_ERR_NOT_SUPPORTED;
}

static inline bool compact_connectivity_adapter_cancel_cellular_foreground_request(void) {
    return false;
}

static inline bool compact_connectivity_adapter_cancel_cellular_requests_for_owner(
    const void *owner) {
    (void)owner;
    return false;
}

/* Wi-Fi-only profiles have no alternate-uplink preference or transport-
 * specific Gateway compatibility rule.  Keep those neutral answers beside
 * the cellular no-ops so the common facade never selects a board. */
static inline bool compact_connectivity_adapter_load_transport_selection(
    bool *out_cellular) {
    if (out_cellular) *out_cellular = false;
    return false;
}

static inline bool compact_connectivity_adapter_apply_startup_transport_toggle(
    bool toggle_requested, bool current_cellular, bool *out_cellular) {
    (void)toggle_requested;
    if (out_cellular) *out_cellular = current_cellular;
    return false;
}

static inline void compact_connectivity_adapter_adapt_gateway_url(
    char *gateway_url, size_t capacity, bool cellular_active) {
    (void)gateway_url;
    (void)capacity;
    (void)cellular_active;
}
