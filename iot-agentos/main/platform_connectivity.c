#include "platform_connectivity.h"

#include "platform_connectivity_profile.h"

static bool http_request_is_valid(const device_connectivity_http_request_t *request) {
    return request && request->method && request->method[0] && request->url &&
           request->url[0] && request->response && request->response_capacity >= 2 &&
           request->response_len && request->status_code && request->truncated &&
           request->timeout_ms > 0;
}

void platform_connectivity_set_network_transport(bool cellular) {
    platform_connectivity_profile_set_network_transport(cellular);
}

device_status_t platform_connectivity_prepare_cellular_transport(void) {
    return platform_connectivity_profile_prepare_cellular_transport();
}

device_status_t platform_connectivity_start_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_connectivity_profile_start_cellular_transport(timeout_ms);
}

bool platform_connectivity_is_cellular_transport_ready(void) {
    return platform_connectivity_profile_is_cellular_transport_ready();
}

device_status_t platform_connectivity_quiesce_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_connectivity_profile_quiesce_cellular_transport(timeout_ms);
}

device_status_t platform_connectivity_deinit_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_connectivity_profile_deinit_cellular_transport(timeout_ms);
}

device_status_t platform_connectivity_reinitialize_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_connectivity_profile_reinitialize_cellular_transport(timeout_ms);
}

device_status_t platform_connectivity_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_connectivity_profile_prepare_system_sleep(timeout_ms);
}

void platform_connectivity_abort_system_sleep_prepare(void) {
    platform_connectivity_profile_abort_system_sleep_prepare();
}

device_status_t platform_connectivity_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    if (!http_request_is_valid(request)) return DEVICE_STATUS_INVALID_ARGUMENT;
    return platform_connectivity_profile_cellular_http_request(request);
}

device_status_t platform_connectivity_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    if (!request || !http_request_is_valid(&request->request) || !request->body_reader ||
        !request->stream_buffer || request->stream_buffer_size == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return platform_connectivity_profile_cellular_http_stream_request(request);
}

bool platform_connectivity_cancel_cellular_foreground_request(void) {
    return platform_connectivity_profile_cancel_cellular_foreground_request();
}

bool platform_connectivity_cancel_cellular_requests_for_owner(const void *owner) {
    if (!owner) return false;
    return platform_connectivity_profile_cancel_cellular_requests_for_owner(owner);
}

bool platform_connectivity_load_transport_selection(bool *out_cellular) {
    if (!out_cellular) return false;
    return platform_connectivity_profile_load_transport_selection(out_cellular);
}

bool platform_connectivity_apply_startup_transport_toggle(uint32_t window_ms,
                                                          bool current_cellular,
                                                          bool *out_cellular) {
    if (!out_cellular) return false;
    return platform_connectivity_profile_apply_startup_transport_toggle(window_ms,
                                                                        current_cellular,
                                                                        out_cellular);
}

void platform_connectivity_adapt_gateway_url(char *gateway_url,
                                             uint32_t gateway_url_capacity,
                                             bool cellular_active) {
    if (!gateway_url || gateway_url_capacity == 0) return;
    platform_connectivity_profile_adapt_gateway_url(gateway_url, gateway_url_capacity,
                                                    cellular_active);
}
