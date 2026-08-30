#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "configuration_policy.h"
#include "device_api.h"

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
    /* Append-only V7 display policy. Panel/controller implementation remains
     * below Display HAL; zero brightness is a live backlight-off request, not
     * a sleep-depth selection. A missing saved bit preserves the profile's
     * compile-time default after an upgrade or factory boot. */
    uint8_t display_brightness;
    bool display_brightness_saved;
    /* Valid values are the product's bounded idle choices, in seconds. Zero
     * means no automatic display-off timeout. */
    uint32_t screen_sleep_seconds;
    bool screen_sleep_seconds_saved;
} configuration_snapshot_t;

/* A copied, revision-bound confirmed configuration. The Service never returns
 * an internal mutable pointer: consumers bind one revision at operation start
 * and retain this value copy for the operation lifetime.
 *
 * `revision == 0` is reserved for compile-time defaults before the first
 * durable configuration commit and is never returned as OK by the revisioned
 * load API. Candidate provisioning state remains outside this public snapshot:
 * it is unconfirmed credential evidence accessed only through the dedicated
 * boot-candidate transaction APIs. */
#define CONFIGURATION_REVISIONED_SNAPSHOT_ABI_VERSION 1u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    uint64_t revision;
    configuration_snapshot_t snapshot;
} configuration_revisioned_snapshot_t;

/* A copied effective configuration binds its durable source revision and the
 * process-local runtime-override revision.  `runtime_override_revision` is
 * never durable and must not be compared across a restart.  Consumers that
 * reconcile an operation use the pair, rather than combining independent
 * scalar reads. */
#define CONFIGURATION_EFFECTIVE_REVISIONED_SNAPSHOT_ABI_VERSION 1u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    uint64_t durable_revision;
    uint64_t runtime_override_revision;
    uint32_t runtime_override_mask;
    configuration_snapshot_t snapshot;
} configuration_effective_revisioned_snapshot_t;

/* A bounded patch to the display-facing product policy.  It contains only
 * user-visible values; panel shape, controller, GPIO and any wake electrical
 * details remain below the Display/Power HAL.  Supplying both fields produces
 * one durable Configuration revision, so a Hub policy cannot leave brightness
 * and idle timeout half-published. */
#define CONFIGURATION_DISPLAY_POLICY_UPDATE_ABI_VERSION 1u
typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    bool has_brightness;
    uint8_t brightness;
    bool has_screen_sleep_seconds;
    uint32_t screen_sleep_seconds;
} configuration_display_policy_update_t;

/* Portal input is a product/value request, not a caller-built durable
 * snapshot.  Configuration Service alone derives the staged snapshot from
 * the confirmed baseline, preserving fields that a setup form does not own
 * (for example output volume, selected uplink and the Wi-Fi catalogue). */
typedef struct {
    char ssid[CONFIGURATION_WIFI_VALUE_CAPACITY];
    char password[CONFIGURATION_WIFI_VALUE_CAPACITY];
    char gateway[CONFIGURATION_GATEWAY_URL_CAPACITY];
    char code[CONFIGURATION_PAIR_CODE_CAPACITY];
    char security[CONFIGURATION_WIFI_MODE_CAPACITY];
    char eap_method[CONFIGURATION_WIFI_MODE_CAPACITY];
    char identity[CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY];
    char username[CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY];
    char ttls_phase2[CONFIGURATION_WIFI_MODE_CAPACITY];
    char ca_mode[CONFIGURATION_WIFI_MODE_CAPACITY];
    char server_domain[CONFIGURATION_WIFI_ENTERPRISE_VALUE_CAPACITY];
} configuration_provisioning_request_t;

device_status_t configuration_service_init(void);
/* Releases Configuration Service PSRAM scratch and closes new mutations after
 * callers/workers have stopped. Its small admission mutex remains allocated
 * so a caller already contending for it cannot observe a deleted FreeRTOS
 * object; it is reused by a later init. This service neither deinitializes
 * NVS nor deletes Persistence's transaction mutex. */
device_status_t configuration_service_deinit(uint32_t timeout_ms);
bool configuration_service_is_initialized(void);

/* Internal System Sleep participant. PREPARE closes new configuration
 * mutations and takes the same serialized snapshot lock used by all public
 * operations, proving no direct configuration write can cross a later shared
 * Persistence checkpoint. ABORT reopens the retained service generation;
 * neither API exposes NVS/RTOS details or enters MCU sleep. */
device_status_t configuration_service_prepare_system_sleep(uint32_t timeout_ms);
void configuration_service_abort_system_sleep_prepare(void);

/* `inout_snapshot` supplies compile-time defaults.  Missing persistent state
 * leaves those defaults intact; malformed persisted V1/V2/V3/V4/V5 state
 * fails closed. */
device_status_t configuration_service_load(configuration_snapshot_t *inout_snapshot);

/* Returns one immutable-by-copy confirmed configuration revision. This is the
 * read contract for services that need stable settings through a multi-step
 * operation; callers must not combine fields from separate ordinary loads.
 * Missing durable state returns NOT_FOUND so startup explicitly decides how
 * compile-time defaults participate in its own policy. */
device_status_t configuration_service_load_revisioned_snapshot(
    configuration_revisioned_snapshot_t *out_snapshot);
/* Re-commits the current confirmed snapshot as a bounded durable checkpoint,
 * preserving its contents while advancing the authoritative revision. */
device_status_t configuration_service_checkpoint_current_snapshot(
    uint64_t *out_revision);

/* Runtime overrides are volatile, authenticated, bounded policy records.
 * Configuration Service is their only in-process owner; no board adapter or
 * product service may keep a competing timer/store. They never mutate the
 * durable snapshot. The source owner provides already-authenticated
 * provenance; this service samples the platform monotonic clock only to bind
 * the supplied expiry to the current process epoch. */
/* Provisioning stages a candidate network/pairing snapshot while retaining
 * the last confirmed snapshot. A successful Gateway token commit promotes
 * the candidate atomically; failed Wi-Fi/pairing paths can discard it without
 * losing the previously confirmed device owner or network. The service
 * validates the request and derives the candidate from the authoritative
 * confirmed snapshot; callers publish their runtime copy only after ESP_OK. */
device_status_t configuration_service_stage_provisioning(
    const configuration_provisioning_request_t *request);

/* Boot consumes a staged candidate exactly as the active configuration, but
 * reports that it remains unconfirmed.  Normal configuration reads keep
 * returning only the confirmed snapshot. */
device_status_t configuration_service_load_boot_candidate(configuration_snapshot_t *inout_snapshot,
                                                           bool *out_staged);
/* Durably claims the bounded candidate-boot budget.  If the budget is
 * exhausted, the service rolls back to confirmed configuration and reports
 * NOT_FOUND so the composition root can reboot before opening radio/Hub work. */
device_status_t configuration_service_begin_staged_provisioning_boot(void);
/* Discards only an unconfirmed provisioning candidate.  The confirmed
 * snapshot, token and transport selection remain intact. */
device_status_t configuration_service_rollback_staged_provisioning(void);

/* Shared, value-only syntax check for portal/BLE/USB feedback before a
 * pairing-code mutation. Durable writes still go through set_pairing_code(). */
bool configuration_service_valid_pairing_code(const char *pair_code);
device_status_t configuration_service_set_pairing_code(const char *pair_code);
/* Commits Hub pairing evidence. This is the only Gateway token transition:
 * it atomically promotes an active staged configuration, or retains an
 * already-confirmed network for reuse-network/voice pairing, then stores the
 * token and clears the one-time code. */
device_status_t configuration_service_commit_gateway_pairing_token(const char *token);
device_status_t configuration_service_set_output_volume(uint8_t percent);
/* The explicit-source form is for already authenticated integration owners
 * (currently the Hub downlink bridge). Configuration validates source, ABI,
 * authentication and TTL before accepting a durable mutation. Most local
 * callers use set_output_volume(), which constructs USER_LOCAL authority. */
device_status_t configuration_service_set_output_volume_with_policy(
    uint8_t percent, const configuration_policy_request_t *policy);
/* Same durable volume mutation with the committed immutable revision returned
 * to the composition root. Audio is applied only after this returns OK; this
 * API itself never calls an Audio/codec implementation. */
device_status_t configuration_service_set_output_volume_with_policy_revision(
    uint8_t percent, const configuration_policy_request_t *policy,
    uint64_t *out_revision);
device_status_t configuration_service_set_display_brightness(uint8_t percent);
device_status_t configuration_service_set_display_brightness_with_policy(
    uint8_t percent, const configuration_policy_request_t *policy);
bool configuration_service_valid_screen_sleep_seconds(uint32_t seconds);
device_status_t configuration_service_set_screen_sleep_seconds(uint32_t seconds);
device_status_t configuration_service_set_screen_sleep_seconds_with_policy(
    uint32_t seconds, const configuration_policy_request_t *policy);
/* Publishes one complete display policy patch and returns the exact durable
 * revision created by that write.  The caller applies Display/Power effects
 * only after this API returns OK, outside Configuration's mutex/NVS callback.
 * A zero revision is never returned on success. */
device_status_t configuration_service_apply_display_policy_with_policy(
    const configuration_display_policy_update_t *update,
    const configuration_policy_request_t *policy,
    uint64_t *out_revision);
device_status_t configuration_service_load_transport_selection(bool default_cellular,
                                                         bool *out_cellular,
                                                         bool *out_saved);
device_status_t configuration_service_set_transport_selection(bool cellular);
device_status_t configuration_service_request_force_setup(void);
device_status_t configuration_service_take_force_setup(bool *out_requested);

/* 多热点列表操作。upsert：同 ssid 更新密码，否则追加；列表已满时顶掉最旧
 * 一条（索引 0），保证门户保存总能成功。delete：按 ssid 删除并写回 NVS。 */
device_status_t configuration_service_list_wifi_networks(configuration_wifi_network_t *out_networks,
                                                   uint8_t capacity, uint8_t *out_count);
device_status_t configuration_service_upsert_wifi_network(const char *ssid, const char *password);
device_status_t configuration_service_delete_wifi_network(const char *ssid);
