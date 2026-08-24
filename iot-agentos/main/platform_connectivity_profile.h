#pragma once

/* Selected physical Connectivity profile seam.  Connectivity Service owns
 * uplink policy, readiness and lifecycle transactions.  This private bridge
 * owns only the selected profile's physical transport adapter and any legacy
 * renderer migration seam below the neutral Platform Connectivity contract. */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

void platform_connectivity_profile_set_network_transport(bool cellular);
device_status_t platform_connectivity_profile_prepare_cellular_transport(void);
device_status_t platform_connectivity_profile_start_cellular_transport(uint32_t timeout_ms);
bool platform_connectivity_profile_is_cellular_transport_ready(void);
device_status_t platform_connectivity_profile_quiesce_cellular_transport(uint32_t timeout_ms);
device_status_t platform_connectivity_profile_prepare_system_sleep(uint32_t timeout_ms);
void platform_connectivity_profile_abort_system_sleep_prepare(void);
device_status_t platform_connectivity_profile_cellular_http_request(
    const device_connectivity_http_request_t *request);
device_status_t platform_connectivity_profile_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request);
bool platform_connectivity_profile_cancel_cellular_foreground_request(void);
bool platform_connectivity_profile_cancel_cellular_requests_for_owner(const void *owner);
bool platform_connectivity_profile_load_transport_selection(bool *out_cellular);
bool platform_connectivity_profile_apply_startup_transport_toggle(uint32_t window_ms,
                                                                  bool current_cellular,
                                                                  bool *out_cellular);
void platform_connectivity_profile_adapt_gateway_url(char *gateway_url,
                                                     uint32_t gateway_url_capacity,
                                                     bool cellular_active);
