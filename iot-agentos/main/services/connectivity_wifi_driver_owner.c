#include "services/connectivity_wifi_driver_owner.h"

#include "esp_err.h"
#include "esp_event.h"
#include "esp_eap_client.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_mac.h"
#include "esp_wifi.h"
#include "mbedtls/platform_util.h"

#include "lwip/inet.h"

#include <string.h>

static const char *TAG = "maclaw_client";

static bool s_wifi_driver_initialized;
static esp_event_handler_instance_t s_wifi_event_instance;
static esp_event_handler_instance_t s_wifi_got_ip_event_instance;
static esp_event_handler_instance_t s_wifi_assigned_ip_event_instance;
static connectivity_wifi_driver_event_callback_t s_event_callback;
static void *s_event_callback_arg;
static bool s_wifi_enterprise_enabled;
static bool s_wifi_started;
static bool s_station_auto_connect;
static bool s_station_expected_disconnect;

typedef struct {
    bool captured;
    bool radio_changed;
    bool wifi_was_started;
    bool station_auto_connect;
    bool station_expected_disconnect;
    wifi_mode_t wifi_mode;
} radio_snapshot_t;

/* Portal rollback state is strictly private to the physical radio owner.
 * Provisioning receives a non-zero generation token, never the state itself.
 * A later capture invalidates the preceding token, so delayed failure cleanup
 * cannot restore a stale Wi-Fi generation over a newer one. */
static radio_snapshot_t s_portal_radio_snapshot;
static uint32_t s_portal_radio_generation;

static uint32_t next_portal_radio_generation(void) {
    ++s_portal_radio_generation;
    if (s_portal_radio_generation == 0u) ++s_portal_radio_generation;
    return s_portal_radio_generation;
}

static bool application_handlers_registered(void) {
    return s_wifi_event_instance && s_wifi_got_ip_event_instance &&
           s_wifi_assigned_ip_event_instance;
}

static connectivity_wifi_driver_security_t security_from_authmode(wifi_auth_mode_t authmode) {
    switch (authmode) {
        case WIFI_AUTH_OPEN: return CONNECTIVITY_WIFI_DRIVER_SECURITY_OPEN;
        case WIFI_AUTH_WEP: return CONNECTIVITY_WIFI_DRIVER_SECURITY_WEP;
        case WIFI_AUTH_WPA_PSK: return CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA;
        case WIFI_AUTH_WPA2_PSK: return CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA2;
        case WIFI_AUTH_WPA_WPA2_PSK: return CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA_WPA2;
        case WIFI_AUTH_WPA3_PSK: return CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA3;
        case WIFI_AUTH_WPA2_WPA3_PSK:
            return CONNECTIVITY_WIFI_DRIVER_SECURITY_WPA2_WPA3;
        case WIFI_AUTH_WPA_ENTERPRISE:
        case WIFI_AUTH_WPA2_ENTERPRISE:
        case WIFI_AUTH_WPA3_ENTERPRISE:
        case WIFI_AUTH_WPA2_WPA3_ENTERPRISE:
        case WIFI_AUTH_WPA3_ENT_192:
            return CONNECTIVITY_WIFI_DRIVER_SECURITY_ENTERPRISE;
        default: return CONNECTIVITY_WIFI_DRIVER_SECURITY_SECURED;
    }
}

/* Keep ESP-IDF callback types and event payloads at the physical boundary.
 * The composition root receives only copied business facts. */
static void wifi_event_adapter(void *arg, esp_event_base_t event_base,
                               int32_t event_id, void *event_data) {
    (void)arg;
    if (!s_event_callback) return;
    connectivity_wifi_driver_event_t event = {0};
    if (event_base == WIFI_EVENT && event_id == WIFI_EVENT_AP_STACONNECTED) {
        const wifi_event_ap_staconnected_t *source = event_data;
        event.kind = CONNECTIVITY_WIFI_DRIVER_EVENT_AP_CLIENT_CONNECTED;
        if (source) memcpy(event.mac, source->mac, sizeof(event.mac));
    } else if (event_base == IP_EVENT && event_id == IP_EVENT_ASSIGNED_IP_TO_CLIENT) {
        const ip_event_assigned_ip_to_client_t *source = event_data;
        event.kind = CONNECTIVITY_WIFI_DRIVER_EVENT_AP_CLIENT_LEASED;
        if (source) {
            (void)esp_ip4addr_ntoa(&source->ip, event.ipv4, sizeof(event.ipv4));
            strlcpy(event.hostname, source->hostname, sizeof(event.hostname));
        }
    } else if (event_base == WIFI_EVENT && event_id == WIFI_EVENT_STA_START) {
        event.kind = CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_STARTED;
    } else if (event_base == WIFI_EVENT && event_id == WIFI_EVENT_STA_DISCONNECTED) {
        const wifi_event_sta_disconnected_t *source = event_data;
        event.kind = CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_DISCONNECTED;
        if (source) {
            memcpy(event.ssid, source->ssid, sizeof(source->ssid));
            event.ssid[sizeof(source->ssid)] = '\0';
        }
    } else if (event_base == IP_EVENT && event_id == IP_EVENT_STA_GOT_IP) {
        event.kind = CONNECTIVITY_WIFI_DRIVER_EVENT_STATION_GOT_IP;
    } else {
        return;
    }
    s_event_callback(s_event_callback_arg, &event);
}

static device_status_t status_from_esp_err(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

device_status_t connectivity_wifi_driver_owner_initialize(
    connectivity_wifi_driver_event_callback_t callback, void *callback_arg) {
    if (!callback) return DEVICE_STATUS_INVALID_ARGUMENT;
    /* A driver without all three application instances is a retained partial
     * generation. Reusing it could route events to stale callback state. */
    if (s_wifi_driver_initialized) {
        return application_handlers_registered() ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
    }
    s_event_callback = callback;
    s_event_callback_arg = callback_arg;
    wifi_init_config_t init = WIFI_INIT_CONFIG_DEFAULT();
    /* ESP-IDF 6.0.2 S3 stability workaround: command traffic does not need
     * AMPDU, while the affected RX timer path can reset the device. */
    init.ampdu_rx_enable = 0;
    init.ampdu_tx_enable = 0;
    esp_err_t err = esp_wifi_init(&init);
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "Wi-Fi driver initialization failed: %s", esp_err_to_name(err));
        return status_from_esp_err(err);
    }
    s_wifi_driver_initialized = true;
    err = esp_event_handler_instance_register(
        WIFI_EVENT, ESP_EVENT_ANY_ID, wifi_event_adapter, NULL,
        &s_wifi_event_instance);
    if (err != ESP_OK) goto fail;
    err = esp_event_handler_instance_register(
        IP_EVENT, IP_EVENT_STA_GOT_IP, wifi_event_adapter, NULL,
        &s_wifi_got_ip_event_instance);
    if (err != ESP_OK) goto fail;
    err = esp_event_handler_instance_register(
        IP_EVENT, IP_EVENT_ASSIGNED_IP_TO_CLIENT, wifi_event_adapter, NULL,
        &s_wifi_assigned_ip_event_instance);
    if (err != ESP_OK) goto fail;
    return DEVICE_STATUS_OK;

fail:
    ESP_LOGW(TAG, "Wi-Fi event registration failed: %s", esp_err_to_name(err));
    /* Best-effort rollback retains any handle/driver state if ESP-IDF refuses
     * cleanup.  The caller must run the common physical root teardown, which
     * will fail closed rather than start a second driver generation. */
    (void)connectivity_wifi_driver_owner_unregister_application_handlers();
    return status_from_esp_err(err);
}

bool connectivity_wifi_driver_owner_initialized(void) {
    return s_wifi_driver_initialized;
}

bool connectivity_wifi_driver_owner_ready(void) {
    return s_wifi_driver_initialized && application_handlers_registered();
}

bool connectivity_wifi_driver_owner_enterprise_enabled(void) {
    return s_wifi_enterprise_enabled;
}

device_status_t connectivity_wifi_driver_owner_configure_enterprise(
    const connectivity_wifi_driver_enterprise_config_t *config) {
    if (!config || !config->identity || !config->username || !config->password) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    const esp_eap_method_t method = config->use_ttls ? ESP_EAP_TYPE_TTLS
                                                     : ESP_EAP_TYPE_PEAP;
    esp_err_t err = esp_eap_client_set_identity(
        (const unsigned char *)config->identity, strlen(config->identity));
    if (err != ESP_OK) return status_from_esp_err(err);
    err = esp_eap_client_set_username((const unsigned char *)config->username,
                                      strlen(config->username));
    if (err != ESP_OK) return status_from_esp_err(err);
    err = esp_eap_client_set_password((const unsigned char *)config->password,
                                      strlen(config->password));
    if (err != ESP_OK) return status_from_esp_err(err);
    if (config->use_ttls) {
        err = esp_eap_client_set_ttls_phase2_method(
            config->ttls_phase2_pap ? ESP_EAP_TTLS_PHASE2_PAP
                                    : ESP_EAP_TTLS_PHASE2_MSCHAPV2);
        if (err != ESP_OK) return status_from_esp_err(err);
    }
    if (config->use_system_ca) {
        err = esp_eap_client_use_default_cert_bundle(true);
        if (err != ESP_OK) return status_from_esp_err(err);
    }
    if (config->server_domain && config->server_domain[0]) {
        err = esp_eap_client_set_domain_name(config->server_domain);
        if (err != ESP_OK) return status_from_esp_err(err);
    }
    err = esp_eap_client_set_eap_methods(method);
    if (err != ESP_OK) return status_from_esp_err(err);
    err = esp_wifi_sta_enterprise_enable();
    if (err != ESP_OK) return status_from_esp_err(err);
    s_wifi_enterprise_enabled = true;
    return DEVICE_STATUS_OK;
}

device_status_t connectivity_wifi_driver_owner_disable_enterprise(void) {
    /* ESP-IDF 6.0.2 can assert if this runs on a cold personal-Wi-Fi boot.
     * Only disable a mode this owner has actually enabled. */
    if (!s_wifi_enterprise_enabled) return DEVICE_STATUS_OK;
    const esp_err_t err = esp_wifi_sta_enterprise_disable();
    if (err == ESP_OK) s_wifi_enterprise_enabled = false;
    return status_from_esp_err(err);
}

device_status_t connectivity_wifi_driver_owner_configure_station(
    const connectivity_wifi_driver_station_config_t *config) {
    if (!config || !config->ssid || !config->ssid[0] ||
        (!config->enterprise && !config->password)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    wifi_config_t station = {
        .sta = {
            .threshold.authmode = config->enterprise ? WIFI_AUTH_WPA2_ENTERPRISE
                                                      : WIFI_AUTH_WPA2_PSK,
        },
    };
    strlcpy((char *)station.sta.ssid, config->ssid, sizeof(station.sta.ssid));
    if (!config->enterprise) {
        strlcpy((char *)station.sta.password, config->password,
                sizeof(station.sta.password));
    }
    esp_err_t err = esp_wifi_set_mode(config->keep_setup_ap ? WIFI_MODE_APSTA
                                                             : WIFI_MODE_STA);
    if (err != ESP_OK) return status_from_esp_err(err);
    /* ESP-IDF 6.0.2 S3 stability workaround: 802.11n management traffic can
     * double-fault after DHCP, so use legacy b/g station negotiation. */
    err = esp_wifi_set_protocol(WIFI_IF_STA, WIFI_PROTOCOL_11B | WIFI_PROTOCOL_11G);
    if (err != ESP_OK) return status_from_esp_err(err);
    err = esp_wifi_set_config(WIFI_IF_STA, &station);
    if (err != ESP_OK) return status_from_esp_err(err);
    /* ESP-IDF 6.0.2 modem sleep can tear down the PHY timer while its ISR is
     * still armed on this S3 build.  This is intentionally best effort. */
    const device_status_t power_save_status =
        connectivity_wifi_driver_owner_disable_power_save();
    if (power_save_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "cannot disable Wi-Fi power save: device status=%d",
                 (int)power_save_status);
    }
    return DEVICE_STATUS_OK;
}

bool connectivity_wifi_driver_owner_started(void) {
    return s_wifi_started;
}

void connectivity_wifi_driver_owner_set_station_policy(bool auto_connect,
                                                        bool expected_disconnect) {
    s_station_auto_connect = auto_connect;
    s_station_expected_disconnect = expected_disconnect;
}

bool connectivity_wifi_driver_owner_take_expected_disconnect(void) {
    if (!s_station_expected_disconnect) return false;
    s_station_expected_disconnect = false;
    return true;
}

bool connectivity_wifi_driver_owner_should_auto_connect(void) {
    return s_station_auto_connect;
}

device_status_t connectivity_wifi_driver_owner_start(void) {
    if (s_wifi_started) return DEVICE_STATUS_OK;
    const esp_err_t err = esp_wifi_start();
    if (err == ESP_OK) s_wifi_started = true;
    return status_from_esp_err(err);
}

device_status_t connectivity_wifi_driver_owner_stop(void) {
    if (!s_wifi_started) return DEVICE_STATUS_OK;
    const esp_err_t err = esp_wifi_stop();
    if (err == ESP_OK || err == ESP_ERR_WIFI_NOT_STARTED) s_wifi_started = false;
    return status_from_esp_err(err == ESP_ERR_WIFI_NOT_STARTED ? ESP_OK : err);
}

device_status_t connectivity_wifi_driver_owner_connect(void) {
    const esp_err_t err = esp_wifi_connect();
    return status_from_esp_err(err == ESP_ERR_WIFI_CONN ? ESP_ERR_INVALID_STATE : err);
}

device_status_t connectivity_wifi_driver_owner_disconnect(void) {
    const esp_err_t err = esp_wifi_disconnect();
    return status_from_esp_err(err == ESP_ERR_WIFI_NOT_CONNECT ? ESP_ERR_NOT_FOUND : err);
}

device_status_t connectivity_wifi_driver_owner_disable_power_save(void) {
    return status_from_esp_err(esp_wifi_set_ps(WIFI_PS_NONE));
}

device_status_t connectivity_wifi_driver_owner_set_mode(bool ap_only) {
    return status_from_esp_err(
        esp_wifi_set_mode(ap_only ? WIFI_MODE_AP : WIFI_MODE_APSTA));
}

device_status_t connectivity_wifi_driver_owner_configure_protected_ap(
    const char *ssid, const char *passphrase) {
    if (!ssid || !ssid[0] || !passphrase || strlen(passphrase) < 8 ||
        strlen(passphrase) >= sizeof(((wifi_config_t *)0)->ap.password)) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    wifi_config_t ap = {
        .ap = {.channel = 1, .max_connection = 4, .authmode = WIFI_AUTH_WPA2_PSK},
    };
    strlcpy((char *)ap.ap.ssid, ssid, sizeof(ap.ap.ssid));
    strlcpy((char *)ap.ap.password, passphrase, sizeof(ap.ap.password));
    ap.ap.ssid_len = strlen((const char *)ap.ap.ssid);
    const device_status_t status =
        status_from_esp_err(esp_wifi_set_config(WIFI_IF_AP, &ap));
    /* The radio owns its own copied AP config after esp_wifi_set_config().
     * Do not leave a second passphrase copy in this owner stack frame. */
    mbedtls_platform_zeroize(&ap, sizeof(ap));
    return status;
}

device_status_t connectivity_wifi_driver_owner_confirm_ap_mode(void) {
    wifi_mode_t active_mode = WIFI_MODE_NULL;
    const esp_err_t err = esp_wifi_get_mode(&active_mode);
    if (err != ESP_OK || (active_mode != WIFI_MODE_AP && active_mode != WIFI_MODE_APSTA)) {
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    return DEVICE_STATUS_OK;
}

device_status_t connectivity_wifi_driver_owner_capture_portal_radio(uint32_t *out_token) {
    if (!out_token) return DEVICE_STATUS_INVALID_ARGUMENT;
    radio_snapshot_t snapshot = {
        .wifi_was_started = s_wifi_started,
        .station_auto_connect = s_station_auto_connect,
        .station_expected_disconnect = s_station_expected_disconnect,
    };
    const esp_err_t err = esp_wifi_get_mode(&snapshot.wifi_mode);
    if (err != ESP_OK) return status_from_esp_err(err);
    snapshot.captured = true;
    s_portal_radio_snapshot = snapshot;
    *out_token = next_portal_radio_generation();
    return DEVICE_STATUS_OK;
}

void connectivity_wifi_driver_owner_note_portal_radio_changed(uint32_t token) {
    if (token != 0u && token == s_portal_radio_generation &&
        s_portal_radio_snapshot.captured) {
        s_portal_radio_snapshot.radio_changed = true;
    }
}

device_status_t connectivity_wifi_driver_owner_restore_portal_radio(uint32_t token) {
    if (token == 0u) return DEVICE_STATUS_OK;
    if (token != s_portal_radio_generation || !s_portal_radio_snapshot.captured) {
        return DEVICE_STATUS_BUSY;
    }
    const radio_snapshot_t snapshot = s_portal_radio_snapshot;
    connectivity_wifi_driver_owner_set_station_policy(false, true);
    if (snapshot.radio_changed && s_wifi_started && !snapshot.wifi_was_started) {
        device_status_t stop_status = connectivity_wifi_driver_owner_stop();
        if (stop_status != DEVICE_STATUS_OK) return stop_status;
    }
    if (snapshot.radio_changed) {
        const esp_err_t err = esp_wifi_set_mode(snapshot.wifi_mode);
        if (err != ESP_OK) return status_from_esp_err(err);
    }
    connectivity_wifi_driver_owner_set_station_policy(snapshot.station_auto_connect,
                                                        snapshot.station_expected_disconnect);
    if (snapshot.wifi_was_started && snapshot.station_auto_connect &&
        (snapshot.wifi_mode == WIFI_MODE_STA || snapshot.wifi_mode == WIFI_MODE_APSTA)) {
        const device_status_t connect_status = connectivity_wifi_driver_owner_connect();
        if (connect_status != DEVICE_STATUS_OK && connect_status != DEVICE_STATUS_BUSY) {
            return connect_status;
        }
    }
    /* A successful rollback consumes the transaction token. */
    memset(&s_portal_radio_snapshot, 0, sizeof(s_portal_radio_snapshot));
    return DEVICE_STATUS_OK;
}

device_status_t connectivity_wifi_driver_owner_scan_visible(
    uint32_t maximum_records, connectivity_wifi_driver_scan_observer_t observer,
    void *context) {
    if (maximum_records == 0 || !observer || !s_wifi_started) {
        return DEVICE_STATUS_INVALID_ARGUMENT;
    }
    wifi_ap_record_t *records = heap_caps_calloc(
        maximum_records, sizeof(*records), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!records) return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    const wifi_scan_config_t scan_config = {.show_hidden = false};
    esp_err_t err = esp_wifi_scan_start(&scan_config, true);
    uint16_t count = maximum_records > UINT16_MAX ? UINT16_MAX : (uint16_t)maximum_records;
    if (err == ESP_OK) err = esp_wifi_scan_get_ap_records(&count, records);
    if (err == ESP_OK) {
        for (uint16_t index = 0; index < count; ++index) {
            char ssid[sizeof(records[index].ssid) + 1];
            memcpy(ssid, records[index].ssid, sizeof(records[index].ssid));
            ssid[sizeof(records[index].ssid)] = '\0';
            if (!observer(ssid, records[index].rssi,
                          security_from_authmode(records[index].authmode), context)) {
                break;
            }
        }
    }
    heap_caps_free(records);
    return status_from_esp_err(err);
}

device_status_t connectivity_wifi_driver_owner_current_station_ssid(
    char *out_ssid, uint32_t capacity) {
    if (!out_ssid || capacity == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    wifi_ap_record_t record = {0};
    const esp_err_t err = esp_wifi_sta_get_ap_info(&record);
    if (err != ESP_OK) return status_from_esp_err(err);
    const size_t copy_length = sizeof(record.ssid) < capacity - 1
                                   ? sizeof(record.ssid)
                                   : capacity - 1;
    memcpy(out_ssid, record.ssid, copy_length);
    out_ssid[copy_length] = '\0';
    return DEVICE_STATUS_OK;
}

device_status_t connectivity_wifi_driver_owner_read_station_mac(uint8_t out_mac[6]) {
    if (!out_mac) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(esp_read_mac(out_mac, ESP_MAC_WIFI_STA));
}

device_status_t connectivity_wifi_driver_owner_read_softap_mac(uint8_t out_mac[6]) {
    if (!out_mac) return DEVICE_STATUS_INVALID_ARGUMENT;
    return status_from_esp_err(esp_read_mac(out_mac, ESP_MAC_WIFI_SOFTAP));
}

device_status_t connectivity_wifi_driver_owner_unregister_application_handlers(void) {
    esp_err_t first_error = ESP_OK;
    /* Unwind newest registrations first. Continue after an ESP-IDF error so
     * retries retain only the exact handle that failed to unregister. */
    if (s_wifi_assigned_ip_event_instance) {
        const esp_err_t err = esp_event_handler_instance_unregister(
            IP_EVENT, IP_EVENT_ASSIGNED_IP_TO_CLIENT, s_wifi_assigned_ip_event_instance);
        if (err == ESP_OK) s_wifi_assigned_ip_event_instance = NULL;
        else first_error = err;
    }
    if (s_wifi_got_ip_event_instance) {
        const esp_err_t err = esp_event_handler_instance_unregister(
            IP_EVENT, IP_EVENT_STA_GOT_IP, s_wifi_got_ip_event_instance);
        if (err == ESP_OK) s_wifi_got_ip_event_instance = NULL;
        else if (first_error == ESP_OK) first_error = err;
    }
    if (s_wifi_event_instance) {
        const esp_err_t err = esp_event_handler_instance_unregister(
            WIFI_EVENT, ESP_EVENT_ANY_ID, s_wifi_event_instance);
        if (err == ESP_OK) s_wifi_event_instance = NULL;
        else if (first_error == ESP_OK) first_error = err;
    }
    if (first_error == ESP_OK) {
        s_event_callback = NULL;
        s_event_callback_arg = NULL;
    }
    return status_from_esp_err(first_error);
}

device_status_t connectivity_wifi_driver_owner_deinitialize(void) {
    if (!s_wifi_driver_initialized) return DEVICE_STATUS_OK;
    if (s_wifi_event_instance || s_wifi_got_ip_event_instance ||
        s_wifi_assigned_ip_event_instance) {
        ESP_LOGW(TAG, "Wi-Fi driver deinit rejected while event handlers remain registered");
        return DEVICE_STATUS_BUSY;
    }
    const esp_err_t err = esp_wifi_deinit();
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "cannot deinitialize Wi-Fi driver: %s", esp_err_to_name(err));
        return status_from_esp_err(err);
    }
    s_wifi_driver_initialized = false;
    s_wifi_enterprise_enabled = false;
    s_wifi_started = false;
    s_station_auto_connect = false;
    s_station_expected_disconnect = false;
    /* A deinitialized driver cannot safely honor a rollback token captured by
     * its former physical generation. Invalidate the retained snapshot before
     * a later initialize can create a new radio generation. */
    memset(&s_portal_radio_snapshot, 0, sizeof(s_portal_radio_snapshot));
    (void)next_portal_radio_generation();
    s_event_callback = NULL;
    s_event_callback_arg = NULL;
    return DEVICE_STATUS_OK;
}
