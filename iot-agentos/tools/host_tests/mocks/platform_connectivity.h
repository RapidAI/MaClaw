#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

void platform_connectivity_set_network_transport(bool cellular);
device_status_t platform_connectivity_prepare_cellular_transport(void);
device_status_t platform_connectivity_start_cellular_transport(uint32_t timeout_ms);
bool platform_connectivity_is_cellular_transport_ready(void);
device_status_t platform_connectivity_quiesce_cellular_transport(uint32_t timeout_ms);
device_status_t platform_connectivity_deinit_cellular_transport(uint32_t timeout_ms);
device_status_t platform_connectivity_reinitialize_cellular_transport(uint32_t timeout_ms);
device_status_t platform_connectivity_prepare_system_sleep(uint32_t timeout_ms);
void platform_connectivity_abort_system_sleep_prepare(void);
device_status_t platform_connectivity_cellular_http_request(
    const device_connectivity_http_request_t *request);
device_status_t platform_connectivity_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request);
bool platform_connectivity_cancel_cellular_foreground_request(void);
bool platform_connectivity_cancel_cellular_requests_for_owner(const void *owner);
bool platform_connectivity_load_transport_selection(bool *out_cellular);
bool platform_connectivity_apply_startup_transport_toggle(uint32_t window_ms,
                                                          bool current_cellular,
                                                          bool *out_cellular);
void platform_connectivity_adapt_gateway_url(char *gateway_url,
                                             uint32_t gateway_url_capacity,
                                             bool cellular_active);
