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
#define CONFIGURATION_WIFI_NETWORK_CAPACITY 5u

/* 多热点列表条目：只保存个人（WPA-PSK 类）热点的 ssid+密码。
 * 企业热点仍只存在于下方主凭据字段，不进列表。 */
typedef struct {
    char ssid[CONFIGURATION_WIFI_VALUE_CAPACITY];
    char password[CONFIGURATION_WIFI_VALUE_CAPACITY];
} configuration_wifi_network_t;

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
    /* v3 新增：已存个人热点列表（最多 CONFIGURATION_WIFI_NETWORK_CAPACITY 条）。
     * 启动连网时在列表中挑当前可见且 RSSI 最强的热点；门户可删除条目。 */
    configuration_wifi_network_t wifi_networks[CONFIGURATION_WIFI_NETWORK_CAPACITY];
    uint8_t wifi_network_count;
} configuration_snapshot_t;

esp_err_t configuration_service_init(void);
/* Releases Configuration Service PSRAM scratch and closes new mutations after
 * callers/workers have stopped. Its small admission mutex remains allocated
 * so a caller already contending for it cannot observe a deleted FreeRTOS
 * object; it is reused by a later init. This service neither deinitializes
 * NVS nor deletes Persistence's transaction mutex. */
esp_err_t configuration_service_deinit(uint32_t timeout_ms);
bool configuration_service_is_initialized(void);

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

/* 多热点列表操作。upsert：同 ssid 更新密码，否则追加；列表已满时顶掉最旧
 * 一条（索引 0），保证门户保存总能成功。delete：按 ssid 删除并写回 NVS。 */
esp_err_t configuration_service_list_wifi_networks(configuration_wifi_network_t *out_networks,
                                                   uint8_t capacity, uint8_t *out_count);
esp_err_t configuration_service_upsert_wifi_network(const char *ssid, const char *password);
esp_err_t configuration_service_delete_wifi_network(const char *ssid);
