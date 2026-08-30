#pragma once

/*
 * Wi-Fi cold-start connection policy.
 *
 * This service owns only value-level ordering: saved-network RSSI selection,
 * attempt epochs, enterprise/personal branch selection and fallback. The
 * composition root retains credentials, ESP-IDF radio/netif/event objects,
 * Connectivity wait primitives and status/UI publication behind callbacks.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

#define WIFI_STARTUP_SERVICE_MAX_SAVED_NETWORKS 8u

typedef struct {
    const char *ssid;
    const char *password;
} wifi_startup_service_saved_network_t;

typedef struct {
    const char *ssid;
    const char *password;
    bool enterprise;
    bool keep_setup_ap;
} wifi_startup_service_station_config_t;

typedef struct {
    const char *identity;
    const char *username;
    const char *password;
    const char *server_domain;
    bool use_ttls;
    bool ttls_phase2_pap;
    bool use_system_ca;
} wifi_startup_service_enterprise_config_t;

typedef bool (*wifi_startup_service_scan_observer_t)(
    const char *ssid, int8_t rssi, void *context);

typedef struct {
    device_status_t (*ensure_network)(void *context);
    device_status_t (*ensure_station)(void *context);
    void (*set_station_policy)(bool auto_connect, bool expected_disconnect,
                               void *context);
    device_status_t (*configure_station)(
        const wifi_startup_service_station_config_t *config, void *context);
    bool (*enterprise_enabled)(void *context);
    device_status_t (*configure_enterprise)(
        const wifi_startup_service_enterprise_config_t *config, void *context);
    device_status_t (*disable_enterprise)(void *context);
    bool (*wifi_started)(void *context);
    device_status_t (*wifi_start)(void *context);
    device_status_t (*wifi_connect)(void *context);
    device_status_t (*wifi_disconnect)(void *context);
    device_status_t (*scan_visible)(uint32_t maximum_records,
                                    wifi_startup_service_scan_observer_t observer,
                                    void *observer_context, void *context);
    /* Records the selected candidate in composition-root-owned runtime
     * configuration before its physical station configuration is submitted. */
    void (*select_saved_network)(const char *ssid, const char *password,
                                 void *context);
    uint32_t (*begin_attempt)(const char *network_id, void *context);
    bool (*wait_attempt)(uint32_t attempt_epoch, uint32_t timeout_ms,
                         void *context);
    void (*publish_network_ready)(const char *ssid, bool ready, void *context);
    bool (*setup_portal_active)(void *context);
    void *context;
} wifi_startup_service_host_t;

typedef struct {
    const char *ssid;
    const char *password;
    bool boot_provisioning_staged;
    bool enterprise;
    wifi_startup_service_enterprise_config_t enterprise_config;
    const wifi_startup_service_saved_network_t *saved_networks;
    uint32_t saved_network_count;
    uint32_t scan_maximum_records;
    uint32_t candidate_connect_timeout_ms;
    uint32_t connect_timeout_ms;
} wifi_startup_service_request_t;

/* Returns OK only after the selected station has obtained the current
 * Connectivity attempt's readiness observation. Candidate scan failures and
 * candidate exhaustion intentionally fall back to the configured station,
 * matching the pre-extraction cold-start behavior. */
device_status_t wifi_startup_service_connect(
    const wifi_startup_service_host_t *host,
    const wifi_startup_service_request_t *request);
