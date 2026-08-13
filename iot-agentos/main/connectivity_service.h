#pragma once

/* Internal Connectivity Service state.  The public contract is declared in
 * device_api.h; this header exists only to keep the Device API facade free of
 * ESP-IDF synchronization details. */

#include <stdbool.h>

#include "esp_err.h"
#include "device_api.h"

/* The service owns selected-uplink policy.  The Platform Connectivity port
 * performs the matching profile-local transport hint only after the service
 * has atomically invalidated stale readiness for a real selection change.
 * Selection is durable configuration and may be restored before the runtime
 * Connectivity generation is initialized. */
void connectivity_service_set_active_uplink(device_uplink_t uplink);
bool connectivity_service_is_active_cellular(void);
void connectivity_service_restore_selected_uplink(void);
bool connectivity_service_apply_startup_transport_toggle(uint32_t window_ms);
/* The legacy composition root still owns esp_wifi configuration and driver
 * calls, but it must bracket every station connect with this generation-bound
 * service session.  IP/disconnect callbacks publish observations here; no
 * app/domain code may hold an ESP-IDF event group or infer readiness itself. */
bool connectivity_service_initialize(void);
/* Closes Connectivity admission and releases its Wi-Fi-attempt event group
 * after existing waiters have observed the closed generation.  This is a
 * service-state lifecycle boundary only: the composition root still owns the
 * ESP-NETIF/Wi-Fi/SNTP physical stop transaction. */
esp_err_t connectivity_service_deinit(uint32_t timeout_ms);
uint32_t connectivity_service_begin_wifi_attempt(const char *network_id);
bool connectivity_service_wait_wifi_attempt(uint32_t attempt_epoch,
                                             uint32_t timeout_ms);
/* The ID comes from WIFI_EVENT_STA_DISCONNECTED.  Ignore a late disconnect
 * for an old candidate after the adapter has moved to another candidate. */
bool connectivity_service_observe_wifi_disconnected(const char *network_id);
/* `connected_network_id` is the SSID read from the live station association,
 * not a configured credential.  The service accepts DHCP only when it
 * matches the current attempt, preventing a late IP event for an old scanned
 * candidate from completing the next candidate's wait. */
bool connectivity_service_observe_wifi_got_ip(const char *connected_network_id);
/* Readiness publication is accepted only for the live Connectivity
 * generation; late Wi-Fi/modem callbacks after deinit are ignored. */
void connectivity_service_set_wifi_ready(bool ready);
void connectivity_service_set_cellular_ready(bool ready);
bool connectivity_service_is_active_uplink_ready(void);
bool connectivity_service_get_snapshot(device_connectivity_snapshot_t *out_snapshot);

/* Provisioning is a Connectivity-owned logical session.  This narrow state
 * contract deliberately does not own Wi-Fi/AP/DHCP/DNS/HTTP resources: those
 * remain in the legacy composition root until their stop transaction is fully
 * specified.  Keeping the session state here stops unrelated application
 * workers from reading a main.c-local portal flag. */
void connectivity_service_begin_provisioning(bool pairing_recovery);
void connectivity_service_end_provisioning(void);
bool connectivity_service_is_provisioning_active(void);
bool connectivity_service_is_pairing_recovery_provisioning(void);

/* Cellular operations remain profile-private below Platform Connectivity, but
 * the service owns their Device-facing admission and argument contract.  This
 * keeps Device API from becoming a second physical-port caller while a future
 * Connectivity lifecycle coordinator absorbs Wi-Fi/portal ownership. */
device_status_t connectivity_service_prepare_cellular_transport(void);
device_status_t connectivity_service_start_cellular_transport(uint32_t timeout_ms);
/* One bounded cellular session transition. The service owns publication of the
 * selected uplink's readiness, so callers cannot retain a stale ready state
 * across a failed or late ML307 restart. UI/gateway policy stays above it. */
device_status_t connectivity_service_establish_cellular_transport(uint32_t timeout_ms);
bool connectivity_service_is_cellular_transport_ready(void);
device_status_t connectivity_service_quiesce_cellular_transport(uint32_t timeout_ms);
device_status_t connectivity_service_cellular_http_request(
    const device_connectivity_http_request_t *request);
device_status_t connectivity_service_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request);
bool connectivity_service_cancel_cellular_foreground_request(void);
bool connectivity_service_cancel_cellular_requests_for_owner(const void *owner);
void connectivity_service_adapt_gateway_url(char *gateway_url,
                                            uint32_t gateway_url_capacity);
