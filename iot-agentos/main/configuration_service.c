#include "configuration_service.h"
#include "configuration_runtime_override_service.h"
#include "configuration_runtime_override_store.h"
#include "configuration_revision.h"
#include "configuration_transaction.h"
#include "provisioning_failure_injection.h"

#include <stddef.h>
#include <string.h>

#include "esp_heap_caps.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "persistence_service.h"

#define CONFIGURATION_NAMESPACE "maclaw"
#define CONFIGURATION_STORE_KEY "configuration"
#define CONFIGURATION_STORE_MAGIC 0x43464731u /* CFG1 */
#define CONFIGURATION_STORE_VERSION 7u

typedef struct {
    uint32_t magic;
    uint32_t version;
    /* The confirmed snapshot is the only configuration trusted for ordinary
     * boots.  A provisioning candidate is persisted beside it so a reset or
     * power loss between portal save and Hub confirmation cannot destroy the
     * last known-good network/owner. */
    configuration_provisioning_transaction_t provisioning;
    uint8_t force_setup;
    uint8_t reserved0;
    uint8_t reserved[2];
    /* A monotonically increasing durable publication identity. It is kept
     * beside the provisioning value model so candidate derivation remains a
     * platform-neutral pure transition. */
    uint64_t revision;
} configuration_store_t;

/* V6 was the last credential snapshot before display policy joined the one
 * Configuration record. Keep this exact old layout; fields must never be
 * reinterpreted merely because the current snapshot grew. */
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
    bool cellular_transport_selected;
    bool cellular_transport_selection_saved;
    configuration_wifi_network_t wifi_networks[CONFIGURATION_WIFI_NETWORK_CAPACITY];
    uint8_t wifi_network_count;
} configuration_snapshot_v6_t;

typedef struct {
    configuration_snapshot_v6_t confirmed_snapshot;
    configuration_snapshot_v6_t staged_snapshot;
    bool staged;
    uint32_t staged_boot_attempts;
} configuration_provisioning_transaction_v6_t;

typedef struct {
    uint32_t magic;
    uint32_t version;
    configuration_provisioning_transaction_v6_t provisioning;
    uint8_t force_setup;
    uint8_t reserved0;
    uint8_t reserved[2];
    uint64_t revision;
} configuration_store_v6_t;

/* V5 introduced staged-boot attempts but had no immutable configuration
 * revision. Retain its exact record so deployed configuration survives the
 * V5 -> V7 migration without being treated as corrupt. */
typedef struct {
    configuration_snapshot_v6_t confirmed_snapshot;
    configuration_snapshot_v6_t staged_snapshot;
    bool staged;
    uint32_t staged_boot_attempts;
} configuration_provisioning_transaction_v5_t;

typedef struct {
    uint32_t magic;
    uint32_t version;
    configuration_provisioning_transaction_v5_t provisioning;
    uint8_t force_setup;
    uint8_t reserved0;
    uint8_t reserved[2];
} configuration_store_v5_t;

/* V4 first introduced confirmed/staged snapshots.  V5 adds only a durable
 * bounded boot-attempt counter, so retain the exact V4 shape for a lossless
 * migration instead of rejecting every already-staged device after upgrade. */
typedef struct {
    configuration_snapshot_v6_t confirmed_snapshot;
    configuration_snapshot_v6_t staged_snapshot;
    bool staged;
} configuration_provisioning_transaction_v4_t;

typedef struct {
    uint32_t magic;
    uint32_t version;
    configuration_provisioning_transaction_v4_t provisioning;
    uint8_t force_setup;
    uint8_t reserved0;
    uint8_t reserved[2];
} configuration_store_v4_t;

/* V3 had one destructive active snapshot.  Keep the exact on-flash shape so
 * deployed images can migrate to an empty staging slot without guessing which
 * credentials were already confirmed. */
typedef struct {
    uint32_t magic;
    uint32_t version;
    configuration_snapshot_v6_t snapshot;
    uint8_t force_setup;
    uint8_t reserved[3];
} configuration_store_v3_t;

/* V1 predates the normalized transport selection.  Keep this exact shape so
 * deployed V1 snapshots can be expanded without treating valid user
 * credentials as corrupt merely because the schema grew. */
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
} configuration_snapshot_v1_t;

typedef struct {
    uint32_t magic;
    uint32_t version;
    configuration_snapshot_v1_t snapshot;
    uint8_t force_setup;
    uint8_t reserved[3];
} configuration_store_v1_t;

/* V2 predates the multi-network list.  Keep this exact shape so deployed V2
 * snapshots can be expanded: their single credential migrates to list entry 0
 * without losing the network the user already configured. */
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
    bool cellular_transport_selected;
    bool cellular_transport_selection_saved;
} configuration_snapshot_v2_t;

typedef struct {
    uint32_t magic;
    uint32_t version;
    configuration_snapshot_v2_t snapshot;
    uint8_t force_setup;
    uint8_t reserved[3];
} configuration_store_v2_t;

static SemaphoreHandle_t s_mutation_lock;
/* Compile-time defaults become the mutation seed after the composition root
 * has loaded them.  This prevents a first-time pairing/volume write from
 * replacing a configured firmware default with empty network fields. */
static configuration_snapshot_t s_default_snapshot;
static bool s_default_snapshot_available;
/* ~1 KB snapshot/store copies overflowed small task stacks (the provisioning
 * portal's httpd task rebooted mid-save at a 6 KB stack).  Every mutation
 * path is serialized by s_mutation_lock, so these PSRAM scratch buffers are
 * shared safely and keep the working copies off all caller stacks. */
static configuration_snapshot_t *s_scratch_snapshot_a;
static configuration_snapshot_t *s_scratch_snapshot_b;
static configuration_store_t *s_scratch_store;
/* Volatile only: never serialized by write_store_locked().  It is protected
 * by the same admission/mutation lock as the durable snapshot so a caller can
 * receive one coherent durable+override copied result. */
static configuration_runtime_override_store_t *s_runtime_override_store;
static bool s_stopping;
/* System Sleep retains Configuration's scratch and mutex but closes every
 * direct mutation/read admission while Power establishes its durable fence. */
static volatile bool s_system_sleep_preparing;
/* Retained lifecycle shell: it serializes construction with rollback without
 * being deleted while a concurrent caller may already be waiting on it. */
static SemaphoreHandle_t s_deinit_lock;
static volatile bool s_initializing;

/* Configuration deinit has one external wait today, but define its deadline
 * explicitly so a later drain/rollback step cannot accidentally receive a
 * fresh copy of the public timeout. */
static TickType_t configuration_stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t configuration_stop_remaining_ticks(TickType_t started,
                                                     TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

static bool lock(void) {
    /* Checking admission only before the blocking take is insufficient: a
     * caller can observe the old generation, queue on this retained mutex,
     * then acquire it after deinit has closed admission and reclaimed the
     * shared PSRAM scratch. Re-check while owning the same mutex that guards
     * every scratch mutation, and relinquish it without touching service
     * state when this is such a late waiter. */
    if (!s_mutation_lock ||
        __atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE) ||
        s_system_sleep_preparing ||
        xSemaphoreTake(s_mutation_lock, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return false;
    }
    const bool admitted = !__atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE) &&
                          !s_system_sleep_preparing &&
                          s_scratch_snapshot_a && s_scratch_snapshot_b &&
                          s_scratch_store && s_runtime_override_store;
    if (!admitted) xSemaphoreGive(s_mutation_lock);
    return admitted;
}

static void unlock(void) {
    if (s_mutation_lock) xSemaphoreGive(s_mutation_lock);
}

static bool string_terminated(const char *value, size_t capacity) {
    return value && memchr(value, '\0', capacity) != NULL;
}

static bool valid_snapshot(const configuration_snapshot_t *snapshot) {
    if (!snapshot || snapshot->output_volume > 100 ||
        snapshot->display_brightness > 100 ||
        (snapshot->screen_sleep_seconds_saved &&
         snapshot->screen_sleep_seconds != 0u &&
         snapshot->screen_sleep_seconds != 60u &&
         snapshot->screen_sleep_seconds != 180u &&
         snapshot->screen_sleep_seconds != 300u &&
         snapshot->screen_sleep_seconds != 600u &&
         snapshot->screen_sleep_seconds != 1800u &&
         snapshot->screen_sleep_seconds != 3600u &&
         snapshot->screen_sleep_seconds != 7200u &&
         snapshot->screen_sleep_seconds != 10800u &&
         snapshot->screen_sleep_seconds != 14400u &&
         snapshot->screen_sleep_seconds != 18000u) ||
        snapshot->wifi_network_count > CONFIGURATION_WIFI_NETWORK_CAPACITY ||
        !string_terminated(snapshot->wifi_ssid, sizeof(snapshot->wifi_ssid)) ||
        !string_terminated(snapshot->wifi_password, sizeof(snapshot->wifi_password)) ||
        !string_terminated(snapshot->wifi_security, sizeof(snapshot->wifi_security)) ||
        !string_terminated(snapshot->wifi_eap_method, sizeof(snapshot->wifi_eap_method)) ||
        !string_terminated(snapshot->wifi_identity, sizeof(snapshot->wifi_identity)) ||
        !string_terminated(snapshot->wifi_username, sizeof(snapshot->wifi_username)) ||
        !string_terminated(snapshot->wifi_ttls_phase2, sizeof(snapshot->wifi_ttls_phase2)) ||
        !string_terminated(snapshot->wifi_ca_mode, sizeof(snapshot->wifi_ca_mode)) ||
        !string_terminated(snapshot->wifi_server_domain, sizeof(snapshot->wifi_server_domain)) ||
        !string_terminated(snapshot->gateway_url, sizeof(snapshot->gateway_url)) ||
        !string_terminated(snapshot->pair_code, sizeof(snapshot->pair_code)) ||
        !string_terminated(snapshot->gateway_token, sizeof(snapshot->gateway_token))) {
        return false;
    }
    for (uint8_t i = 0; i < snapshot->wifi_network_count; ++i) {
        if (!string_terminated(snapshot->wifi_networks[i].ssid,
                                sizeof(snapshot->wifi_networks[i].ssid)) ||
            !snapshot->wifi_networks[i].ssid[0] ||
            !string_terminated(snapshot->wifi_networks[i].password,
                                sizeof(snapshot->wifi_networks[i].password))) {
            return false;
        }
        for (uint8_t j = 0; j < i; ++j) {
            if (!strcmp(snapshot->wifi_networks[i].ssid,
                        snapshot->wifi_networks[j].ssid)) {
                return false;
            }
        }
    }
    return true;
}

static bool valid_screen_sleep_seconds_value(uint32_t seconds) {
    switch (seconds) {
        case 0u: case 60u: case 180u: case 300u: case 600u: case 1800u:
        case 3600u: case 7200u: case 10800u: case 14400u: case 18000u:
            return true;
        default:
            return false;
    }
}

/* V6 snapshot fields are a strict prefix of the V7 product snapshot. Do not
 * use a blind whole-struct assignment: padding and the new display policy
 * must be initialized deterministically on every legacy migration. */
static void snapshot_from_v6(configuration_snapshot_t *out,
                             const configuration_snapshot_v6_t *legacy) {
    memset(out, 0, sizeof(*out));
    memcpy(out, legacy, sizeof(*legacy));
}

/* 旧版数据只有单组凭据：主凭据是个人热点且列表为空时，把它迁成列表第 1 条，
 * 保证老用户升级后已配网络直接进入多热点选网。 */
static void sync_networks_from_primary(configuration_snapshot_t *snapshot) {
    if (!snapshot || snapshot->wifi_network_count > 0 || !snapshot->wifi_ssid[0] ||
        !strcmp(snapshot->wifi_security, "enterprise")) {
        return;
    }
    strlcpy(snapshot->wifi_networks[0].ssid, snapshot->wifi_ssid,
            sizeof(snapshot->wifi_networks[0].ssid));
    strlcpy(snapshot->wifi_networks[0].password, snapshot->wifi_password,
            sizeof(snapshot->wifi_networks[0].password));
    snapshot->wifi_network_count = 1;
}

/* General saved-network management is a confirmed-configuration mutation.
 * It intentionally remains separate from provisioning candidate derivation:
 * the latter lives in Configuration Transaction so every provisioning surface
 * follows one pure value rule, while this helper services the explicit
 * catalogue-management API below. */
static void upsert_network_in_snapshot(configuration_snapshot_t *snapshot,
                                       const char *ssid, const char *password) {
    if (!snapshot || !ssid || !ssid[0]) return;
    uint8_t slot = snapshot->wifi_network_count;
    for (uint8_t i = 0; i < snapshot->wifi_network_count; ++i) {
        if (!strcmp(snapshot->wifi_networks[i].ssid, ssid)) {
            slot = i;
            break;
        }
    }
    if (slot == snapshot->wifi_network_count) {
        if (slot >= CONFIGURATION_WIFI_NETWORK_CAPACITY) {
            /* The documented catalogue policy is FIFO eviction. */
            memmove(&snapshot->wifi_networks[0], &snapshot->wifi_networks[1],
                    (CONFIGURATION_WIFI_NETWORK_CAPACITY - 1u) *
                        sizeof(snapshot->wifi_networks[0]));
            slot = CONFIGURATION_WIFI_NETWORK_CAPACITY - 1u;
        } else {
            snapshot->wifi_network_count = slot + 1u;
        }
    }
    strlcpy(snapshot->wifi_networks[slot].ssid, ssid,
            sizeof(snapshot->wifi_networks[slot].ssid));
    strlcpy(snapshot->wifi_networks[slot].password, password ? password : "",
            sizeof(snapshot->wifi_networks[slot].password));
}

static bool valid_store(const configuration_store_t *store) {
    return store && store->magic == CONFIGURATION_STORE_MAGIC &&
           store->version == CONFIGURATION_STORE_VERSION && store->revision != 0u &&
           store->force_setup <= 1 &&
           valid_snapshot(&store->provisioning.confirmed_snapshot) &&
           store->provisioning.staged_boot_attempts <=
               CONFIGURATION_TRANSACTION_MAX_STAGED_BOOT_ATTEMPTS &&
           (!store->provisioning.staged || valid_snapshot(&store->provisioning.staged_snapshot));
}

static esp_err_t migrate_v3_locked(configuration_store_t *store);
static esp_err_t import_legacy_display_policy(configuration_snapshot_t *snapshot,
                                              bool *out_found);
static esp_err_t write_store_locked(const configuration_store_t *store);

static esp_err_t migrate_v6_locked(configuration_store_t *store) {
    configuration_store_v6_t *legacy =
        heap_caps_calloc(1, sizeof(*legacy), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!legacy) return ESP_ERR_NO_MEM;
    size_t size = sizeof(*legacy);
    esp_err_t err = device_status_to_platform_error(persistence_service_read_blob(
        CONFIGURATION_NAMESPACE, CONFIGURATION_STORE_KEY, legacy, &size));
    if (err != ESP_OK || legacy->magic != CONFIGURATION_STORE_MAGIC ||
        legacy->version != 6u || legacy->force_setup > 1u || legacy->revision == 0u ||
        legacy->provisioning.staged_boot_attempts >
            CONFIGURATION_TRANSACTION_MAX_STAGED_BOOT_ATTEMPTS) {
        heap_caps_free(legacy);
        return ESP_ERR_INVALID_STATE;
    }
    memset(store, 0, sizeof(*store));
    store->magic = CONFIGURATION_STORE_MAGIC;
    store->version = CONFIGURATION_STORE_VERSION;
    snapshot_from_v6(&store->provisioning.confirmed_snapshot,
                     &legacy->provisioning.confirmed_snapshot);
    snapshot_from_v6(&store->provisioning.staged_snapshot,
                     &legacy->provisioning.staged_snapshot);
    store->provisioning.staged = legacy->provisioning.staged;
    store->provisioning.staged_boot_attempts = legacy->provisioning.staged_boot_attempts;
    store->force_setup = legacy->force_setup;
    store->revision = legacy->revision;
    const bool valid = valid_snapshot(&store->provisioning.confirmed_snapshot) &&
        (!store->provisioning.staged ||
         valid_snapshot(&store->provisioning.staged_snapshot));
    heap_caps_free(legacy);
    if (!valid) return ESP_ERR_INVALID_STATE;
    return device_status_to_platform_error(persistence_service_write_blob(
        CONFIGURATION_NAMESPACE, CONFIGURATION_STORE_KEY, store, sizeof(*store)));
}

static esp_err_t migrate_v5_locked(configuration_store_t *store) {
    configuration_store_v5_t *legacy =
        heap_caps_calloc(1, sizeof(*legacy), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!legacy) return ESP_ERR_NO_MEM;
    size_t size = sizeof(*legacy);
    esp_err_t err = device_status_to_platform_error(persistence_service_read_blob(
        CONFIGURATION_NAMESPACE, CONFIGURATION_STORE_KEY, legacy, &size));
    if (err != ESP_OK || legacy->magic != CONFIGURATION_STORE_MAGIC ||
        legacy->version != 5u || legacy->force_setup > 1u ||
        legacy->provisioning.staged_boot_attempts >
            CONFIGURATION_TRANSACTION_MAX_STAGED_BOOT_ATTEMPTS) {
        heap_caps_free(legacy);
        return ESP_ERR_INVALID_STATE;
    }
    memset(store, 0, sizeof(*store));
    store->magic = CONFIGURATION_STORE_MAGIC;
    store->version = CONFIGURATION_STORE_VERSION;
    snapshot_from_v6(&store->provisioning.confirmed_snapshot,
                     &legacy->provisioning.confirmed_snapshot);
    snapshot_from_v6(&store->provisioning.staged_snapshot,
                     &legacy->provisioning.staged_snapshot);
    store->provisioning.staged = legacy->provisioning.staged;
    store->provisioning.staged_boot_attempts = legacy->provisioning.staged_boot_attempts;
    store->force_setup = legacy->force_setup;
    /* The former format has no comparable revision. Its first V6 image is the
     * authoritative initial publication, never a synthetic wraparound. */
    store->revision = 1u;
    const bool valid = valid_snapshot(&store->provisioning.confirmed_snapshot) &&
        (!store->provisioning.staged ||
         valid_snapshot(&store->provisioning.staged_snapshot));
    heap_caps_free(legacy);
    if (!valid) return ESP_ERR_INVALID_STATE;
    return device_status_to_platform_error(persistence_service_write_blob(
        CONFIGURATION_NAMESPACE, CONFIGURATION_STORE_KEY, store, sizeof(*store)));
}

static esp_err_t migrate_v4_locked(configuration_store_t *store) {
    configuration_store_v4_t *legacy =
        heap_caps_calloc(1, sizeof(*legacy), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!legacy) return ESP_ERR_NO_MEM;
    size_t size = sizeof(*legacy);
    esp_err_t err = device_status_to_platform_error(persistence_service_read_blob(
        CONFIGURATION_NAMESPACE, CONFIGURATION_STORE_KEY, legacy, &size));
    if (err != ESP_OK || legacy->magic != CONFIGURATION_STORE_MAGIC ||
        legacy->version != 4u || legacy->force_setup > 1u) {
        heap_caps_free(legacy);
        return ESP_ERR_INVALID_STATE;
    }
    memset(store, 0, sizeof(*store));
    store->magic = CONFIGURATION_STORE_MAGIC;
    store->version = CONFIGURATION_STORE_VERSION;
    snapshot_from_v6(&store->provisioning.confirmed_snapshot,
                     &legacy->provisioning.confirmed_snapshot);
    snapshot_from_v6(&store->provisioning.staged_snapshot,
                     &legacy->provisioning.staged_snapshot);
    store->provisioning.staged = legacy->provisioning.staged;
    store->force_setup = legacy->force_setup;
    store->revision = 1u;
    const bool valid = valid_snapshot(&store->provisioning.confirmed_snapshot) &&
        (!store->provisioning.staged ||
         valid_snapshot(&store->provisioning.staged_snapshot));
    heap_caps_free(legacy);
    if (!valid) return ESP_ERR_INVALID_STATE;
    return device_status_to_platform_error(persistence_service_write_blob(
        CONFIGURATION_NAMESPACE, CONFIGURATION_STORE_KEY, store, sizeof(*store)));
}

static esp_err_t read_optional_string(const char *key, char *out, size_t capacity,
                                      bool *out_found) {
    size_t size = capacity;
    esp_err_t err = device_status_to_platform_error(persistence_service_read_string(CONFIGURATION_NAMESPACE, key, out, &size));
    if (err == ESP_OK) {
        *out_found = true;
        return ESP_OK;
    }
    return err == ESP_ERR_NOT_FOUND ? ESP_OK : err;
}

/* Import only the historical product configuration keys.  Device identity is
 * deliberately not copied: it is derived from the physical chip MAC. */
static esp_err_t load_legacy(configuration_snapshot_t *snapshot, bool *out_found,
                             bool *out_force_setup) {
    if (!snapshot || !out_found || !out_force_setup) return ESP_ERR_INVALID_ARG;
    *out_found = false;
    *out_force_setup = false;
    struct {
        const char *key;
        char *value;
        size_t capacity;
    } fields[] = {
        {"wifi_ssid", snapshot->wifi_ssid, sizeof(snapshot->wifi_ssid)},
        {"wifi_pass", snapshot->wifi_password, sizeof(snapshot->wifi_password)},
        {"wifi_sec", snapshot->wifi_security, sizeof(snapshot->wifi_security)},
        {"wifi_eap", snapshot->wifi_eap_method, sizeof(snapshot->wifi_eap_method)},
        {"wifi_ident", snapshot->wifi_identity, sizeof(snapshot->wifi_identity)},
        {"wifi_user", snapshot->wifi_username, sizeof(snapshot->wifi_username)},
        {"wifi_ttls", snapshot->wifi_ttls_phase2, sizeof(snapshot->wifi_ttls_phase2)},
        {"wifi_ca", snapshot->wifi_ca_mode, sizeof(snapshot->wifi_ca_mode)},
        {"wifi_domain", snapshot->wifi_server_domain, sizeof(snapshot->wifi_server_domain)},
        {"gateway_url", snapshot->gateway_url, sizeof(snapshot->gateway_url)},
        {"pair_code", snapshot->pair_code, sizeof(snapshot->pair_code)},
        {"gateway_token", snapshot->gateway_token, sizeof(snapshot->gateway_token)},
    };
    for (size_t i = 0; i < sizeof(fields) / sizeof(fields[0]); ++i) {
        esp_err_t err = read_optional_string(fields[i].key, fields[i].value,
                                             fields[i].capacity, out_found);
        if (err != ESP_OK) return err;
    }
    uint8_t volume = 0;
    esp_err_t err = device_status_to_platform_error(persistence_service_read_u8(CONFIGURATION_NAMESPACE, "output_vol", &volume));
    if (err == ESP_OK) {
        if (volume > 100) return ESP_ERR_INVALID_STATE;
        snapshot->output_volume = volume;
        snapshot->output_volume_saved = true;
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) return err;
    uint8_t force_setup = 0;
    err = device_status_to_platform_error(persistence_service_read_u8(CONFIGURATION_NAMESPACE, "force_setup", &force_setup));
    if (err == ESP_OK) {
        if (force_setup > 1) return ESP_ERR_INVALID_STATE;
        *out_force_setup = force_setup != 0;
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) return err;
    uint8_t transport = 0;
    err = device_status_to_platform_error(persistence_service_read_u8(CONFIGURATION_NAMESPACE, "net_transport", &transport));
    if (err == ESP_OK) {
        if (transport > 1) return ESP_ERR_INVALID_STATE;
        snapshot->cellular_transport_selected = transport != 0;
        snapshot->cellular_transport_selection_saved = true;
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) return err;
    bool display_found = false;
    err = import_legacy_display_policy(snapshot, &display_found);
    if (err != ESP_OK) return err;
    *out_found = *out_found || display_found;
    return valid_snapshot(snapshot) ? ESP_OK : ESP_ERR_INVALID_STATE;
}

static esp_err_t import_legacy_display_policy(configuration_snapshot_t *snapshot,
                                              bool *out_found);

/* V1 migration holds its own large by-value frames.  Keeping it out of
 * load_locked() means the steady-state V2 read path never reserves stack for
 * a migration that already happened. */
static esp_err_t migrate_v1_locked(configuration_store_t *store) {
    configuration_store_v1_t *legacy =
        heap_caps_calloc(1, sizeof(*legacy), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!legacy) return ESP_ERR_NO_MEM;
    size_t size = sizeof(*legacy);
    esp_err_t err = device_status_to_platform_error(persistence_service_read_blob(CONFIGURATION_NAMESPACE,
                                                  CONFIGURATION_STORE_KEY,
                                                  legacy, &size));
    if (err != ESP_OK || legacy->magic != CONFIGURATION_STORE_MAGIC ||
        legacy->version != 1u || legacy->force_setup > 1) {
        heap_caps_free(legacy);
        return ESP_ERR_INVALID_STATE;
    }
    store->magic = CONFIGURATION_STORE_MAGIC;
    store->version = CONFIGURATION_STORE_VERSION;
    memcpy(&store->provisioning.confirmed_snapshot, &legacy->snapshot,
           sizeof(legacy->snapshot));
    store->force_setup = legacy->force_setup;
    store->revision = 1u;
    heap_caps_free(legacy);
    if (!valid_snapshot(&store->provisioning.confirmed_snapshot)) return ESP_ERR_INVALID_STATE;
    // 单组凭据迁成列表第 1 条（仅个人热点）。
    sync_networks_from_primary(&store->provisioning.confirmed_snapshot);
    /* V1 images still kept Fangtang's normalized selection in the
     * transitional scalar key.  Fold it into the expanded snapshot during
     * the same schema migration, without exposing that key to callers. */
    uint8_t transport = 0;
    esp_err_t transport_err = device_status_to_platform_error(persistence_service_read_u8(
        CONFIGURATION_NAMESPACE, "net_transport", &transport));
    if (transport_err == ESP_OK) {
        if (transport > 1) return ESP_ERR_INVALID_STATE;
        store->provisioning.confirmed_snapshot.cellular_transport_selected = transport != 0;
        store->provisioning.confirmed_snapshot.cellular_transport_selection_saved = true;
    } else if (transport_err != ESP_ERR_NOT_FOUND) {
        return transport_err;
    }
    return device_status_to_platform_error(persistence_service_write_blob(CONFIGURATION_NAMESPACE,
                                          CONFIGURATION_STORE_KEY,
                                          store, sizeof(*store)));
}

/* V2 -> V3：快照尾部追加多热点列表。store 已被调用方清零，前缀整体拷贝后
 * 只需把主凭据同步进列表。 */
static esp_err_t migrate_v2_locked(configuration_store_t *store) {
    configuration_store_v2_t *legacy =
        heap_caps_calloc(1, sizeof(*legacy), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!legacy) return ESP_ERR_NO_MEM;
    size_t size = sizeof(*legacy);
    esp_err_t err = device_status_to_platform_error(persistence_service_read_blob(CONFIGURATION_NAMESPACE,
                                                  CONFIGURATION_STORE_KEY,
                                                  legacy, &size));
    if (err != ESP_OK || legacy->magic != CONFIGURATION_STORE_MAGIC ||
        legacy->version != 2u || legacy->force_setup > 1) {
        heap_caps_free(legacy);
        return ESP_ERR_INVALID_STATE;
    }
    store->magic = CONFIGURATION_STORE_MAGIC;
    store->version = CONFIGURATION_STORE_VERSION;
    /* V2 is an exact prefix through transport selection; the later Wi-Fi
     * catalogue and display fields remain zero-initialized. */
    memcpy(&store->provisioning.confirmed_snapshot, &legacy->snapshot,
           sizeof(legacy->snapshot));
    store->force_setup = legacy->force_setup;
    store->revision = 1u;
    heap_caps_free(legacy);
    if (!valid_snapshot(&store->provisioning.confirmed_snapshot)) return ESP_ERR_INVALID_STATE;
    sync_networks_from_primary(&store->provisioning.confirmed_snapshot);
    return device_status_to_platform_error(persistence_service_write_blob(CONFIGURATION_NAMESPACE,
                                          CONFIGURATION_STORE_KEY,
                                          store, sizeof(*store)));
}

static esp_err_t load_locked(configuration_snapshot_t *inout_snapshot,
                             bool *out_force_setup) {
    if (!valid_snapshot(inout_snapshot) || !out_force_setup) return ESP_ERR_INVALID_ARG;
    *out_force_setup = false;
    /* The store is multi-kilobyte once it retains both confirmed and staged
     * configurations. Callers up the chain already
     * hold their own snapshot by value; measured main-task stack peaked at
     * ~4.9 KB through this path.  Use the lock-protected PSRAM scratch store;
     * it never escapes this service. */
    if (!s_scratch_store) return ESP_ERR_INVALID_STATE;
    configuration_store_t *store = s_scratch_store;
    memset(store, 0, sizeof(*store));
    /* Query length before supplying a current buffer. NVS rejects an older,
     * smaller V1 blob when given the larger current buffer, which would otherwise
     * make a valid expand migration indistinguishable from corruption. */
    size_t size = 0;
    esp_err_t err = device_status_to_platform_error(persistence_service_read_blob(CONFIGURATION_NAMESPACE,
                                                  CONFIGURATION_STORE_KEY,
                                                  NULL, &size));
    if (err == ESP_ERR_NOT_FOUND) {
        bool legacy_found = false;
        err = load_legacy(inout_snapshot, &legacy_found, out_force_setup);
        if (err == ESP_OK && legacy_found) {
            // 散落的旧标量 key 同样只有单组凭据，入库前迁成列表第 1 条。
            sync_networks_from_primary(inout_snapshot);
            store->magic = CONFIGURATION_STORE_MAGIC;
            store->version = CONFIGURATION_STORE_VERSION;
            store->provisioning.confirmed_snapshot = *inout_snapshot;
            store->force_setup = *out_force_setup ? 1u : 0u;
            store->revision = 1u;
            err = device_status_to_platform_error(persistence_service_write_blob(CONFIGURATION_NAMESPACE,
                                                 CONFIGURATION_STORE_KEY,
                                                 store, sizeof(*store)));
        }
        return err;
    }
    if (err != ESP_OK) return err;
    bool migrated = false;
    if (size == sizeof(configuration_store_v1_t)) {
        err = migrate_v1_locked(store);
        if (err != ESP_OK) return err;
        migrated = true;
    } else if (size == sizeof(configuration_store_v2_t)) {
        err = migrate_v2_locked(store);
        if (err != ESP_OK) return err;
        migrated = true;
    } else if (size == sizeof(configuration_store_v3_t)) {
        err = migrate_v3_locked(store);
        if (err != ESP_OK) return err;
        migrated = true;
    } else if (size == sizeof(configuration_store_v4_t)) {
        err = migrate_v4_locked(store);
        if (err != ESP_OK) return err;
        migrated = true;
    } else if (size == sizeof(configuration_store_v5_t)) {
        err = migrate_v5_locked(store);
        if (err != ESP_OK) return err;
        migrated = true;
    } else if (size == sizeof(configuration_store_v6_t)) {
        err = migrate_v6_locked(store);
        if (err != ESP_OK) return err;
        migrated = true;
    } else if (size == sizeof(*store)) {
        err = device_status_to_platform_error(persistence_service_read_blob(CONFIGURATION_NAMESPACE,
                                            CONFIGURATION_STORE_KEY,
                                            store, &size));
        if (err != ESP_OK || !valid_store(store)) return ESP_ERR_INVALID_STATE;
    } else {
        return ESP_ERR_INVALID_STATE;
    }
    bool legacy_display_found = false;
    if (migrated) {
        err = import_legacy_display_policy(&store->provisioning.confirmed_snapshot,
                                           &legacy_display_found);
        if (err != ESP_OK) return err;
        if (store->provisioning.staged) {
            bool ignored = false;
            err = import_legacy_display_policy(&store->provisioning.staged_snapshot,
                                               &ignored);
            if (err != ESP_OK) return err;
        }
    }
    if (!valid_store(store)) return ESP_ERR_INVALID_STATE;
    if (migrated || legacy_display_found) {
        err = write_store_locked(store);
        if (err != ESP_OK) return err;
    }
    *inout_snapshot = store->provisioning.confirmed_snapshot;
    *out_force_setup = store->force_setup != 0;
    return ESP_OK;
}

static esp_err_t write_store_locked(const configuration_store_t *store) {
    if (!valid_store(store)) return ESP_ERR_INVALID_ARG;
    if (!s_scratch_store) return ESP_ERR_INVALID_STATE;
    *s_scratch_store = *store;
    return device_status_to_platform_error(persistence_service_write_blob(
        CONFIGURATION_NAMESPACE, CONFIGURATION_STORE_KEY, s_scratch_store,
        sizeof(*s_scratch_store)));
}

/* Display scalars predate Configuration V7. Import them once into the
 * authoritative record; no caller outside this service owns those keys. */
static esp_err_t import_legacy_display_policy(configuration_snapshot_t *snapshot,
                                              bool *out_found) {
    if (!snapshot || !out_found) return ESP_ERR_INVALID_ARG;
    *out_found = false;
    uint8_t brightness = 0;
    esp_err_t err = device_status_to_platform_error(persistence_service_read_u8(
        CONFIGURATION_NAMESPACE, "brightness", &brightness));
    if (err == ESP_OK) {
        if (brightness > 100u) return ESP_ERR_INVALID_STATE;
        snapshot->display_brightness = brightness;
        snapshot->display_brightness_saved = true;
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) {
        return err;
    }
    uint32_t sleep_seconds = 0;
    size_t size = sizeof(sleep_seconds);
    err = device_status_to_platform_error(persistence_service_read_blob(
        CONFIGURATION_NAMESPACE, "screen_sleep_s", &sleep_seconds, &size));
    if (err == ESP_OK) {
        if (size != sizeof(sleep_seconds) ||
            !valid_screen_sleep_seconds_value(sleep_seconds)) {
            return ESP_ERR_INVALID_STATE;
        }
        snapshot->screen_sleep_seconds = sleep_seconds;
        snapshot->screen_sleep_seconds_saved = true;
        *out_found = true;
    } else if (err != ESP_ERR_NOT_FOUND) {
        return err;
    }
    return ESP_OK;
}

/* Every ordinary durable mutation becomes a new configuration publication.
 * Schema migrations and first-record creation use write_store_locked() with
 * revision 1 instead: they establish the first durable fact rather than
 * mutating an existing revision. */
static esp_err_t commit_store_locked(configuration_store_t *store) {
    if (!valid_store(store)) return ESP_ERR_INVALID_ARG;
    uint64_t next = 0;
    if (!configuration_revision_next(store->revision, &next)) {
        return ESP_ERR_INVALID_STATE;
    }
    store->revision = next;
    return write_store_locked(store);
}

static esp_err_t write_locked(const configuration_snapshot_t *snapshot, bool force_setup) {
    if (!valid_snapshot(snapshot)) return ESP_ERR_INVALID_ARG;
    /* Most mutations (volume, transport selection, recovery pairing code)
     * change confirmed device policy but must not erase a pending candidate.
     * `load_locked()` has just refreshed this private store under the same
     * lock; retain its staged slot when present. */
    if (valid_store(s_scratch_store)) {
        if (!configuration_transaction_apply_confirmed_policy(
                &s_scratch_store->provisioning, snapshot)) {
            return ESP_ERR_INVALID_STATE;
        }
        s_scratch_store->force_setup = force_setup ? 1u : 0u;
        return commit_store_locked(s_scratch_store);
    }
    configuration_store_t fresh = {
        .magic = CONFIGURATION_STORE_MAGIC,
        .version = CONFIGURATION_STORE_VERSION,
        .provisioning.confirmed_snapshot = *snapshot,
        .force_setup = force_setup ? 1u : 0u,
        .revision = 1u,
    };
    return write_store_locked(&fresh);
}

static void seed_snapshot(configuration_snapshot_t *snapshot) {
    memset(snapshot, 0, sizeof(*snapshot));
    if (s_default_snapshot_available) {
        *snapshot = s_default_snapshot;
        return;
    }
    strlcpy(snapshot->wifi_security, "personal", sizeof(snapshot->wifi_security));
    strlcpy(snapshot->wifi_eap_method, "peap", sizeof(snapshot->wifi_eap_method));
    strlcpy(snapshot->wifi_ttls_phase2, "mschapv2", sizeof(snapshot->wifi_ttls_phase2));
    strlcpy(snapshot->wifi_ca_mode, "system", sizeof(snapshot->wifi_ca_mode));
}

static esp_err_t configuration_service_init_legacy(void) {
    if (!persistence_service_is_initialized()) return ESP_ERR_INVALID_STATE;
    /* Publish construction intent before allocating any lifecycle shell. A
     * concurrent rollback which runs in the allocation window must wait for
     * this generation to either become observable or fail, rather than
     * returning success because `s_deinit_lock` is not published yet. */
    bool expected = false;
    if (!__atomic_compare_exchange_n(&s_initializing, &expected, true, false,
                                     __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
        return ESP_ERR_INVALID_STATE;
    }
    if (!s_mutation_lock) s_mutation_lock = xSemaphoreCreateMutex();
    if (!s_deinit_lock) s_deinit_lock = xSemaphoreCreateMutex();
    if (!s_mutation_lock || !s_deinit_lock) {
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_ERR_NO_MEM;
    }
    if (xSemaphoreTake(s_deinit_lock, pdMS_TO_TICKS(3000)) != pdTRUE) {
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_ERR_TIMEOUT;
    }
    if (!__atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE) &&
        s_scratch_snapshot_a && s_scratch_snapshot_b && s_scratch_store) {
        xSemaphoreGive(s_deinit_lock);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_OK;
    }
    /* Keep late mutation waiters closed until every scratch object exists. */
    __atomic_store_n(&s_stopping, true, __ATOMIC_RELEASE);
    if (!s_scratch_snapshot_a) {
        s_scratch_snapshot_a = heap_caps_calloc(1, sizeof(*s_scratch_snapshot_a),
                                                MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_scratch_snapshot_b) {
        s_scratch_snapshot_b = heap_caps_calloc(1, sizeof(*s_scratch_snapshot_b),
                                                MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_scratch_store) {
        s_scratch_store = heap_caps_calloc(1, sizeof(*s_scratch_store),
                                           MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    }
    if (!s_runtime_override_store) {
        s_runtime_override_store = heap_caps_calloc(
            1, sizeof(*s_runtime_override_store), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
        if (s_runtime_override_store) {
            configuration_runtime_override_store_init(s_runtime_override_store);
        }
    }
    bool ready = s_mutation_lock && s_scratch_snapshot_a && s_scratch_snapshot_b &&
                 s_scratch_store && s_runtime_override_store;
    if (!ready) {
        xSemaphoreGive(s_deinit_lock);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_ERR_NO_MEM;
    }
    __atomic_store_n(&s_stopping, false, __ATOMIC_RELEASE);
    s_system_sleep_preparing = false;
    xSemaphoreGive(s_deinit_lock);
    __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
    return ESP_OK;
}

static esp_err_t configuration_service_deinit_legacy(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = configuration_stop_timeout_ticks(timeout_ms);
    /* Init publishes `s_initializing` before its mutex handles. Share this
     * caller's deadline while that short construction transaction resolves. */
    while (__atomic_load_n(&s_initializing, __ATOMIC_ACQUIRE)) {
        if (configuration_stop_remaining_ticks(started, budget) == 0) {
            return ESP_ERR_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    if (!s_mutation_lock) return ESP_OK;
    if (!s_deinit_lock || xSemaphoreTake(s_deinit_lock, budget) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    /* Close admission before waiting for an in-flight mutation. Every public
     * call obtains this mutex through lock(), so a successful take proves no
    * caller still owns the shared scratch snapshots. */
    __atomic_store_n(&s_stopping, true, __ATOMIC_RELEASE);
    const TickType_t remaining = configuration_stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_mutation_lock, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_lock);
        return ESP_ERR_TIMEOUT;
    }
    if (s_scratch_snapshot_a) {
        heap_caps_free(s_scratch_snapshot_a);
        s_scratch_snapshot_a = NULL;
    }
    if (s_scratch_snapshot_b) {
        heap_caps_free(s_scratch_snapshot_b);
        s_scratch_snapshot_b = NULL;
    }
    if (s_scratch_store) {
        heap_caps_free(s_scratch_store);
        s_scratch_store = NULL;
    }
    /* Restart deliberately loses the monotonic epoch. Never carry a short
     * remote policy into a new boot where its original expiry is ambiguous. */
    if (s_runtime_override_store) {
        heap_caps_free(s_runtime_override_store);
        s_runtime_override_store = NULL;
    }
    memset(&s_default_snapshot, 0, sizeof(s_default_snapshot));
    s_default_snapshot_available = false;
    /* Keep the admission mutex allocated. A task that sampled it immediately
     * before `s_stopping` changed may still be queued on it; deleting a live
     * FreeRTOS mutex would turn an orderly deinit into a use-after-free. The
     * next init reuses it and reopens admission only after scratch allocation
     * is complete. */
    xSemaphoreGive(s_mutation_lock);
    xSemaphoreGive(s_deinit_lock);
    return ESP_OK;
}

bool configuration_service_is_initialized(void) {
    return s_mutation_lock != NULL && s_scratch_snapshot_a &&
           s_scratch_snapshot_b && s_scratch_store && s_runtime_override_store &&
           !__atomic_load_n(&s_initializing, __ATOMIC_ACQUIRE) &&
           !__atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE);
}

/* V3 -> V4: the single destructive snapshot becomes the confirmed baseline;
 * no candidate exists until a future portal save stages one explicitly. */
static esp_err_t migrate_v3_locked(configuration_store_t *store) {
    configuration_store_v3_t *legacy =
        heap_caps_calloc(1, sizeof(*legacy), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!legacy) return ESP_ERR_NO_MEM;
    size_t size = sizeof(*legacy);
    esp_err_t err = device_status_to_platform_error(persistence_service_read_blob(
        CONFIGURATION_NAMESPACE, CONFIGURATION_STORE_KEY, legacy, &size));
    configuration_snapshot_t migrated_snapshot;
    snapshot_from_v6(&migrated_snapshot, &legacy->snapshot);
    if (err != ESP_OK || legacy->magic != CONFIGURATION_STORE_MAGIC ||
        legacy->version != 3u || legacy->force_setup > 1 ||
        !valid_snapshot(&migrated_snapshot)) {
        heap_caps_free(legacy);
        return ESP_ERR_INVALID_STATE;
    }
    store->magic = CONFIGURATION_STORE_MAGIC;
    store->version = CONFIGURATION_STORE_VERSION;
    store->provisioning.confirmed_snapshot = migrated_snapshot;
    store->force_setup = legacy->force_setup;
    store->revision = 1u;
    heap_caps_free(legacy);
    return device_status_to_platform_error(persistence_service_write_blob(
        CONFIGURATION_NAMESPACE, CONFIGURATION_STORE_KEY, store, sizeof(*store)));
}

device_status_t configuration_service_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    if (!s_deinit_lock || !s_mutation_lock) return DEVICE_STATUS_UNAVAILABLE;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = configuration_stop_timeout_ticks(timeout_ms);
    if (xSemaphoreTake(s_deinit_lock, budget) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    const TickType_t remaining = configuration_stop_remaining_ticks(started, budget);
    if (remaining == 0 || xSemaphoreTake(s_mutation_lock, remaining) != pdTRUE) {
        xSemaphoreGive(s_deinit_lock);
        return DEVICE_STATUS_TIMEOUT;
    }
    const bool ready = !__atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE) &&
                       !s_system_sleep_preparing && s_scratch_snapshot_a &&
                       s_scratch_snapshot_b && s_scratch_store && s_runtime_override_store;
    if (ready) {
        /* An override expiry belongs to this boot's esp_timer monotonic epoch.
         * A future verified sleep profile may not retain that epoch, and an
         * ABORT after an incomplete electrical entry cannot honestly recover
         * a deadline that may already have elapsed. Close the override epoch
         * before Power can advance to a physical COMMIT; normal durable policy
         * remains untouched. */
        const configuration_runtime_override_store_result_t clear_result =
            configuration_runtime_override_store_clear_all(s_runtime_override_store);
        if (clear_result == CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK) {
            s_system_sleep_preparing = true;
        } else {
            xSemaphoreGive(s_mutation_lock);
            xSemaphoreGive(s_deinit_lock);
            return clear_result == CONFIGURATION_RUNTIME_OVERRIDE_STORE_REVISION_EXHAUSTED
                       ? DEVICE_STATUS_INTERNAL_ERROR : DEVICE_STATUS_BUSY;
        }
    }
    xSemaphoreGive(s_mutation_lock);
    xSemaphoreGive(s_deinit_lock);
    return ready ? DEVICE_STATUS_OK : DEVICE_STATUS_BUSY;
}

void configuration_service_abort_system_sleep_prepare(void) {
    if (!s_deinit_lock || !s_mutation_lock) return;
    if (xSemaphoreTake(s_deinit_lock, pdMS_TO_TICKS(3000)) != pdTRUE) return;
    if (xSemaphoreTake(s_mutation_lock, pdMS_TO_TICKS(3000)) == pdTRUE) {
        if (!__atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE)) {
            s_system_sleep_preparing = false;
        }
        xSemaphoreGive(s_mutation_lock);
    }
    xSemaphoreGive(s_deinit_lock);
}

static esp_err_t configuration_service_load_legacy(configuration_snapshot_t *inout_snapshot) {
    if (!inout_snapshot) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    bool ignored_force_setup = false;
    esp_err_t err = load_locked(inout_snapshot, &ignored_force_setup);
    if (err == ESP_OK) {
        s_default_snapshot = *inout_snapshot;
        s_default_snapshot_available = true;
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_load_revisioned_snapshot_legacy(
    configuration_revisioned_snapshot_t *out_snapshot) {
    if (!out_snapshot) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a || !s_scratch_store) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    /* Do not manufacture a revision for compile-time defaults. A caller that
     * needs defaults deliberately chooses its own startup policy; a service
     * requesting an immutable effective configuration must receive a durable
     * publication or an explicit NOT_FOUND. */
    seed_snapshot(s_scratch_snapshot_a);
    bool ignored_force_setup = false;
    esp_err_t err = load_locked(s_scratch_snapshot_a, &ignored_force_setup);
    if (err == ESP_OK) {
        if (!valid_store(s_scratch_store)) {
            err = ESP_ERR_INVALID_STATE;
        } else {
            memset(out_snapshot, 0, sizeof(*out_snapshot));
            out_snapshot->struct_size = sizeof(*out_snapshot);
            out_snapshot->abi_version = CONFIGURATION_REVISIONED_SNAPSHOT_ABI_VERSION;
            out_snapshot->revision = s_scratch_store->revision;
            out_snapshot->snapshot = s_scratch_store->provisioning.confirmed_snapshot;
        }
    }
    unlock();
    return err;
}

static uint64_t configuration_monotonic_ms(void) {
    const int64_t now_us = esp_timer_get_time();
    return now_us <= 0 ? 0u : (uint64_t)now_us / 1000u;
}

static esp_err_t configuration_service_apply_runtime_override_legacy(
    const configuration_runtime_override_t *override) {
    if (!override) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_runtime_override_store) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    const configuration_runtime_override_store_result_t result =
        configuration_runtime_override_store_put(s_runtime_override_store, override,
                                                 configuration_monotonic_ms());
    unlock();
    switch (result) {
        case CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK: return ESP_OK;
        case CONFIGURATION_RUNTIME_OVERRIDE_STORE_EXPIRED: return ESP_ERR_INVALID_ARG;
        case CONFIGURATION_RUNTIME_OVERRIDE_STORE_REVISION_EXHAUSTED:
            return ESP_ERR_INVALID_STATE;
        default: return ESP_ERR_INVALID_ARG;
    }
}

static esp_err_t configuration_service_remove_runtime_override_legacy(
    configuration_runtime_override_value_kind_t kind) {
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_runtime_override_store) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    const configuration_runtime_override_store_result_t result =
        configuration_runtime_override_store_remove(s_runtime_override_store, kind);
    unlock();
    switch (result) {
        case CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK: return ESP_OK;
        case CONFIGURATION_RUNTIME_OVERRIDE_STORE_NOT_FOUND: return ESP_ERR_NOT_FOUND;
        case CONFIGURATION_RUNTIME_OVERRIDE_STORE_REVISION_EXHAUSTED:
            return ESP_ERR_INVALID_STATE;
        default: return ESP_ERR_INVALID_ARG;
    }
}

static esp_err_t configuration_service_clear_runtime_overrides_legacy(void) {
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_runtime_override_store) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    const configuration_runtime_override_store_result_t result =
        configuration_runtime_override_store_clear_all(s_runtime_override_store);
    unlock();
    return result == CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK ? ESP_OK :
           result == CONFIGURATION_RUNTIME_OVERRIDE_STORE_REVISION_EXHAUSTED
               ? ESP_ERR_INVALID_STATE : ESP_ERR_INVALID_ARG;
}

static esp_err_t configuration_service_next_runtime_override_expiry_ms_legacy(
    uint64_t *out_expires_at_monotonic_ms) {
    if (!out_expires_at_monotonic_ms) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_runtime_override_store) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    const configuration_runtime_override_store_result_t result =
        configuration_runtime_override_store_next_expiry(
            s_runtime_override_store, out_expires_at_monotonic_ms);
    unlock();
    return result == CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK ? ESP_OK
         : result == CONFIGURATION_RUNTIME_OVERRIDE_STORE_REVISION_EXHAUSTED
             ? ESP_ERR_INVALID_STATE : ESP_ERR_INVALID_ARG;
}

static esp_err_t configuration_service_load_effective_revisioned_snapshot_legacy(
    configuration_effective_revisioned_snapshot_t *out_snapshot) {
    if (!out_snapshot) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a || !s_scratch_store || !s_runtime_override_store) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    seed_snapshot(s_scratch_snapshot_a);
    bool ignored_force_setup = false;
    esp_err_t err = load_locked(s_scratch_snapshot_a, &ignored_force_setup);
    if (err == ESP_OK && !valid_store(s_scratch_store)) err = ESP_ERR_INVALID_STATE;
    uint64_t override_revision = 0u;
    uint32_t override_mask = 0u;
    if (err == ESP_OK) {
        const configuration_runtime_override_store_result_t result =
            configuration_runtime_override_store_resolve(
                s_runtime_override_store, &s_scratch_store->provisioning.confirmed_snapshot,
                configuration_monotonic_ms(), s_scratch_snapshot_a,
                &override_revision, &override_mask);
        if (result != CONFIGURATION_RUNTIME_OVERRIDE_STORE_OK) err = ESP_ERR_INVALID_STATE;
    }
    if (err == ESP_OK) {
        memset(out_snapshot, 0, sizeof(*out_snapshot));
        out_snapshot->struct_size = sizeof(*out_snapshot);
        out_snapshot->abi_version = CONFIGURATION_EFFECTIVE_REVISIONED_SNAPSHOT_ABI_VERSION;
        out_snapshot->durable_revision = s_scratch_store->revision;
        out_snapshot->runtime_override_revision = override_revision;
        out_snapshot->runtime_override_mask = override_mask;
        out_snapshot->snapshot = *s_scratch_snapshot_a;
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_load_boot_candidate_legacy(
    configuration_snapshot_t *inout_snapshot, bool *out_staged) {
    if (!inout_snapshot || !out_staged) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    bool ignored_force_setup = false;
    esp_err_t err = load_locked(inout_snapshot, &ignored_force_setup);
    if (err == ESP_OK) {
        /* Preserve the caller's confirmed/default seed before replacing it
         * with a candidate. This covers a factory image with no NVS blob:
         * its compiled token/transport defaults still form the rollback
         * baseline when the first portal save stages a candidate. */
        s_default_snapshot = *inout_snapshot;
        s_default_snapshot_available = true;
        /* A factory/no-NVS device has a valid caller-provided default but no
         * durable store yet. It is an ordinary confirmed first boot, not a
         * corrupt transaction. The first portal save below creates both the
         * baseline and its staged successor atomically. */
        *out_staged = valid_store(s_scratch_store) &&
                      s_scratch_store->provisioning.staged;
        if (valid_store(s_scratch_store)) {
            const configuration_snapshot_t *boot = configuration_transaction_boot_snapshot(
                &s_scratch_store->provisioning, out_staged);
            if (!boot) {
                err = ESP_ERR_INVALID_STATE;
            } else {
                *inout_snapshot = *boot;
            }
        }
        if (valid_store(s_scratch_store)) {
            /* Defaults must remain the confirmed fallback, not the candidate;
             * a rollback later in this boot may restart into that baseline. */
            s_default_snapshot = s_scratch_store->provisioning.confirmed_snapshot;
            s_default_snapshot_available = true;
        }
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_begin_staged_provisioning_boot_legacy(void) {
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a || !s_scratch_store) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    seed_snapshot(s_scratch_snapshot_a);
    bool ignored_force_setup = false;
    esp_err_t err = load_locked(s_scratch_snapshot_a, &ignored_force_setup);
    if (err != ESP_OK || !valid_store(s_scratch_store)) {
        unlock();
        return err == ESP_OK ? ESP_ERR_INVALID_STATE : err;
    }
    if (!s_scratch_store->provisioning.staged) {
        unlock();
        return ESP_ERR_NOT_FOUND;
    }
    if (!configuration_transaction_begin_staged_boot(&s_scratch_store->provisioning)) {
        /* The value model has removed only the candidate. Persist that rollback
         * before reporting expiry; a reset cannot restore the exhausted slot. */
        err = commit_store_locked(s_scratch_store);
        unlock();
        return err == ESP_OK ? ESP_ERR_NOT_FOUND : err;
    }
    /* Claim the attempt before any Wi-Fi/Hub side effect. A reset after this
     * commit consumes one of the finite attempts rather than extending the
     * candidate lifetime indefinitely. */
    err = commit_store_locked(s_scratch_store);
    unlock();
    return err;
}

static esp_err_t configuration_service_rollback_staged_provisioning_legacy(void) {
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a || !s_scratch_store) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    seed_snapshot(s_scratch_snapshot_a);
    bool ignored_force_setup = false;
    esp_err_t err = load_locked(s_scratch_snapshot_a, &ignored_force_setup);
    if (err == ESP_OK && valid_store(s_scratch_store)) {
        if (s_scratch_store->provisioning.staged) {
            configuration_transaction_rollback(&s_scratch_store->provisioning);
            err = commit_store_locked(s_scratch_store);
        }
    } else if (err == ESP_OK) {
        err = ESP_ERR_INVALID_STATE;
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_stage_provisioning_legacy(
    const configuration_provisioning_request_t *request) {
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    /* The portal owns network and pairing fields, but it does not own the
     * selected physical uplink nor the one-shot setup request.  Preserve them
     * from the authoritative durable snapshot rather than trusting a stale
     * caller copy assembled in main.c. */
    configuration_snapshot_t *current = s_scratch_snapshot_a;
    seed_snapshot(current);
    bool force_setup = false;
    esp_err_t err = load_locked(current, &force_setup);
    /* No durable blob on a factory/default boot is a legitimate baseline for
     * the first provisioning transaction, not a failed read. `current` was
     * seeded above with the compiled/default confirmed configuration. */
    if (err == ESP_ERR_NOT_FOUND) err = ESP_OK;
    if (err == ESP_OK) {
        /* This is STAGE, not confirmation.  Keep the last confirmed owner
         * and network durable until Wi-Fi + Hub pairing later produces the
         * new token.  A reset in between will boot the candidate once, while
         * an explicit rollback can recover the confirmed record unchanged. */
        const bool first_record = !valid_store(s_scratch_store);
        if (first_record) {
            /* First provisioning has no older NVS blob. Its compiled/default
             * snapshot becomes the confirmed baseline, while submitted portal
             * data remains a candidate until Hub confirmation. */
            *s_scratch_store = (configuration_store_t){
                .magic = CONFIGURATION_STORE_MAGIC,
                .version = CONFIGURATION_STORE_VERSION,
                .provisioning.confirmed_snapshot = *current,
                .force_setup = 0u,
                .revision = 1u,
            };
        }
        /* The value model validates the request and derives from the
         * confirmed baseline. Rejection leaves the current durable record
         * untouched; only Configuration owns the following persistence. */
        if (!configuration_transaction_stage_provisioning_request(
                &s_scratch_store->provisioning, request)) {
            err = ESP_ERR_INVALID_ARG;
        } else {
            s_scratch_store->force_setup = 0u;
            /* The factory baseline and its first candidate become one durable
             * initial publication. Existing records advance exactly once for
             * every later stage mutation. */
            err = first_record ? write_store_locked(s_scratch_store)
                               : commit_store_locked(s_scratch_store);
        }
    }
    unlock();
    return err;
}

static esp_err_t mutate_pairing_code(const char *value) {
    if (!configuration_transaction_valid_pairing_code(value)) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    /* A mutation must retain all existing values.  Seed strings are only used
     * if a pre-service image had no configuration blob nor legacy keys. */
    seed_snapshot(snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(snapshot, &force_setup);
    if (err == ESP_OK) {
        /* A staged pair code is the exact external evidence that will either
         * promote or roll back the candidate.  Replacing only confirmed state
         * here would be overwritten by that promotion, so reject the ambiguous
         * second pairing flow until the candidate reaches a terminal state. */
        if (valid_store(s_scratch_store) && s_scratch_store->provisioning.staged) {
            err = ESP_ERR_INVALID_STATE;
        } else {
            strlcpy(snapshot->pair_code, value, sizeof(snapshot->pair_code));
            err = write_locked(snapshot, force_setup);
        }
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_set_pairing_code_legacy(const char *pair_code) {
    return mutate_pairing_code(pair_code);
}

static esp_err_t configuration_service_commit_gateway_pairing_token_legacy(const char *token) {
    if (!token || !token[0] || strlen(token) >= CONFIGURATION_GATEWAY_TOKEN_CAPACITY) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a || !s_scratch_store) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    seed_snapshot(s_scratch_snapshot_a);
    bool ignored_force_setup = false;
    esp_err_t err = load_locked(s_scratch_snapshot_a, &ignored_force_setup);
    if (err == ESP_OK) {
        /* Configuration owns the complete durable pairing transaction;
         * Gateway only supplies the Hub token as external evidence. */
        if (!valid_store(s_scratch_store) ||
            !configuration_transaction_commit_gateway_pairing_token(
                &s_scratch_store->provisioning, token)) {
            err = ESP_ERR_INVALID_STATE;
        } else {
            err = commit_store_locked(s_scratch_store);
        }
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_set_output_volume_with_policy_revision_legacy(
    uint8_t percent, const configuration_policy_request_t *policy, uint64_t *out_revision) {
    if (percent > 100 || !out_revision) return ESP_ERR_INVALID_ARG;
    if (configuration_policy_authorize(CONFIGURATION_KEY_OUTPUT_VOLUME, policy) !=
        CONFIGURATION_POLICY_ALLOW_DURABLE) {
        return ESP_ERR_INVALID_ARG;
    }
    *out_revision = 0u;
    if (!lock()) return ESP_ERR_TIMEOUT;
    /* 快照随 v3 多热点列表变大，与其它路径一样改用锁保护的 PSRAM scratch，
     * 避免把 ~1.6 KB 的结构体压进调用方任务栈。 */
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(snapshot, &force_setup);
    if (err == ESP_OK) {
        snapshot->output_volume = percent;
        snapshot->output_volume_saved = true;
        err = write_locked(snapshot, force_setup);
        if (err == ESP_OK) {
            if (!valid_store(s_scratch_store) || s_scratch_store->revision == 0u) {
                err = ESP_ERR_INVALID_STATE;
            } else {
                *out_revision = s_scratch_store->revision;
            }
        }
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_set_output_volume_with_policy_legacy(
    uint8_t percent, const configuration_policy_request_t *policy) {
    uint64_t ignored_revision = 0u;
    return configuration_service_set_output_volume_with_policy_revision_legacy(
        percent, policy, &ignored_revision);
}

static esp_err_t configuration_service_set_output_volume_legacy(uint8_t percent) {
    const configuration_policy_request_t local = {
        .struct_size = sizeof(local),
        .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
        .source = CONFIGURATION_SOURCE_USER_LOCAL,
        .authenticated = true,
        .ttl_ms = 0u,
    };
    return configuration_service_set_output_volume_with_policy_legacy(percent, &local);
}

static esp_err_t configuration_service_set_display_brightness_with_policy_legacy(
    uint8_t percent, const configuration_policy_request_t *policy) {
    if (percent > 100u) return ESP_ERR_INVALID_ARG;
    if (configuration_policy_authorize(CONFIGURATION_KEY_DISPLAY_BRIGHTNESS, policy) !=
        CONFIGURATION_POLICY_ALLOW_DURABLE) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(snapshot, &force_setup);
    if (err == ESP_OK) {
        snapshot->display_brightness = percent;
        snapshot->display_brightness_saved = true;
        err = write_locked(snapshot, force_setup);
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_set_display_brightness_legacy(uint8_t percent) {
    const configuration_policy_request_t local = {
        .struct_size = sizeof(local),
        .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
        .source = CONFIGURATION_SOURCE_USER_LOCAL,
        .authenticated = true,
        .ttl_ms = 0u,
    };
    return configuration_service_set_display_brightness_with_policy_legacy(percent, &local);
}

static esp_err_t configuration_service_set_screen_sleep_seconds_with_policy_legacy(
    uint32_t seconds, const configuration_policy_request_t *policy) {
    if (!valid_screen_sleep_seconds_value(seconds)) return ESP_ERR_INVALID_ARG;
    if (configuration_policy_authorize(CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS, policy) !=
        CONFIGURATION_POLICY_ALLOW_DURABLE) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(snapshot, &force_setup);
    if (err == ESP_OK) {
        snapshot->screen_sleep_seconds = seconds;
        snapshot->screen_sleep_seconds_saved = true;
        err = write_locked(snapshot, force_setup);
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_set_screen_sleep_seconds_legacy(uint32_t seconds) {
    const configuration_policy_request_t local = {
        .struct_size = sizeof(local),
        .abi_version = CONFIGURATION_POLICY_REQUEST_ABI_VERSION,
        .source = CONFIGURATION_SOURCE_USER_LOCAL,
        .authenticated = true,
        .ttl_ms = 0u,
    };
    return configuration_service_set_screen_sleep_seconds_with_policy_legacy(seconds, &local);
}

static esp_err_t configuration_service_apply_display_policy_with_policy_legacy(
    const configuration_display_policy_update_t *update,
    const configuration_policy_request_t *policy,
    uint64_t *out_revision) {
    if (!update || !out_revision || update->struct_size != sizeof(*update) ||
        update->abi_version != CONFIGURATION_DISPLAY_POLICY_UPDATE_ABI_VERSION ||
        (!update->has_brightness && !update->has_screen_sleep_seconds) ||
        (update->has_brightness && update->brightness > 100u) ||
        (update->has_screen_sleep_seconds &&
         !valid_screen_sleep_seconds_value(update->screen_sleep_seconds)) ||
        (update->has_brightness &&
         configuration_policy_authorize(CONFIGURATION_KEY_DISPLAY_BRIGHTNESS, policy) !=
             CONFIGURATION_POLICY_ALLOW_DURABLE) ||
        (update->has_screen_sleep_seconds &&
         configuration_policy_authorize(CONFIGURATION_KEY_SCREEN_SLEEP_SECONDS, policy) !=
             CONFIGURATION_POLICY_ALLOW_DURABLE)) {
        return ESP_ERR_INVALID_ARG;
    }
    *out_revision = 0u;
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a || !s_scratch_store) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(snapshot, &force_setup);
    if (err == ESP_OK) {
        if (update->has_brightness) {
            snapshot->display_brightness = update->brightness;
            snapshot->display_brightness_saved = true;
        }
        if (update->has_screen_sleep_seconds) {
            snapshot->screen_sleep_seconds = update->screen_sleep_seconds;
            snapshot->screen_sleep_seconds_saved = true;
        }
        err = write_locked(snapshot, force_setup);
        if (err == ESP_OK) {
            if (!valid_store(s_scratch_store) || s_scratch_store->revision == 0u) {
                err = ESP_ERR_INVALID_STATE;
            } else {
                *out_revision = s_scratch_store->revision;
            }
        }
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_load_transport_selection_legacy(bool default_cellular,
                                                         bool *out_cellular,
                                                         bool *out_saved) {
    if (!out_cellular || !out_saved) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool ignored_force_setup = false;
    esp_err_t err = load_locked(snapshot, &ignored_force_setup);
    if (err == ESP_OK) {
        *out_cellular = snapshot->cellular_transport_selection_saved
                            ? snapshot->cellular_transport_selected
                            : default_cellular;
        *out_saved = snapshot->cellular_transport_selection_saved;
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_set_transport_selection_legacy(bool cellular) {
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(snapshot, &force_setup);
    if (err == ESP_OK) {
        snapshot->cellular_transport_selected = cellular;
        snapshot->cellular_transport_selection_saved = true;
        err = write_locked(snapshot, force_setup);
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_request_force_setup_legacy(void) {
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool ignored = false;
    esp_err_t err = load_locked(snapshot, &ignored);
    if (err == ESP_OK) err = write_locked(snapshot, true);
    unlock();
    return err;
}

static esp_err_t configuration_service_take_force_setup_legacy(bool *out_requested) {
    if (!out_requested || !lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool requested = false;
    esp_err_t err = load_locked(snapshot, &requested);
    /* C7's production SAFE_MODE candidate is the inability to consume this
     * authoritative one-shot configuration fact. Keep the fault seam at the
     * transaction boundary: it runs only in a dedicated test artifact after
     * the durable record has been read, and before the flag could be cleared
     * or any credential-bearing state is written back. */
    if (err == ESP_OK &&
        provisioning_failure_injection_safe_mode_force_setup_take_fails()) {
        err = ESP_FAIL;
    }
    if (err == ESP_OK && requested) err = write_locked(snapshot, false);
    unlock();
    if (err == ESP_OK) *out_requested = requested;
    return err;
}

static esp_err_t configuration_service_list_wifi_networks_legacy(configuration_wifi_network_t *out_networks,
                                                   uint8_t capacity, uint8_t *out_count) {
    if (!out_networks || !out_count) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool ignored = false;
    esp_err_t err = load_locked(snapshot, &ignored);
    if (err == ESP_OK) {
        uint8_t count = snapshot->wifi_network_count;
        if (count > capacity) count = capacity;
        memcpy(out_networks, snapshot->wifi_networks,
               count * sizeof(*out_networks));
        *out_count = count;
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_upsert_wifi_network_legacy(const char *ssid, const char *password) {
    if (!ssid || !ssid[0] || strlen(ssid) >= CONFIGURATION_WIFI_VALUE_CAPACITY ||
        (password && strlen(password) >= CONFIGURATION_WIFI_VALUE_CAPACITY)) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(snapshot, &force_setup);
    if (err == ESP_OK) {
        upsert_network_in_snapshot(snapshot, ssid, password);
        err = write_locked(snapshot, force_setup);
    }
    unlock();
    return err;
}

static esp_err_t configuration_service_delete_wifi_network_legacy(const char *ssid) {
    if (!ssid || !ssid[0] || strlen(ssid) >= CONFIGURATION_WIFI_VALUE_CAPACITY) {
        return ESP_ERR_INVALID_ARG;
    }
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(snapshot, &force_setup);
    if (err == ESP_OK) {
        uint8_t found = snapshot->wifi_network_count;
        for (uint8_t i = 0; i < snapshot->wifi_network_count; ++i) {
            if (!strcmp(snapshot->wifi_networks[i].ssid, ssid)) {
                found = i;
                break;
            }
        }
        if (found == snapshot->wifi_network_count) {
            err = ESP_ERR_NOT_FOUND;
        } else {
            // 删除后前移后续条目，保持列表紧凑有序。
            memmove(&snapshot->wifi_networks[found],
                    &snapshot->wifi_networks[found + 1u],
                    (snapshot->wifi_network_count - found - 1u) *
                        sizeof(snapshot->wifi_networks[0]));
            --snapshot->wifi_network_count;
            memset(&snapshot->wifi_networks[snapshot->wifi_network_count], 0,
                   sizeof(snapshot->wifi_networks[0]));
            /* 主凭据若正是被删的个人热点，一并清除：否则重启后的单凭据回退
             * 会把刚删掉的网络又连回去。 */
            if (!strcmp(snapshot->wifi_ssid, ssid) &&
                strcmp(snapshot->wifi_security, "enterprise")) {
                snapshot->wifi_ssid[0] = '\0';
                snapshot->wifi_password[0] = '\0';
            }
            err = write_locked(snapshot, force_setup);
        }
    }
    unlock();
    return err;
}

/* Configuration owns an intentionally legacy private persistence implementation,
 * but callers must receive the stable Device contract used by every hardware
 * profile. Keep this translation at the service boundary only. */
static device_status_t configuration_status_from_legacy_error(esp_err_t err) {
    switch (err) {
        case ESP_OK: return DEVICE_STATUS_OK;
        case ESP_ERR_INVALID_ARG: return DEVICE_STATUS_INVALID_ARGUMENT;
        case ESP_ERR_INVALID_STATE: return DEVICE_STATUS_BUSY;
        case ESP_ERR_TIMEOUT: return DEVICE_STATUS_TIMEOUT;
        case ESP_ERR_NOT_FOUND: return DEVICE_STATUS_NOT_FOUND;
        case ESP_ERR_NO_MEM: return DEVICE_STATUS_RESOURCE_EXHAUSTED;
        default: return DEVICE_STATUS_INTERNAL_ERROR;
    }
}

device_status_t configuration_service_init(void) {
    return configuration_status_from_legacy_error(configuration_service_init_legacy());
}

device_status_t configuration_service_deinit(uint32_t timeout_ms) {
    return configuration_status_from_legacy_error(configuration_service_deinit_legacy(timeout_ms));
}

device_status_t configuration_service_load(configuration_snapshot_t *snapshot) {
    return configuration_status_from_legacy_error(configuration_service_load_legacy(snapshot));
}

device_status_t configuration_service_load_revisioned_snapshot(
    configuration_revisioned_snapshot_t *out_snapshot) {
    return configuration_status_from_legacy_error(
        configuration_service_load_revisioned_snapshot_legacy(out_snapshot));
}

device_status_t configuration_service_apply_runtime_override(
    const configuration_runtime_override_t *override) {
    return configuration_status_from_legacy_error(
        configuration_service_apply_runtime_override_legacy(override));
}

device_status_t configuration_service_remove_runtime_override(
    configuration_runtime_override_value_kind_t kind) {
    return configuration_status_from_legacy_error(
        configuration_service_remove_runtime_override_legacy(kind));
}

device_status_t configuration_service_clear_runtime_overrides(void) {
    return configuration_status_from_legacy_error(
        configuration_service_clear_runtime_overrides_legacy());
}

device_status_t configuration_service_next_runtime_override_expiry_ms(
    uint64_t *out_expires_at_monotonic_ms) {
    return configuration_status_from_legacy_error(
        configuration_service_next_runtime_override_expiry_ms_legacy(
            out_expires_at_monotonic_ms));
}

device_status_t configuration_service_load_effective_revisioned_snapshot(
    configuration_effective_revisioned_snapshot_t *out_snapshot) {
    return configuration_status_from_legacy_error(
        configuration_service_load_effective_revisioned_snapshot_legacy(out_snapshot));
}

device_status_t configuration_service_stage_provisioning(
    const configuration_provisioning_request_t *request) {
    return configuration_status_from_legacy_error(
        configuration_service_stage_provisioning_legacy(request));
}

device_status_t configuration_service_load_boot_candidate(configuration_snapshot_t *snapshot,
                                                           bool *out_staged) {
    return configuration_status_from_legacy_error(
        configuration_service_load_boot_candidate_legacy(snapshot, out_staged));
}

device_status_t configuration_service_begin_staged_provisioning_boot(void) {
    return configuration_status_from_legacy_error(
        configuration_service_begin_staged_provisioning_boot_legacy());
}

device_status_t configuration_service_rollback_staged_provisioning(void) {
    return configuration_status_from_legacy_error(
        configuration_service_rollback_staged_provisioning_legacy());
}

bool configuration_service_valid_pairing_code(const char *pair_code) {
    return configuration_transaction_valid_pairing_code(pair_code);
}

device_status_t configuration_service_set_pairing_code(const char *pair_code) {
    return configuration_status_from_legacy_error(configuration_service_set_pairing_code_legacy(pair_code));
}

device_status_t configuration_service_commit_gateway_pairing_token(const char *token) {
    return configuration_status_from_legacy_error(
        configuration_service_commit_gateway_pairing_token_legacy(token));
}

device_status_t configuration_service_set_output_volume(uint8_t percent) {
    return configuration_status_from_legacy_error(configuration_service_set_output_volume_legacy(percent));
}

device_status_t configuration_service_set_output_volume_with_policy(
    uint8_t percent, const configuration_policy_request_t *policy) {
    return configuration_status_from_legacy_error(
        configuration_service_set_output_volume_with_policy_legacy(percent, policy));
}

device_status_t configuration_service_set_output_volume_with_policy_revision(
    uint8_t percent, const configuration_policy_request_t *policy, uint64_t *out_revision) {
    return configuration_status_from_legacy_error(
        configuration_service_set_output_volume_with_policy_revision_legacy(percent, policy,
                                                                              out_revision));
}

device_status_t configuration_service_set_display_brightness(uint8_t percent) {
    return configuration_status_from_legacy_error(
        configuration_service_set_display_brightness_legacy(percent));
}

device_status_t configuration_service_set_display_brightness_with_policy(
    uint8_t percent, const configuration_policy_request_t *policy) {
    return configuration_status_from_legacy_error(
        configuration_service_set_display_brightness_with_policy_legacy(percent, policy));
}

bool configuration_service_valid_screen_sleep_seconds(uint32_t seconds) {
    return valid_screen_sleep_seconds_value(seconds);
}

device_status_t configuration_service_set_screen_sleep_seconds(uint32_t seconds) {
    return configuration_status_from_legacy_error(
        configuration_service_set_screen_sleep_seconds_legacy(seconds));
}

device_status_t configuration_service_set_screen_sleep_seconds_with_policy(
    uint32_t seconds, const configuration_policy_request_t *policy) {
    return configuration_status_from_legacy_error(
        configuration_service_set_screen_sleep_seconds_with_policy_legacy(seconds, policy));
}

device_status_t configuration_service_apply_display_policy_with_policy(
    const configuration_display_policy_update_t *update,
    const configuration_policy_request_t *policy,
    uint64_t *out_revision) {
    return configuration_status_from_legacy_error(
        configuration_service_apply_display_policy_with_policy_legacy(update, policy,
                                                                        out_revision));
}

device_status_t configuration_service_load_transport_selection(bool default_cellular,
                                                                bool *out_cellular,
                                                                bool *out_saved) {
    return configuration_status_from_legacy_error(
        configuration_service_load_transport_selection_legacy(default_cellular, out_cellular, out_saved));
}

device_status_t configuration_service_set_transport_selection(bool cellular) {
    return configuration_status_from_legacy_error(configuration_service_set_transport_selection_legacy(cellular));
}

device_status_t configuration_service_request_force_setup(void) {
    return configuration_status_from_legacy_error(configuration_service_request_force_setup_legacy());
}

device_status_t configuration_service_take_force_setup(bool *out_requested) {
    return configuration_status_from_legacy_error(configuration_service_take_force_setup_legacy(out_requested));
}

device_status_t configuration_service_list_wifi_networks(configuration_wifi_network_t *out_networks,
                                                          uint8_t capacity, uint8_t *out_count) {
    return configuration_status_from_legacy_error(
        configuration_service_list_wifi_networks_legacy(out_networks, capacity, out_count));
}

device_status_t configuration_service_upsert_wifi_network(const char *ssid, const char *password) {
    return configuration_status_from_legacy_error(
        configuration_service_upsert_wifi_network_legacy(ssid, password));
}

device_status_t configuration_service_delete_wifi_network(const char *ssid) {
    return configuration_status_from_legacy_error(configuration_service_delete_wifi_network_legacy(ssid));
}
