#pragma once

/*
 * Connectivity physical-root lifecycle coordinator.
 *
 * Owns the value-level cold-start and terminal-stop ordering for one logical
 * Connectivity generation: logical service -> bound physical teardown bridge
 * -> ESP-NETIF/default-loop root -> Wi-Fi driver/routes, plus the inverse
 * terminal stop. The composition root retains every SDK callback, event route
 * and physical owner behind this value-only host contract.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

typedef struct {
    uint32_t struct_size;
    uint64_t (*now_ms)(void *context);
    device_status_t (*initialize_logical)(void *context);
    device_status_t (*configure_physical_lifecycle)(void *context);
    bool (*physical_has_resources)(void *context);
    bool (*physical_core_ready)(void *context);
    device_status_t (*ensure_physical_core)(void *context);
    bool (*wifi_has_resources)(void *context);
    bool (*wifi_ready)(void *context);
    device_status_t (*initialize_wifi)(void *context);
    void (*open_wifi_callback_admission)(void *context);
    device_status_t (*stop_physical)(uint32_t timeout_ms,
                                     bool *out_wifi_radio_stopped,
                                     void *context);
    device_status_t (*deinitialize_logical)(uint32_t timeout_ms, void *context);
    void *context;
} connectivity_network_lifecycle_service_host_t;

/* The host is immutable after first configuration. Rebinding a physical root
 * while resources exist is rejected, so a terminal stop can never be routed
 * to another composition-root generation. */
device_status_t connectivity_network_lifecycle_service_init(
    const connectivity_network_lifecycle_service_host_t *host);

/* Idempotently establishes the logical service and physical singleton root.
 * A failed first allocation is rolled back terminally; partial roots never
 * become a replacement generation. */
device_status_t connectivity_network_lifecycle_service_ensure_core(void);

/* Establishes the normalized Wi-Fi driver/event route after the core root.
 * A partially initialized driver is fail-closed and causes the same terminal
 * root rollback as a failed core allocation. */
device_status_t connectivity_network_lifecycle_service_ensure_wifi(void);

/* Stops physical resources before the logical Connectivity generation under a
 * single monotonic deadline. Later logical teardown is not attempted after a
 * physical failure, preserving live callback safety. */
device_status_t connectivity_network_lifecycle_service_stop(uint32_t timeout_ms,
                                                             bool *out_wifi_radio_stopped);
