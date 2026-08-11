#include "configuration_service.h"

#include <string.h>

#include "esp_heap_caps.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "nvs.h" /* legacy schema import error code */
#include "persistence_service.h"

#define CONFIGURATION_NAMESPACE "maclaw"
#define CONFIGURATION_STORE_KEY "configuration"
#define CONFIGURATION_STORE_MAGIC 0x43464731u /* CFG1 */
#define CONFIGURATION_STORE_VERSION 3u

typedef struct {
    uint32_t magic;
    uint32_t version;
    configuration_snapshot_t snapshot;
    uint8_t force_setup;
    uint8_t reserved[3];
} configuration_store_t;

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
static bool s_stopping;
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
        xSemaphoreTake(s_mutation_lock, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return false;
    }
    const bool admitted = !__atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE) &&
                          s_scratch_snapshot_a && s_scratch_snapshot_b &&
                          s_scratch_store;
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

/* A provisioning save changes the primary credential and the personal-network
 * catalogue as one user-visible operation.  Keep the catalogue edit on the
 * same in-memory snapshot so one Persistence Service commit makes both durable
 * together; a reboot can never retain a new primary Wi-Fi credential while
 * silently losing its multi-network recovery entry. */
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
            /* The service's documented catalogue policy is FIFO eviction. */
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
           store->version == CONFIGURATION_STORE_VERSION && store->force_setup <= 1 &&
           valid_snapshot(&store->snapshot);
}

static esp_err_t read_optional_string(const char *key, char *out, size_t capacity,
                                      bool *out_found) {
    size_t size = capacity;
    esp_err_t err = persistence_service_read_string(CONFIGURATION_NAMESPACE, key, out, &size);
    if (err == ESP_OK) {
        *out_found = true;
        return ESP_OK;
    }
    return err == ESP_ERR_NVS_NOT_FOUND ? ESP_OK : err;
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
    esp_err_t err = persistence_service_read_u8(CONFIGURATION_NAMESPACE, "output_vol", &volume);
    if (err == ESP_OK) {
        if (volume > 100) return ESP_ERR_INVALID_STATE;
        snapshot->output_volume = volume;
        snapshot->output_volume_saved = true;
        *out_found = true;
    } else if (err != ESP_ERR_NVS_NOT_FOUND) return err;
    uint8_t force_setup = 0;
    err = persistence_service_read_u8(CONFIGURATION_NAMESPACE, "force_setup", &force_setup);
    if (err == ESP_OK) {
        if (force_setup > 1) return ESP_ERR_INVALID_STATE;
        *out_force_setup = force_setup != 0;
        *out_found = true;
    } else if (err != ESP_ERR_NVS_NOT_FOUND) return err;
    uint8_t transport = 0;
    err = persistence_service_read_u8(CONFIGURATION_NAMESPACE, "net_transport", &transport);
    if (err == ESP_OK) {
        if (transport > 1) return ESP_ERR_INVALID_STATE;
        snapshot->cellular_transport_selected = transport != 0;
        snapshot->cellular_transport_selection_saved = true;
        *out_found = true;
    } else if (err != ESP_ERR_NVS_NOT_FOUND) return err;
    return valid_snapshot(snapshot) ? ESP_OK : ESP_ERR_INVALID_STATE;
}

/* V1 migration holds its own large by-value frames.  Keeping it out of
 * load_locked() means the steady-state V2 read path never reserves stack for
 * a migration that already happened. */
static esp_err_t migrate_v1_locked(configuration_store_t *store) {
    configuration_store_v1_t *legacy =
        heap_caps_calloc(1, sizeof(*legacy), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!legacy) return ESP_ERR_NO_MEM;
    size_t size = sizeof(*legacy);
    esp_err_t err = persistence_service_read_blob(CONFIGURATION_NAMESPACE,
                                                  CONFIGURATION_STORE_KEY,
                                                  legacy, &size);
    if (err != ESP_OK || legacy->magic != CONFIGURATION_STORE_MAGIC ||
        legacy->version != 1u || legacy->force_setup > 1) {
        heap_caps_free(legacy);
        return ESP_ERR_INVALID_STATE;
    }
    store->magic = CONFIGURATION_STORE_MAGIC;
    store->version = CONFIGURATION_STORE_VERSION;
    memcpy(&store->snapshot, &legacy->snapshot, sizeof(legacy->snapshot));
    store->force_setup = legacy->force_setup;
    heap_caps_free(legacy);
    if (!valid_snapshot(&store->snapshot)) return ESP_ERR_INVALID_STATE;
    // 单组凭据迁成列表第 1 条（仅个人热点）。
    sync_networks_from_primary(&store->snapshot);
    /* V1 images still kept Fangtang's normalized selection in the
     * transitional scalar key.  Fold it into the expanded snapshot during
     * the same schema migration, without exposing that key to callers. */
    uint8_t transport = 0;
    esp_err_t transport_err = persistence_service_read_u8(
        CONFIGURATION_NAMESPACE, "net_transport", &transport);
    if (transport_err == ESP_OK) {
        if (transport > 1) return ESP_ERR_INVALID_STATE;
        store->snapshot.cellular_transport_selected = transport != 0;
        store->snapshot.cellular_transport_selection_saved = true;
    } else if (transport_err != ESP_ERR_NVS_NOT_FOUND) {
        return transport_err;
    }
    return persistence_service_write_blob(CONFIGURATION_NAMESPACE,
                                          CONFIGURATION_STORE_KEY,
                                          store, sizeof(*store));
}

/* V2 -> V3：快照尾部追加多热点列表。store 已被调用方清零，前缀整体拷贝后
 * 只需把主凭据同步进列表。 */
static esp_err_t migrate_v2_locked(configuration_store_t *store) {
    configuration_store_v2_t *legacy =
        heap_caps_calloc(1, sizeof(*legacy), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!legacy) return ESP_ERR_NO_MEM;
    size_t size = sizeof(*legacy);
    esp_err_t err = persistence_service_read_blob(CONFIGURATION_NAMESPACE,
                                                  CONFIGURATION_STORE_KEY,
                                                  legacy, &size);
    if (err != ESP_OK || legacy->magic != CONFIGURATION_STORE_MAGIC ||
        legacy->version != 2u || legacy->force_setup > 1) {
        heap_caps_free(legacy);
        return ESP_ERR_INVALID_STATE;
    }
    store->magic = CONFIGURATION_STORE_MAGIC;
    store->version = CONFIGURATION_STORE_VERSION;
    memcpy(&store->snapshot, &legacy->snapshot, sizeof(legacy->snapshot));
    store->force_setup = legacy->force_setup;
    heap_caps_free(legacy);
    if (!valid_snapshot(&store->snapshot)) return ESP_ERR_INVALID_STATE;
    sync_networks_from_primary(&store->snapshot);
    return persistence_service_write_blob(CONFIGURATION_NAMESPACE,
                                          CONFIGURATION_STORE_KEY,
                                          store, sizeof(*store));
}

static esp_err_t load_locked(configuration_snapshot_t *inout_snapshot,
                             bool *out_force_setup) {
    if (!valid_snapshot(inout_snapshot) || !out_force_setup) return ESP_ERR_INVALID_ARG;
    *out_force_setup = false;
    /* The V2 store is close to a kilobyte and callers up the chain already
     * hold their own snapshot by value; measured main-task stack peaked at
     * ~4.9 KB through this path.  Use the lock-protected PSRAM scratch store;
     * it never escapes this service. */
    if (!s_scratch_store) return ESP_ERR_INVALID_STATE;
    configuration_store_t *store = s_scratch_store;
    memset(store, 0, sizeof(*store));
    /* Query length before supplying a V2 buffer.  NVS rejects an older,
     * smaller V1 blob when given the larger V2 buffer, which would otherwise
     * make a valid expand migration indistinguishable from corruption. */
    size_t size = 0;
    esp_err_t err = persistence_service_read_blob(CONFIGURATION_NAMESPACE,
                                                  CONFIGURATION_STORE_KEY,
                                                  NULL, &size);
    if (err == ESP_ERR_NVS_NOT_FOUND) {
        bool legacy_found = false;
        err = load_legacy(inout_snapshot, &legacy_found, out_force_setup);
        if (err == ESP_OK && legacy_found) {
            // 散落的旧标量 key 同样只有单组凭据，入库前迁成列表第 1 条。
            sync_networks_from_primary(inout_snapshot);
            store->magic = CONFIGURATION_STORE_MAGIC;
            store->version = CONFIGURATION_STORE_VERSION;
            store->snapshot = *inout_snapshot;
            store->force_setup = *out_force_setup ? 1u : 0u;
            err = persistence_service_write_blob(CONFIGURATION_NAMESPACE,
                                                 CONFIGURATION_STORE_KEY,
                                                 store, sizeof(*store));
        }
        return err;
    }
    if (err != ESP_OK) return err;
    if (size == sizeof(configuration_store_v1_t)) {
        err = migrate_v1_locked(store);
        if (err != ESP_OK) return err;
    } else if (size == sizeof(configuration_store_v2_t)) {
        err = migrate_v2_locked(store);
        if (err != ESP_OK) return err;
    } else if (size == sizeof(*store)) {
        err = persistence_service_read_blob(CONFIGURATION_NAMESPACE,
                                            CONFIGURATION_STORE_KEY,
                                            store, &size);
        if (err != ESP_OK || !valid_store(store)) return ESP_ERR_INVALID_STATE;
    } else {
        return ESP_ERR_INVALID_STATE;
    }
    *inout_snapshot = store->snapshot;
    *out_force_setup = store->force_setup != 0;
    return ESP_OK;
}

static esp_err_t write_locked(const configuration_snapshot_t *snapshot, bool force_setup) {
    if (!valid_snapshot(snapshot)) return ESP_ERR_INVALID_ARG;
    if (!s_scratch_store) return ESP_ERR_INVALID_STATE;
    configuration_store_t *store = s_scratch_store;
    *store = (configuration_store_t){
        .magic = CONFIGURATION_STORE_MAGIC,
        .version = CONFIGURATION_STORE_VERSION,
        .snapshot = *snapshot,
        .force_setup = force_setup ? 1u : 0u,
    };
    return persistence_service_write_blob(CONFIGURATION_NAMESPACE,
                                          CONFIGURATION_STORE_KEY,
                                          store, sizeof(*store));
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

esp_err_t configuration_service_init(void) {
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
    bool ready = s_mutation_lock && s_scratch_snapshot_a && s_scratch_snapshot_b &&
                 s_scratch_store;
    if (!ready) {
        xSemaphoreGive(s_deinit_lock);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_ERR_NO_MEM;
    }
    __atomic_store_n(&s_stopping, false, __ATOMIC_RELEASE);
    xSemaphoreGive(s_deinit_lock);
    __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
    return ESP_OK;
}

esp_err_t configuration_service_deinit(uint32_t timeout_ms) {
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
           s_scratch_snapshot_b && s_scratch_store &&
           !__atomic_load_n(&s_initializing, __ATOMIC_ACQUIRE) &&
           !__atomic_load_n(&s_stopping, __ATOMIC_ACQUIRE);
}

esp_err_t configuration_service_load(configuration_snapshot_t *inout_snapshot) {
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

esp_err_t configuration_service_save_provisioning(const configuration_snapshot_t *snapshot) {
    if (!snapshot) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a || !s_scratch_snapshot_b) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    /* The portal owns network and pairing fields, but it does not own the
     * selected physical uplink nor the one-shot setup request.  Preserve them
     * from the authoritative durable snapshot rather than trusting a stale
     * caller copy assembled in main.c. */
    configuration_snapshot_t *current = s_scratch_snapshot_a;
    configuration_snapshot_t *next = s_scratch_snapshot_b;
    seed_snapshot(current);
    bool force_setup = false;
    esp_err_t err = load_locked(current, &force_setup);
    if (err == ESP_OK) {
        *next = *snapshot;
        next->cellular_transport_selected = current->cellular_transport_selected;
        next->cellular_transport_selection_saved =
            current->cellular_transport_selection_saved;
        /* 多热点列表同样归服务侧管理：调用方快照不含列表知识，不能让它在
         * 配网保存时把已存热点清掉。对于个人 Wi-Fi，主凭据与目录项必须
         * 在这一个 commit 中一起更新，不能依赖门户返回后的第二笔写入。 */
        next->wifi_network_count = current->wifi_network_count;
        memcpy(next->wifi_networks, current->wifi_networks,
               sizeof(next->wifi_networks));
        if (strcmp(next->wifi_security, "enterprise")) {
            upsert_network_in_snapshot(next, next->wifi_ssid,
                                       next->wifi_password);
        }
        next->gateway_token[0] = '\0';
        err = write_locked(next, false);
    }
    unlock();
    return err;
}

static esp_err_t mutate_string(const char *value, size_t capacity, bool token,
                               bool clear_pair_code) {
    if (!value || !value[0] || strlen(value) >= capacity) return ESP_ERR_INVALID_ARG;
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
        if (token) {
            strlcpy(snapshot->gateway_token, value, sizeof(snapshot->gateway_token));
            if (clear_pair_code) snapshot->pair_code[0] = '\0';
        } else {
            strlcpy(snapshot->pair_code, value, sizeof(snapshot->pair_code));
        }
        err = write_locked(snapshot, force_setup);
    }
    unlock();
    return err;
}

esp_err_t configuration_service_set_pairing_code(const char *pair_code) {
    return mutate_string(pair_code, CONFIGURATION_PAIR_CODE_CAPACITY, false, false);
}

esp_err_t configuration_service_set_gateway_token(const char *token,
                                                   bool clear_pair_code) {
    return mutate_string(token, CONFIGURATION_GATEWAY_TOKEN_CAPACITY, true,
                         clear_pair_code);
}

esp_err_t configuration_service_set_output_volume(uint8_t percent) {
    if (percent > 100) return ESP_ERR_INVALID_ARG;
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
    }
    unlock();
    return err;
}

esp_err_t configuration_service_load_transport_selection(bool default_cellular,
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

esp_err_t configuration_service_set_transport_selection(bool cellular) {
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

esp_err_t configuration_service_request_force_setup(void) {
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

esp_err_t configuration_service_take_force_setup(bool *out_requested) {
    if (!out_requested || !lock()) return ESP_ERR_TIMEOUT;
    if (!s_scratch_snapshot_a) {
        unlock();
        return ESP_ERR_INVALID_STATE;
    }
    configuration_snapshot_t *snapshot = s_scratch_snapshot_a;
    seed_snapshot(snapshot);
    bool requested = false;
    esp_err_t err = load_locked(snapshot, &requested);
    if (err == ESP_OK && requested) err = write_locked(snapshot, false);
    unlock();
    if (err == ESP_OK) *out_requested = requested;
    return err;
}

esp_err_t configuration_service_list_wifi_networks(configuration_wifi_network_t *out_networks,
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

esp_err_t configuration_service_upsert_wifi_network(const char *ssid, const char *password) {
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

esp_err_t configuration_service_delete_wifi_network(const char *ssid) {
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
