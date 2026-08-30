#pragma once

/*
 * Provisioning Service (A10 first increment).
 *
 * Owns the portal user-space generation that used to live in main.c: the
 * composite start/stop mutex, HTTP admission + captive-portal routes, the
 * captive DNS worker, session scratch, the provisioning power lease and the
 * post-save restart coordinator.
 *
 * SoftAP/STA/DHCP/netif/driver and Wi-Fi event policy stay with the
 * composition root (B3).  Pairing-recovery session bits stay on
 * Connectivity Service.  The service generates a per-portal WPA2 passphrase;
 * the composition root applies it to the physical AP but never persists it.
 *
 * The public contract exposes value types only: no ESP-IDF error codes,
 * FreeRTOS handles, httpd objects or JSON types.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "configuration_service.h"
#include "device_api.h"

#define PROVISIONING_AP_SSID_CAPACITY 33u
#define PROVISIONING_AP_PASSPHRASE_CAPACITY 17u
#define PROVISIONING_WIFI_VALUE_CAPACITY CONFIGURATION_WIFI_VALUE_CAPACITY
#define PROVISIONING_WIFI_ENTERPRISE_CAPACITY CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY
#define PROVISIONING_WIFI_MODE_CAPACITY CONFIGURATION_WIFI_MODE_CAPACITY
#define PROVISIONING_GATEWAY_URL_CAPACITY CONFIGURATION_GATEWAY_URL_CAPACITY
#define PROVISIONING_PAIR_CODE_CAPACITY CONFIGURATION_PAIR_CODE_CAPACITY

typedef struct {
    char wifi_ssid[PROVISIONING_WIFI_VALUE_CAPACITY];
    char wifi_password[PROVISIONING_WIFI_VALUE_CAPACITY];
    char wifi_security[PROVISIONING_WIFI_MODE_CAPACITY];
    char wifi_eap_method[PROVISIONING_WIFI_MODE_CAPACITY];
    char wifi_identity[PROVISIONING_WIFI_ENTERPRISE_CAPACITY];
    char wifi_username[PROVISIONING_WIFI_ENTERPRISE_CAPACITY];
    char wifi_ttls_phase2[PROVISIONING_WIFI_MODE_CAPACITY];
    char wifi_ca_mode[PROVISIONING_WIFI_MODE_CAPACITY];
    char wifi_server_domain[PROVISIONING_WIFI_ENTERPRISE_CAPACITY];
    char gateway_url[PROVISIONING_GATEWAY_URL_CAPACITY];
} provisioning_runtime_wifi_t;

/* Opaque radio rollback token.  Provisioning only carries this value through
 * start-failure recovery; the physical Wi-Fi owner retains its snapshot. */
typedef uint32_t provisioning_radio_token_t;

/* Provisioning consumes only presentation-safe scan facts. The physical Wi-Fi
 * owner maps SDK auth modes before this callback is reached. */
typedef enum {
    PROVISIONING_SCAN_SECURITY_OPEN = 0,
    PROVISIONING_SCAN_SECURITY_WEP,
    PROVISIONING_SCAN_SECURITY_WPA,
    PROVISIONING_SCAN_SECURITY_WPA2,
    PROVISIONING_SCAN_SECURITY_WPA_WPA2,
    PROVISIONING_SCAN_SECURITY_WPA3,
    PROVISIONING_SCAN_SECURITY_WPA2_WPA3,
    PROVISIONING_SCAN_SECURITY_ENTERPRISE,
    PROVISIONING_SCAN_SECURITY_SECURED,
} provisioning_scan_security_t;

typedef bool (*provisioning_scan_observer_t)(const char *ssid, int8_t rssi,
                                              provisioning_scan_security_t security,
                                              void *context);

typedef struct {
    device_status_t (*init_network)(void);
    device_status_t (*ensure_ap_netif)(void);
    bool (*ap_netif_ready)(void);
    device_status_t (*configure_ap_dhcp)(void);
    /* Verify that AP clients can reach only the local setup subnet/service.
     * The composition root owns lwIP route/NAPT and DHCP detail; a non-OK
     * result keeps the credential-bearing portal fail-closed. */
    device_status_t (*verify_ap_client_isolation)(void);
    device_status_t (*ensure_sta_netif)(void);
    bool (*sta_netif_ready)(void);
    bool (*wifi_started)(void);
    void (*set_wifi_started)(bool started);
    void (*set_station_policy)(bool auto_connect, bool expected_disconnect);
    device_status_t (*wifi_disconnect)(void);
    device_status_t (*wifi_set_mode)(bool ap_only);
    /* The passphrase is a per-portal ephemeral WPA2 PSK.  It must never be
     * written to NVS, logs or runtime Wi-Fi credential state. */
    device_status_t (*wifi_configure_protected_ap)(const char *ssid,
                                                    const char *passphrase);
    device_status_t (*wifi_disable_ps)(void);
    device_status_t (*wifi_start)(void);
    device_status_t (*wifi_connect)(void);
    device_status_t (*wifi_confirm_ap_mode)(void);
    device_status_t (*read_softap_mac)(uint8_t mac[6]);
    device_status_t (*capture_radio)(provisioning_radio_token_t *token);
    void (*note_radio_changed)(provisioning_radio_token_t token);
    device_status_t (*restore_radio)(provisioning_radio_token_t token);
    device_status_t (*scan_visible_wifi)(uint32_t maximum_records,
                                         provisioning_scan_observer_t observer,
                                         void *context);
    device_status_t (*wake_word_stop)(void);
    device_status_t (*wake_word_start)(void);
    void (*show_text)(const char *title, const char *body);
    /* Physical display only; the QR contains the temporary AP passphrase. */
    void (*show_qr)(const char *ap_ssid, const char *ap_passphrase);
    void (*copy_runtime_wifi)(provisioning_runtime_wifi_t *out);
    void (*sync_runtime_after_network_delete)(const char *ssid);
    /* Copies the selected runtime SSID into caller storage. A copy seam keeps
     * the portal from retaining a pointer into another service's state. */
    void (*copy_preferred_scan_ssid)(char *out, uint32_t capacity);
} provisioning_service_host_t;

device_status_t provisioning_service_init(const provisioning_service_host_t *host);

/* keep_station is pairing-recovery: SoftAP stays beside an existing STA. */
void provisioning_service_start_portal(bool keep_station);

/* Bounded stop used by startup rollback and the post-save coordinator. */
device_status_t provisioning_service_stop_portal(uint32_t timeout_ms,
                                                 bool restore_wake_word);
device_status_t provisioning_service_stop_restart(uint32_t timeout_ms);

/* Future system-sleep admission boundary.  It never stops or recreates a
 * portal, captive DNS worker, or post-save restart: each carries network or
 * terminal configuration semantics that cannot be safely replayed by ABORT.
 * PREPARE instead closes only new portal admission and fails closed while a
 * portal generation or committed restart remains live. */
device_status_t provisioning_service_prepare_system_sleep(uint32_t timeout_ms);
void provisioning_service_abort_system_sleep_prepare(void);

/* True while HTTP or captive DNS still owns a live generation. */
bool provisioning_service_has_live_resources(void);
