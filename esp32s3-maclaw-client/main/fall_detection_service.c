#include "fall_detection_service.h"

#include <limits.h>
#include <stdio.h>
#include <string.h>

#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs.h"
#include "persistence_service.h"

/* Raw samples arrive at 125 Hz on the current COM6 adapter.  Sampling the
 * HAL at 10 Hz keeps shared-I2C traffic bounded without classifying a single
 * noisy sample as a fall.  These are deliberately conservative engineering
 * defaults, not medical thresholds; field calibration remains required. */
#define FALL_SAMPLE_PERIOD_MS 100u
#define FALL_FREEFALL_MAGNITUDE_MG 350
#define FALL_FREEFALL_MIN_US 100000LL
#define FALL_IMPACT_MAGNITUDE_MG 2500
#define FALL_IMPACT_WINDOW_US 1500000LL
#define FALL_STILL_MAGNITUDE_MIN_MG 800
#define FALL_STILL_MAGNITUDE_MAX_MG 1200
#define FALL_STILL_MAX_GYRO_MDPS 100000
#define FALL_STILL_MIN_US 3000000LL
#define FALL_ORIENTATION_COS_PERMILLE 707
#define FALL_CONFIRMATION_WINDOW_US 15000000LL
#define FALL_DETECTION_NAMESPACE "fall_detect"
#define FALL_DETECTION_STORE_KEY "config"
#define FALL_DETECTION_STORE_MAGIC 0x46444331u /* FDC1 */
#define FALL_DETECTION_STORE_VERSION 2u
#define FALL_DETECTION_REPLAY_COUNT 4u
#define FALL_DETECTION_IDEMPOTENCY_KEY_CAPACITY 64u
#define FALL_DETECTION_ERROR_CAPACITY 112u

static const char *TAG = "fall_detection";

typedef enum {
    CLASSIFIER_MONITORING = 0,
    CLASSIFIER_FREEFALL,
    CLASSIFIER_POST_IMPACT,
} classifier_state_t;

typedef struct {
    bool have_baseline;
    int32_t baseline_x;
    int32_t baseline_y;
    int32_t baseline_z;
    int64_t freefall_start_us;
    int64_t impact_us;
    int64_t still_start_us;
    bool orientation_changed;
    classifier_state_t state;
} fall_classifier_t;

typedef struct {
    uint32_t magic;
    uint32_t version;
    uint8_t enabled;
    uint8_t reserved[3];
    uint32_t revision;
} fall_detection_store_v1_t;

typedef struct {
    char key[FALL_DETECTION_IDEMPOTENCY_KEY_CAPACITY];
    int32_t status;
    uint8_t enabled;
    uint8_t reserved[3];
    uint32_t configuration_revision;
    char detail[FALL_DETECTION_ERROR_CAPACITY];
} fall_detection_replay_t;

typedef struct {
    uint32_t magic;
    uint32_t version;
    uint8_t enabled;
    uint8_t reserved[3];
    uint32_t revision;
    uint32_t replay_next;
    fall_detection_replay_t replay[FALL_DETECTION_REPLAY_COUNT];
} fall_detection_store_t;

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_initialized;
static bool s_available;
static bool s_enabled;
static uint32_t s_configuration_revision;
static fall_detection_state_t s_state = FALL_DETECTION_STATE_UNAVAILABLE;
static uint32_t s_suspected_count;
static uint64_t s_confirmation_deadline_us;
/* Changes whenever a candidate window becomes invalid.  Callbacks run outside
 * the service lock because UI/display work can block, so this generation
 * prevents a disable/re-enable request from presenting a stale candidate. */
static uint32_t s_event_generation;
static fall_detection_callback_t s_callback;
static void *s_callback_context;
static device_power_lease_t s_confirmation_lease = DEVICE_POWER_LEASE_INVALID;
static TaskHandle_t s_task;
static SemaphoreHandle_t s_stopped;
static volatile bool s_stop_requested;
static portMUX_TYPE s_lifecycle_lock = portMUX_INITIALIZER_UNLOCKED;
static uint32_t s_tool_admissions;
/* Serialises the read/check/write replay transaction.  Persistence Service
 * serialises individual NVS calls; it cannot make a load followed by save
 * atomic relative to another Gateway retry. */
static SemaphoreHandle_t s_mutation_mutex;

static bool admit_tool(void) {
    bool admitted = false;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_initialized && !s_stop_requested) {
        ++s_tool_admissions;
        admitted = true;
    }
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    return admitted;
}

static void release_tool(void) {
    taskENTER_CRITICAL(&s_lifecycle_lock);
    if (s_tool_admissions > 0) --s_tool_admissions;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
}

static bool has_configured_motion_sensor(void) {
    return device_profile_has_capability(DEVICE_CAPABILITY_MOTION_SENSOR);
}

static bool valid_store(const fall_detection_store_t *store) {
    return store && store->magic == FALL_DETECTION_STORE_MAGIC &&
           store->version == FALL_DETECTION_STORE_VERSION && store->enabled <= 1;
}

/* A missing namespace is a normal first boot.  Other read errors are treated
 * as a startup failure instead of silently converting user safety preferences
 * to an implicit default. */
static esp_err_t load_store(fall_detection_store_t *out_store, bool *out_migrated) {
    if (!out_store || !persistence_service_is_initialized()) return ESP_ERR_INVALID_STATE;
    if (out_migrated) *out_migrated = false;
    fall_detection_store_t store = {
        .magic = FALL_DETECTION_STORE_MAGIC,
        .version = FALL_DETECTION_STORE_VERSION,
        .enabled = 1,
        .revision = 1,
    };
    size_t size = 0;
    esp_err_t err = persistence_service_read_blob(FALL_DETECTION_NAMESPACE,
                                                  FALL_DETECTION_STORE_KEY,
                                                  NULL, &size);
    if (err == ESP_ERR_NVS_NOT_FOUND) {
        *out_store = store;
        return ESP_OK;
    }
    if (err != ESP_OK) return err;
    if (size == sizeof(fall_detection_store_v1_t)) {
        fall_detection_store_v1_t legacy = {0};
        err = persistence_service_read_blob(FALL_DETECTION_NAMESPACE,
                                            FALL_DETECTION_STORE_KEY, &legacy, &size);
        if (err != ESP_OK || legacy.magic != FALL_DETECTION_STORE_MAGIC ||
            legacy.version != 1u || legacy.enabled > 1) return ESP_ERR_INVALID_STATE;
        store = (fall_detection_store_t){
            .magic = FALL_DETECTION_STORE_MAGIC,
            .version = FALL_DETECTION_STORE_VERSION,
            .enabled = legacy.enabled,
            .revision = legacy.revision ? legacy.revision : 1u,
        };
        if (out_migrated) *out_migrated = true;
    } else if (size == sizeof(store)) {
        err = persistence_service_read_blob(FALL_DETECTION_NAMESPACE,
                                            FALL_DETECTION_STORE_KEY, &store, &size);
        if (err != ESP_OK || !valid_store(&store)) return ESP_ERR_INVALID_STATE;
    } else {
        return ESP_ERR_INVALID_SIZE;
    }
    if (store.revision == 0) store.revision = 1;
    *out_store = store;
    return ESP_OK;
}

static bool valid_idempotency_key(const char *key) {
    if (!key || !key[0]) return false;
    size_t length = strlen(key);
    if (length >= FALL_DETECTION_IDEMPOTENCY_KEY_CAPACITY) return false;
    for (size_t i = 0; i < length; ++i) {
        if ((unsigned char)key[i] > 0x7fu) return false;
    }
    return true;
}

static esp_err_t save_store(const fall_detection_store_t *store) {
    if (!valid_store(store)) return ESP_ERR_INVALID_ARG;
    return persistence_service_write_blob(FALL_DETECTION_NAMESPACE,
                                          FALL_DETECTION_STORE_KEY,
                                          store, sizeof(*store));
}

static int64_t square_i32(int32_t value) {
    int64_t wide = value;
    return wide * wide;
}

static int64_t acceleration_magnitude_squared(const device_motion_sample_t *sample) {
    return square_i32(sample->acceleration_mg_x) +
           square_i32(sample->acceleration_mg_y) +
           square_i32(sample->acceleration_mg_z);
}

static bool magnitude_is_between(const device_motion_sample_t *sample,
                                 int32_t minimum_mg, int32_t maximum_mg) {
    int64_t value = acceleration_magnitude_squared(sample);
    return value >= square_i32(minimum_mg) && value <= square_i32(maximum_mg);
}

static int32_t max_abs_gyro_mdps(const device_motion_sample_t *sample) {
    int64_t x = sample->angular_rate_mdps_x;
    int64_t y = sample->angular_rate_mdps_y;
    int64_t z = sample->angular_rate_mdps_z;
    if (x < 0) x = -x;
    if (y < 0) y = -y;
    if (z < 0) z = -z;
    int64_t maximum = x > y ? x : y;
    if (z > maximum) maximum = z;
    return maximum > INT32_MAX ? INT32_MAX : (int32_t)maximum;
}

static bool is_still(const device_motion_sample_t *sample) {
    return magnitude_is_between(sample, FALL_STILL_MAGNITUDE_MIN_MG,
                                FALL_STILL_MAGNITUDE_MAX_MG) &&
           max_abs_gyro_mdps(sample) <= FALL_STILL_MAX_GYRO_MDPS;
}

static void update_baseline_if_stable(fall_classifier_t *classifier,
                                      const device_motion_sample_t *sample) {
    if (!is_still(sample)) return;
    classifier->baseline_x = sample->acceleration_mg_x;
    classifier->baseline_y = sample->acceleration_mg_y;
    classifier->baseline_z = sample->acceleration_mg_z;
    classifier->have_baseline = true;
}

/* Compare only directions.  Squared arithmetic avoids floating point and is
 * safe for the configured 8g range.  A negative dot is necessarily changed. */
static bool orientation_changed_from_baseline(const fall_classifier_t *classifier,
                                              const device_motion_sample_t *sample) {
    if (!classifier->have_baseline) return false;
    /* Scale first so the squared comparison remains safely in signed 64-bit
     * arithmetic even at the configured 8g full scale.  Direction thresholds
     * are intentionally coarse enough that 32mg quantisation is immaterial. */
    int32_t baseline_x = classifier->baseline_x / 32;
    int32_t baseline_y = classifier->baseline_y / 32;
    int32_t baseline_z = classifier->baseline_z / 32;
    int32_t current_x = sample->acceleration_mg_x / 32;
    int32_t current_y = sample->acceleration_mg_y / 32;
    int32_t current_z = sample->acceleration_mg_z / 32;
    int64_t dot = (int64_t)baseline_x * current_x +
                  (int64_t)baseline_y * current_y +
                  (int64_t)baseline_z * current_z;
    if (dot <= 0) return true;
    int64_t baseline_sq = square_i32(baseline_x) + square_i32(baseline_y) +
                          square_i32(baseline_z);
    int64_t current_sq = square_i32(current_x) + square_i32(current_y) +
                         square_i32(current_z);
    if (baseline_sq == 0 || current_sq == 0) return false;
    /* dot / (|a||b|) <= .707.  Square both positive sides; use permille to
     * retain a clear, reviewable threshold without a sqrt dependency. */
    int64_t left = dot * 1000LL;
    int64_t right_sq = (int64_t)FALL_ORIENTATION_COS_PERMILLE *
                       FALL_ORIENTATION_COS_PERMILLE * baseline_sq * current_sq;
    return left * left <= right_sq;
}

static void classifier_reset(fall_classifier_t *classifier) {
    classifier->state = CLASSIFIER_MONITORING;
    classifier->freefall_start_us = 0;
    classifier->impact_us = 0;
    classifier->still_start_us = 0;
    classifier->orientation_changed = false;
}

static bool classifier_observe(fall_classifier_t *classifier,
                               const device_motion_sample_t *sample) {
    int64_t now_us = (int64_t)sample->timestamp_us;
    int64_t magnitude_sq = acceleration_magnitude_squared(sample);
    bool freefall = magnitude_sq <= square_i32(FALL_FREEFALL_MAGNITUDE_MG);
    bool impact = magnitude_sq >= square_i32(FALL_IMPACT_MAGNITUDE_MG);

    switch (classifier->state) {
        case CLASSIFIER_MONITORING:
            update_baseline_if_stable(classifier, sample);
            if (freefall) {
                classifier->state = CLASSIFIER_FREEFALL;
                classifier->freefall_start_us = now_us;
            }
            break;
        case CLASSIFIER_FREEFALL:
            if (impact && now_us - classifier->freefall_start_us >= FALL_FREEFALL_MIN_US) {
                classifier->state = CLASSIFIER_POST_IMPACT;
                classifier->impact_us = now_us;
                classifier->still_start_us = 0;
                classifier->orientation_changed = false;
            } else if (!freefall || now_us - classifier->freefall_start_us >
                                     FALL_IMPACT_WINDOW_US) {
                classifier_reset(classifier);
                update_baseline_if_stable(classifier, sample);
            }
            break;
        case CLASSIFIER_POST_IMPACT:
            if (now_us - classifier->impact_us > FALL_IMPACT_WINDOW_US + FALL_STILL_MIN_US) {
                classifier_reset(classifier);
                update_baseline_if_stable(classifier, sample);
                break;
            }
            if (is_still(sample)) {
                if (orientation_changed_from_baseline(classifier, sample)) {
                    classifier->orientation_changed = true;
                }
                if (classifier->still_start_us == 0) classifier->still_start_us = now_us;
                if (classifier->orientation_changed &&
                    now_us - classifier->still_start_us >= FALL_STILL_MIN_US) {
                    classifier_reset(classifier);
                    return true;
                }
            } else {
                classifier->still_start_us = 0;
            }
            break;
    }
    return false;
}

static void notify_if_current(fall_detection_event_t event, uint32_t generation) {
    taskENTER_CRITICAL(&s_lock);
    bool current = !s_stop_requested && s_enabled && s_event_generation == generation;
    taskEXIT_CRITICAL(&s_lock);
    if (current && s_callback) s_callback(event, s_callback_context);
}

static bool begin_confirmation_window(void) {
    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    device_status_t lease_status = device_power_lease_acquire(
        DEVICE_POWER_LEASE_OWNER_FALL_DETECTION, &lease);
    if (lease_status != DEVICE_STATUS_OK) {
        ESP_LOGW(TAG, "suspected fall ignored: cannot acquire presentation lease (%d)",
                 (int)lease_status);
        return false;
    }
    uint64_t deadline = (uint64_t)esp_timer_get_time() + FALL_CONFIRMATION_WINDOW_US;
    bool accepted = false;
    uint32_t event_generation = 0;
    taskENTER_CRITICAL(&s_lock);
    /* Tool execution can disable monitoring between the task's last sample
     * and this commit.  Do not resurrect a prompt after the user's explicit
     * opt-out; release the lease obtained before taking the short lock. */
    if (s_enabled && s_state == FALL_DETECTION_STATE_MONITORING) {
        s_confirmation_lease = lease;
        s_state = FALL_DETECTION_STATE_PENDING_CONFIRMATION;
        s_confirmation_deadline_us = deadline;
        ++s_suspected_count;
        ++s_event_generation;
        if (s_event_generation == 0) ++s_event_generation;
        event_generation = s_event_generation;
        accepted = true;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (!accepted) {
        device_power_lease_release(lease);
        return false;
    }
    ESP_LOGW(TAG, "suspected fall: local confirmation window opened");
    notify_if_current(FALL_DETECTION_EVENT_SUSPECTED, event_generation);
    return true;
}

static void finish_confirmation_window(void) {
    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    uint32_t event_generation = 0;
    taskENTER_CRITICAL(&s_lock);
    if (s_state == FALL_DETECTION_STATE_PENDING_CONFIRMATION) {
        lease = s_confirmation_lease;
        s_confirmation_lease = DEVICE_POWER_LEASE_INVALID;
        s_confirmation_deadline_us = 0;
        s_state = FALL_DETECTION_STATE_MONITORING;
        event_generation = s_event_generation;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (lease == DEVICE_POWER_LEASE_INVALID) return;
    device_power_lease_release(lease);
    ESP_LOGW(TAG, "suspected fall was not cancelled locally");
    notify_if_current(FALL_DETECTION_EVENT_CONFIRMED, event_generation);
}

static void fall_detection_task(void *arg) {
    (void)arg;
    fall_classifier_t classifier = {0};
    classifier_reset(&classifier);
    unsigned consecutive_read_errors = 0;
    for (;;) {
        if (s_stop_requested) break;
        uint64_t deadline_us;
        fall_detection_state_t state;
        taskENTER_CRITICAL(&s_lock);
        deadline_us = s_confirmation_deadline_us;
        state = s_state;
        bool enabled = s_enabled;
        taskEXIT_CRITICAL(&s_lock);
        uint64_t now_us = (uint64_t)esp_timer_get_time();
        if (!enabled) {
            /* Disable is a policy boundary, not merely a hidden prompt.  Drop
             * the in-flight classifier evidence so later re-enable cannot
             * combine motion from two different user-consent periods. */
            classifier_reset(&classifier);
        }
        if (state == FALL_DETECTION_STATE_PENDING_CONFIRMATION &&
            now_us >= deadline_us) {
            finish_confirmation_window();
        } else if (enabled && state == FALL_DETECTION_STATE_MONITORING) {
            device_motion_sample_t sample = {0};
            device_status_t status = device_motion_get_sample(&sample);
            if (status == DEVICE_STATUS_OK) {
                consecutive_read_errors = 0;
                if (classifier_observe(&classifier, &sample)) {
                    (void)begin_confirmation_window();
                }
            } else if (++consecutive_read_errors == 10) {
                ESP_LOGW(TAG, "motion HAL unavailable during monitoring: %d", (int)status);
                consecutive_read_errors = 0;
            }
        }
        vTaskDelay(pdMS_TO_TICKS(FALL_SAMPLE_PERIOD_MS));
    }
    s_task = NULL;
    if (s_stopped) xSemaphoreGive(s_stopped);
    vTaskDelete(NULL);
}

device_status_t fall_detection_service_init(fall_detection_callback_t callback,
                                            void *context) {
    if (!callback || !persistence_service_is_initialized()) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lock);
    bool initialized = s_initialized;
    bool same_callback = s_callback == callback && s_callback_context == context;
    taskEXIT_CRITICAL(&s_lock);
    if (initialized) return same_callback ? (s_available ? DEVICE_STATUS_OK
                                                         : DEVICE_STATUS_UNAVAILABLE)
                                          : DEVICE_STATUS_BUSY;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_stop_requested = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (!device_profile_has_capability(DEVICE_CAPABILITY_MOTION_SENSOR)) {
        taskENTER_CRITICAL(&s_lock);
        s_initialized = true;
        s_available = false;
        s_enabled = false;
        s_state = FALL_DETECTION_STATE_UNAVAILABLE;
        taskEXIT_CRITICAL(&s_lock);
        return DEVICE_STATUS_UNAVAILABLE;
    }
    /* Load before publishing service state so a task can never observe an
     * uninitialised preference. */
    s_mutation_mutex = xSemaphoreCreateMutex();
    s_stopped = xSemaphoreCreateBinary();
    if (!s_mutation_mutex || !s_stopped) {
        if (s_mutation_mutex) vSemaphoreDelete(s_mutation_mutex);
        if (s_stopped) vSemaphoreDelete(s_stopped);
        s_mutation_mutex = NULL;
        s_stopped = NULL;
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    fall_detection_store_t store;
    bool store_migrated = false;
    esp_err_t store_err = load_store(&store, &store_migrated);
    if (store_err != ESP_OK) {
        ESP_LOGE(TAG, "cannot load persisted configuration: %s", esp_err_to_name(store_err));
        vSemaphoreDelete(s_stopped);
        vSemaphoreDelete(s_mutation_mutex);
        s_stopped = NULL;
        s_mutation_mutex = NULL;
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    if (store_migrated && save_store(&store) != ESP_OK) {
        ESP_LOGE(TAG, "cannot persist migrated configuration schema");
        vSemaphoreDelete(s_stopped);
        vSemaphoreDelete(s_mutation_mutex);
        s_stopped = NULL;
        s_mutation_mutex = NULL;
        return DEVICE_STATUS_INTERNAL_ERROR;
    }
    taskENTER_CRITICAL(&s_lock);
    s_callback = callback;
    s_callback_context = context;
    /* Mark initialization in progress before task creation so a concurrent
     * caller cannot create a second classifier task.  It is not published as
     * available until FreeRTOS accepted the worker. */
    s_initialized = true;
    s_available = false;
    s_enabled = store.enabled != 0;
    s_configuration_revision = store.revision;
    s_state = s_enabled ? FALL_DETECTION_STATE_MONITORING : FALL_DETECTION_STATE_DISABLED;
    taskEXIT_CRITICAL(&s_lock);
    if (xTaskCreate(fall_detection_task, "maclaw_fall", 4096, NULL, 3, &s_task) != pdPASS) {
        taskENTER_CRITICAL(&s_lock);
        s_available = false;
        s_enabled = false;
        s_state = FALL_DETECTION_STATE_UNAVAILABLE;
        taskEXIT_CRITICAL(&s_lock);
        vSemaphoreDelete(s_stopped);
        vSemaphoreDelete(s_mutation_mutex);
        s_stopped = NULL;
        s_mutation_mutex = NULL;
        return DEVICE_STATUS_RESOURCE_EXHAUSTED;
    }
    taskENTER_CRITICAL(&s_lock);
    s_available = true;
    s_enabled = store.enabled != 0;
    s_state = s_enabled ? FALL_DETECTION_STATE_MONITORING : FALL_DETECTION_STATE_DISABLED;
    taskEXIT_CRITICAL(&s_lock);
    ESP_LOGI(TAG, "monitoring started: freefall/impact/orientation/stillness pipeline");
    return DEVICE_STATUS_OK;
}

device_status_t fall_detection_service_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return DEVICE_STATUS_INVALID_ARGUMENT;
    taskENTER_CRITICAL(&s_lifecycle_lock);
    bool initialized = s_initialized;
    s_stop_requested = true;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    if (!initialized) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        s_stop_requested = false;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        return DEVICE_STATUS_OK;
    }

    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    taskENTER_CRITICAL(&s_lock);
    lease = s_confirmation_lease;
    s_confirmation_lease = DEVICE_POWER_LEASE_INVALID;
    s_confirmation_deadline_us = 0;
    s_enabled = false;
    s_available = false;
    s_state = FALL_DETECTION_STATE_UNAVAILABLE;
    ++s_event_generation;
    if (s_event_generation == 0) ++s_event_generation;
    TaskHandle_t task = s_task;
    taskEXIT_CRITICAL(&s_lock);
    device_power_lease_release(lease);
    if (task) {
        xTaskNotifyGive(task);
        if (!s_stopped || xSemaphoreTake(s_stopped, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
            return DEVICE_STATUS_TIMEOUT;
        }
    }
    TickType_t started = xTaskGetTickCount();
    const TickType_t timeout = pdMS_TO_TICKS(timeout_ms);
    for (;;) {
        taskENTER_CRITICAL(&s_lifecycle_lock);
        uint32_t admissions = s_tool_admissions;
        taskEXIT_CRITICAL(&s_lifecycle_lock);
        if (admissions == 0) break;
        if ((xTaskGetTickCount() - started) >= timeout) return DEVICE_STATUS_TIMEOUT;
        vTaskDelay(pdMS_TO_TICKS(1));
    }
    if (s_mutation_mutex &&
        xSemaphoreTake(s_mutation_mutex, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return DEVICE_STATUS_TIMEOUT;
    }
    if (s_mutation_mutex) {
        xSemaphoreGive(s_mutation_mutex);
        vSemaphoreDelete(s_mutation_mutex);
        s_mutation_mutex = NULL;
    }
    if (s_stopped) {
        vSemaphoreDelete(s_stopped);
        s_stopped = NULL;
    }
    taskENTER_CRITICAL(&s_lock);
    s_initialized = false;
    s_callback = NULL;
    s_callback_context = NULL;
    taskEXIT_CRITICAL(&s_lock);
    taskENTER_CRITICAL(&s_lifecycle_lock);
    s_stop_requested = false;
    taskEXIT_CRITICAL(&s_lifecycle_lock);
    ESP_LOGI(TAG, "monitoring stopped");
    return DEVICE_STATUS_OK;
}

bool fall_detection_service_is_available(void) {
    taskENTER_CRITICAL(&s_lock);
    bool available = s_available;
    taskEXIT_CRITICAL(&s_lock);
    return available;
}

bool fall_detection_service_is_initialized(void) {
    taskENTER_CRITICAL(&s_lock);
    bool initialized = s_initialized;
    taskEXIT_CRITICAL(&s_lock);
    return initialized;
}

bool fall_detection_service_get_snapshot(fall_detection_snapshot_t *out_snapshot) {
    if (!out_snapshot) return false;
    taskENTER_CRITICAL(&s_lock);
    out_snapshot->available = s_available;
    out_snapshot->enabled = s_enabled;
    out_snapshot->state = s_state;
    out_snapshot->suspected_count = s_suspected_count;
    out_snapshot->confirmation_deadline_us = s_confirmation_deadline_us;
    out_snapshot->configuration_revision = s_configuration_revision;
    taskEXIT_CRITICAL(&s_lock);
    /* Before app_main reaches the optional service, diagnostics must still
     * distinguish a sensor-capable profile that is initializing from a board
     * that can never provide motion samples. */
    if (!out_snapshot->available && !out_snapshot->enabled &&
        out_snapshot->state == FALL_DETECTION_STATE_UNAVAILABLE) {
        out_snapshot->available = has_configured_motion_sensor();
    }
    return true;
}

static cJSON *configuration_result_json(bool enabled, uint32_t revision, bool replayed) {
    cJSON *result = cJSON_CreateObject();
    if (!result || !cJSON_AddBoolToObject(result, "available", true) ||
        !cJSON_AddBoolToObject(result, "enabled", enabled) ||
        !cJSON_AddNumberToObject(result, "configurationRevision", revision) ||
        !cJSON_AddBoolToObject(result, "replayed", replayed)) {
        cJSON_Delete(result);
        return NULL;
    }
    return result;
}

static esp_err_t update_enabled(bool enabled, const char *idempotency_key,
                                cJSON **out_result, char *error,
                                size_t error_size) {
    if (out_result) *out_result = NULL;
    if (!valid_idempotency_key(idempotency_key)) {
        if (error && error_size) {
            strlcpy(error, "idempotencyKey must be 1..63 ASCII characters", error_size);
        }
        return ESP_ERR_INVALID_ARG;
    }
    taskENTER_CRITICAL(&s_lock);
    bool initialized = s_initialized;
    bool available = s_available;
    taskEXIT_CRITICAL(&s_lock);
    if (!initialized || !available) return ESP_ERR_NOT_SUPPORTED;
    if (!s_mutation_mutex ||
        xSemaphoreTake(s_mutation_mutex, pdMS_TO_TICKS(3000)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }

    fall_detection_store_t store;
    bool migrated = false;
    esp_err_t load_err = load_store(&store, &migrated);
    if (load_err != ESP_OK) {
        xSemaphoreGive(s_mutation_mutex);
        return load_err;
    }
    for (size_t i = 0; i < FALL_DETECTION_REPLAY_COUNT; ++i) {
        fall_detection_replay_t *replay = &store.replay[i];
        if (strcmp(replay->key, idempotency_key)) continue;
        if (replay->status != ESP_OK) {
            if (error && error_size) strlcpy(error, replay->detail, error_size);
            xSemaphoreGive(s_mutation_mutex);
            return (esp_err_t)replay->status;
        }
        cJSON *cached = configuration_result_json(replay->enabled != 0,
                                                  replay->configuration_revision, true);
        if (!cached) {
            xSemaphoreGive(s_mutation_mutex);
            return ESP_ERR_NO_MEM;
        }
        *out_result = cached;
        xSemaphoreGive(s_mutation_mutex);
        return ESP_OK;
    }

    /* The persisted store is the mutation truth.  Runtime state can lag it
     * during initialization or after a recoverable task failure, so calculate
     * both the revision and no-op behavior from the just-loaded record. */
    bool changed = (store.enabled != (enabled ? 1u : 0u));
    store.enabled = enabled ? 1 : 0;
    if (changed) store.revision = store.revision == UINT32_MAX ? 1u : store.revision + 1u;
    if (!store.revision) store.revision = 1u;

    /* Persist the successful mutation and its response atomically.  A no-op
     * still receives a replay entry: otherwise a lost ACK would make a retry
     * indistinguishable from a fresh command. */
    fall_detection_replay_t *replay =
        &store.replay[store.replay_next % FALL_DETECTION_REPLAY_COUNT];
    memset(replay, 0, sizeof(*replay));
    strlcpy(replay->key, idempotency_key, sizeof(replay->key));
    replay->status = ESP_OK;
    replay->enabled = store.enabled;
    replay->configuration_revision = store.revision;
    store.replay_next = (store.replay_next + 1u) % FALL_DETECTION_REPLAY_COUNT;

    /* Allocate the response before committing durable state.  An allocation
     * failure must not report a failed tool call after it has already changed
     * the user's monitoring preference. */
    cJSON *result = configuration_result_json(enabled, store.revision, false);
    if (!result) {
        xSemaphoreGive(s_mutation_mutex);
        return ESP_ERR_NO_MEM;
    }
    esp_err_t err = save_store(&store);
    if (err != ESP_OK) {
        cJSON_Delete(result);
        xSemaphoreGive(s_mutation_mutex);
        return err;
    }

    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    taskENTER_CRITICAL(&s_lock);
    s_enabled = enabled;
    s_configuration_revision = store.revision;
    if (!enabled && s_state == FALL_DETECTION_STATE_PENDING_CONFIRMATION) {
        lease = s_confirmation_lease;
        s_confirmation_lease = DEVICE_POWER_LEASE_INVALID;
        s_confirmation_deadline_us = 0;
    }
    ++s_event_generation;
    if (s_event_generation == 0) ++s_event_generation;
    s_state = enabled ? FALL_DETECTION_STATE_MONITORING : FALL_DETECTION_STATE_DISABLED;
    taskEXIT_CRITICAL(&s_lock);
    device_power_lease_release(lease);
    ESP_LOGI(TAG, "monitoring %s (configuration revision=%lu)",
             enabled ? "enabled" : "disabled", (unsigned long)store.revision);
    *out_result = result;
    xSemaphoreGive(s_mutation_mutex);
    return ESP_OK;
}

static cJSON *status_json(void) {
    fall_detection_snapshot_t snapshot = {0};
    if (!fall_detection_service_get_snapshot(&snapshot)) return NULL;
    cJSON *result = cJSON_CreateObject();
    if (!result || !cJSON_AddBoolToObject(result, "available", snapshot.available) ||
        !cJSON_AddBoolToObject(result, "enabled", snapshot.enabled) ||
        !cJSON_AddNumberToObject(result, "state", snapshot.state) ||
        !cJSON_AddNumberToObject(result, "suspectedCount", snapshot.suspected_count) ||
        !cJSON_AddNumberToObject(result, "configurationRevision",
                                 snapshot.configuration_revision)) {
        cJSON_Delete(result);
        return NULL;
    }
    return result;
}

esp_err_t fall_detection_service_execute_tool(const char *name, cJSON *arguments,
                                              const char *idempotency_key,
                                              cJSON **out_result, char *error,
                                              size_t error_size) {
    if (out_result) *out_result = NULL;
    if (error && error_size) error[0] = '\0';
    if (!name || !out_result) return ESP_ERR_INVALID_ARG;
    if (!admit_tool()) return ESP_ERR_INVALID_STATE;
    if (!strcmp(name, "fall_detection_status")) {
        *out_result = status_json();
        esp_err_t result = *out_result ? ESP_OK : ESP_ERR_NO_MEM;
        release_tool();
        return result;
    }
    if (strcmp(name, "fall_detection_set")) {
        release_tool();
        return ESP_ERR_NOT_SUPPORTED;
    }
    if (!idempotency_key || !idempotency_key[0]) {
        if (error && error_size) strlcpy(error, "idempotencyKey is required", error_size);
        release_tool();
        return ESP_ERR_INVALID_ARG;
    }
    cJSON *enabled = arguments ? cJSON_GetObjectItemCaseSensitive(arguments, "enabled") : NULL;
    if (!cJSON_IsBool(enabled)) {
        if (error && error_size) strlcpy(error, "enabled boolean is required", error_size);
        release_tool();
        return ESP_ERR_INVALID_ARG;
    }
    cJSON *result = NULL;
    esp_err_t err = update_enabled(cJSON_IsTrue(enabled), idempotency_key, &result,
                                   error, error_size);
    if (err != ESP_OK) {
        if (error && error_size && !error[0]) {
            snprintf(error, error_size, "fall detection unavailable: %s", esp_err_to_name(err));
        }
        release_tool();
        return err;
    }
    *out_result = result;
    release_tool();
    return ESP_OK;
}

bool fall_detection_service_cancel_from_user(void) {
    device_power_lease_t lease = DEVICE_POWER_LEASE_INVALID;
    taskENTER_CRITICAL(&s_lock);
    if (s_state == FALL_DETECTION_STATE_PENDING_CONFIRMATION) {
        lease = s_confirmation_lease;
        s_confirmation_lease = DEVICE_POWER_LEASE_INVALID;
        s_confirmation_deadline_us = 0;
        s_state = FALL_DETECTION_STATE_MONITORING;
        ++s_event_generation;
        if (s_event_generation == 0) ++s_event_generation;
    }
    taskEXIT_CRITICAL(&s_lock);
    if (lease == DEVICE_POWER_LEASE_INVALID) return false;
    device_power_lease_release(lease);
    ESP_LOGI(TAG, "suspected fall cancelled by local user interaction");
    return true;
}
