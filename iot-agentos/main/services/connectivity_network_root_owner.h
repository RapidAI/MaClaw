#pragma once

/*
 * Private owner for the ESP-IDF Wi-Fi physical-root transaction.
 *
 * The contract deliberately consists only of status, booleans and normalized
 * callback values. It is not a Device/Platform API: Connectivity policy,
 * credentials, retry decisions, UI and provisioning business state remain
 * above this owner. The owner keeps the dependent ESP-NETIF/default event
 * loop/Wi-Fi driver teardown order in one place for every board.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"
#include "services/connectivity_wifi_driver_owner.h"

typedef struct {
    /* Terminal portal work owns HTTP/DNS workers and may race a radio stop.
     * It must drain before callback admission and physical Wi-Fi resources. */
    device_status_t (*stop_provisioning)(void *context, uint32_t timeout_ms);
    /* Close and drain normalized Wi-Fi/IP callback admission before physical
     * event handlers/default loop can be released. */
    device_status_t (*stop_callback_admission)(void *context, uint32_t timeout_ms);
    /* Stop application SNTP users before the Wi-Fi radio is stopped. */
    device_status_t (*stop_clock_sync)(void *context, uint32_t timeout_ms);
    /* The generic physical stop is deliberately a second phase.  It must
     * never tear down the radio/netifs below a still-live captive portal or
     * post-save restart coordinator; the caller first drains that terminal
     * work through stop_provisioning(). */
    bool (*provisioning_has_live_resources)(void *context);
    void *context;
} connectivity_network_root_owner_lifecycle_host_t;

/* The caller configures these value-only lifecycle bridges before resources
 * are allocated. The host is copied. A different host cannot replace the
 * current physical generation while it retains resources; this prevents a
 * later restart path from stopping callbacks through an unrelated context. */
device_status_t connectivity_network_root_owner_configure_lifecycle_host(
    const connectivity_network_root_owner_lifecycle_host_t *host);

/* ESP-NETIF/default-loop singleton allocation. The lifecycle host must be
 * configured first, otherwise allocation is rejected: a physical generation
 * is never created without its complete teardown bridge. A partial generation
 * remains fail-closed until stop() has actually released it. */
device_status_t connectivity_network_root_owner_ensure_core(void);
bool connectivity_network_root_owner_core_ready(void);
bool connectivity_network_root_owner_has_resources(void);

/* Wi-Fi driver plus the application's normalized event routes. Like core
 * allocation, this requires the lifecycle host to have been bound first. */
device_status_t connectivity_network_root_owner_initialize_wifi(
    connectivity_wifi_driver_event_callback_t callback, void *callback_arg);
bool connectivity_network_root_owner_wifi_ready(void);
bool connectivity_network_root_owner_wifi_has_resources(void);

/* Drains terminal portal/restart user-space work before the generic
 * Connectivity worker sweep. It is separate because a post-save coordinator
 * itself owns portal cleanup. An OK return also means a post-stop live-resource
 * observation was false; a portal generation that appears during the stop
 * callback makes this phase fail closed with BUSY. */
device_status_t connectivity_network_root_owner_stop_provisioning(uint32_t timeout_ms);

/* Stops physical resources in the only safe order after
 * stop_provisioning() has drained portal/restart user-space work (verified
 * through provisioning_has_live_resources): callback
 * drain, SNTP, radio,
 * handler instances, AP/STA netifs, Wi-Fi driver, default loop and ESP-NETIF.
 * On failure no subsequent stage runs and the retained generation remains
 * fail-closed. `out_wifi_radio_stopped` reports the committed radio-stop fact
 * even when a later teardown stage fails. */
device_status_t connectivity_network_root_owner_stop(
    uint32_t timeout_ms, bool *out_wifi_radio_stopped);
