#pragma once

/*
 * Private legacy Connectivity transport seam.
 *
 * Platform Connectivity must translate shared uplink policy only to the
 * selected profile transport.  During renderer decomposition these entry
 * points keep the existing compact ML307 adapter and Wi-Fi-only round no-ops
 * behind a narrow contract, without making either profile bridge depend on
 * the broad board_port compatibility facade.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "device_api.h"
#include "esp_err.h"

void legacy_connectivity_transport_set_network_transport(bool cellular);
bool legacy_connectivity_transport_load_selection(bool *out_cellular);
bool legacy_connectivity_transport_apply_startup_toggle(uint32_t window_ms,
                                                         bool current_cellular,
                                                         bool *out_cellular);
void legacy_connectivity_transport_adapt_gateway_url(char *gateway_url,
                                                     size_t capacity,
                                                     bool cellular_active);
esp_err_t legacy_connectivity_transport_prepare_cellular(void);
esp_err_t legacy_connectivity_transport_start_cellular(uint32_t timeout_ms);
bool legacy_connectivity_transport_cellular_ready(void);
esp_err_t legacy_connectivity_transport_quiesce_cellular(uint32_t timeout_ms);
esp_err_t legacy_connectivity_transport_http_request(
    const device_connectivity_http_request_t *request);
esp_err_t legacy_connectivity_transport_http_stream_request(
    const device_connectivity_stream_request_t *request);
bool legacy_connectivity_transport_cancel_foreground_request(void);
bool legacy_connectivity_transport_cancel_requests_for_owner(const void *owner);

/* Renderer source owners implement these narrow symbols directly.  The
 * selected Platform Connectivity bridge therefore never needs a broad
 * board_port compatibility name to reach its profile-private transport. */
