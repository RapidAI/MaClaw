#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"

#define CONFIGURATION_WIFI_VALUE_CAPACITY 65u
#define CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY 128u
#define CONFIGURATION_WIFI_MODE_CAPACITY 12u
#define CONFIGURATION_GATEWAY_URL_CAPACITY 256u
#define CONFIGURATION_PAIR_CODE_CAPACITY 7u
#define CONFIGURATION_GATEWAY_TOKEN_CAPACITY 96u

/* Product configuration and credentials have one durable snapshot.  This
 * service intentionally exposes no NVS handles and does not serialize these
 * secrets into diagnostics or device identity. */
typedef struct {
    char wifi_ssid[CONFIGURATION_WIFI_VALUE_CAPACITY];
    char wifi_password[CONFIGURATION_WIFI_VALUE_CAPACITY];
    char wifi_security[CONFIGURATION_WIFI_MODE_CAPACITY];
    char wifi_eap_method[CONFIGURATION_WIFI_MODE_CAPACITY];
    char wifi_identity[CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY];
    char wifi_username[CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY];
    char wifi_ttls_phase2[CONFIGURATION_WIFI_MODE_CAPACITY];
    char wifi_ca_mode[CONFIGURATION_WIFI_MODE_CAPACITY];
    char wifi_server_domain[CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY];
    char gateway_url[CONFIGURATION_GATEWAY_URL_CAPACITY];
    char pair_code[CONFIGURATION_PAIR_CODE_CAPACITY];
    char gateway_token[CONFIGURATION_GATEWAY_TOKEN_CAPACITY];
    uint8_t output_volume;
    bool output_volume_saved;
    /* A profile may have more than one physical uplink.  The selected link is
     * product configuration, while GPIO/modem implementation remains in its
     * board adapter.  The saved bit preserves an older image's board default
     * until the user explicitly changes the selection. */
    bool cellular_transport_selected;
    bool cellular_transport_selection_saved;
} configuration_snapshot_t;

esp_err_t configuration_service_init(void);

/* `inout_snapshot` supplies compile-time defaults.  Missing persistent state
 * leaves those defaults intact; malformed persisted state fails closed. */
esp_err_t configuration_service_load(configuration_snapshot_t *inout_snapshot);

/* Provisioning replaces network fields, pairing code and atomically clears an
 * old token, so a new network cannot accidentally authenticate as its former
 * owner. The service preserves its authoritative transport-selection field;
 * callers publish their runtime copy only after ESP_OK. */
esp_err_t configuration_service_save_provisioning(const configuration_snapshot_t *snapshot);
esp_err_t configuration_service_set_pairing_code(const char *pair_code);
esp_err_t configuration_service_set_gateway_token(const char *token,
                                                   bool clear_pair_code);
esp_err_t configuration_service_set_output_volume(uint8_t percent);
esp_err_t configuration_service_load_transport_selection(bool default_cellular,
                                                         bool *out_cellular,
                                                         bool *out_saved);
esp_err_t configuration_service_set_transport_selection(bool cellular);
esp_err_t configuration_service_request_force_setup(void);
esp_err_t configuration_service_take_force_setup(bool *out_requested);
