#pragma once

/* Private compact-board Connectivity HAL implementation boundary.
 *
 * Connectivity Service owns transport policy; the compact renderer only
 * supplies board-port facade hooks.  This service is the selected profile's
 * sole source owner for cellular implementation, uplink preference migration
 * and board-specific gateway compatibility.  It is not a public HAL API.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"
#include "esp_err.h"

bool compact_connectivity_service_load_transport_selection(bool *out_cellular);
bool compact_connectivity_service_apply_startup_transport_toggle(
    bool toggle_requested, bool current_cellular, bool *out_cellular);
void compact_connectivity_service_adapt_gateway_url(char *gateway_url, size_t capacity,
                                                    bool cellular_active);
esp_err_t compact_connectivity_service_prepare_cellular_transport(void);
bool compact_connectivity_service_cancel_cellular_foreground_request(void);
bool compact_connectivity_service_cancel_cellular_requests_for_owner(const void *owner);
esp_err_t compact_connectivity_service_start_cellular_transport(uint32_t timeout_ms);
bool compact_connectivity_service_is_cellular_transport_ready(void);
esp_err_t compact_connectivity_service_quiesce_cellular_transport(uint32_t timeout_ms);
esp_err_t compact_connectivity_service_prepare_system_sleep(uint32_t timeout_ms);
void compact_connectivity_service_abort_system_sleep_prepare(void);
esp_err_t compact_connectivity_service_cellular_http_request(
    const device_connectivity_http_request_t *request);
esp_err_t compact_connectivity_service_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request);
