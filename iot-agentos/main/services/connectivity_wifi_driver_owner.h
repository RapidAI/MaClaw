#pragma once

/* Private physical Wi-Fi driver and application event-instance owner.
 *
 * The root supplies a business event callback and controls when Connectivity
 * callback admission opens/closes. This contract hides all ESP-IDF types and
 * retains the exact registered instances for fail-closed teardown. The owner
 * translates ESP-IDF events to normalized values before invoking the root.
 */

#include <stdbool.h>
#include <stdint.h>

#include "device_api.h"

#define CONNECTIVITY_WIFI_DRIVER_SSID_CAPACITY 33u
#define CONNECTIVITY_WIFI_DRIVER_HOSTNAME_CAPACITY 33u
#define CONNECTIVITY_WIFI_DRIVER_IPV4_TEXT_CAPACITY 16u

typedef enum {
    CONNECTIVITY_WIFI_DRIVER_EVENT_AP_CLIENT_CONNECTED = 0,
    CONNECTIVITY_WIFI_DRIVER_EVENT_AP_CLIENT_LEASED,
    CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_STARTED,
    CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_DISCONNECTED,
    CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_GOT_IP,
} connectivity_wifi_driver_event_kind_t;

/* Each field is a copied, NUL-terminated value. Unused fields are empty/zero.
 * The callback must not retain this pointer after it returns. */
typedef struct {
    connectivity_wifi_driver_event_kind_t kind;
    char ssid[CONNECTIVITY_WIFI_DRIVER_SSID_CAPACITY];
    uint8_t mac[6];
    char ipv4[CONNECTIVITY_WIFI_DRIVER_IPV4_TEXT_CAPACITY];
    char hostname[CONNECTIVITY_WIFI_DRIVER_HOSTNAME_CAPACITY];
} connectivity_wifi_driver_event_t;

typedef void (*connectivity_wifi_driver_event_callback_t)(
    void *arg, const connectivity_wifi_driver_event_t *event);

/* Parsed by the Configuration/business boundary; this physical owner only
 * translates the normalized enterprise values into ESP-IDF EAP calls. */
typedef struct {
    const char *identity;
    const char *username;
    const char *password;
    const char *server_domain;
    bool use_ttls;
    bool ttls_phase2_pap;
    bool use_system_ca;
} connectivity_wifi_driver_enterprise_config_t;

/* Normalized station policy.  `keep_setup_ap` preserves an existing portal's
 * APSTA radio shape; `enterprise` selects the matching WPA threshold. */
typedef struct {
    const char *ssid;
    const char *password;
    bool enterprise;
    bool keep_setup_ap;
} connectivity_wifi_driver_station_config_t;

/* Normalized physical scan fact. The Wi-Fi auth-mode enum remains private to
 * the source owner, so every service can consume this without ESP-IDF types. */
typedef enum {
    CONNECTIVITY_WIFI_DRIVER_SECURITY_OPEN = 0,
    CONNECTIVITY_WIFI_DRIVER_SECURITY_WEP,
    CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA,
    CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA2,
    CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA_WPA2,
    CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA3,
    CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA2_WPA3,
    CONNECTIVITY_WIFI_DRIVER_SECURITY_ENTERPRISE,
    CONNECTIVITY_WIFI_DRIVER_SECURITY_SECURED,
} connectivity_wifi_driver_security_t;

typedef bool (*connectivity_wifi_driver_scan_observer_t)(
    const char *ssid, int8_t rssi, connectivity_wifi_driver_security_t security,
    void *context);

device_status_t connectivity_wifi_driver_owner_initialize(
    connectivity_wifi_driver_event_callback_t callback, void *callback_arg);
bool connectivity_wifi_driver_owner_initialized(void);
/* True only when the driver and every application event route are owned by
 * this generation. A retained partial generation is intentionally not ready. */
bool connectivity_wifi_driver_owner_ready(void);
bool connectivity_wifi_driver_owner_enterprise_enabled(void);
device_status_t connectivity_wifi_driver_owner_configure_enterprise(
    const connectivity_wifi_driver_enterprise_config_t *config);
device_status_t connectivity_wifi_driver_owner_disable_enterprise(void);
device_status_t connectivity_wifi_driver_owner_configure_station(
    const connectivity_wifi_driver_station_config_t *config);
bool connectivity_wifi_driver_owner_started(void);
void connectivity_wifi_driver_owner_set_station_policy(bool auto_connect,
                                                        bool expected_disconnect);
bool connectivity_wifi_driver_owner_take_expected_disconnect(void);
bool connectivity_wifi_driver_owner_should_auto_connect(void);
device_status_t connectivity_wifi_driver_owner_start(void);
device_status_t connectivity_wifi_driver_owner_stop(void);
device_status_t connectivity_wifi_driver_owner_connect(void);
device_status_t connectivity_wifi_driver_owner_disconnect(void);
device_status_t connectivity_wifi_driver_owner_disable_power_save(void);
device_status_t connectivity_wifi_driver_owner_set_mode(bool ap_only);
device_status_t connectivity_wifi_driver_owner_configure_protected_ap(
    const char *ssid, const char *passphrase);
device_status_t connectivity_wifi_driver_owner_confirm_ap_mode(void);
/* Provisioning receives only an opaque generation token.  This physical owner
 * retains the private radio snapshot and rejects stale rollback attempts. */
device_status_t connectivity_wifi_driver_owner_capture_portal_radio(uint32_t *out_token);
void connectivity_wifi_driver_owner_note_portal_radio_changed(uint32_t token);
device_status_t connectivity_wifi_driver_owner_restore_portal_radio(uint32_t token);
device_status_t connectivity_wifi_driver_owner_scan_visible(
    uint32_t maximum_records, connectivity_wifi_driver_scan_observer_t observer,
    void *context);
device_status_t connectivity_wifi_driver_owner_current_station_ssid(
    char *out_ssid, uint32_t capacity);
device_status_t connectivity_wifi_driver_owner_read_station_mac(uint8_t out_mac[6]);
device_status_t connectivity_wifi_driver_owner_read_softap_mac(uint8_t out_mac[6]);

/* Must be called after the root stopped callback admission and the radio.
 * It unregisters only instances created by initialize; Wi-Fi netifs must be
 * released by their own physical owner before deinitialize. */
device_status_t connectivity_wifi_driver_owner_unregister_application_handlers(void);
device_status_t connectivity_wifi_driver_owner_deinitialize(void);
