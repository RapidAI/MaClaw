#include "weather_cache_service.h"

#include <string.h>

#include "nvs.h" /* legacy cache import error code */
#include "persistence_service.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "esp_timer.h"

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

static portMUX_TYPE s_lifecycle_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_initialized;
static bool s_stopping;
static volatile bool s_initializing;
static uint32_t s_active_calls;

static TickType_t weather_stop_timeout_ticks(uint32_t timeout_ms) {
    TickType_t ticks = pdMS_TO_TICKS(timeout_ms);
    return ticks == 0 ? 1 : ticks;
}

static TickType_t weather_stop_remaining_ticks(TickType_t started, TickType_t budget) {
    const TickType_t elapsed = xTaskGetTickCount() - started;
    return elapsed >= budget ? 0 : budget - elapsed;
}

static bool admission_enter(void) {
    bool admitted = false;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_initialized && !s_stopping) {
        ++s_active_calls;
        admitted = true;
    }
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return admitted;
}

static void admission_exit(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_active_calls > 0) --s_active_calls;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

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
    if (!persistence_service_is_initialized()) return ESP_ERR_INVALID_STATE;
    bool expected = false;
    if (!__atomic_compare_exchange_n(&s_initializing, &expected, true, false,
                                     __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
        return ESP_ERR_INVALID_STATE;
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_initialized && !s_stopping) {
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_OK;
    }
    if (s_stopping) {
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
        return ESP_ERR_INVALID_STATE;
    }
    s_initialized = true;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    __atomic_store_n(&s_initializing, false, __ATOMIC_RELEASE);
    return ESP_OK;
}

esp_err_t weather_cache_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = weather_stop_timeout_ticks(timeout_ms);
    while (__atomic_load_n(&s_initializing, __ATOMIC_ACQUIRE)) {
        if (weather_stop_remaining_ticks(started, budget) == 0) return ESP_ERR_TIMEOUT;
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool already_stopped = !s_initialized && !s_stopping;
    s_initialized = false;
    s_stopping = true;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (already_stopped) return ESP_OK;
    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        const uint32_t active_calls = s_active_calls;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (active_calls == 0) break;
        if (weather_stop_remaining_ticks(started, budget) == 0) return ESP_ERR_TIMEOUT;
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_stopping = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return ESP_OK;
}

bool weather_cache_service_is_initialized(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    const bool initialized = s_initialized && !s_stopping &&
                             !__atomic_load_n(&s_initializing, __ATOMIC_ACQUIRE);
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return initialized;
}

esp_err_t weather_cache_service_load(weather_cache_snapshot_t *out_snapshot) {
    if (!out_snapshot) return ESP_ERR_INVALID_ARG;
    if (!admission_enter()) return ESP_ERR_INVALID_STATE;
    esp_err_t result = ESP_OK;
    clear_snapshot(out_snapshot);
    weather_cache_store_t store = {0};
    size_t size = sizeof(store);
    esp_err_t err = persistence_service_read_blob(WEATHER_CACHE_NAMESPACE,
                                                  WEATHER_CACHE_STORE_KEY,
                                                  &store, &size);
    if (err == ESP_ERR_NVS_NOT_FOUND) {
        bool legacy_found = false;
        err = load_legacy(out_snapshot, &legacy_found);
        if (err != ESP_OK || !legacy_found) {
            result = err;
            goto done;
        }
        weather_cache_store_t legacy_store = {
            .magic = WEATHER_CACHE_STORE_MAGIC,
            .version = WEATHER_CACHE_STORE_VERSION,
            .temperature_c = out_snapshot->temperature_c,
            .expires_at_ms = out_snapshot->expires_at_ms,
        };
        strlcpy(legacy_store.summary, out_snapshot->summary, sizeof(legacy_store.summary));
        strlcpy(legacy_store.location, out_snapshot->location, sizeof(legacy_store.location));
        result = persistence_service_write_blob(WEATHER_CACHE_NAMESPACE,
                                                WEATHER_CACHE_STORE_KEY,
                                                &legacy_store, sizeof(legacy_store));
        goto done;
    }
    if (err != ESP_OK) {
        result = err;
        goto done;
    }
    if (size != sizeof(store) || !valid_store(&store)) {
        result = ESP_ERR_INVALID_STATE;
        goto done;
    }
    copy_store_to_snapshot(&store, out_snapshot);
done:
    admission_exit();
    return result;
}

esp_err_t weather_cache_service_save(const weather_cache_snapshot_t *snapshot) {
    if (!snapshot || !snapshot->valid || !snapshot->summary[0] ||
        snapshot->temperature_c < -80 || snapshot->temperature_c > 80 ||
        !admission_enter()) return ESP_ERR_INVALID_ARG;
    weather_cache_store_t store = {
        .magic = WEATHER_CACHE_STORE_MAGIC,
        .version = WEATHER_CACHE_STORE_VERSION,
        .temperature_c = snapshot->temperature_c,
        .expires_at_ms = snapshot->expires_at_ms,
    };
    strlcpy(store.summary, snapshot->summary, sizeof(store.summary));
    strlcpy(store.location, snapshot->location, sizeof(store.location));
    esp_err_t err = persistence_service_write_blob(WEATHER_CACHE_NAMESPACE,
                                                    WEATHER_CACHE_STORE_KEY,
                                                    &store, sizeof(store));
    admission_exit();
    return err;
}
