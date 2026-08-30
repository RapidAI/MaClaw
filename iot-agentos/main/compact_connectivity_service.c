#include "compact_connectivity_service.h"

/* Renderer and profile selector never gain a route to modem UART, GPIO,
 * ML307 requests or profile-specific persistence details. */
#include "boards/compact_connectivity_adapter.h"

bool compact_connectivity_service_load_transport_selection(bool *out_cellular) {
    return compact_connectivity_adapter_load_transport_selection(out_cellular);
}
bool compact_connectivity_service_apply_startup_transport_toggle(
    bool toggle_requested, bool current_cellular, bool *out_cellular) {
    return compact_connectivity_adapter_apply_startup_transport_toggle(
        toggle_requested, current_cellular, out_cellular);
}
void compact_connectivity_service_adapt_gateway_url(char *gateway_url, size_t capacity,
                                                    bool cellular_active) {
    compact_connectivity_adapter_adapt_gateway_url(gateway_url, capacity, cellular_active);
}
esp_err_t compact_connectivity_service_prepare_cellular_transport(void) {
    return compact_connectivity_adapter_prepare_cellular_transport();
}
bool compact_connectivity_service_cancel_cellular_foreground_request(void) {
    return compact_connectivity_adapter_cancel_cellular_foreground_request();
}
bool compact_connectivity_service_cancel_cellular_requests_for_owner(const void *owner) {
    return compact_connectivity_adapter_cancel_cellular_requests_for_owner(owner);
}
esp_err_t compact_connectivity_service_start_cellular_transport(uint32_t timeout_ms) {
    return compact_connectivity_adapter_start_cellular_transport(timeout_ms);
}
bool compact_connectivity_service_is_cellular_transport_ready(void) {
    return compact_connectivity_adapter_is_cellular_transport_ready();
}
esp_err_t compact_connectivity_service_quiesce_cellular_transport(uint32_t timeout_ms) {
    return compact_connectivity_adapter_quiesce_cellular_transport(timeout_ms);
}
esp_err_t compact_connectivity_service_deinit_cellular_transport(uint32_t timeout_ms) {
    return compact_connectivity_adapter_deinit_cellular_transport(timeout_ms);
}
esp_err_t compact_connectivity_service_reinitialize_cellular_transport(uint32_t timeout_ms) {
    return compact_connectivity_adapter_reinitialize_cellular_transport(timeout_ms);
}
esp_err_t compact_connectivity_service_prepare_system_sleep(uint32_t timeout_ms) {
    return compact_connectivity_adapter_prepare_system_sleep(timeout_ms);
}
void compact_connectivity_service_abort_system_sleep_prepare(void) {
    compact_connectivity_adapter_abort_system_sleep_prepare();
}
esp_err_t compact_connectivity_service_cellular_http_request(
    const device_connectivity_http_request_t *request) {
    return compact_connectivity_adapter_cellular_http_request(request);
}
esp_err_t compact_connectivity_service_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request) {
    return compact_connectivity_adapter_cellular_http_stream_request(request);
}
