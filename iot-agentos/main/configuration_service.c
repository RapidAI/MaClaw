#include "configuration_service.h"

#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "nvs.h" /* legacy schema import error code */
#include "persistence_service.h"

#define CONFIGURATION_NAMESPACE "maclaw"
#define CONFIGURATION_STORE_KEY "configuration"
#define CONFIGURATION_STORE_MAGIC 0x43464731u /* CFG1 */
#define CONFIGURATION_STORE_VERSION 2u

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

static SemaphoreHandle_t s_mutation_lock;
/* Compile-time defaults become the mutation seed after the composition root
 * has loaded them.  This prevents a first-time pairing/volume write from
 * replacing a configured firmware default with empty network fields. */
static configuration_snapshot_t s_default_snapshot;
static bool s_default_snapshot_available;

static bool lock(void) {
    return s_mutation_lock &&
           xSemaphoreTake(s_mutation_lock, pdMS_TO_TICKS(3000)) == pdTRUE;
}

static void unlock(void) {
    if (s_mutation_lock) xSemaphoreGive(s_mutation_lock);
}

static bool string_terminated(const char *value, size_t capacity) {
    return value && memchr(value, '\0', capacity) != NULL;
}

static bool valid_snapshot(const configuration_snapshot_t *snapshot) {
    return snapshot && snapshot->output_volume <= 100 &&
           string_terminated(snapshot->wifi_ssid, sizeof(snapshot->wifi_ssid)) &&
           string_terminated(snapshot->wifi_password, sizeof(snapshot->wifi_password)) &&
           string_terminated(snapshot->wifi_security, sizeof(snapshot->wifi_security)) &&
           string_terminated(snapshot->wifi_eap_method, sizeof(snapshot->wifi_eap_method)) &&
           string_terminated(snapshot->wifi_identity, sizeof(snapshot->wifi_identity)) &&
           string_terminated(snapshot->wifi_username, sizeof(snapshot->wifi_username)) &&
           string_terminated(snapshot->wifi_ttls_phase2, sizeof(snapshot->wifi_ttls_phase2)) &&
           string_terminated(snapshot->wifi_ca_mode, sizeof(snapshot->wifi_ca_mode)) &&
           string_terminated(snapshot->wifi_server_domain, sizeof(snapshot->wifi_server_domain)) &&
           string_terminated(snapshot->gateway_url, sizeof(snapshot->gateway_url)) &&
           string_terminated(snapshot->pair_code, sizeof(snapshot->pair_code)) &&
           string_terminated(snapshot->gateway_token, sizeof(snapshot->gateway_token));
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

static esp_err_t load_locked(configuration_snapshot_t *inout_snapshot,
                             bool *out_force_setup) {
    if (!valid_snapshot(inout_snapshot) || !out_force_setup) return ESP_ERR_INVALID_ARG;
    *out_force_setup = false;
    configuration_store_t store = {0};
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
        if (err != ESP_OK) return err;
        if (!legacy_found) return ESP_OK;
        store.magic = CONFIGURATION_STORE_MAGIC;
        store.version = CONFIGURATION_STORE_VERSION;
        store.snapshot = *inout_snapshot;
        store.force_setup = *out_force_setup ? 1u : 0u;
        err = persistence_service_write_blob(CONFIGURATION_NAMESPACE,
                                             CONFIGURATION_STORE_KEY,
                                             &store, sizeof(store));
        return err;
    }
    if (err != ESP_OK) return err;
    if (size == sizeof(configuration_store_v1_t)) {
        configuration_store_v1_t legacy = {0};
        size = sizeof(legacy);
        err = persistence_service_read_blob(CONFIGURATION_NAMESPACE,
                                            CONFIGURATION_STORE_KEY,
                                            &legacy, &size);
        if (err != ESP_OK || legacy.magic != CONFIGURATION_STORE_MAGIC ||
            legacy.version != 1u || legacy.force_setup > 1) return ESP_ERR_INVALID_STATE;
        configuration_snapshot_t migrated = {0};
        memcpy(&migrated, &legacy.snapshot, sizeof(legacy.snapshot));
        if (!valid_snapshot(&migrated)) return ESP_ERR_INVALID_STATE;
        store = (configuration_store_t){
            .magic = CONFIGURATION_STORE_MAGIC,
            .version = CONFIGURATION_STORE_VERSION,
            .snapshot = migrated,
            .force_setup = legacy.force_setup,
        };
        /* V1 images still kept Fangtang's normalized selection in the
         * transitional scalar key.  Fold it into the expanded snapshot during
         * the same schema migration, without exposing that key to callers. */
        uint8_t transport = 0;
        esp_err_t transport_err = persistence_service_read_u8(
            CONFIGURATION_NAMESPACE, "net_transport", &transport);
        if (transport_err == ESP_OK) {
            if (transport > 1) return ESP_ERR_INVALID_STATE;
            store.snapshot.cellular_transport_selected = transport != 0;
            store.snapshot.cellular_transport_selection_saved = true;
        } else if (transport_err != ESP_ERR_NVS_NOT_FOUND) {
            return transport_err;
        }
        err = persistence_service_write_blob(CONFIGURATION_NAMESPACE,
                                             CONFIGURATION_STORE_KEY,
                                             &store, sizeof(store));
        if (err != ESP_OK) return err;
    } else if (size == sizeof(store)) {
        size = sizeof(store);
        err = persistence_service_read_blob(CONFIGURATION_NAMESPACE,
                                            CONFIGURATION_STORE_KEY,
                                            &store, &size);
        if (err != ESP_OK || !valid_store(&store)) return ESP_ERR_INVALID_STATE;
    } else {
        return ESP_ERR_INVALID_STATE;
    }
    *inout_snapshot = store.snapshot;
    *out_force_setup = store.force_setup != 0;
    return ESP_OK;
}

static esp_err_t write_locked(const configuration_snapshot_t *snapshot, bool force_setup) {
    if (!valid_snapshot(snapshot)) return ESP_ERR_INVALID_ARG;
    configuration_store_t store = {
        .magic = CONFIGURATION_STORE_MAGIC,
        .version = CONFIGURATION_STORE_VERSION,
        .snapshot = *snapshot,
        .force_setup = force_setup ? 1u : 0u,
    };
    return persistence_service_write_blob(CONFIGURATION_NAMESPACE,
                                          CONFIGURATION_STORE_KEY,
                                          &store, sizeof(store));
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
    if (!s_mutation_lock) s_mutation_lock = xSemaphoreCreateMutex();
    return s_mutation_lock ? ESP_OK : ESP_ERR_NO_MEM;
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
    /* The portal owns network and pairing fields, but it does not own the
     * selected physical uplink nor the one-shot setup request.  Preserve them
     * from the authoritative durable snapshot rather than trusting a stale
     * caller copy assembled in main.c. */
    configuration_snapshot_t current;
    seed_snapshot(&current);
    bool force_setup = false;
    esp_err_t err = load_locked(&current, &force_setup);
    if (err == ESP_OK) {
        configuration_snapshot_t next = *snapshot;
        next.cellular_transport_selected = current.cellular_transport_selected;
        next.cellular_transport_selection_saved =
            current.cellular_transport_selection_saved;
        next.gateway_token[0] = '\0';
        err = write_locked(&next, false);
    }
    unlock();
    return err;
}

static esp_err_t mutate_string(const char *value, size_t capacity, bool token,
                               bool clear_pair_code) {
    if (!value || !value[0] || strlen(value) >= capacity) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    configuration_snapshot_t snapshot;
    /* A mutation must retain all existing values.  Seed strings are only used
     * if a pre-service image had no configuration blob nor legacy keys. */
    seed_snapshot(&snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(&snapshot, &force_setup);
    if (err == ESP_OK) {
        if (token) {
            strlcpy(snapshot.gateway_token, value, sizeof(snapshot.gateway_token));
            if (clear_pair_code) snapshot.pair_code[0] = '\0';
        } else {
            strlcpy(snapshot.pair_code, value, sizeof(snapshot.pair_code));
        }
        err = write_locked(&snapshot, force_setup);
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
    configuration_snapshot_t snapshot;
    seed_snapshot(&snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(&snapshot, &force_setup);
    if (err == ESP_OK) {
        snapshot.output_volume = percent;
        snapshot.output_volume_saved = true;
        err = write_locked(&snapshot, force_setup);
    }
    unlock();
    return err;
}

esp_err_t configuration_service_load_transport_selection(bool default_cellular,
                                                         bool *out_cellular,
                                                         bool *out_saved) {
    if (!out_cellular || !out_saved) return ESP_ERR_INVALID_ARG;
    if (!lock()) return ESP_ERR_TIMEOUT;
    configuration_snapshot_t snapshot;
    seed_snapshot(&snapshot);
    bool ignored_force_setup = false;
    esp_err_t err = load_locked(&snapshot, &ignored_force_setup);
    if (err == ESP_OK) {
        *out_cellular = snapshot.cellular_transport_selection_saved
                            ? snapshot.cellular_transport_selected
                            : default_cellular;
        *out_saved = snapshot.cellular_transport_selection_saved;
    }
    unlock();
    return err;
}

esp_err_t configuration_service_set_transport_selection(bool cellular) {
    if (!lock()) return ESP_ERR_TIMEOUT;
    configuration_snapshot_t snapshot;
    seed_snapshot(&snapshot);
    bool force_setup = false;
    esp_err_t err = load_locked(&snapshot, &force_setup);
    if (err == ESP_OK) {
        snapshot.cellular_transport_selected = cellular;
        snapshot.cellular_transport_selection_saved = true;
        err = write_locked(&snapshot, force_setup);
    }
    unlock();
    return err;
}

esp_err_t configuration_service_request_force_setup(void) {
    if (!lock()) return ESP_ERR_TIMEOUT;
    configuration_snapshot_t snapshot;
    seed_snapshot(&snapshot);
    bool ignored = false;
    esp_err_t err = load_locked(&snapshot, &ignored);
    if (err == ESP_OK) err = write_locked(&snapshot, true);
    unlock();
    return err;
}

esp_err_t configuration_service_take_force_setup(bool *out_requested) {
    if (!out_requested || !lock()) return ESP_ERR_TIMEOUT;
    configuration_snapshot_t snapshot;
    seed_snapshot(&snapshot);
    bool requested = false;
    esp_err_t err = load_locked(&snapshot, &requested);
    if (err == ESP_OK && requested) err = write_locked(&snapshot, false);
    unlock();
    if (err == ESP_OK) *out_requested = requested;
    return err;
}
