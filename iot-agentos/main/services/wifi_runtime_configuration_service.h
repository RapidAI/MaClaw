#pragma once

/*
 * Boot-scoped Wi-Fi runtime configuration.
 *
 * Configuration Service remains the durable authority. This value-only owner
 * retains the exact boot candidate copied under Configuration's boot-snapshot
 * transaction, plus the selected saved-network runtime adjustment required by
 * cold-start fallback and portal deletion. It intentionally owns no radio,
 * storage, HTTP, RTOS, allocator, or board object.
 */

#include <stdbool.h>
#include <stdint.h>

#include "configuration_service.h"
#include "device_api.h"

typedef struct {
    char ssid[CONFIGURATION_WIFI_VALUE_CAPACITY];
    char password[CONFIGURATION_WIFI_VALUE_CAPACITY];
    char security[CONFIGURATION_WIFI_MODE_CAPACITY];
    char eap_method[CONFIGURATION_WIFI_MODE_CAPACITY];
    char identity[CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY];
    char username[CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY];
    char ttls_phase2[CONFIGURATION_WIFI_MODE_CAPACITY];
    char ca_mode[CONFIGURATION_WIFI_MODE_CAPACITY];
    char server_domain[CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY];
    configuration_wifi_network_t saved_networks[CONFIGURATION_WIFI_NETWORK_CAPACITY];
    uint8_t saved_network_count;
} wifi_runtime_configuration_snapshot_t;

device_status_t wifi_runtime_configuration_service_init(void);

/* Accepts Configuration's already-validated boot candidate exactly once. */
bool wifi_runtime_configuration_service_capture_boot_snapshot(
    const configuration_snapshot_t *snapshot);

/* Produces a stable by-value copy for a caller's transaction. */
bool wifi_runtime_configuration_service_get_snapshot(
    wifi_runtime_configuration_snapshot_t *out_snapshot);

/* Candidate Wi-Fi selection changes only this boot's runtime station values;
 * Configuration's durable snapshot remains unchanged until its own mutation
 * transaction succeeds. */
bool wifi_runtime_configuration_service_select_saved_network(const char *ssid,
                                                              const char *password);

/* Synchronizes the durable personal-network catalogue after a successful
 * portal delete. If the active non-enterprise network was deleted, clear its
 * runtime credentials so the root will reopen setup instead of reconnecting
 * a removed network. */
bool wifi_runtime_configuration_service_sync_saved_networks_after_delete(
    const configuration_wifi_network_t *networks, uint8_t network_count,
    const char *deleted_ssid);
