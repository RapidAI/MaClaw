#include "platform_connectivity_profile.h"

#include "compact_connectivity_service.h"
#include "legacy_connectivity_transport.h"

static device_status_t compact_status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_NOT_SUPPORTED: return DEVICE_STATUS_UNAVAILABLE;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_FAIL: return DEVICE_STATUS_IO_ERROR;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

void platform_connectivity_profile_set_network_transport(bool cellular) {
    legacy_connectivity_transport_set_network_transport(cellular);
}

device_status_t platform_connectivity_profile_prepare_cellular_transport(void) {
    return compact_status_from_esp_err(legacy_connectivity_transport_prepare_cellular());
}

device_status_t platform_connectivity_profile_start_cellular_transport(uint32_t timeout_ms) {
    return compact_status_from_esp_err(legacy_connectivity_transport_start_cellular(timeout_ms));
}

bool platform_connectivity_profile_is_cellular_transport_ready(void) {
    return legacy_connectivity_transport_cellular_ready();
}

device_status_t platform_connectivity_profile_quiesce_cellular_transport(uint32_t timeout_ms) {
    return compact_status_from_esp_err(legacy_connectivity_transport_quiesce_cellular(timeout_ms));
}
device_status_t platform_connectivity_profile_deinit_cellular_transport(uint32_t timeout_ms) {
    return compact_status_from_esp_err(legacy_connectivity_transport_deinit_cellular(timeout_ms));
}
device_status_t platform_connectivity_profile_reinitialize_cellular_transport(uint32_t timeout_ms) {
    return compact_status_from_esp_err(legacy_connectivity_transport_reinitialize_cellular(timeout_ms));
}

device_status_t platform_connectivity_profile_prepare_system_sleep(uint32_t timeout_ms) {
    return compact_status_from_esp_err(
        compact_connectivity_service_prepare_system_sleep(timeout_ms));
}

void platform_connectivity_profile_abort_system_sleep_prepare(void) {
    compact_connectivity_service_abort_system_sleep_prepare();
}

device_status_t platform_connectivity_profile_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    return compact_status_from_esp_err(legacy_connectivity_transport_http_request(request));
}

device_status_t platform_connectivity_profile_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    return compact_status_from_esp_err(legacy_connectivity_transport_http_stream_request(request));
}

bool platform_connectivity_profile_cancel_cellular_foreground_request(void) {
    return legacy_connectivity_transport_cancel_foreground_request();
}

bool platform_connectivity_profile_cancel_cellular_requests_for_owner(const void *owner) {
    return legacy_connectivity_transport_cancel_requests_for_owner(owner);
}

bool platform_connectivity_profile_load_transport_selection(bool *out_cellular) {
    return legacy_connectivity_transport_load_selection(out_cellular);
}

bool platform_connectivity_profile_apply_startup_transport_toggle(uint32_t window_ms,
                                                                  bool current_cellular,
                                                                  bool *out_cellular) {
    return legacy_connectivity_transport_apply_startup_toggle(window_ms, current_cellular,
                                                               out_cellular);
}

void platform_connectivity_profile_adapt_gateway_url(char *gateway_url,
                                                     uint32_t gateway_url_capacity,
                                                     bool cellular_active) {
    legacy_connectivity_transport_adapt_gateway_url(gateway_url, gateway_url_capacity,
                                                     cellular_active);
}
