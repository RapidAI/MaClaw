#pragma once

/* Internal Connectivity Service state.  The public contract is declared in
 * device_api.h; this header exists only to keep the Device API facade free of
 * ESP-IDF synchronization details. */

#include <stdbool.h>

#include "esp_err.h"
#include "device_api.h"
#include "fault_domain.h"

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
device_status_t connectivity_service_initialize(void);
/* Closes Connectivity admission and releases its Wi-Fi-attempt event group
 * after existing waiters have observed the closed generation.  This is a
 * service-state lifecycle boundary only: the composition root still owns the
 * ESP-NETIF/Wi-Fi/SNTP physical stop transaction. */
device_status_t connectivity_service_deinit(uint32_t timeout_ms);
/* Read-only diagnostic evidence for the logical Connectivity lifecycle. It
 * does not claim that Wi-Fi, ESP-NETIF or a cellular modem was restarted. */
bool connectivity_service_get_fault_domain_snapshot(
    fault_domain_snapshot_t *out_snapshot);

/* ESP-IDF default-loop registration remains a composition-root physical
 * concern, but the callback's admission, drain and reversible System Sleep
 * fence belong to Connectivity.  The root calls open only after every event
 * instance has registered, and calls stop before it unregisters or tears down
 * the Wi-Fi/default-loop transaction.  No caller receives an SDK event or
 * synchronization handle. */
void connectivity_service_open_wifi_event_callback_admission(void);
bool connectivity_service_wifi_event_callback_enter(void);
void connectivity_service_wifi_event_callback_leave(void);
device_status_t connectivity_service_stop_wifi_event_callback_admission(
    uint32_t timeout_ms);
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

/* System Sleep PREPARE closes new logical network admission and waits for
 * requests already accepted through this service to return.  It never stops a
 * Wi-Fi driver, powers down a modem, or makes a sleep-entry claim; those are
 * separate profile-private commit responsibilities.  Every successful begin
 * must be paired with end on the same synchronous request path. */
device_status_t connectivity_service_begin_network_request(void);
void connectivity_service_end_network_request(void);
/* The legacy composition root retains concrete Wi-Fi HTTP client and Gateway
 * worker ownership.  It registers this small cancellation bridge so the
 * hardware-neutral service can request bounded cancellation without learning
 * ESP client handles, task objects, or selected transport details.  The
 * bridge must be idempotent and must not reopen network admission. */
typedef device_status_t (*connectivity_service_system_sleep_request_canceller_t)(
    uint32_t timeout_ms, void *context);
/* If PREPARE's composition-root bridge stopped a bounded Gateway worker, this
 * paired callback restores only that pre-existing work during rollback. It is
 * called before normal network admission reopens and must be idempotent. */
typedef void (*connectivity_service_system_sleep_request_resumer_t)(void *context);
void connectivity_service_set_system_sleep_request_canceller(
    connectivity_service_system_sleep_request_canceller_t canceller,
    void *context);
void connectivity_service_set_system_sleep_request_resumer(
    connectivity_service_system_sleep_request_resumer_t resumer,
    void *context);
device_status_t connectivity_service_prepare_system_sleep(uint32_t timeout_ms);
void connectivity_service_abort_system_sleep_prepare(void);

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
/* Lifecycle evidence only: terminal root teardown uses this to determine
 * whether a prior cellular session remains to be drained after durable policy
 * switched back to Wi-Fi. It is not a live uplink-switch request. */
bool connectivity_service_has_cellular_transport_session(void);
device_status_t connectivity_service_quiesce_cellular_transport(uint32_t timeout_ms);
/* Terminally destroys a drained cellular physical generation.  A failed
 * drain leaves admission closed and the old generation intact for retry. */
device_status_t connectivity_service_deinit_cellular_transport(uint32_t timeout_ms);
/* Explicit fresh-generation start after terminal deinit; never lazy-starts
 * from a stale readiness callback. */
device_status_t connectivity_service_reinitialize_cellular_transport(uint32_t timeout_ms);
device_status_t connectivity_service_cellular_http_request(
    const device_connectivity_http_request_t *request);
device_status_t connectivity_service_cellular_http_stream_request(
    const device_connectivity_stream_request_t *request);
bool connectivity_service_cancel_cellular_foreground_request(void);
bool connectivity_service_cancel_cellular_requests_for_owner(const void *owner);
void connectivity_service_adapt_gateway_url(char *gateway_url,
                                            uint32_t gateway_url_capacity);
