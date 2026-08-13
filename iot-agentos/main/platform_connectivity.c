#include "platform_connectivity.h"

#include "board_port.h"

static device_status_t status_from_esp_err(esp_err_t err) {
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

static bool http_request_is_valid(const device_connectivity_http_request_t *request) {
    return request && request->method && request->method[0] && request->url &&
           request->url[0] && request->response && request->response_capacity >= 2 &&
           request->response_len && request->status_code && request->truncated &&
           request->timeout_ms > 0;
}

void platform_connectivity_set_network_transport(bool cellular) {
    board_port_set_network_transport(cellular);
}

device_status_t platform_connectivity_prepare_cellular_transport(void) {
    return status_from_esp_err(board_port_prepare_cellular_transport());
}

device_status_t platform_connectivity_start_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_start_cellular_transport(timeout_ms));
}

bool platform_connectivity_is_cellular_transport_ready(void) {
    return board_port_is_cellular_transport_ready();
}

device_status_t platform_connectivity_quiesce_cellular_transport(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_quiesce_cellular_transport(timeout_ms));
}

device_status_t platform_connectivity_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    if (!http_request_is_valid(request)) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(board_port_cellular_http_request(request));
}

device_status_t platform_connectivity_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    if (!request || !http_request_is_valid(&request->request) || !request->body_reader ||
        !request->stream_buffer || request->stream_buffer_size == 0) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    return status_from_esp_err(board_port_cellular_http_stream_request(request));
}

bool platform_connectivity_cancel_cellular_foreground_request(void) {
    return board_port_cancel_cellular_foreground_request();
}

bool platform_connectivity_cancel_cellular_requests_for_owner(const void *owner) {
    if (!owner) return false;
    return board_port_cancel_cellular_requests_for_owner(owner);
}

bool platform_connectivity_load_transport_selection(bool *out_cellular) {
    if (!out_cellular) return false;
    return board_port_load_transport_selection(out_cellular);
}

bool platform_connectivity_apply_startup_transport_toggle(uint32_t window_ms,
                                                          bool current_cellular,
                                                          bool *out_cellular) {
    if (!out_cellular) return false;
    return board_port_apply_startup_transport_toggle(window_ms, current_cellular,
                                                     out_cellular);
}

void platform_connectivity_adapt_gateway_url(char *gateway_url,
                                             uint32_t gateway_url_capacity,
                                             bool cellular_active) {
    if (!gateway_url || gateway_url_capacity == 0) return;
    board_port_adapt_gateway_url(gateway_url, gateway_url_capacity, cellular_active);
}
