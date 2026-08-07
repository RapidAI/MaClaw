#include "weather_cache_service.h"

#include <string.h>

#include "nvs.h" /* legacy cache import error code */
#include "persistence_service.h"

#define WEATHER_CACHE_NAMESPACE "maclaw"
#define WEATHER_CACHE_STORE_KEY "weather_cache"
#define WEATHER_CACHE_STORE_MAGIC 0x57544331u /* WTC1 */
#define WEATHER_CACHE_STORE_VERSION 1u

typedef struct {
    uint32_t magic;
    uint32_t version;
    char summary[WEATHER_CACHE_SUMMARY_CAPACITY];
    char location[WEATHER_CACHE_LOCATION_CAPACITY];
    int32_t temperature_c;
    int64_t expires_at_ms;
} weather_cache_store_t;

static bool valid_store(const weather_cache_store_t *store) {
    return store && store->magic == WEATHER_CACHE_STORE_MAGIC &&
           store->version == WEATHER_CACHE_STORE_VERSION && store->summary[0] != '\0' &&
           memchr(store->summary, '\0', sizeof(store->summary)) != NULL &&
           memchr(store->location, '\0', sizeof(store->location)) != NULL &&
           store->temperature_c >= -80 && store->temperature_c <= 80;
}

static void clear_snapshot(weather_cache_snapshot_t *snapshot) {
    memset(snapshot, 0, sizeof(*snapshot));
}

static void copy_store_to_snapshot(const weather_cache_store_t *store,
                                   weather_cache_snapshot_t *snapshot) {
    clear_snapshot(snapshot);
    strlcpy(snapshot->summary, store->summary, sizeof(snapshot->summary));
    strlcpy(snapshot->location, store->location, sizeof(snapshot->location));
    snapshot->temperature_c = store->temperature_c;
    snapshot->expires_at_ms = store->expires_at_ms;
    snapshot->valid = true;
}

/* Import the original four independent cache keys once.  The cache is
 * non-critical, so a missing optional legacy field remains empty/zero; a
 * malformed summary is ignored rather than becoming visible UI content. */
static esp_err_t load_legacy(weather_cache_snapshot_t *snapshot, bool *out_found) {
    if (!snapshot || !out_found) return ESP_ERR_INVALID_ARG;
    clear_snapshot(snapshot);
    *out_found = false;
    size_t size = sizeof(snapshot->summary);
    esp_err_t err = persistence_service_read_string(WEATHER_CACHE_NAMESPACE, "weather",
                                                    snapshot->summary, &size);
    if (err == ESP_ERR_NVS_NOT_FOUND) return ESP_OK;
    if (err != ESP_OK) return err;
    if (!snapshot->summary[0]) return ESP_OK;
    snapshot->valid = true;
    *out_found = true;
    size = sizeof(snapshot->location);
    err = persistence_service_read_string(WEATHER_CACHE_NAMESPACE, "weather_loc",
                                          snapshot->location, &size);
    if (err != ESP_OK && err != ESP_ERR_NVS_NOT_FOUND) return err;
    int64_t expiry = 0;
    err = persistence_service_read_i64(WEATHER_CACHE_NAMESPACE, "weather_exp", &expiry);
    if (err == ESP_OK) snapshot->expires_at_ms = expiry;
    else if (err != ESP_ERR_NVS_NOT_FOUND) return err;
    int32_t temperature = 0;
    err = persistence_service_read_i32(WEATHER_CACHE_NAMESPACE, "weather_temp", &temperature);
    if (err == ESP_OK && temperature >= -80 && temperature <= 80) {
        snapshot->temperature_c = temperature;
    } else if (err != ESP_OK && err != ESP_ERR_NVS_NOT_FOUND) {
        return err;
    }
    return ESP_OK;
}

esp_err_t weather_cache_service_init(void) {
    return persistence_service_is_initialized() ? ESP_OK : ESP_ERR_INVALID_STATE;
}

esp_err_t weather_cache_service_load(weather_cache_snapshot_t *out_snapshot) {
    if (!out_snapshot || !persistence_service_is_initialized()) return ESP_ERR_INVALID_ARG;
    clear_snapshot(out_snapshot);
    weather_cache_store_t store = {0};
    size_t size = sizeof(store);
    esp_err_t err = persistence_service_read_blob(WEATHER_CACHE_NAMESPACE,
                                                  WEATHER_CACHE_STORE_KEY,
                                                  &store, &size);
    if (err == ESP_ERR_NVS_NOT_FOUND) {
        bool legacy_found = false;
        err = load_legacy(out_snapshot, &legacy_found);
        if (err != ESP_OK || !legacy_found) return err;
        return weather_cache_service_save(out_snapshot);
    }
    if (err != ESP_OK) return err;
    if (size != sizeof(store) || !valid_store(&store)) return ESP_ERR_INVALID_STATE;
    copy_store_to_snapshot(&store, out_snapshot);
    return ESP_OK;
}

esp_err_t weather_cache_service_save(const weather_cache_snapshot_t *snapshot) {
    if (!snapshot || !snapshot->valid || !snapshot->summary[0] ||
        snapshot->temperature_c < -80 || snapshot->temperature_c > 80 ||
        !persistence_service_is_initialized()) return ESP_ERR_INVALID_ARG;
    weather_cache_store_t store = {
        .magic = WEATHER_CACHE_STORE_MAGIC,
        .version = WEATHER_CACHE_STORE_VERSION,
        .temperature_c = snapshot->temperature_c,
        .expires_at_ms = snapshot->expires_at_ms,
    };
    strlcpy(store.summary, snapshot->summary, sizeof(store.summary));
    strlcpy(store.location, snapshot->location, sizeof(store.location));
    return persistence_service_write_blob(WEATHER_CACHE_NAMESPACE,
                                          WEATHER_CACHE_STORE_KEY,
                                          &store, sizeof(store));
}
